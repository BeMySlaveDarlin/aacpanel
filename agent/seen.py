#!/usr/bin/env python3
"""Whether a prompt landed in the conversation: a mark in, a fact out."""

import datetime
import json
import os
import socket
import threading

import chat

SOCKET_DIR = os.environ.get("AACP_SEEN_DIR", "/run/aacpanel-agent")
SOCKET_NAME = "seen.sock"

MAX_REQUEST = 4096

MAX_MARK = 200

MAX_CHUNK = 4 << 20

MAX_PATHS = 64

# How much of the end of a conversation is read for whether its turn is over:
# the last word of the conversation is at the very end unless it is one huge
# answer.
TURN_TAIL = 512 << 10

CHIPS = ("[Image#", "[Pastedtext#")

_paths = {}
_paths_lock = threading.Lock()


def squeeze(text):
    """Returns the string without any whitespace."""
    return "".join(str(text or "").split())


def content_text(raw):
    """Returns the text of a content field, however it arrived."""
    if isinstance(raw, str):
        return raw
    if not isinstance(raw, list):
        return ""
    out = []
    for block in raw:
        if isinstance(block, dict) and block.get("type") == "text":
            text = block.get("text")
            if isinstance(text, str):
                out.append(text)
    return "\n".join(out)


def own_words(record):
    """Returns the text of a human prompt and whether it was queued, or None."""
    if not isinstance(record, dict):
        return None
    kind = record.get("type")
    if kind == "queue-operation" and record.get("operation") == "enqueue":
        return content_text(record.get("content")), True
    if kind == "user":
        message = record.get("message")
        if not isinstance(message, dict):
            return None
        return content_text(message.get("content")), False
    return None


def ours(said, mark):
    """Reports whether this is the prompt that was sent, by its mark or by an attachment chip."""
    packed = squeeze(said)
    if not packed:
        return False
    if mark and mark in packed:
        return True
    return any(chip in packed for chip in CHIPS)


def scan(chunk, mark):
    """Returns whether the prompt was seen in the tail and whether it was queued."""
    seen = False
    queued = False
    for line in chunk.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            record = json.loads(line)
        except ValueError:
            continue
        said = own_words(record)
        if not said:
            continue
        text, in_queue = said
        if ours(text, mark):
            seen = True
            queued = queued or in_queue
    return seen, queued


def tail(path, pos):
    """Returns the tail of a file from a position: whole lines and how far it was read."""
    with open(path, "rb") as f:
        f.seek(pos)
        chunk = f.read(MAX_CHUNK)
    cut = chunk.rfind(b"\n")
    if cut < 0:
        return "", pos
    return chunk[:cut].decode("utf-8", "replace"), pos + cut + 1


def turn_ended(text):
    """Reports whether the session's own turn is over, by the last word of its conversation, or None.

    Claude keeps a session busy while an agent it sent off to work runs, long
    after the turn that sent it: the turn is over when the last word is an
    answer that ended it, and the reason an answer ended is written once it
    has. A prompt, the result of a call or the news of a task after it is a
    turn going on; the words of an agent are its own.
    """
    for line in reversed(text.splitlines()):
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            record = json.loads(line)
        except ValueError:
            continue
        if not isinstance(record, dict) or record.get("isSidechain"):
            continue
        if record.get("type") == "user":
            return False
        if record.get("type") == "assistant":
            message = record.get("message")
            return isinstance(message, dict) and message.get("stop_reason") == "end_turn"
    return None


def last_mode(text, since):
    """Returns the permission mode a console was last in, or "".

    Every message a person sends carries the mode, so the end of a conversation
    has it. Only a message sent since the console started counts (since, in
    epoch milliseconds): an older one was written by another process of the
    same conversation and says nothing of this one.
    """
    for line in reversed(text.splitlines()):
        line = line.strip()
        if not line.startswith("{") or '"permissionMode"' not in line:
            continue
        try:
            record = json.loads(line)
        except ValueError:
            continue
        mode = record.get("permissionMode") if isinstance(record, dict) else None
        if not isinstance(mode, str) or not mode:
            continue
        at = epoch_ms(record.get("timestamp"))
        return mode if at is not None and at >= since else ""
    return ""


