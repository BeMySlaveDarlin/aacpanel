"""Briefs: a long piece a session publishes and a person walks through.

A brief is not a question of a session. A question stops the turn and is
answered in seconds; a brief is read for as long as it takes and outlives the
conversation that wrote it. That is why it is kept on disk in full, keyed by
its own name, and swept by age rather than by whether its session is still
alive.

Nothing here renders anything. The document is data: every field is text, the
only markup it may carry is inline markdown, and the panel turns that into
nodes on its own side.
"""

import calendar
import json
import os
import re
import socket
import threading
import time

import ctx
import paths

SOCKET_DIR = os.environ.get("AACP_BRIEF_DIR", "/run/aacpanel-agent")
SOCKET_NAME = "brief.sock"

STORE = os.environ.get("AACP_BRIEF_STORE", paths.state("briefs"))

# A brief is written once and read many times, so the ceilings are generous
# where the reader gains by it and tight where a runaway document would cost
# the phone its scroll.
#
# The count of questions is the owner's number: a hundred is more than anyone
# answers in one sitting, and a document that asks more than that is two
# documents. The size of the request follows from it rather than standing on
# its own — a hundred questions with their facts and options fit inside it with
# room to spare, and the ceiling is there so a runaway document is refused
# rather than read into memory whole.
MAX_REQUEST = 4 * 1024 * 1024
MAX_QUESTIONS = 100
MAX_OPTIONS = 8
MAX_FACTS = 16
MAX_READ = 6
MAX_SECTIONS = 12
MAX_CHIPS = 3
MAX_SUMMARY = 4
MAX_LINEAGE = 12
MAX_CLOSING = 6

MAX_ID = 64
MAX_KEY = 4
MAX_TITLE = 200
MAX_EYEBROW = 120
MAX_LINE = 400
MAX_SRC = 60
MAX_ASK = 800
MAX_NOTE = 800
MAX_BLOCK = 2000
MAX_PLACEHOLDER = 120

# How many briefs the machine keeps and for how long. A brief is a working
# document, not an archive: what nobody answered in a month will not be
# answered.
MAX_BRIEFS = 64
MAX_AGE = 30 * 24 * 3600

STAMP = "%Y-%m-%dT%H:%M:%SZ"

SLUG = re.compile(r"^[a-z0-9][a-z0-9-]{0,63}$")

TONES = ("stuck", "born", "plain", "scope")
KINDS = ("pick", "multi", "text", "none")


class Refused(ValueError):
    """Says why a document was not taken, in words meant for whoever sent it."""


def _line(text, limit=MAX_LINE):
    """One line: whitespace collapsed, cut to the ceiling."""
    return " ".join(str(text or "").split())[:limit]


def _block(text, limit=MAX_BLOCK):
    """A paragraph: line breaks kept, cut to the ceiling."""
    raw = str(text or "").replace("\r\n", "\n").replace("\r", "\n")
    return raw.strip()[:limit]


def _blocks(value, limit, each=MAX_BLOCK):
    """A list of paragraphs, from a list or from one string."""
    if isinstance(value, str):
        value = [value]
    if not isinstance(value, list):
        return []
    out = []
    for item in value[:limit]:
        body = _block(item, each)
        if body:
            out.append(body)
    return out


def _slug(value, what):
    got = _line(value, MAX_ID).lower()
    if not SLUG.match(got):
        raise Refused(
            f"{what} must be a name of lowercase latin letters, digits and dashes, "
            f"up to {MAX_ID} characters: got {got!r}")
    return got


def _facts(raw):
    out = []
    for item in (raw or [])[:MAX_FACTS]:
        if not isinstance(item, dict):
            continue
        text = _line(item.get("text"))
        if not text:
            continue
        fact = {"text": text}
        src = _line(item.get("src"), MAX_SRC)
        if src:
            fact["src"] = src
        if item.get("flag"):
            fact["flag"] = True
        out.append(fact)
    return out


