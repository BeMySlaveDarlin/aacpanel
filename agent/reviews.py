#!/usr/bin/env python3
"""Reviews: a reading of a branch, handed to the session as a file.

A person reads a branch in the panel and writes on its lines. What comes of it
reaches the session as a file rather than as text in its terminal: the session
is told where the reading lies and opens it when it gets to it. A file is also
the cheaper thing to extend — a field added here is a field an older reader
steps over, while a line of text has to be parsed by whoever reads it.

The shelf is written from one side only. The panel runs in a container with the
state directory mounted read-only, so it cannot lay the file down itself: it
hands the reading over the socket here, and the file is named after the reading
on this side. That is why the name is checked and never repaired — a name that
had to be repaired is not a name the panel minted, and this is the one place
where reading a branch turns into writing to disk.

Nothing here renders anything. The reading is data: the quote of a line is kept
as the file reads, and what to make of it is the reader's business.
"""

import datetime
import json
import os
import re
import socket
import threading
import time

import paths

# The socket stands in the shelf directory rather than in /run, where the
# sockets of the sessions are: this one is dialled from the container, and the
# state directory is the only thing mounted there. Only *.json is ever read
# back as a reading, so a socket beside them is not one of them.
SOCKET_DIR = os.environ.get("AACP_REVIEW_DIR", paths.state("reviews"))
SOCKET_NAME = "review.sock"

STORE = os.environ.get("AACP_REVIEW_STORE", paths.state("reviews"))

# What a reading may hold. The panel holds a reading to the same numbers before
# it saves one, and they are repeated here because a file on this socket does
# not have to have come from the panel.
MAX_NOTES = 200
MAX_TEXT = 4000
MAX_QUOTE = 2000

MAX_ID = 120
MAX_NOTE_ID = 64
MAX_LINE = 400

# Two hundred notes at their ceilings, in an alphabet where a character costs
# four bytes, with the json around them. A request past this is not a reading.
MAX_REQUEST = 8 * 1024 * 1024

# How much of this the machine keeps.
#
# The age of the file and the age of the sending are two different rules on
# purpose. What a reading says about itself can be absent or a lie — a stamp
# set in the future would sit on the shelf for ever — so a coarse rule on the
# file itself stands behind the one that reads it.
MAX_REVIEWS = 64
MAX_STORE = 32 * 1024 * 1024
MAX_AGE = 90 * 24 * 3600
SENT_AGE = 30 * 24 * 3600

STAMP = "%Y-%m-%dT%H:%M:%SZ"

# The name of a reading as the panel mints it, and nothing else: it becomes the
# name of a file, and every character that is not this one is a way out of the
# shelf.
NAME = re.compile(r"^[A-Za-z0-9_-]{1,%d}$" % MAX_ID)


class Refused(ValueError):
    """The reading was not taken, and the reason is for the panel to show."""


def _line(value, limit=MAX_LINE):
    """One line: whitespace collapsed, cut to the ceiling."""
    return " ".join(str(value or "").split())[:limit]


def _body(value):
    """What somebody wrote: line breaks kept, ends trimmed."""
    return str(value or "").replace("\r\n", "\n").replace("\r", "\n").strip()


def _quote(value):
    """The line a note stands on, kept as the file reads it.

    Leading whitespace is the indentation of the line and is what tells two
    otherwise identical lines apart, so nothing is collapsed here.
    """
    return str(value or "").replace("\r\n", "\n").replace("\r", "\n").rstrip("\n")


def _count(value):
    """Returns a line number, or zero when what came is not one."""
    try:
        return int(value)
    except (TypeError, ValueError, OverflowError):
        return 0


def seconds(stamp):
    """Returns a stamp in seconds, or zero when it cannot be read."""
    if not isinstance(stamp, str) or not stamp:
        return 0
    try:
        when = datetime.datetime.fromisoformat(stamp.replace("Z", "+00:00"))
    except ValueError:
        return 0
    if when.tzinfo is None:
        when = when.replace(tzinfo=datetime.timezone.utc)
    return when.timestamp()


