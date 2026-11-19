"""Briefs: a task and the decisions a person makes about it.

A brief is not a question of a session. A question stops the turn and is
answered in seconds; a brief is read for as long as it takes and outlives the
conversation that wrote it. That is why it is kept on disk in full, keyed by
its own name, and swept by age rather than by whether its session is still
alive.

Nothing here renders anything. The document is structured data; markdown in
its text fields is turned into nodes by the panel.
"""

import calendar
import json
import os
import re
import socket
import threading
import time

import ctx
import held
import paths

SOCKET_DIR = os.environ.get("AACP_BRIEF_DIR", "/run/aacpanel-agent")
SOCKET_NAME = "brief.sock"

STORE = os.environ.get("AACP_BRIEF_STORE", paths.state("briefs"))

MAX_REQUEST = 4 * 1024 * 1024
MAX_DECISIONS = 100
MAX_OPTIONS = 8
MAX_CONTEXT = 6
MAX_ID = 64
MAX_KEY = 4
MAX_TITLE = 120
MAX_LINE = 400
MAX_ASK = 600
MAX_BLOCK = 2000

OLD_FIELDS = frozenset(("eyebrow", "lede", "summary", "lineage", "sections", "closing",
                        "questions", "facts", "read", "chips", "capture"))
OLD_FORM = ("this is the old form of a brief: a brief is a task, why, and decisions with options "
            "— call brief_publish without doc for the rules.")

# How many briefs the machine keeps and for how long. A brief is a working
# document, not an archive: what nobody answered in a month will not be
# answered.
MAX_BRIEFS = 64
MAX_AGE = 30 * 24 * 3600

STAMP = "%Y-%m-%dT%H:%M:%SZ"

SLUG = re.compile(r"^[a-z0-9][a-z0-9-]{0,63}$")


class Refused(ValueError):
    """Says why a document was not taken, in words meant for whoever sent it."""


def legacy_guide():
    """Only the panel's first import needs this fixed file, never a caller's path."""
    config = os.environ.get("XDG_CONFIG_HOME") or os.path.expanduser("~/.config")
    try:
        with open(os.path.join(config, "aacpanel", "brief-guide.md"), encoding="utf-8") as f:
            text = f.read(MAX_REQUEST + 1)
        if len(text.encode("utf-8")) > MAX_REQUEST:
            return {"ok": False, "error": "the brief guide is longer than 4 MB"}
        return {"ok": True, "briefGuide": text}
    except FileNotFoundError:
        return {"ok": True, "briefGuide": ""}
    except (OSError, UnicodeError) as err:
        return {"ok": False, "error": "the brief guide was not read: " + str(err)}


def _line(text, limit=MAX_LINE):
    """One line: whitespace collapsed, cut to the ceiling."""
    return " ".join(str(text or "").split())[:limit]


def _block(text, limit=MAX_BLOCK):
    """A paragraph: line breaks kept, cut to the ceiling."""
    raw = str(text or "").replace("\r\n", "\n").replace("\r", "\n")
    return raw.strip()[:limit]


def _slug(value, what):
    got = _line(value, MAX_ID).lower()
    if not SLUG.fullmatch(got):
        raise Refused(
            f"{what} must be a name of lowercase latin letters, digits and dashes, "
            f"up to {MAX_ID} characters: got {got!r}")
    return got


def _new_form(value):
    if isinstance(value, dict):
        if OLD_FIELDS.intersection(value):
            raise Refused(OLD_FORM)
        for child in value.values():
            _new_form(child)
    elif isinstance(value, list):
        for child in value:
            _new_form(child)


def _required(raw, key, limit, where, block=False):
    value = raw.get(key)
    if not isinstance(value, str) or not value.strip():
        raise Refused(f"{where}: {key} is required text")
    return _block(value, limit) if block else _line(value, limit)


