"""The piece of a transcript one feed window is folded from.

The fold keeps no more rows than the window was asked for and lets the rest
go, so records far enough from the end cannot reach the window: a piece of
the end that folds into more rows than the window holds gives the same page
as the whole file. The piece is taken this wide at first and four times
wider while the page still has no row to spare.

Between requests the piece stays parsed, and a file that has grown is read
on from where the last read stopped, the way the state of a session is. So
the second opening of a conversation costs only what was written since the
first, and neither opening costs the length of the conversation.
"""
import json
import os
import threading
from collections import deque

import held

from .queue import Pending
from .records import parse


# The shelf of briefs, read lazily and once: a feed is folded on every request,
# and a conversation that never published one must not pay for the store. The
# reader is the one function the cards need — a brief by its name — so a feed
# built in a test, or on a host where briefs were never set up, simply has no
# titles to draw and says so by naming the brief instead.
_shelf = None


def shelf_of():
    """Returns a reader of published briefs, or None when there is no store."""
    global _shelf
    if _shelf is None:
        try:
            import briefs
            _shelf = briefs.SHELF.of
        except Exception:
            _shelf = False
    return _shelf or None


FIRST_SPAN = 2 * 1024 * 1024

SPAN_STEP = 4

# A window after a position is folded from a piece that begins earlier than
# the position: a call and its answer lie in neighbouring records, and the
# card of an answer is drawn only by a reader that saw the call.
OVERLAP = 1024 * 1024

# A live session writes without end, and a piece that only grew would hold
# the whole conversation by the evening: its head is let go once it stands
# this much wider than the widest it was ever asked for. Not wider than the
# last request alone: a conversation whose end is sparse needs a wide piece
# on every opening, and a piece cut back after each would be read again
# from the disk on the next.
KEEP_SPANS = 2

# Pieces are kept for this many conversations, the ones opened long ago
# giving way to the ones opened now.
MAX_PIECES = 8

# A transcript only grows, and the piece trusts what it has parsed as long as
# the file is at least as long as it was. That is blind to a file written
# anew under the same name, so this many bytes from the head of the piece
# are looked at again on every request: a record carries its uuid near its
# head, and a file that says something else at the same place is another
# file. One page of the disk, whatever the count: the read costs the same.
STAMP = 1024


def record_of(raw):
    """Returns the record of one line, or None when the line holds none."""
    try:
        record = json.loads(raw.decode("utf-8", "replace"))
    except ValueError:
        return None
    return record if isinstance(record, dict) else None


def permits_of(path, sidechain):
    """Returns the answers to the permissions of the conversation of a transcript, by call.

    Read after the length of the file is taken, and only the records within
    that length are parsed against them: the holder keeps an answer before
    claude has it, so every result written by then has its answer on the disk.
    A side chain has none — the holder keeps the answers of the conversation
    it holds, and a piece remembers what it parsed.
    """
    name = os.path.basename(path)
    if sidechain or not name.endswith(".jsonl"):
        return {}
    return held.permits(name[:-len(".jsonl")])


def fresh(item):
    """Returns a copy of a kept item, so a fold cannot spoil what is kept.

    The fold marks up what it takes — it hangs files on an answer and marks
    the ones outside the conversation — and a piece is read by one request
    after another.
    """
    out = dict(item)
    files = out.get("files")
    if files is not None:
        out["files"] = [dict(entry) for entry in files]
    return out


class Stream:
    """Records of a transcript from a byte position on, parsed one by one."""

    def __init__(self, path, start, sidechain=False):
        self.path = path
        self.start = max(0, int(start))
        self.head = self.start == 0
        self.sidechain = sidechain
        self.cwd = ""

    def __iter__(self):
        pending, asks, sent, briefs, calls = Pending(), {}, set(), set(), {}
        size = os.path.getsize(self.path)
        permits = permits_of(self.path, self.sidechain)
        with open(self.path, "rb") as f:
            pos = self.start
            if pos:
                f.seek(pos)
                # The seek lands in the middle of a line: it belongs to the
                # records before the piece and is read past.
                pos += len(f.readline())
            for raw in f:
                if pos >= size:
                    break
                line_pos = pos
                pos += len(raw)
                record = record_of(raw)
                if record is None:
                    continue
                if not self.cwd and isinstance(record.get("cwd"), str):
                    self.cwd = record["cwd"]
                items = parse(record, line_pos, pending, asks, self.sidechain, sent,
                              briefs, shelf_of(), calls, permits)
                if items:
                    yield line_pos, items


