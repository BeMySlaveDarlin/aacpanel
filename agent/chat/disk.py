"""What is read from disk on request from the feed: task output and project files."""
import base64
import datetime
import glob
import json
import os
import re
import stat

import projects
import sesstate

from .locate import UUID_RE


TASK_ID_RE = re.compile(r"^[A-Za-z0-9]{1,32}$")

TASK_TAIL = 24 * 1024


def task_output(session, cwd, task_id):
    """Returns the tail of a background task output, or None when there is no file."""
    if not TASK_ID_RE.match(task_id or "") or not UUID_RE.match(session or ""):
        return None
    slug = projects.slug(cwd or "")
    if not slug:
        return None
    roots = []
    for tmpdir in (session_tmpdir(session), os.environ.get("CLAUDE_CODE_TMPDIR")):
        if tmpdir:
            roots.append(os.path.join(os.path.expanduser(tmpdir), f"claude-{os.getuid()}"))
    roots.append(f"/tmp/claude-{os.getuid()}")
    for root in roots:
        path = os.path.join(root, slug, session, "tasks", f"{task_id}.output")
        try:
            size = os.path.getsize(path)
        except OSError:
            continue
        with open(path, "rb") as f:
            if size > TASK_TAIL:
                f.seek(size - TASK_TAIL)
            data = f.read()
        text = data.decode("utf-8", "replace")
        return {"text": text, "cut": size > TASK_TAIL, "size": size}
    return None


def session_tmpdir(session):
    """Returns CLAUDE_CODE_TMPDIR of a live session, taken from its own process."""
    if not session:
        return None
    import archive  # noqa: PLC0415
    import ctx  # noqa: PLC0415
    files = []
    for root in archive.live_dirs():
        try:
            files.extend(glob.glob(os.path.join(root, "*.json")))
        except OSError:
            continue
    for path in sorted(files):
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        if data.get("sessionId") != session or not data.get("pid"):
            continue
        pid = data["pid"]
        start = ctx.proc_start(pid)
        if start is None or (data.get("procStart") and str(data["procStart"]) != start):
            continue
        try:
            with open(f"/proc/{pid}/environ", "rb") as f:
                raw = f.read()
        except OSError:
            return None
        for item in raw.split(b"\0"):
            if item.startswith(b"CLAUDE_CODE_TMPDIR="):
                value = item.split(b"=", 1)[1].decode("utf-8", "replace")
                return value or None
        return None
    return None


MAX_FILE = 64 * 1024

# MAX_WINDOW is the largest window of text one request is given, and only a
# reader that asks for it gets it: a page is drawn once it is read whole, and
# by the default window that would be a request for every 64 KB of it. The
# reply carries the text escaped for JSON, six bytes at most for one on disk,
# so a window this size stays under the reply cap of the service whatever
# the file holds.
MAX_WINDOW = 1024 * 1024

MAX_MEDIA = 4 * 1024 * 1024

IMAGE_MEDIA = {
    ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
    ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
    ".bmp": "image/bmp", ".ico": "image/x-icon", ".svg": "image/svg+xml",
    ".apng": "image/apng", ".jfif": "image/jpeg", ".tif": "image/tiff",
    ".tiff": "image/tiff", ".heic": "image/heic", ".heif": "image/heif",
}

PLAY_MEDIA = {
    ".mp4": ("video", "video/mp4"), ".m4v": ("video", "video/mp4"),
    ".webm": ("video", "video/webm"), ".ogv": ("video", "video/ogg"),
    ".mov": ("video", "video/quicktime"), ".mkv": ("video", "video/x-matroska"),
    ".mp3": ("audio", "audio/mpeg"), ".m4a": ("audio", "audio/mp4"),
    ".wav": ("audio", "audio/wav"), ".oga": ("audio", "audio/ogg"),
    ".ogg": ("audio", "audio/ogg"), ".opus": ("audio", "audio/ogg"),
    ".flac": ("audio", "audio/flac"), ".aac": ("audio", "audio/aac"),
    ".pdf": ("pdf", "application/pdf"),
}

EXEC_MAGIC = (b"\x7fELF", b"MZ")

EXEC_BITS = stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH

TICKED = re.compile(r"`([^`\n]{1,200})`")

LINE_SUFFIX = re.compile(r":\d+(?::\d+)?$")

PATHISH = re.compile(r"\.[A-Za-z0-9]{1,10}$")

MAX_PROBE = 40
MAX_FILES = 6


def media_of(name):
    """Returns the image type by extension, or None when it is not an image."""
    return IMAGE_MEDIA.get(os.path.splitext(name)[1].lower())


def as_is(name):
    """Returns the kind and type of a file that travels as bytes, or (None, None)."""
    ext = os.path.splitext(name)[1].lower()
    media = IMAGE_MEDIA.get(ext)
    if media:
        return "image", media
    kind, media = PLAY_MEDIA.get(ext, (None, None))
    return kind, media


