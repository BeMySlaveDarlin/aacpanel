#!/usr/bin/env python3
"""Usage collection socket: the transcript list and parsing on request."""

import json
import os
import socket
import tempfile
import threading

import contours
import paths

SOCKET_DIR = os.environ.get("AACP_USAGE_DIR", "")
SOCKET_NAME = "usage.sock"

MAX_REQUEST = 64 << 20

WORKERS_ENV = "AACP_USAGE_WORKERS"
WORKERS_DEFAULT = 4

POOL_FROM = 4

OVERRIDE = {}


def socket_dir():
    """Returns the socket directory of this machine."""
    return SOCKET_DIR or os.path.join(paths.state_dir(), "usage")


def socket_path():
    return os.path.join(socket_dir(), SOCKET_NAME)


def workers():
    """Returns how many processes should parse the files."""
    try:
        n = int(os.environ.get(WORKERS_ENV, WORKERS_DEFAULT))
    except ValueError:
        return WORKERS_DEFAULT
    return max(1, min(n, 64))


def _usage(name):
    override = OVERRIDE.get(name)
    if override is not None:
        return override
    import usage
    return getattr(usage, name)


_roots_memo = None


def reset_roots():
    """Forgets the contour layout."""
    global _roots_memo
    _roots_memo = None


def roots():
    """Returns the transcript roots as a pair of contour and directory per contour."""
    global _roots_memo
    if _roots_memo is None:
        _roots_memo = [(name, os.path.join(config_dir, "projects"))
                       for name, config_dir in contours.profiles()]
    return _roots_memo


def locate(path):
    """Returns the real path of a file and its contour, or (None, "") when it cannot be read."""
    if not isinstance(path, str) or not path.endswith(".jsonl"):
        return None, ""
    try:
        real = os.path.realpath(path)
    except OSError:
        return None, ""
    for name, root in roots():
        base = os.path.realpath(root)
        if real == base or real.startswith(base + os.sep):
            return real, name
    return None, ""


def listing():
    """Returns the transcripts of every contour, one row per file."""
    out = []
    for item in _usage("scan_list")(roots(), heads=True):
        item = dict(item)
        item["head"] = item.get("head", b"").hex()
        out.append(item)
    return out


def agents_of(real, rows, tools):
    """Returns whose rows this file carries."""
    if (os.sep + "subagents" + os.sep) in real:
        seen = {r.get("agent", "") for r in rows} | {t.get("agent", "") for t in tools}
        return sorted(a for a in seen if a)
    return ["", "sidechain"]


def parse_one(task):
    """Parses one file of the job."""
    path = task.get("path") if isinstance(task, dict) else None
    real, contour = locate(path)
    if not real:
        return {"path": path if isinstance(path, str) else "",
                "error": "the file is outside the transcript roots"}
    try:
        offset = int(task.get("offset") or 0)
    except (TypeError, ValueError):
        offset = 0
    try:
        parsed = _usage("parse_file")(real, max(0, offset), contour)
        rows, tools, events, session, new_offset = parsed
    except (OSError, ValueError) as e:
        return {"path": path, "error": f"the file was not parsed: {e}"}
    rows, tools, events = rows or [], tools or [], events or []
    session = dict(session or {})
    session["contour"] = contour
    session["agents"] = agents_of(real, rows, tools)
    return {"path": path, "session": session, "rows": rows, "tools": tools,
            "events": events, "offset": new_offset,
            "since": getattr(parsed, "since", ""),
            "rewind": bool(getattr(parsed, "rewind", False))}


def parse_all(files):
    """Parses the job in the order it arrived in."""
    files = [f for f in files if isinstance(f, dict)]
    if len(files) < POOL_FROM:
        for task in files:
            yield parse_one(task)
        return

    import multiprocessing
    tmp = os.path.join(paths.state_dir(), "tmp")
    os.makedirs(tmp, exist_ok=True)
    tempfile.tempdir = tmp
    os.environ["TMPDIR"] = tmp

    ctx = multiprocessing.get_context("forkserver")
    pool = ctx.Pool(processes=workers())
    try:
        for reply in pool.imap(parse_one, files, chunksize=1):
            yield reply
    finally:
        pool.terminate()
        pool.join()


def answer(request):
    """Yields the reply to one request line by line."""
    reset_roots()
    op = request.get("op") if isinstance(request, dict) else None
    if op == "ping":
        yield {"pong": True, "workers": workers()}
        return
    if op == "list":
        try:
            for item in listing():
                yield {"file": item}
        except OSError as e:
            yield {"error": f"the transcript list was not built: {e}"}
        except (ImportError, AttributeError) as e:
            yield {"error": f"the transcript parser is not installed: {e}"}
        return
    if op == "parse":
        files = request.get("files")
        if not isinstance(files, list):
            yield {"error": "the scan job carries no list of files"}
            return
        try:
            for reply in parse_all(files):
                yield {"result": reply}
        except (ImportError, AttributeError) as e:
            yield {"error": f"the transcript parser is not installed: {e}"}
        return
    yield {"error": f"unknown request: {op!r}"}


def read_request(conn):
    """Reads the whole request until the client closes its half of the stream."""
    chunks = []
    size = 0
    while True:
        part = conn.recv(64 << 10)
        if not part:
            break
        size += len(part)
        if size > MAX_REQUEST:
            raise ValueError("the request is longer than the cap")
        chunks.append(part)
    return json.loads(b"".join(chunks).decode("utf-8"))


def handle(conn):
    """Serves one request with a stream of replies and a mandatory end line."""
    with conn:
        try:
            conn.settimeout(300)
            request = read_request(conn)
            if not isinstance(request, dict):
                raise ValueError("an object was expected")
        except (OSError, ValueError, UnicodeDecodeError) as e:
            send(conn, {"error": f"the request cannot be parsed: {e}"})
            send(conn, {"end": True})
            return
        try:
            for line in answer(request):
                if not send(conn, line):
                    return
        except Exception as e:  # noqa: BLE001
            send(conn, {"error": f"usage collection broke off: {e}"})
        send(conn, {"end": True})


def send(conn, line):
    """Sends one reply line and reports whether it went through."""
    try:
        conn.sendall((json.dumps(line, ensure_ascii=False) + "\n").encode("utf-8"))
        return True
    except OSError:
        return False


def listen():
    """Opens a 0600 socket in the state directory."""
    directory = socket_dir()
    os.makedirs(directory, exist_ok=True)
    path = os.path.join(directory, SOCKET_NAME)
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.bind(path)
    os.chmod(path, 0o600)
    sock.listen(4)
    return sock


def worker():
    """Runs the usage collection thread."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: the usage collection is unavailable, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: collecting the usage on {socket_path()}", flush=True)
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()


if __name__ == "__main__":
    worker()
