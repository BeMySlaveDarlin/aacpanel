"""Session questions: taken from the hook and handed to the panel."""

import calendar
import json
import os
import socket
import threading
import time

import paths

SOCKET_DIR = os.environ.get("AACP_ASK_DIR", "/run/aacpanel-agent")
SOCKET_NAME = "ask.sock"

STORE = os.environ.get("AACP_ASK_STORE", paths.state("asked.json"))

MAX_TEXT = 400
MAX_OPTIONS = 12
MAX_PREVIEW = 4000
MAX_PREVIEW_LINES = 40
MAX_PREVIEW_WIDTH = 200
MAX_QUESTIONS = 8
MAX_REQUEST = 64 * 1024

MAX_AGE = 24 * 3600


def _short(text, limit=MAX_TEXT):
    return " ".join(str(text or "").split())[:limit]


def _block(text, limit=MAX_PREVIEW):
    raw = str(text or "").replace("\r\n", "\n").replace("\r", "\n")
    if not raw.strip():
        return ""
    lines = [line[:MAX_PREVIEW_WIDTH] for line in raw.split("\n")[:MAX_PREVIEW_LINES]]
    out = "\n".join(lines)
    return out[:limit]


def clean(payload):
    """Returns the question from the hook payload in the form the panel gets."""
    tool_input = payload.get("tool_input") or {}
    raw = tool_input.get("questions")
    if not isinstance(raw, list) or not raw:
        return None

    questions = []
    for item in raw[:MAX_QUESTIONS]:
        if not isinstance(item, dict):
            continue
        options = []
        for opt in (item.get("options") or [])[:MAX_OPTIONS]:
            if not isinstance(opt, dict):
                continue
            label = _short(opt.get("label"), 200)
            if not label:
                continue
            option = {"label": label, "description": _short(opt.get("description"))}
            preview = _block(opt.get("preview"))
            if preview:
                option["preview"] = preview
            options.append(option)
        text = _short(item.get("question"))
        if not text:
            continue
        questions.append({
            "text": text,
            "header": _short(item.get("header"), 60),
            "multi": bool(item.get("multiSelect")),
            "options": options,
        })
    if not questions:
        return None

    session = str(payload.get("session_id") or "")
    if not session:
        return None
    return {
        "sessionId": session,
        "toolUseId": str(payload.get("tool_use_id") or ""),
        "cwd": str(payload.get("cwd") or "")[:400],
        "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "questions": questions,
    }


class Book:
    """Pending questions, one per conversation, keyed by its identifier."""

    def __init__(self, path=STORE):
        self.path = path
        self._asks = {}
        self._lock = threading.Lock()
        self.load()

    def load(self):
        try:
            with open(self.path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            return
        if isinstance(data, dict):
            with self._lock:
                self._asks = {k: v for k, v in data.items() if isinstance(v, dict)}

    def _write(self):
        tmp = self.path + ".tmp"
        try:
            os.makedirs(os.path.dirname(self.path), exist_ok=True)
            with open(tmp, "w", encoding="utf-8") as f:
                json.dump(self._asks, f, ensure_ascii=False)
            os.replace(tmp, self.path)
        except OSError as e:
            print(f"aacpanel-agent: the questions of the sessions were not written to {self.path}: {e}", flush=True)
            return False
        return True

    def _commit(self, before):
        if self._write():
            return True
        self._asks = before
        return False

    def save(self):
        with self._lock:
            return self._write()

    def put(self, ask):
        """Remembers a question and reports whether it reached the file."""
        with self._lock:
            before = dict(self._asks)
            self._asks[ask["sessionId"]] = ask
            return self._commit(before)

    def of(self, session):
        """Returns the question of this conversation, or None."""
        with self._lock:
            return self._asks.get(session)

    def drop(self, session):
        with self._lock:
            if session not in self._asks:
                return None
            before = dict(self._asks)
            gone = self._asks.pop(session)
            if not self._commit(before):
                return None
        return gone

    def answered(self, session, tool_ids, ended=""):
        """Drops the question that has been answered or that the turn outlived.

        A turn does not end while its question waits: the answer comes back to
        the call first, and the call id is in tool_ids. A turn that ended after
        the question arrived means the question never stood on the screen — the
        hook reported a call the conversation did not make — or it went with an
        interrupted turn. Kept, it would offer the panel a dialog to answer, and
        the keys would go into the composer."""
        with self._lock:
            ask = self._asks.get(session)
            if not ask:
                return False
            done = bool(ask.get("toolUseId")) and ask["toolUseId"] in tool_ids
            if not (done or _outlived(ask.get("at"), ended)):
                return False
            before = dict(self._asks)
            self._asks.pop(session, None)
            return self._commit(before)

    def sweep(self, alive):
        """Forgets the questions of dead sessions and the very old ones."""
        now = time.time()
        stale, why = [], {}
        with self._lock:
            for session, ask in self._asks.items():
                if session not in alive:
                    stale.append(session)
                    why[session] = f"the session is not among the live ones ({len(alive)} of them)"
                    continue
                try:
                    born = calendar.timegm(time.strptime(ask.get("at", ""), "%Y-%m-%dT%H:%M:%SZ"))
                except ValueError:
                    stale.append(session)
                    why[session] = f"the time of the question was not parsed: {ask.get('at')!r}"
                    continue
                if now - born > MAX_AGE:
                    stale.append(session)
                    why[session] = f"the question is older than a day ({int(now - born)} s)"
            if not stale:
                return stale
            before = dict(self._asks)
            for session in stale:
                self._asks.pop(session, None)
            if not self._commit(before):
                return []
        for session in stale:
            print(f"aacpanel-agent: the question of {session} is forgotten — {why[session]}", flush=True)
        return stale


def _moment(stamp):
    """Returns the seconds of an ISO time in UTC, with or without a fraction."""
    head = str(stamp or "").split(".")[0].rstrip("Z")
    try:
        return calendar.timegm(time.strptime(head, "%Y-%m-%dT%H:%M:%S"))
    except ValueError:
        return None


def _outlived(asked_at, ended):
    """Says whether a turn ended after the question was asked.

    The question is stamped to the second and the end of a turn to the
    millisecond: an end within the same second may be the end of the turn
    before, so only a later second counts."""
    start, end = _moment(asked_at), _moment(ended)
    return start is not None and end is not None and end > start


BOOK = Book()


def handle(conn, book=None):
    """Serves one hook message and closes the connection."""
    book = book or BOOK
    with conn:
        try:
            conn.settimeout(5)
            raw = conn.recv(MAX_REQUEST)
            payload = json.loads(raw.decode("utf-8"))
            if not isinstance(payload, dict):
                raise ValueError("an object was expected")
            ask = clean(payload)
            if ask is None:
                reply = {"ok": False, "error": "the message carries no question"}
            elif not book.put(ask):
                reply = {"ok": False, "error": "the question was not saved: the store is unavailable"}
            else:
                reply = {"ok": True}
        except (OSError, ValueError, UnicodeDecodeError) as e:
            reply = {"ok": False, "error": f"the message was not parsed: {e}"}
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
    """Runs the question receiving thread."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: the questions of the sessions are unavailable, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: listening for the questions of the sessions on {os.path.join(SOCKET_DIR, SOCKET_NAME)}", flush=True)
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()
