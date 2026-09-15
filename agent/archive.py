"""Archive of claude sessions: the list of conversations straight from the transcripts."""

import datetime
import glob
import json
import os
import re
import threading

import contours
import models
import paths

PROJECTS = os.environ.get("AACP_CLAUDE_PROJECTS")
LIVE = os.environ.get("AACP_CLAUDE_SESSIONS")


def profile_dirs():
    """Returns pairs of profile and conversation directory, the personal one first."""
    if PROJECTS:
        return [("", PROJECTS)]
    return [(name, os.path.join(d, "projects")) for name, d in contours.profiles()]


def resolve_profiles(profiles):
    """Returns profile and directory pairs for the requested contours, in registry order.

    Asked for nothing in particular, the archive answers for every contour of the
    machine. The panel names the contour it wants — a page per contour, so that a
    busy one does not push a quiet one off the screen — and what is left asking
    for nothing is a caller with no map of contours at all, which is exactly the
    caller that wants to see everything there is. The price is the size of the
    answer: a page over every contour is sorted by the time of the last message,
    so it is dominated by whichever contour was worked in last.
    """
    pairs = profile_dirs()
    if not pairs:
        return []
    if not profiles:
        return pairs
    wanted = {_named(p) for p in profiles}
    return [(name, d) for name, d in pairs if name in wanted]


def _named(pick):
    if PROJECTS or not pick.startswith("/"):
        return pick
    want = os.path.normpath(pick)
    for name, d in contours.profiles():
        if os.path.normpath(d) == want:
            return name
    return pick


def live_dirs():
    """Returns the live session directories of every contour, the personal one first."""
    return contours.dirs("sessions", LIVE)


BACKGROUND_KINDS = ("bg",)


def background(data):
    """Reports whether this is a background claude process rather than a console session."""
    kind = data.get("kind") or ""
    return kind in BACKGROUND_KINDS or kind.startswith("bg-")


INDEX_PATH = os.environ.get("AACP_SESSIONS_INDEX", paths.state("sessions-index.json"))

HOME_SLUG = os.path.expanduser("~").replace("/", "-")

UUID_RE = re.compile(r"^[0-9a-fA-F-]{36}$")

DEFAULT_LIMIT = 20
MAX_LIMIT = 100

MODEL_LIMITS = models.MODEL_LIMITS
DEFAULT_LIMIT_TOKENS = models.DEFAULT_LIMIT_TOKENS
limit_for = models.limit_for


# How far back to look for the last stamp before reading the whole transcript.
TAIL_STEPS = (64 * 1024, 1024 * 1024)


def tail_stamp(path, size):
    """Returns the last stamp of a transcript, reading no more of its tail than it takes."""
    for back in TAIL_STEPS + (size,):
        start = max(0, size - back)
        out = ""
        try:
            with open(path, "rb") as f:
                f.seek(start)
                for line in f:
                    try:
                        record = json.loads(line)
                    except ValueError:
                        continue
                    if isinstance(record, dict) and record.get("timestamp"):
                        out = record["timestamp"]
        except OSError:
            return ""
        if out or start == 0:
            return out
    return ""