def epoch_ms(stamp):
    """Returns an ISO timestamp as epoch milliseconds, or None."""
    if not isinstance(stamp, str) or not stamp:
        return None
    try:
        return int(datetime.datetime.fromisoformat(stamp.replace("Z", "+00:00")).timestamp() * 1000)
    except ValueError:
        return None


def end_of(path):
    """Returns the whole lines at the end of a file."""
    with open(path, "rb") as f:
        size = f.seek(0, os.SEEK_END)
        f.seek(max(0, size - TURN_TAIL))
        chunk = f.read(TURN_TAIL)
    if size > TURN_TAIL:
        chunk = chunk[chunk.find(b"\n") + 1:]
    return chunk.decode("utf-8", "replace")


def locate(session):
    """Returns the conversation file for a uuid, remembering it for the time of sending."""
    with _paths_lock:
        known = _paths.get(session)
    if known and os.path.isfile(known):
        return known
    path = chat.transcript_path(session)
    with _paths_lock:
        if not path:
            _paths.pop(session, None)
        else:
            if len(_paths) >= MAX_PATHS:
                _paths.clear()
            _paths[session] = path
    return path


def answer(request):
    """Returns the reply to one request: the fact and nothing but the fact."""
    session = request.get("session")
    if not isinstance(session, str) or not session:
        return {"ok": False, "error": "the conversation is not named"}

    mark = request.get("mark")
    if mark is not None and not isinstance(mark, str):
        return {"ok": False, "error": "the mark is not a string"}
    mark = squeeze(mark)
    if len(mark) > MAX_MARK:
        return {"ok": False, "error": f"the mark is longer than {MAX_MARK} characters"}

    path = locate(session)
    if request.get("ask") == "mode":
        since = request.get("since")
        if not isinstance(since, int) or isinstance(since, bool) or since < 0:
            return {"ok": False, "error": "since is not a time"}
        if not path:
            return {"ok": True, "found": False, "mode": ""}
        try:
            return {"ok": True, "found": True, "mode": last_mode(end_of(path), since)}
        except OSError:
            return {"ok": True, "found": False, "mode": ""}
    if request.get("ask") == "turn":
        if not path:
            return {"ok": True, "found": False, "ended": False}
        try:
            ended = turn_ended(end_of(path))
        except OSError:
            return {"ok": True, "found": False, "ended": False}
        return {"ok": True, "found": ended is not None, "ended": bool(ended)}
    if not path:
        return {"ok": True, "found": False, "pos": 0, "seen": False, "queued": False}

    pos = request.get("pos")
    if not isinstance(pos, int) or isinstance(pos, bool) or pos < 0:
        try:
            size = os.path.getsize(path)
        except OSError:
            return {"ok": True, "found": False, "pos": 0, "seen": False, "queued": False}
        return {"ok": True, "found": True, "pos": size, "seen": False, "queued": False}

    try:
        chunk, pos = tail(path, pos)
    except OSError:
        return {"ok": True, "found": False, "pos": pos, "seen": False, "queued": False}
    seen, queued = scan(chunk, mark)
    return {"ok": True, "found": True, "pos": pos, "seen": seen, "queued": queued}


def handle(conn):
    """Serves one request and answers once."""
    with conn:
        try:
            conn.settimeout(5)
            raw = conn.recv(MAX_REQUEST)
            request = json.loads(raw.decode("utf-8"))
            if not isinstance(request, dict):
                raise ValueError("an object was expected")
            reply = answer(request)
        except (OSError, ValueError, UnicodeDecodeError) as e:
            reply = {"ok": False, "error": f"the request was not parsed: {e}"}
        try:
            conn.sendall(json.dumps(reply, ensure_ascii=False).encode("utf-8"))
        except OSError:
            pass


def listen():
    """Opens a 0600 socket in its own directory."""
    os.makedirs(SOCKET_DIR, exist_ok=True)
    path = os.path.join(SOCKET_DIR, SOCKET_NAME)
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.bind(path)
    os.chmod(path, 0o600)
    sock.listen(8)
    return sock


def worker():
    """Runs the delivery answering thread."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: the delivery acknowledgement is unavailable, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: acknowledging the delivery of replies on {os.path.join(SOCKET_DIR, SOCKET_NAME)}", flush=True)
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()


if __name__ == "__main__":
    worker()