def _options(raw, where):
    out = []
    seen = set()
    for item in (raw or [])[:MAX_OPTIONS]:
        if not isinstance(item, dict):
            continue
        label = _line(item.get("label"), MAX_TITLE)
        if not label:
            continue
        key = _line(item.get("key"), MAX_KEY) or chr(ord("A") + len(out))
        if key in seen:
            raise Refused(f"{where}: two options carry the key {key!r}")
        seen.add(key)
        option = {"key": key, "label": label}
        note = _line(item.get("note"), MAX_NOTE)
        if note:
            option["note"] = note
        out.append(option)
    return out


def _chips(raw):
    out = []
    for item in (raw or [])[:MAX_CHIPS]:
        if isinstance(item, str):
            item = {"text": item}
        if not isinstance(item, dict):
            continue
        text = _line(item.get("text"), MAX_EYEBROW)
        if not text:
            continue
        tone = _line(item.get("tone"), 16) or "plain"
        out.append({"tone": tone if tone in TONES else "plain", "text": text})
    return out


def _answered(raw, options, where):
    """What the agent already knows was decided, so a reissue keeps it."""
    if not isinstance(raw, dict):
        return None
    keys = {o["key"] for o in options}
    picks = raw.get("picks")
    if isinstance(picks, str):
        picks = [picks]
    if not isinstance(picks, list):
        picks = [raw.get("pick")] if raw.get("pick") else []
    got = []
    for key in picks[:MAX_OPTIONS]:
        key = _line(key, MAX_KEY)
        if not key:
            continue
        if key not in keys:
            raise Refused(f"{where}: the answer names option {key!r}, which the question does not offer")
        got.append(key)
    out = {}
    if got:
        out["picks"] = got
    note = _line(raw.get("note"), MAX_NOTE)
    if note:
        out["note"] = note
    at = _line(raw.get("at"), 40)
    if at:
        out["at"] = at
    return out or None


def _question(raw, seat, seen):
    if not isinstance(raw, dict):
        raise Refused(f"question {seat}: an object was expected")
    qid = _slug(raw.get("id") or f"q{seat}", f"the id of question {seat}")
    if qid in seen:
        raise Refused(f"two questions carry the id {qid!r}")
    seen.add(qid)

    title = _line(raw.get("title"), MAX_TITLE)
    if not title:
        raise Refused(f"question {qid}: a title is what the person sees in the list, and it is empty")

    kind = _line(raw.get("kind"), 16) or "pick"
    if kind not in KINDS:
        raise Refused(f"question {qid}: kind {kind!r} is unknown, it is one of {', '.join(KINDS)}")

    options = _options(raw.get("options"), f"question {qid}")
    if kind in ("pick", "multi") and not options:
        raise Refused(
            f"question {qid}: a {kind} question is answered by choosing, and it carries no options. "
            f"A question that only takes words is kind \"text\"; one that asks nothing is kind \"none\"")
    if kind in ("text", "none") and options:
        raise Refused(f"question {qid}: kind {kind!r} offers no choice, so options have nowhere to go")

    out = {"id": qid, "title": title, "kind": kind}

    seat_mark = _line(raw.get("n"), 8)
    out["n"] = seat_mark or f"{seat:02d}"

    chips = _chips(raw.get("chips"))
    if chips:
        out["chips"] = chips
    ask = _block(raw.get("ask"), MAX_ASK)
    if ask:
        out["ask"] = ask
    facts = _facts(raw.get("facts"))
    if facts:
        out["facts"] = facts
    if options:
        out["options"] = options
    read = _blocks(raw.get("read"), MAX_READ)
    if read:
        out["read"] = read

    capture = raw.get("capture")
    if capture is not False:
        note = {}
        if isinstance(capture, dict):
            holder = capture.get("note")
            if isinstance(holder, dict):
                placeholder = _line(holder.get("placeholder"), MAX_PLACEHOLDER)
                if placeholder:
                    note["placeholder"] = placeholder
        out["capture"] = {"note": note}

    answered = _answered(raw.get("answered"), options, f"question {qid}")
    if answered:
        out["answered"] = answered
    return out


