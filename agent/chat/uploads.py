"""Files the panel sent into a session: found by the message that names them.

The panel hands a file to a session through the executor, which keeps it in a
directory of its own and types its path into the message, a line for each file.
The transcript holds the path and nothing else, so the file is looked up on
disk here: the collector reads the owner's home, the service facing the
internet reads nothing of the host, and the executor does not read transcripts.
A picture is drawn over the message; any other file stands under it as a file.

A picture the feed cannot show from the file itself — one over MAX_MEDIA, which
does not fit in one answer of the socket, or a HEIC that not every browser
draws — is shown by the smaller JPEG the phone drew when it sent it. The
executor keeps that copy in PREVIEWS under the name of the file and .jpg, and
never names it in the message.
"""
import base64
import os
import re
import stat

from .disk import MAX_MEDIA

# The pictures every browser draws in an <img>. SVG stays out: it is a page,
# and one opened by its address runs its script with the panel's cookies.
UPLOAD_MEDIA = {
    ".png": "image/png", ".apng": "image/apng", ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg", ".jfif": "image/jpeg", ".gif": "image/gif",
    ".webp": "image/webp", ".avif": "image/avif", ".bmp": "image/bmp",
}

# The pictures a phone camera takes that only some browsers draw: they are
# shown by their copy alone.
UPLOAD_HEIF = {".heic", ".heif"}

PREVIEWS = "previews"

# The executor puts the time a file came and three random bytes before the
# name it was sent under, so that two files of one name never meet in the
# directory. The feed names the file by what follows.
STAMPED = re.compile(r"[0-9]{8}-[0-9]{6}-[0-9a-f]{6}-(.+)", re.S)


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
    """Returns what is served for a sent picture by its name — the real path, the
    type, the size and whether it is the copy — or None.

    The name is the name of a file directly in the directory: a path, a
    hidden file or a link out of the directory is refused. A picture over
    MAX_MEDIA — it would not fit in one answer of the socket — and a HEIC
    are served by their copy, held to the same rules inside PREVIEWS; without
    one there is nothing to serve.
    """
    found = kept_file(name)
    if not found:
        return None
    ext = os.path.splitext(name)[1].lower()
    media = UPLOAD_MEDIA.get(ext)
    if not media and ext not in UPLOAD_HEIF:
        return None
    if media and found[1] <= MAX_MEDIA:
        return found[0], media, found[1], False
    # A shelf that is a link fails the same test: the real path of a copy
    # through it never lies directly in the shelf as named.
    copy = _plain_file(os.path.join(os.path.dirname(found[0]), PREVIEWS), name + ".jpg")
    if not copy or copy[1] > MAX_MEDIA:
        return None
    return copy[0], "image/jpeg", copy[1], True


def kept_file(name):
    """Returns the real path and size of a file the panel sent, by its name, or None.

    The name is the name of a file directly in the directory: a path, a
    hidden file, a link out of the directory and an empty file are refused.
    """
    if not isinstance(name, str) or not name or name != os.path.basename(name) \
            or name.startswith(".") or "\x00" in name:
        return None
    return _plain_file(os.path.realpath(files_dir()), name)


def sent_as(name):
    """Returns the name a file the panel sent went under, without what the executor put before it."""
    found = STAMPED.fullmatch(name)
    return found.group(1) if found else name


def uploads_home(path):
    """Returns the directory of the files the panel sent when the path names one directly in it, or None."""
    home = files_dir()
    if isinstance(path, str) and os.path.isabs(path) and os.path.dirname(os.path.normpath(path)) == home:
        return home
    return None


def _plain_file(home, name):
    """Returns the real path and size of a regular file, not empty, directly in home, or None."""
    real = os.path.realpath(os.path.join(home, name))
    if os.path.dirname(real) != home:
        return None
    try:
        st = os.stat(real)
    except OSError:
        return None
    if not stat.S_ISREG(st.st_mode) or st.st_size <= 0:
        return None
    return real, st.st_size


def uploads_in(text):
    """Returns the files the panel sent that a message names, a path a line, as the executor writes them.

    The pictures come as shots, drawn over the message. Every other file, and
    a picture the feed has no way to draw, comes as a file the feed opens by
    its path, named by the name it went under.
    """
    if not text:
        return [], []
    home = files_dir()
    shots, files, seen = [], [], set()
    for line in text.split("\n"):
        path = line.strip()
        if not path.startswith("/") or os.path.dirname(path) != home:
            continue
        name = os.path.basename(path)
        if name in seen:
            continue
        found = kept_file(name)
        if not found:
            continue
        seen.add(name)
        drawn = picture(name)
        if not drawn:
            files.append({"path": path, "name": sent_as(name), "size": found[1]})
            continue
        _, media, size, copy = drawn
        shot = {"upload": name, "path": path, "media": media, "bytes": size}
        if copy:
            shot["preview"] = True
        shots.append(shot)
    return shots, files


def attach_uploads(items):
    """Hangs on every message of the person the files the panel sent that it names.

    It is read anew with every window: a file swept from the directory goes
    back to being the path it was typed as.
    """
    for item in items:
        if item.get("role") != "me":
            continue
        shots, files = uploads_in(item.get("text"))
        if shots:
            item["shots"] = shots
        if files:
            item["files"] = files
    return items


def upload(name):
    """Returns a sent picture as its media type and base64 bytes, or None."""
    found = picture(name)
    if not found:
        return None
    real, media, _, _ = found
    with open(real, "rb") as f:
        data = f.read(MAX_MEDIA + 1)
    if not data or len(data) > MAX_MEDIA:
        return None
    return {"media": media, "data": base64.b64encode(data).decode("ascii")}
