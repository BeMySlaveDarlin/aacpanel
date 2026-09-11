"""What is read from disk on request from the feed: task output and project files."""
import base64
import glob
import json
import os
import re
import stat

import sesstate

from .locate import UUID_RE


TASK_ID_RE = re.compile(r"^[A-Za-z0-9]{1,32}$")

TASK_TAIL = 24 * 1024


def task_output(session, cwd, task_id):
    """Returns the tail of a background task output, or None when there is no file."""
    if not TASK_ID_RE.match(task_id or "") or not UUID_RE.match(session or ""):
        return None
    slug = (cwd or "").replace("/", "-")
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
        real = sesstate.inside(raw, cwd)
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
    """Attaches files named by model answers to the items of a ready feed window."""
    if not cwd:
        return items
    for item in items:
        if item.get("role") != "ai":
            continue
        found = named_files(item.get("text"), cwd)
        if found:
            item["files"] = found
    return items


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
    """Returns a chunk of a project file by a path from the feed, or None when it cannot be read."""
    real = sesstate.inside(path_in_repo, cwd)
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
        want = max(1, min(int(limit or MAX_FILE), MAX_FILE))
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
