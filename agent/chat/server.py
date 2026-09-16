"""Chat socket and its handles: one client, one request, one reply."""
import json
import os
import socket
import threading

import archive
import asked
import briefs
import pages
import sesstate

from .disk import MAX_FILE, MAX_RAW, read_file, read_raw, task_output
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
    # Briefs are read here and written nowhere near here: publishing has a
    # socket of its own, which the service cannot reach at all.
    want_briefs = request.get("briefs")
    if isinstance(want_briefs, dict):
        session = want_briefs.get("session")
        return {"ok": True,
                "briefs": briefs.SHELF.cards(session=session if isinstance(session, str) and session else None)}

    want_brief = request.get("brief")
    if isinstance(want_brief, str) and want_brief:
        doc = briefs.SHELF.of(want_brief)
        if doc is None:
            return {"ok": False,
                    "error": "there is no brief under this name: it was either never published or has been swept"}
        return {"ok": True, "brief": doc}

    # A published page is read here for the same reason a brief is: the copy
    # arrives on a socket of its own, which the service cannot reach at all.
    # Removing a brief is asked for here, by the panel on behalf of the person
    # and by the publisher on behalf of a session. The rule differs between
    # them: a session names the directory it works in and may remove only the
    # documents of that directory, while the panel names none and removes what
    # the person is looking at.
    drop_brief = request.get("dropBrief")
    if isinstance(drop_brief, dict):
        brief_id = drop_brief.get("id")
        cwd = drop_brief.get("cwd")
        ok, why = briefs.SHELF.drop(
            brief_id if isinstance(brief_id, str) else "",
            cwd if isinstance(cwd, str) and cwd else None,
        )
        return {"ok": True, "dropped": brief_id} if ok else {"ok": False, "error": why}

    want_pages = request.get("pages")
    if isinstance(want_pages, dict):
        session = want_pages.get("session")
        return {"ok": True,
                "pages": pages.SHELF.cards(session=session if isinstance(session, str) and session else None)}

    want_page = request.get("page")
    if isinstance(want_page, str) and want_page:
        doc = pages.SHELF.of(want_page)
        if doc is None:
            return {"ok": False,
                    "error": "there is no copy of this page: it was published before the panel kept them, or has been swept"}
        return {"ok": True, "page": doc}

    want_archive = request.get("archive")
    if isinstance(want_archive, dict):
        try:
            page = archive_index().page(
                limit=want_archive.get("limit"),
                offset=want_archive.get("offset"),
                skip=want_archive.get("skip") or (),
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

    want = request.get("raw")
    if isinstance(want, str) and want:
        found = read_raw(want, transcript_cwd(path),
                         offset=request.get("offset") or 0,
                         limit=request.get("bytes") or MAX_RAW)
        if found is None:
            return {"ok": False,
                    "error": "the file was not opened: it either does not exist, or lies "
                             "outside the directory of this conversation"}
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