def _stamp(value):
    """Returns a time in the one shape the shelf compares, or an empty string.

    A time arrives as the panel wrote it, which is however Go prints one: with
    fractions, and with an offset that is not always Z. It is kept in one shape
    so that the sweep does not have to know about the others.
    """
    when = seconds(_line(value, 64))
    if not when:
        return ""
    return time.strftime(STAMP, time.gmtime(when))


def name(value):
    """Returns the name a reading is kept under, or raises Refused."""
    got = str(value or "")
    if not NAME.match(got):
        raise Refused("the name of a reading carries latin letters, digits and dashes and nothing else")
    return got


def _note(item):
    """Returns one note as the shelf keeps it, or raises Refused."""
    if not isinstance(item, dict):
        raise Refused("a note is an object")

    note_id = _line(item.get("id"), MAX_NOTE_ID)
    if not note_id:
        raise Refused("every note carries a name of its own")

    path = _line(item.get("path"))
    line = _count(item.get("line"))
    if not path or line <= 0:
        raise Refused("a note stands on a line of a file, and this one names none")

    text = _body(item.get("text"))
    if not text:
        raise Refused("a note with nothing written in it is not a note")
    if len(text) > MAX_TEXT:
        raise Refused(f"a note runs past {MAX_TEXT} characters")

    quote = _quote(item.get("quote"))
    if len(quote) > MAX_QUOTE:
        raise Refused(f"the line a note stands on runs past {MAX_QUOTE} characters")

    return {"id": note_id, "path": path, "line": line,
            "quote": quote, "text": text, "at": _stamp(item.get("at"))}


def _notes(raw):
    """Returns the notes of a reading, or raises Refused."""
    if raw is None:
        raw = []
    if not isinstance(raw, list):
        raise Refused("the notes of a reading are a list")
    if len(raw) > MAX_NOTES:
        raise Refused(f"a reading holds at most {MAX_NOTES} notes")

    out = []
    seen = set()
    for item in raw:
        note = _note(item)
        # The session finds a note again by its name. Two notes under one name
        # mean one of them is the note nobody can point at.
        if note["id"] in seen:
            raise Refused("every note carries a name of its own")
        seen.add(note["id"])
        out.append(note)
    return out


def clean(payload):
    """Returns the reading in the form the shelf keeps, or raises Refused."""
    if not isinstance(payload, dict):
        raise Refused("a reading is an object")

    review_id = name(payload.get("id"))

    session = _line(payload.get("session"), MAX_ID)
    if not session:
        raise Refused("a reading belongs to a conversation, and this one names none")

    return {
        "id": review_id,
        "session": session,
        "cwd": _line(payload.get("cwd")),
        "base": _line(payload.get("base")),
        "at": _stamp(payload.get("at")) or time.strftime(STAMP, time.gmtime()),
        "notes": _notes(payload.get("notes")),
    }


def card(review):
    """Returns what a list shows about a reading without carrying its notes."""
    notes = review.get("notes")
    return {
        "id": review.get("id", ""),
        "session": review.get("session", ""),
        "cwd": review.get("cwd", ""),
        "base": review.get("base", ""),
        "at": review.get("at", ""),
        "notes": len(notes) if isinstance(notes, list) else 0,
    }


