"""Chat socket and its handles: one client, one request, one reply."""
import json
import os
import socket
import threading

import archive
import asked
import sesstate

from .disk import MAX_FILE, read_file, task_output
from .locate import subagent_path, transcript_cwd, transcript_path
from .mail import agent_mail
from .spots import call, image
from .window import feed

import chat


MAX_REQUEST = 4096


_archive = None


def archive_index():
    global _archive
    if _archive is None:
        _archive = archive.Index()
    return _archive


def observe_names():
    """Remembers the names of live sessions."""
    try:
        archive_index().observe()
    except OSError:
        pass


def answer(request):
    """Returns the reply to one request, with the addressee echoed back."""
    reply = _answer(request)
    sub = request.get("subagent")
    if isinstance(sub, str) and sub and isinstance(reply, dict) and reply.get("ok"):
        reply["subagent"] = sub
    return reply


def _answer(request):
    want_archive = request.get("archive")
    if isinstance(want_archive, dict):
        try:
            page = archive_index().page(
                limit=want_archive.get("limit"),
                offset=want_archive.get("offset"),
                skip=want_archive.get("skip") or (),
                started=bool(want_archive.get("started")),
                profile=want_archive.get("profile"),
                profiles=[p for p in (want_archive.get("profiles") or ())
                          if isinstance(p, str) and p],
            )
        except OSError as e:
            return {"ok": False, "error": f"the archive was not read: {e}"}
        return {"ok": True, "archive": page}

    session = request.get("session")
    profile = request.get("profile")

    sub = request.get("subagent")
    sub = sub if isinstance(sub, str) else ""

    if sub:
        path = subagent_path(session, sub, profile)
        if not path:
            return {"ok": False,
                    "error": "there is no feed of this agent on disk: it has either just started "
                             "and said nothing yet, or belongs to another conversation"}
    else:
        path = transcript_path(session, profile)
        if not path:
            return {"ok": False,
                    "error": "there is no transcript with this identifier on disk: "
                             "the conversation has either not started yet, or was deleted"}

    want = request.get("image")
    if isinstance(want, dict):
        try:
            found = image(path, int(want.get("pos", -1)), int(want.get("index", -1)))
        except (OSError, ValueError, TypeError) as e:
            return {"ok": False, "error": f"the attachment was not read: {e}"}
        if not found:
            return {"ok": False, "error": "there is no attachment at this position"}
        return {"ok": True, "session": session, **found}

    want = request.get("call")
    if isinstance(want, dict):
        try:
            found = call(path, int(want.get("pos", -1)), int(want.get("index", -1)))
        except (OSError, ValueError, TypeError) as e:
            return {"ok": False, "error": f"the call was not read: {e}"}
        if not found:
            return {"ok": False, "error": "there is no call at this position"}
        return {"ok": True, "session": session, **found}

    want = request.get("task")
    if isinstance(want, dict):
        try:
            found = task_output(session, transcript_cwd(path), str(want.get("id") or ""))
        except OSError as e:
            return {"ok": False, "error": f"the task output was not read: {e}"}
        if found is None:
            return {"ok": False,
                    "error": "there is no output of this task on disk: it has either written "
                             "nothing yet, or its directory is already gone"}
        return {"ok": True, "session": session, **found}

    want = request.get("file")
    if isinstance(want, str) and want:
        found = read_file(want, transcript_cwd(path),
                          offset=request.get("offset") or 0,
                          limit=request.get("bytes") or MAX_FILE)
        if found is None:
            return {"ok": False,
                    "error": "the file was not opened: it either does not exist, or lies "
                             "outside the directory of this conversation"}
        return {"ok": True, "session": session, **found}

    want = request.get("agent")
    if isinstance(want, str) and want:
        try:
            letters = agent_mail(path, want)
        except OSError as e:
            return {"ok": False, "error": f"the letters were not read: {e}"}
        return {"ok": True, "session": session, "letters": letters}

    before = request.get("before")
    after = request.get("after")
    try:
        chunk = feed(path, request.get("limit"),
                     before=int(before) if before is not None else None,
                     after=int(after) if after is not None else None,
                     sidechain=bool(sub))
    except (OSError, ValueError) as e:
        return {"ok": False, "error": f"the transcript was not read: {e}"}

    items = chunk["items"]
    reply = {
        "ok": True,
        "session": session,
        "items": items,
        "total": chunk["total"],
        "moreBefore": chunk["moreBefore"],
        "first": items[0]["pos"] if items else None,
        "last": chunk.get("last") if items else None,
        "size": os.path.getsize(path),
    }
    if request.get("state") and not sub:
        state = sesstate.SHARED.state(path)
        found = state.snapshot() if state else None
        if found is not None:
            if session:
                asked.BOOK.answered(session, set(state.answered))
            ask = asked.BOOK.of(session)
            if ask:
                found = {**found, "ask": ask}
            reply["state"] = found
    return reply


def serve(sock):
    """Serves one client per request without keeping the connection."""
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()


def handle(conn):
    with conn:
        try:
            conn.settimeout(10)
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
    os.makedirs(chat.SOCKET_DIR, exist_ok=True)
    path = os.path.join(chat.SOCKET_DIR, chat.SOCKET_NAME)
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
    """Runs the chat thread of the agent."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: the chat is unavailable, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: the chat listens on {os.path.join(chat.SOCKET_DIR, chat.SOCKET_NAME)}", flush=True)
    serve(sock)