def is_exec(real, st):
    """Reports whether the file is a program, by its mode bits or its header."""
    if st.st_mode & EXEC_BITS:
        return True
    try:
        with open(real, "rb") as f:
            head = f.read(4)
    except OSError:
        return True
    return head.startswith(EXEC_MAGIC)


def named_files(text, cwd):
    """Returns files named in a prompt: name, size and the path to read them by."""
    if not text or not cwd:
        return []
    out, seen = [], set()
    for token in TICKED.findall(text)[:MAX_PROBE]:
        raw = LINE_SUFFIX.sub("", token.strip())
        if not raw or raw in seen or " " in raw or "\t" in raw:
            continue
        seen.add(raw)
        if "/" not in raw and not PATHISH.search(raw):
            continue
        real = sesstate.opens(raw, cwd)
        if not real:
            continue
        try:
            st = os.stat(real)
        except OSError:
            continue
        if not stat.S_ISREG(st.st_mode):
            continue
        item = {"path": raw, "name": os.path.basename(real), "size": st.st_size}
        media = media_of(real)
        if media:
            item["media"] = media
        elif is_binary(real):
            continue
        out.append(item)
        if len(out) >= MAX_FILES:
            break
    return out


def is_binary(real):
    """Reports whether the file is binary, judging by its first kilobyte."""
    try:
        with open(real, "rb") as f:
            return b"\x00" in f.read(1024)
    except OSError:
        return True


def attach_files(items, cwd):
    """Attaches to every answer the files it names and the files it changed.

    A file changed by a call gets in even when the answer says nothing about it:
    the list is there to show what the turn did, not what it talked about. What
    a command changes stays out — nobody but git knows that.
    """
    if not cwd:
        return items
    edited = []
    for item in items:
        if item.get("role") == "tools":
            edited += [c["edited"] for c in item.get("calls", []) if c.get("edited")]
            continue
        if item.get("role") != "ai":
            continue
        found = named_files(item.get("text"), cwd) + disk_files(edited, cwd)
        edited = []
        if found:
            item["files"] = dedupe_files(found)
    return items


def dedupe_files(files):
    """Returns the list without repeats, keeping the order of the first mention."""
    out, seen = [], set()
    for item in files:
        if item["path"] in seen:
            continue
        seen.add(item["path"])
        out.append(item)
    return out[:MAX_FILES]


def disk_files(paths, cwd):
    """Returns the ones of the paths that are readable regular files the reader opens."""
    out = []
    for raw in paths:
        real = sesstate.opens(raw, cwd)
        if not real:
            continue
        try:
            st = os.stat(real)
        except OSError:
            continue
        if not stat.S_ISREG(st.st_mode):
            continue
        item = {"path": raw, "name": os.path.basename(real), "size": st.st_size}
        media = media_of(real)
        if media:
            item["media"] = media
        elif is_binary(real):
            continue
        out.append(item)
    return out


MAX_RAW = 1024 * 1024

# FIND_SKIP are the directories the search for a file named without its
# directory does not go into: history and installed code, never what a
# session talks about.
FIND_SKIP = frozenset({".git", "node_modules", "__pycache__", ".venv", "venv", ".cache"})

# FIND_BUDGET caps the entries the search looks at. A project is a few
# thousand of them; a session in the home directory runs over half a million,
# six seconds of walk for every range of a download.
FIND_BUDGET = 50000


def located(path_in_repo, cwd):
    """Returns the real path of a file the reader opens, or None.

    A session names a file it has just written by its name alone, and the file
    lies directories down from where the conversation runs: a relative path
    that is not there is looked for below it.
    """
    real = sesstate.opens(path_in_repo, cwd)
    if not real or os.path.exists(real):
        return real
    raw = os.path.expanduser(str(path_in_repo).strip())
    if os.path.isabs(raw):
        return real
    return find_below(raw, cwd) or real


def find_below(name, cwd):
    """Returns the freshest file below the directory whose path ends with the name, or None.

    Of several files under one name the session most likely speaks of the
    one it changed last. A path that climbs out of the directory is not
    looked for, and a secret is never found.
    """
    tail = _tail(name)
    if not tail or not cwd:
        return None
    base = os.path.basename(tail)
    best, best_at, seen = None, -1.0, 0
    for root, dirs, files in os.walk(cwd):
        dirs[:] = [d for d in dirs if d not in FIND_SKIP]
        seen += len(dirs) + len(files)
        full = os.path.join(root, base)
        if base in files and full.endswith(os.sep + tail):
            real = sesstate.opens(full, cwd)
            try:
                st = os.stat(real) if real else None
            except OSError:
                st = None
            if st and stat.S_ISREG(st.st_mode) and st.st_mtime > best_at:
                best, best_at = real, st.st_mtime
        if seen > FIND_BUDGET:
            break
    return best


# MENTION_PATH is an absolute path as a transcript holds it, in a call or in
# a command; MENTION_CD is the directory a command went to before it named a
# file by its name alone.
MENTION_PATH = re.compile(r"~?/[^\s\"'`\\|;&<>(),:]+")
MENTION_CD = re.compile(r"\bcd\s+(~?/[^\s\"'`\\|;&<>(),:]+)")