def _sections(raw):
    out = []
    for item in (raw or [])[:MAX_SECTIONS]:
        if not isinstance(item, dict):
            continue
        body = _blocks(item.get("body"), MAX_READ)
        title = _line(item.get("title"), MAX_TITLE)
        if not body and not title:
            continue
        section = {}
        if title:
            section["title"] = title
        if body:
            section["body"] = body
        out.append(section)
    return out


def _summary(raw):
    out = []
    for item in (raw or [])[:MAX_SUMMARY]:
        if not isinstance(item, dict):
            continue
        label = _line(item.get("label"), MAX_EYEBROW)
        n = _line(item.get("n"), 8)
        if not label or not n:
            continue
        out.append({"n": n, "label": label})
    return out


def _lineage(raw):
    out = []
    for item in (raw or [])[:MAX_LINEAGE]:
        if not isinstance(item, dict):
            continue
        src = _line(item.get("from"), MAX_EYEBROW)
        dst = _line(item.get("to"), MAX_LINE)
        if not src or not dst:
            continue
        out.append({"from": src, "to": dst})
    return out


def session_cwd(session_id):
    """Returns the directory of a live session, or "" when it is not among them.

    The directory a brief belongs to is the directory of the session, and that
    is not the directory the publishing script was started in: a session that
    runs it after a cd hands over the directory of the shell, and the document
    then lands in the conversation of whatever repository the script lives in.
    The registry of live sessions knows where each session itself works, and
    every contour of the machine is read, not only the personal one.
    """
    if not session_id:
        return ""
    for live in ctx.live_sessions():
        if live.get("sessionId") == session_id:
            return live.get("cwd") or ""
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

    brief_id = _slug(doc.get("id"), "the id of the brief")
    title = _line(doc.get("title"), MAX_TITLE)
    if not title:
        raise Refused("a brief carries a title: it is what the person sees before opening it")

    raw_questions = doc.get("questions")
    if raw_questions is None:
        raw_questions = []
    if not isinstance(raw_questions, list):
        raise Refused("questions come as a list")
    if len(raw_questions) > MAX_QUESTIONS:
        raise Refused(f"{len(raw_questions)} questions, and the ceiling is {MAX_QUESTIONS}")

    seen = set()
    questions = [_question(q, i + 1, seen) for i, q in enumerate(raw_questions)]

    # What the session says about its own directory is a fallback, not the
    # answer: it is the directory of the process that ran the script.
    out = {
        "id": brief_id,
        "sessionId": session,
        "cwd": session_cwd(session) or _line(payload.get("cwd"), 400),
        "title": title,
        "at": time.strftime(STAMP, time.gmtime()),
        "questions": questions,
    }

    eyebrow = _line(doc.get("eyebrow"), MAX_EYEBROW)
    if eyebrow:
        out["eyebrow"] = eyebrow
    lede = _block(doc.get("lede"))
    if lede:
        out["lede"] = lede
    summary = _summary(doc.get("summary"))
    if summary:
        out["summary"] = summary
    lineage = _lineage(doc.get("lineage"))
    if lineage:
        out["lineage"] = lineage
    sections = _sections(doc.get("sections"))
    if sections:
        out["sections"] = sections
    closing = _blocks(doc.get("closing"), MAX_CLOSING)
    if closing:
        out["closing"] = closing
    return out


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
                # Only the questions that ask something are counted: a block
                # of kind "none" is a section with a number on it, and a shelf
                # that counts it promises an answer the document has no room
                # for — on the card, on the phone and in the progress it shows.
                questions = [q for q in doc.get("questions") or []
                             if isinstance(q, dict) and q.get("kind", "pick") != "none"]
                out.append({
                    "id": doc.get("id") or "",
                    "sessionId": doc.get("sessionId") or "",
                    "cwd": doc.get("cwd") or "",
                    "title": doc.get("title") or "",
                    "eyebrow": doc.get("eyebrow") or "",
                    "at": doc.get("at") or "",
                    "questions": len(questions),
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