def seconds(stamp):
    """Returns the stamp in seconds, or None when it cannot be read."""
    if not isinstance(stamp, str) or not stamp:
        return None
    try:
        return datetime.datetime.fromisoformat(stamp.replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


class Index:
    """Transcript parsing with memory: what is counted is not counted again.

    One index serves every request, and the requests arrive in threads of their
    own: the panel asks for each contour separately, so three pages are being
    built while a fourth is being written to disk. json.dump walks the very
    dictionaries the other threads are adding to, and a dictionary that changes
    size mid-walk raises — the answer is then lost and the panel shows that
    contour as unavailable. Everything that touches the three dictionaries holds
    the lock; reading a transcript, which is the slow part, does not.
    """

    def __init__(self, path=INDEX_PATH):
        self.path = path
        self.scanned = {}
        self.names = {}
        self.stamps = {}
        self.dirty = False
        self.lock = threading.RLock()
        self.load()

    def load(self):
        try:
            with open(self.path, encoding="utf-8") as f:
                saved = json.load(f)
        except (OSError, ValueError):
            return
        if not isinstance(saved, dict):
            return
        scanned = saved.get("scanned")
        names = saved.get("names")
        stamps = saved.get("stamps")
        if isinstance(scanned, dict):
            self.scanned = {k: v for k, v in scanned.items() if isinstance(v, dict)}
        if isinstance(names, dict):
            self.names = {k: v for k, v in names.items() if isinstance(v, str)}
        if isinstance(stamps, dict):
            self.stamps = {k: v for k, v in stamps.items() if isinstance(v, dict)}

    def save(self):
        """Writes the index atomically, with nothing changing under the writer."""
        with self.lock:
            if not self.dirty:
                return
            tmp = self.path + ".tmp"
            try:
                os.makedirs(os.path.dirname(self.path), exist_ok=True)
                with open(tmp, "w", encoding="utf-8") as f:
                    json.dump({"scanned": self.scanned, "names": self.names,
                               "stamps": self.stamps}, f)
                os.replace(tmp, self.path)
                self.dirty = False
            except OSError:
                pass

    def live_files(self):
        """Returns the live session files of every contour."""
        out = []
        for live in live_dirs():
            out.extend(glob.glob(os.path.join(live, "*.json")))
        return out

    def observe(self):
        """Remembers the names of live sessions while they are alive."""
        for path in self.live_files():
            try:
                with open(path, encoding="utf-8") as f:
                    data = json.load(f)
            except (OSError, ValueError):
                continue
            sid, name = data.get("sessionId"), data.get("name")
            if not sid or not isinstance(name, str) or not name:
                continue
            if name.startswith("tmp-"):
                continue
            if background(data):
                continue
            with self.lock:
                if self.names.get(sid) != name:
                    self.names[sid] = name
                    self.dirty = True
        self.save()

    def entry(self, path, size, mtime, profile):
        """Returns the parse of one transcript, from memory or from disk."""
        sid = os.path.basename(path)[:-len(".jsonl")]
        with self.lock:
            was = self.scanned.get(sid)
            if was and was.get("size") == size and was.get("mtime") == mtime:
                if was.get("profile") != profile:
                    was["profile"] = profile
                    self.dirty = True
                return was
        # The transcript is read without the lock: it is the slow part, and two
        # threads reading the same one cost a repeated read, not a wrong answer.
        found = scan(path)
        found.update({"sessionId": sid, "size": size, "mtime": mtime, "profile": profile,
                      "slug": os.path.basename(os.path.dirname(path))})
        with self.lock:
            self.scanned[sid] = found
            self.stamps.pop(sid, None)
            self.dirty = True
        return found

    def when(self, sid, path, size, mtime):
        """Returns when the conversation last spoke: the stamp the card shows, in seconds.

        The time a file was touched is not it: claude appends a title or a mode to an old
        transcript long after the talk ended, and sorting by that throws a conversation of
        last spring over today's.
        """
        with self.lock:
            was = self.scanned.get(sid)
            if was and was.get("size") == size and was.get("mtime") == mtime:
                at = seconds(was.get("lastAt"))
                return mtime if at is None else at
            seen = self.stamps.get(sid)
            fresh = bool(seen) and seen.get("size") == size and seen.get("mtime") == mtime

        if not fresh:
            # Reading the tail of a transcript is disk work, so it happens with
            # the lock down, the same way a full parse does.
            seen = {"size": size, "mtime": mtime, "lastAt": tail_stamp(path, size)}
            with self.lock:
                self.stamps[sid] = seen
                self.dirty = True
        at = seconds(seen.get("lastAt"))
        return mtime if at is None else at

    def page(self, limit=DEFAULT_LIMIT, offset=0, skip=(), profile=None, profiles=None):
        """Returns a page of the archive for the named profiles, the freshest first."""
        limit = max(1, min(int(limit or DEFAULT_LIMIT), MAX_LIMIT))
        offset = max(0, int(offset or 0))
        skip = set(skip or ())

        wanted = [p for p in (profiles or ()) if p] or ([profile] if profile else [])
        picked = resolve_profiles(wanted)

        files = []
        for contour, root in picked:
            for path in glob.glob(os.path.join(root, "*", "*.jsonl")):
                name = os.path.basename(path)
                if not UUID_RE.match(name[:-len(".jsonl")]):
                    continue
                if name[:-len(".jsonl")] in skip:
                    continue
                try:
                    st = os.stat(path)
                except OSError:
                    continue
                if st.st_size < 2:
                    continue
                sid = name[:-len(".jsonl")]
                files.append((self.when(sid, path, st.st_size, st.st_mtime),
                              st.st_mtime, st.st_size, path, contour))
        files.sort(key=lambda f: (f[0], f[1], f[3]), reverse=True)

        rows = []
        for _, mtime, size, path, contour in files[offset:]:
            rows.append(present(self.entry(path, size, mtime, contour), self.names))
            if len(rows) >= limit:
                break
        self.save()
        return {"rows": rows, "total": len(files), "limit": limit, "offset": offset}


def scan(path):
    """Returns everything the archive card needs, in one pass over the transcript."""
    out = {
        "cwd": "", "model": "", "effort": "", "mode": "",
        "startedAt": "", "lastAt": "", "lastRequestAt": "",
        "tokens": 0, "tokensMax": 0, "tokensIn": 0, "tokensOut": 0,
        "messages": 0, "compacts": 0,
        "stale": False,
    }
    with open(path, encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                record = json.loads(line)
            except ValueError:
                continue
            if not isinstance(record, dict):
                continue
            stamp = record.get("timestamp")
            if stamp:
                if not out["startedAt"]:
                    out["startedAt"] = stamp
                out["lastAt"] = stamp
            if not out["cwd"] and record.get("cwd"):
                out["cwd"] = record["cwd"]
            if record.get("isCompactSummary"):
                out["compacts"] += 1
                out["stale"] = out["tokens"] > 0
            if record.get("type") in ("user", "assistant"):
                out["messages"] += 1
            out["effort"] = record.get("effort") or out["effort"]
            out["mode"] = record.get("permissionMode") or out["mode"]
            message = record.get("message") or {}
            usage = message.get("usage")
            if not usage or message.get("model") == "<synthetic>":
                continue
            total = (usage.get("input_tokens", 0)
                     + usage.get("cache_creation_input_tokens", 0)
                     + usage.get("cache_read_input_tokens", 0))
            out["tokens"] = total
            out["tokensMax"] = max(out["tokensMax"], total)
            out["tokensIn"] += total
            out["tokensOut"] += usage.get("output_tokens", 0)
            out["lastRequestAt"] = stamp or out["lastRequestAt"]
            out["model"] = message.get("model") or out["model"]
            out["stale"] = False
    return out


def present(found, names):
    """Returns the archive row the panel expects, built from a parse."""
    sid = found["sessionId"]
    cwd = found.get("cwd") or ""
    name = names.get(sid)
    guessed = not name
    if guessed:
        name = os.path.basename(cwd.rstrip("/")) or cwd or sid[:8]

    limit, known = limit_for(found.get("model"))
    tokens = found.get("tokens") or 0
    peak = found.get("tokensMax") or 0
    if peak > limit:
        limit, known = DEFAULT_LIMIT_TOKENS, False
    return {
        "sessionId": sid,
        "name": name,
        "nameGuessed": guessed,
        "cwd": cwd,
        "profile": found.get("profile") or "",
        "slug": found.get("slug") or "",
        "home": bool(cwd) and cwd.rstrip("/") == os.path.expanduser("~"),
        "model": found.get("model") or "",
        "effort": found.get("effort") or "",
        "pct": round(tokens / limit * 100, 1) if limit else 0.0,
        "pctMax": round(peak / limit * 100, 1) if limit else 0.0,
        "tokens": tokens,
        "tokensMax": peak,
        "limit": limit,
        "limitKnown": known,
        "messages": found.get("messages") or 0,
        "compacts": found.get("compacts") or 0,
        "stale": bool(found.get("stale")),
        "startedAt": found.get("startedAt") or "",
        "lastAt": found.get("lastAt") or "",
        "lastRequestAt": found.get("lastRequestAt") or "",
        "noRequests": not found.get("lastRequestAt"),
    }