def _decision(raw, seat, seen):
    where = f"decision {seat}"
    if not isinstance(raw, dict):
        raise Refused(f"{where}: an object was expected")
    identity = raw.get("id", str(seat))
    if not isinstance(identity, str) or not identity.strip() or len(identity) > 40:
        raise Refused(f"{where}: id must be text of at most 40 characters")
    identity = identity.strip()
    if identity in seen:
        raise Refused(f"two decisions carry the id {identity!r}")
    seen.add(identity)
    multi = raw.get("multi", False)
    if not isinstance(multi, bool):
        raise Refused(f"{where}: multi must be a boolean")
    options = raw.get("options")
    if not isinstance(options, list) or not 2 <= len(options) <= MAX_OPTIONS:
        raise Refused(f"{where}: options must be a list of 2 to {MAX_OPTIONS} options")
    clean_options = []
    keys = set()
    for i, option in enumerate(options):
        at = f"{where}, option {i + 1}"
        if not isinstance(option, dict):
            raise Refused(f"{at}: an object was expected")
        key = option.get("key", chr(ord("A") + i))
        if not isinstance(key, str) or not key.strip() or len(key) > MAX_KEY:
            raise Refused(f"{at}: key must be text of at most {MAX_KEY} characters")
        key = key.strip()
        if key in keys:
            raise Refused(f"{where}: two options carry the key {key!r}")
        keys.add(key)
        hours = option.get("hours")
        if isinstance(hours, bool) or not isinstance(hours, (int, float)) or not 0.1 <= hours <= 1000:
            raise Refused(f"{at}: hours must be a number from 0.1 to 1000")
        recommended = option.get("recommended", False)
        if not isinstance(recommended, bool):
            raise Refused(f"{at}: recommended must be a boolean")
        clean_options.append({
            "key": key,
            "label": _required(option, "label", MAX_TITLE, at),
            "reason": _required(option, "reason", 400, at),
            "cost": _required(option, "cost", 300, at),
            "hours": hours,
            "recommended": recommended,
        })
    recommended = sum(option["recommended"] for option in clean_options)
    if recommended == 0 or (not multi and recommended != 1):
        raise Refused(f"{where}: recommend {'at least one option' if multi else 'exactly one option'}")
    return {"id": identity, "title": _required(raw, "title", MAX_TITLE, where),
            "ask": _block(raw.get("ask"), MAX_ASK), "multi": multi, "options": clean_options}


def session_cwd(session_id):
    """Returns the directory of a live session, or "" when it is not among them.

    The directory a brief belongs to is the directory of the session, and that
    is not always the directory the publisher hands over: a publisher started
    from a shell hands over wherever the agent last went with cd, and the
    document then lands in the conversation of that directory. The registry of
    live sessions knows where each session itself works, and every contour of
    the machine is read, not only the personal one. A thread of codex works
    where the executor that follows it says, the directory its row names: the
    server of the thread names it by the path the kernel resolves, and the
    conversation finds its briefs by the row's.
    """
    if not session_id:
        return ""
    for live in ctx.live_sessions():
        if live.get("sessionId") == session_id:
            return live.get("cwd") or ""
    for thread in held.codex_threads():
        if thread.get("sessionId") == session_id:
            return thread.get("cwd") or ""
    return ""


def clean(payload):
    """Returns the brief in the form the panel gets, or raises Refused."""
    if not isinstance(payload, dict):
        raise Refused("an object was expected")
    session = _line(payload.get("sessionId"), 80)
    if not session:
        raise Refused("a brief carries the session that wrote it: there is nowhere to send the answer without it")
    doc = payload.get("doc")
    if not isinstance(doc, dict):
        raise Refused("the document itself is missing")

    _new_form(doc)
    brief_id = _slug(doc.get("id"), "the id of the brief")
    title = _required(doc, "title", MAX_TITLE, "the brief")
    task = _required(doc, "task", 400, "the brief")
    why = _required(doc, "why", 800, "the brief", block=True)
    raw_decisions = doc.get("decisions")
    if not isinstance(raw_decisions, list) or not 1 <= len(raw_decisions) <= MAX_DECISIONS:
        raise Refused(f"decisions must be a list of 1 to {MAX_DECISIONS} decisions")
    seen = set()
    decisions = [_decision(d, i + 1, seen) for i, d in enumerate(raw_decisions)]
    context = doc.get("context", [])
    if not isinstance(context, list) or len(context) > MAX_CONTEXT:
        raise Refused(f"context must be a list of at most {MAX_CONTEXT} blocks")
    blocks = []
    for item in context:
        if not isinstance(item, dict):
            raise Refused("context: an object was expected")
        blocks.append({"title": _required(item, "title", 80, "context"),
                       "text": _required(item, "text", MAX_BLOCK, "context", block=True)})
    # The publisher's directory can be wherever the agent last ran cd; the
    # live session's directory is where the person finds this document.
    return {
        "id": brief_id, "sessionId": session,
        "cwd": session_cwd(session) or _line(payload.get("cwd"), 400),
        "title": title, "task": task, "why": why, "context": blocks,
        "at": time.strftime(STAMP, time.gmtime()), "decisions": decisions,
    }