def mentioned(transcript, name):
    """Returns the file the conversation last reached under the name, or None.

    A file outside the directory of the conversation is named by its name
    alone once the session has been at it: by a call with the whole path, or
    by a command that went to its directory first.
    """
    tail = _tail(name)
    if not tail:
        return None
    needle = tail.encode("utf-8")
    places = []
    try:
        with open(transcript, "rb") as f:
            for raw in f:
                if needle not in raw:
                    continue
                line = raw.decode("utf-8", "replace")
                places += [p for p in MENTION_PATH.findall(line) if p.endswith("/" + tail)]
                places += [os.path.join(d, tail) for d in MENTION_CD.findall(line)]
    except OSError:
        return None
    for place in reversed(places):
        real = sesstate.opens(place, None)
        if real and os.path.isfile(real):
            return real
    return None


def _tail(name):
    """Returns the name as the end of a path to match others by, None for one that climbs."""
    tail = os.path.normpath(str(name or "").strip())
    if tail in (".", "..") or tail.startswith(".." + os.sep) or os.path.isabs(tail):
        return None
    return tail


def read_raw(path_in_repo, cwd, offset=0, limit=MAX_RAW):
    """Returns a range of bytes of a project file, or None when it cannot be read.

    Every kind of file travels this way, an executable and a binary included:
    the range is asked for to save the file on a device, not to show it, and
    there the whole point is the bytes as they lie on disk. The cap is on one
    range, not on the file: the caller walks it by the offsets it gets back.
    """
    real = located(path_in_repo, cwd)
    if not real:
        return None
    try:
        st = os.stat(real)
        if not stat.S_ISREG(st.st_mode):
            return None
        size = st.st_size
        start = max(0, min(int(offset or 0), size))
        want = max(1, min(int(limit or MAX_RAW), MAX_RAW))
        with open(real, "rb") as f:
            f.seek(start)
            data = f.read(want)
    except (OSError, ValueError, TypeError):
        return None
    out = {"kind": "raw", "size": size, "name": os.path.basename(real),
           "offset": start, "data": base64.b64encode(data).decode("ascii"),
           "mtime": changed_at(st)}
    _, media = as_is(real)
    if media:
        out["media"] = media
    end = start + len(data)
    if end < size:
        out["next"] = end
    return out


def changed_at(st):
    """Returns when the file last changed, in the clock of this host.

    The panel names the copy a device saves by this time, and the name has
    to say what a directory listing here says: the stamp carries the offset
    of the host, since the panel may keep a clock of its own.
    """
    at = datetime.datetime.fromtimestamp(st.st_mtime).astimezone()
    return at.isoformat(timespec="seconds")


def trim_utf8(data):
    """Returns the chunk without a character torn in half at its end."""
    for back in range(1, min(4, len(data)) + 1):
        byte = data[-back]
        if byte < 0x80:
            return data
        if byte >= 0xC0:
            need = 2 if byte < 0xE0 else (3 if byte < 0xF0 else 4)
            return data if back >= need else data[:-back]
    return data


def read_file(path_in_repo, cwd, offset=0, limit=MAX_FILE):
    """Returns a chunk of a project file by a path from the feed, or None when it cannot be read.

    A chunk of text is MAX_FILE long unless the reader asks for more, and
    never longer than MAX_WINDOW.
    """
    real = located(path_in_repo, cwd)
    if not real:
        return None
    try:
        st = os.stat(real)
        if not stat.S_ISREG(st.st_mode):
            return None
        size = st.st_size
        if is_exec(real, st):
            return {"kind": "exec", "size": size, "name": os.path.basename(real),
                    "mode": stat.filemode(st.st_mode)}
        kind, media = as_is(real)
        if kind:
            if size > MAX_MEDIA:
                return {"kind": kind, "media": media, "size": size,
                        "name": os.path.basename(real), "tooBig": True}
            with open(real, "rb") as f:
                raw = f.read(MAX_MEDIA)
            return {"kind": kind, "media": media, "size": size,
                    "name": os.path.basename(real),
                    "data": base64.b64encode(raw).decode("ascii")}
        start = max(0, min(int(offset or 0), size))
        want = max(1, min(int(limit or MAX_FILE), MAX_WINDOW))
        with open(real, "rb") as f:
            f.seek(start)
            data = f.read(want)
    except (OSError, ValueError, TypeError):
        return None
    name = os.path.basename(real)
    if b"\x00" in data[:1024]:
        return {"kind": "binary", "binary": True, "size": size, "name": name}
    end = start + len(data)
    if end < size:
        nl = data.rfind(b"\n")
        data = data[:nl + 1] if nl > len(data) // 2 else trim_utf8(data)
        end = start + len(data)
    out = {"kind": "text", "text": data.decode("utf-8", "replace"),
           "size": size, "name": name, "offset": start,
           "cut": end < size}
    if end < size:
        out["next"] = end
    return out