class Piece:
    """A parsed piece of the end of a transcript, kept between requests."""

    def __init__(self, path, start, span, sidechain=False):
        self.path = path
        self.sidechain = sidechain
        self.head = start == 0
        # The widest the piece was ever asked to be: what its head is trimmed
        # against, so that a later request as wide as an earlier one finds
        # the piece still there.
        self.span = span
        # How far back the piece was asked to reach. Its first record may
        # begin a line later, and after a seek it does — the width asked for
        # is what a later request is answered against, not that line.
        self.reach = start
        self.start = start
        self.pos = start
        self.stamp_at = start
        self.stamp = b""
        self.rows = deque()
        self.cwd = ""
        self.pending = Pending()
        self.asks = {}
        self.sent = set()
        self.briefs = set()
        self.calls = {}

    @classmethod
    def of(cls, path, size, span, sidechain=False):
        """Reads a piece of this width from the end of the file."""
        start = max(0, size - span)
        piece = cls(path, start, span, sidechain)
        with open(path, "rb") as f:
            if start:
                f.seek(start)
                piece.start = piece.pos = start + len(f.readline())
            piece.stamp_at = piece.pos
            piece.stamp = f.read(STAMP)
        piece.read_on(size)
        return piece

    def covers(self, size, span):
        """Reports whether the piece reaches this far back from the end of the file."""
        return self.pos <= size and self.reach <= max(0, size - span)

    def intact(self):
        """Reports whether the file still holds what the piece was read from."""
        try:
            with open(self.path, "rb") as f:
                f.seek(self.stamp_at)
                return f.read(STAMP) == self.stamp
        except OSError:
            return False

    def read_on(self, size):
        """Reads the records that appeared since the last read of this file, up to this length."""
        if self.pos >= size:
            return
        permits = permits_of(self.path, self.sidechain)
        with open(self.path, "rb") as f:
            f.seek(self.pos)
            for raw in f:
                if self.pos >= size:
                    break
                if not raw.endswith(b"\n"):
                    # A record still being written: its end is not here yet.
                    break
                line_pos = self.pos
                self.pos += len(raw)
                record = record_of(raw)
                if record is None:
                    continue
                if not self.cwd and isinstance(record.get("cwd"), str):
                    self.cwd = record["cwd"]
                items = parse(record, line_pos, self.pending, self.asks,
                              self.sidechain, self.sent, self.briefs, shelf_of(),
                              self.calls, permits)
                if items:
                    self.rows.append((line_pos, items))
            if len(self.stamp) < STAMP:
                # The piece was taken from a file shorter than the stamp: now
                # that it holds more, the stamp is taken from what it holds.
                f.seek(self.stamp_at)
                self.stamp = f.read(STAMP)

    def edge(self, size):
        """Returns the rows of a record whose end has not been written yet.

        Reading it into the piece would mean parsing it twice — once now and
        once when its end arrives — so it is parsed on a copy of what the
        piece knows, and the copy is thrown away with the answer.
        """
        if self.pos >= size:
            return []
        with open(self.path, "rb") as f:
            f.seek(self.pos)
            raw = f.read(size - self.pos)
        record = record_of(raw)
        if record is None:
            return []
        items = parse(record, self.pos, self.pending.clone(), dict(self.asks),
                      self.sidechain, set(self.sent), set(self.briefs), shelf_of(),
                      dict(self.calls), permits_of(self.path, self.sidechain))
        return [(self.pos, items)] if items else []

    def trim(self):
        """Lets go of the head of a piece grown past the width it was asked for.

        Only the rows go: what was parsed stays known to the piece, so the
        rows it keeps are what a read from its first start would give.
        """
        goal = self.span * KEEP_SPANS
        while len(self.rows) > 1 and self.pos - self.start > goal:
            self.rows.popleft()
            self.start = self.reach = self.rows[0][0]
            self.head = False


class View:
    """What one request sees of a piece: its rows as they were at the time."""

    def __init__(self, path, rows, cwd, head):
        self.path = path
        self.rows = rows
        self.cwd = cwd
        self.head = head

    def __iter__(self):
        for line_pos, items in self.rows:
            yield line_pos, [fresh(item) for item in items]


class Cache:
    """Parsed ends of transcripts, by file.

    Requests for one file take turns, since they read the same piece on;
    requests for different files do not wait for one another.
    """

    def __init__(self):
        self._pieces = {}
        self._locks = {}
        self._lock = threading.Lock()

    def view(self, path, span, sidechain=False):
        """Returns the end of the file, this wide and read on to where it ends now."""
        key = (path, bool(sidechain))
        with self._lock:
            lock = self._locks.setdefault(key, threading.Lock())
        with lock:
            size = os.path.getsize(path)
            with self._lock:
                piece = self._pieces.get(key)
            if piece is None or not piece.covers(size, span) or not piece.intact():
                piece = Piece.of(path, size, span, sidechain)
            else:
                piece.read_on(size)
            piece.span = max(piece.span, span)
            piece.trim()
            rows = list(piece.rows) + piece.edge(size)
            with self._lock:
                self._pieces.pop(key, None)
                self._pieces[key] = piece
                while len(self._pieces) > MAX_PIECES:
                    old = next(iter(self._pieces))
                    del self._pieces[old]
                    self._locks.pop(old, None)
        return View(path, rows, piece.cwd, piece.head)

    def forget(self):
        """Lets go of every piece, so the next request reads from the disk."""
        with self._lock:
            self._pieces.clear()
            self._locks.clear()


PIECES = Cache()
