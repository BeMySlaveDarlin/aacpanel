"""Pictures the panel sent into a session: found by the message that names them.

The panel hands a file to a session through the executor, which keeps it in a
directory of its own and types its path into the message, a line for each file.
The transcript holds the path and nothing else, so the picture is looked up on
disk here: the collector reads the owner's home, the service facing the
internet reads nothing of the host, and the executor does not read transcripts.
"""
import base64
import os
import stat

from .disk import MAX_MEDIA

# The pictures every browser draws in an <img>. SVG stays out: it is a page,
# and one opened by its address runs its script with the panel's cookies.
UPLOAD_MEDIA = {
    ".png": "image/png", ".apng": "image/apng", ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg", ".jfif": "image/jpeg", ".gif": "image/gif",
    ".webp": "image/webp", ".avif": "image/avif", ".bmp": "image/bmp",
}


def files_dir():
    """Returns the directory the executor keeps the files it was sent in.

    It is found the way the executor finds it, by the same variables in the
    same order. The collector is a system unit and does not see what is set
    for the user's own manager alone: AACP_FILES goes into the host
    description, which both units read.
    """
    own = os.environ.get("AACP_FILES")
    if own:
        return os.path.normpath(own)
    data = os.environ.get("XDG_DATA_HOME")
    if data:
        return os.path.normpath(os.path.join(data, "aacpanel-exec", "files"))
    home = os.environ.get("HOME")
    if not home:
        return os.path.join(os.environ.get("TMPDIR") or "/tmp", f"aacpanel-exec-files-{os.getuid()}")
    return os.path.normpath(os.path.join(home, ".local", "share", "aacpanel-exec", "files"))


def picture(name):
    """Returns the real path, type and size of a sent picture by its name, or None.

    The name is the name of a file directly in the directory: a path, a
    hidden file or a link out of the directory is refused. So is a picture
    over MAX_MEDIA — it would not fit in one answer of the socket.
    """
    if not isinstance(name, str) or not name or name != os.path.basename(name) \
            or name.startswith(".") or "\x00" in name:
        return None
    media = UPLOAD_MEDIA.get(os.path.splitext(name)[1].lower())
    if not media:
        return None
    home = os.path.realpath(files_dir())
    real = os.path.realpath(os.path.join(home, name))
    if os.path.dirname(real) != home:
        return None
    try:
        st = os.stat(real)
    except OSError:
        return None
    if not stat.S_ISREG(st.st_mode) or not 0 < st.st_size <= MAX_MEDIA:
        return None
    return real, media, st.st_size


def uploads_in(text):
    """Returns the sent pictures a message names, a path a line, as the executor writes them."""
    if not text:
        return []
    home = files_dir()
    out, seen = [], set()
    for line in text.split("\n"):
        path = line.strip()
        if not path.startswith("/") or os.path.dirname(path) != home:
            continue
        name = os.path.basename(path)
        if name in seen:
            continue
        found = picture(name)
        if not found:
            continue
        seen.add(name)
        _, media, size = found
        out.append({"upload": name, "path": path, "media": media, "bytes": size})
    return out


def attach_uploads(items):
    """Hangs on every message of the person the sent pictures it names.

    It is read anew with every window: a picture swept from the directory
    goes back to being the path it was typed as.
    """
    for item in items:
        if item.get("role") != "me":
            continue
        shots = uploads_in(item.get("text"))
        if shots:
            item["shots"] = shots
    return items


def upload(name):
    """Returns a sent picture as its media type and base64 bytes, or None."""
    found = picture(name)
    if not found:
        return None
    real, media, _ = found
    with open(real, "rb") as f:
        data = f.read(MAX_MEDIA + 1)
    if not data or len(data) > MAX_MEDIA:
        return None
    return {"media": media, "data": base64.b64encode(data).decode("ascii")}