def _seconds(stamp):
    try:
        return calendar.timegm(time.strptime(stamp, STAMP))
    except (TypeError, ValueError):
        return None


class Shelf:
    """Briefs on disk, one file each, named by the brief.

    One file each and not one file for all: a brief runs to tens of kilobytes,
    and the screen opens one of them at a time.
    """

    def __init__(self, path=STORE):
        self.path = path
        self._lock = threading.Lock()

    def _file(self, brief_id):
        return os.path.join(self.path, brief_id + ".json")

    def _read(self, brief_id):
        try:
            with open(self._file(brief_id), encoding="utf-8") as f:
                doc = json.load(f)
        except (OSError, ValueError):
            return None
        return doc if isinstance(doc, dict) else None

    def put(self, brief):
        """Writes the brief, or says why it was not written.

        A document published a second time under the same name keeps the
        answers already given and says so in its head: the person comes back to
        a text that has changed under what they already decided, and a silent
        replacement is the one thing they cannot check.
        """
        with self._lock:
            standing = self._read(brief["id"])
            if standing:
                brief["firstAt"] = standing.get("firstAt") or standing.get("at") or brief.get("at")
                brief["reissuedAt"] = brief.get("at")
            # The id names the document, not the conversation. Two projects
            # reaching for the same name is a mistake worth saying out loud:
            # the alternative is one of them silently overwriting the other.
            if standing and standing.get("cwd") and brief.get("cwd") \
                    and standing["cwd"] != brief["cwd"]:
                return False, (f"the id {brief['id']!r} is taken by a brief from {standing['cwd']}: "
                               f"give this one a name of its own")
            try:
                os.makedirs(self.path, exist_ok=True)
                tmp = self._file(brief["id"]) + ".tmp"
                with open(tmp, "w", encoding="utf-8") as f:
                    json.dump(brief, f, ensure_ascii=False)
                os.replace(tmp, self._file(brief["id"]))
            except OSError as e:
                print(f"aacpanel-agent: the brief was not written to {self.path}: {e}", flush=True)
                return False, "the brief was not saved: the store is unavailable"
        return True, ""

    def of(self, brief_id):
        """Returns one brief in full, or None."""
        if not SLUG.match(str(brief_id or "")):
            return None
        with self._lock:
            return self._read(brief_id)

    def drop(self, brief_id, cwd=None):
        """Takes a brief off the shelf, and says what happened.

        A brief belongs to the directory it was written in, and that is what
        "your own" means for a session asking to remove one: the conversation
        carrying the work on is the one that may put down its own documents.
        A person reading the panel asks without a directory and removes what
        they are looking at — every brief on the machine is theirs.
        """
        try:
            name = _slug(brief_id, "the name of a brief")
        except Refused as e:
            return False, str(e)
        if name != str(brief_id):
            return False, "this is not the name of a brief"
        with self._lock:
            doc = self._read(name)
            if not doc:
                return False, "there is no brief under this name: it was never published, or it is already gone"
            if cwd and doc.get("cwd") and doc["cwd"] != cwd:
                return False, (f"this brief was written in {doc['cwd']}, and a session removes only "
                               "the documents of the directory it works in")
            try:
                os.unlink(self._file(name))
            except FileNotFoundError:
                return False, "the brief is already gone"
            except OSError as e:
                return False, f"the brief was not removed: {e}"
        return True, ""

    def cards(self, session=None):
        """Returns the short card of every brief, newest first."""
        out = []
        with self._lock:
            try:
                names = os.listdir(self.path)
            except OSError:
                return out
            for name in names:
                if not name.endswith(".json"):
                    continue
                doc = self._read(name[:-5])
                if not doc:
                    continue
                if session and doc.get("sessionId") != session:
                    continue
                # Only the questions that ask something are counted: a block
                # of kind "none" is a section with a number on it, and a shelf
                # that counts it promises an answer the document has no room
                # for — on the card, on the phone and in the progress it shows.
                questions = [q for q in doc.get("questions") or []
                             if isinstance(q, dict) and q.get("kind", "pick") != "none"]
                # When the document first went out names the publication the
                # panel keeps the answers of: a brief removed and published
                # again under the same name is another document.
                out.append({
                    "id": doc.get("id") or "",
                    "sessionId": doc.get("sessionId") or "",
                    "cwd": doc.get("cwd") or "",
                    "title": doc.get("title") or "",
                    "eyebrow": doc.get("eyebrow") or "",
                    "at": doc.get("at") or "",
                    "firstAt": doc.get("firstAt") or "",
                    "questions": len(doc["decisions"]) if "decisions" in doc else len(questions),
                    **({"decisions": len(doc["decisions"])} if "decisions" in doc else {}),
                })
        out.sort(key=lambda c: c["at"], reverse=True)
        return out

    def sweep(self, now=None):
        """Forgets briefs nobody came back to, and the oldest over the ceiling.

        A live session is not the condition here: a brief is answered after
        the conversation that wrote it has long ended, and sweeping by that
        would take the document out from under the person reading it.
        """
        now = time.time() if now is None else now
        gone = []
        with self._lock:
            try:
                names = sorted(n for n in os.listdir(self.path) if n.endswith(".json"))
            except OSError:
                return gone
            kept = []
            for name in names:
                doc = self._read(name[:-5])
                at = _seconds((doc or {}).get("at") or "")
                if not doc or at is None or now - at > MAX_AGE:
                    gone.append(name[:-5])
                    continue
                kept.append((at, name[:-5]))
            kept.sort(reverse=True)
            for _, brief_id in kept[MAX_BRIEFS:]:
                gone.append(brief_id)
            for brief_id in gone:
                try:
                    os.unlink(self._file(brief_id))
                except OSError:
                    pass
        return gone