class Shelf:
    """Readings on disk, one file each, named by the reading.

    One file each because that is what the session is handed: it opens the file
    it was pointed at and reads nothing else.
    """

    def __init__(self, path=None):
        self.path = path or STORE
        self.lock = threading.Lock()

    def _file(self, review_name):
        return os.path.join(self.path, review_name + ".json")

    def _read(self, review_name):
        try:
            with open(self._file(review_name), encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            return None
        return data if isinstance(data, dict) else None

    def put(self, review):
        """Writes a reading and returns where it lies, or why it was not written.

        A second write of the same reading is taken rather than refused: the
        panel only sends once, so a repeat is a send whose answer went missing,
        and the file it would write is the one already there.
        """
        with self.lock:
            try:
                os.makedirs(self.path, exist_ok=True)
            except OSError as e:
                return "", f"the shelf of readings is not writable: {e}"
            full = self._file(review["id"])
            tmp = full + ".new"
            try:
                with open(tmp, "w", encoding="utf-8") as f:
                    json.dump(review, f, ensure_ascii=False)
                os.replace(tmp, full)
            except OSError as e:
                try:
                    os.unlink(tmp)
                except OSError:
                    pass
                return "", f"the reading was not written: {e}"
        return full, ""

    def of(self, review_name):
        """Returns one reading whole, or None."""
        try:
            got = name(review_name)
        except Refused:
            # The name arrives from outside. A walk out of the shelf has to end
            # here rather than in a neighbouring directory.
            return None
        return self._read(got)

    def cards(self, session=None):
        """Returns the card of every reading, newest first."""
        try:
            names = os.listdir(self.path)
        except OSError:
            return []
        out = []
        for entry in names:
            if not entry.endswith(".json"):
                continue
            review = self._read(entry[: -len(".json")])
            if not review:
                continue
            if session and review.get("session") != session:
                continue
            out.append(card(review))
        out.sort(key=lambda c: str(c.get("at") or ""), reverse=True)
        return out

    def sweep(self):
        """Drops readings by age, by how long ago they went, by count and by room.

        A reading that has just been written is never among them: the largest
        one that can arrive is smaller than the room the shelf keeps, so the
        newest file cannot be the one the size rule reaches.
        """
        gone = []
        with self.lock:
            try:
                names = [n for n in os.listdir(self.path) if n.endswith(".json")]
            except OSError:
                return gone
            now = time.time()
            kept = []
            for entry in names:
                full = os.path.join(self.path, entry)
                try:
                    stat = os.stat(full)
                except OSError:
                    continue
                if now - stat.st_mtime > MAX_AGE:
                    gone.append(entry)
                    continue
                sent = seconds((self._read(entry[: -len(".json")]) or {}).get("at"))
                if sent and now - sent > SENT_AGE:
                    gone.append(entry)
                    continue
                kept.append((stat.st_mtime, stat.st_size, entry))
            kept.sort(reverse=True)
            room = 0
            for i, (_, size, entry) in enumerate(kept):
                room += size
                if i >= MAX_REVIEWS or room > MAX_STORE:
                    gone.append(entry)
            for entry in gone:
                try:
                    os.unlink(os.path.join(self.path, entry))
                except OSError:
                    pass
        return [n[: -len(".json")] for n in gone]


SHELF = Shelf()


def _recv(conn):
    """Reads one request whole: a reading does not fit in a single packet."""
    chunks = []
    size = 0
    while True:
        chunk = conn.recv(64 * 1024)
        if not chunk:
            break
        size += len(chunk)
        if size > MAX_REQUEST:
            raise Refused(f"the reading is longer than {MAX_REQUEST // (1024 * 1024)} MB")
        chunks.append(chunk)
    return b"".join(chunks)


def handle(conn, shelf=None):
    """Takes one reading and closes the connection."""
    shelf = shelf or SHELF
    with conn:
        try:
            conn.settimeout(20)
            payload = json.loads(_recv(conn).decode("utf-8"))
            review = clean(payload)
            where, why = shelf.put(review)
            if where:
                shelf.sweep()
                reply = {"ok": True, "id": review["id"], "path": where,
                         "notes": len(review["notes"])}
            else:
                reply = {"ok": False, "error": why}
        except Refused as e:
            reply = {"ok": False, "error": str(e)}
        except (OSError, ValueError, UnicodeDecodeError) as e:
            reply = {"ok": False, "error": f"the reading was not parsed: {e}"}
        try:
            conn.sendall(json.dumps(reply, ensure_ascii=False).encode("utf-8"))
        except OSError:
            pass


def listen():
    """Opens a 0600 socket in the shelf directory."""
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
    """Runs the thread that takes the readings the panel sends."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: readings will not reach the sessions, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: listening for readings on {os.path.join(SOCKET_DIR, SOCKET_NAME)}", flush=True)
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()
