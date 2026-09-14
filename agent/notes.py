"""A call for the human: a session says a word and the panel carries it to the phone."""

import calendar
import json
import math
import os
import socket
import threading
import time

import paths

SOCKET_DIR = os.environ.get("AACP_NOTIFY_DIR", "/run/aacpanel-agent")
SOCKET_NAME = "notify.sock"

STORE = os.environ.get("AACP_NOTIFY_STORE", paths.state("notes.json"))

MAX_TEXT = 300
MAX_REQUEST = 8 * 1024

# One call a minute from a session. A call is for a person, and a person is not
# called faster than that; a session that has more to say says it in the next one.
MIN_GAP = 60

# A call nobody carried away in this long is not news any more. The panel looks
# at the snapshot every twenty seconds, so the window is wide with room to spare.
MAX_AGE = 5 * 60

STAMP = "%Y-%m-%dT%H:%M:%SZ"


def _short(text, limit=MAX_TEXT):
    return " ".join(str(text or "").split())[:limit]


def _seconds(stamp):
    try:
        return calendar.timegm(time.strptime(stamp, STAMP))
    except (TypeError, ValueError):
        return None


def clean(payload):
    """Returns the call in the form the snapshot carries, or None."""
    session = str(payload.get("sessionId") or "")
    text = _short(payload.get("text"))
    if not session or not text:
        return None
    return {
        "sessionId": session,
        "text": text,
        "at": time.strftime(STAMP, time.gmtime()),
    }


class Board:
    """The call of each conversation, one at a time, kept until it is carried away."""

    def __init__(self, path=STORE):
        self.path = path
        self._notes = {}
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
                self._notes = {k: v for k, v in data.items() if isinstance(v, dict)}

    def _write(self):
        tmp = self.path + ".tmp"
        try:
            os.makedirs(os.path.dirname(self.path), exist_ok=True)
            with open(tmp, "w", encoding="utf-8") as f:
                json.dump(self._notes, f, ensure_ascii=False)
            os.replace(tmp, self.path)
        except OSError as e:
            print(f"aacpanel-agent: the call of a session was not written to {self.path}: {e}", flush=True)
            return False
        return True

    def _commit(self, before):
        if self._write():
            return True
        self._notes = before
        return False

    def put(self, note, now=None):
        """Takes a call, or says why it was not taken."""
        now = time.time() if now is None else now
        with self._lock:
            standing = self._notes.get(note["sessionId"])
            if standing:
                was = _seconds(standing.get("at"))
                if was is not None and now - was < MIN_GAP:
                    gone = now - was
                    left = int(math.ceil(MIN_GAP - gone))
                    return False, f"this session called {int(gone)} s ago: one call a minute, {left} s left"
            before = dict(self._notes)
            self._notes[note["sessionId"]] = note
            if not self._commit(before):
                return False, "the call was not saved: the store is unavailable"
        return True, ""

    def of(self, session):
        """Returns the call of this conversation, or None."""
        with self._lock:
            return self._notes.get(session)

    def sweep(self, alive, now=None):
        """Forgets the calls of dead sessions and the ones nobody came for."""
        now = time.time() if now is None else now
        stale = []
        with self._lock:
            for session, note in self._notes.items():
                if session not in alive:
                    stale.append(session)
                    continue
                at = _seconds(note.get("at"))
                if at is None or now - at > MAX_AGE:
                    stale.append(session)
            if not stale:
                return stale
            before = dict(self._notes)
            for session in stale:
                self._notes.pop(session, None)
            if not self._commit(before):
                return []
        return stale


BOARD = Board()


def handle(conn, board=None):
    """Serves one call and closes the connection."""
    board = board or BOARD
    with conn:
        try:
            conn.settimeout(5)
            raw = conn.recv(MAX_REQUEST)
            payload = json.loads(raw.decode("utf-8"))
            if not isinstance(payload, dict):
                raise ValueError("an object was expected")
            note = clean(payload)
            if note is None:
                reply = {"ok": False, "error": "a call carries a session and a word to say"}
            else:
                ok, why = board.put(note)
                reply = {"ok": True} if ok else {"ok": False, "error": why}
        except (OSError, ValueError, UnicodeDecodeError) as e:
            reply = {"ok": False, "error": f"the call was not parsed: {e}"}
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
    """Runs the thread that takes the calls of the sessions."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: the sessions cannot call, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: listening for the calls of the sessions on {os.path.join(SOCKET_DIR, SOCKET_NAME)}", flush=True)
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()