SHELF = Shelf()


def _recv(conn):
    """Reads one request whole: a brief does not fit in a single packet."""
    chunks = []
    size = 0
    while True:
        chunk = conn.recv(64 * 1024)
        if not chunk:
            break
        size += len(chunk)
        if size > MAX_REQUEST:
            raise Refused(f"the document is longer than {MAX_REQUEST // 1024} KB")
        chunks.append(chunk)
    return b"".join(chunks)


def handle(conn, shelf=None):
    """Serves one publication and closes the connection."""
    shelf = shelf or SHELF
    with conn:
        try:
            conn.settimeout(10)
            payload = json.loads(_recv(conn).decode("utf-8"))
            # The same socket takes away what it brought. A removal is held to
            # the directory of the session asking for it: a session puts down
            # the documents of its own work and nothing else.
            if isinstance(payload, dict) and payload.get("drop"):
                asking = _line(payload.get("sessionId"), 80)
                where = session_cwd(asking) or payload.get("cwd") or None
                ok, why = shelf.drop(payload.get("drop"), where)
                reply = {"ok": True, "dropped": payload.get("drop")} if ok else {"ok": False, "error": why}
                conn.sendall(json.dumps(reply, ensure_ascii=False).encode("utf-8"))
                return
            brief = clean(payload)
            ok, why = shelf.put(brief)
            if ok:
                # Swept here rather than on a timer: briefs arrive rarely, and
                # a pass over a few dozen files costs less than a thread that
                # wakes up all day to find nothing to do.
                shelf.sweep()
            reply = {"ok": True, "id": brief["id"]} if ok else {"ok": False, "error": why}
        except Refused as e:
            reply = {"ok": False, "error": str(e)}
        except (OSError, ValueError, UnicodeDecodeError) as e:
            reply = {"ok": False, "error": f"the brief was not parsed: {e}"}
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
    """Runs the thread that takes the briefs of the sessions."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: the sessions cannot publish briefs, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: listening for briefs on {os.path.join(SOCKET_DIR, SOCKET_NAME)}", flush=True)
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()
