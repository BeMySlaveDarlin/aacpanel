"""What lies on disk: the transcripts of every contour and the order they are handed out in."""

import glob
import hashlib
import os

import contours

PROJECTS = os.environ.get("AACP_CLAUDE_PROJECTS")

HEAD_BYTES = 4096

_MISSING = object()


def roots():
    """Returns pairs of contour and conversation directory, the personal one first."""
    if PROJECTS:
        return [("", PROJECTS)]
    return [(name, os.path.join(d, "projects")) for name, d in contours.profiles()]


def scan_list(pairs=None, heads=False):
    """Returns every transcript of every contour: path, contour, inode and size."""
    out = []
    for contour, root in (pairs if pairs is not None else roots()):
        found = glob.glob(os.path.join(root, "*", "*.jsonl"))
        found += glob.glob(os.path.join(root, "*", "*", "subagents", "*.jsonl"))
        for path in found:
            try:
                st = os.stat(path)
            except OSError:
                continue
            row = {"path": path, "contour": contour,
                   "inode": st.st_ino, "size": st.st_size}
            if heads:
                row["head"] = head_sum(path)
                row["first"] = first_stamp(path)
            out.append(row)
    return out


def first_stamp(path):
    """Returns the time of the first record of a file, empty when there is none."""
    try:
        with open(path, "rb") as f:
            head = f.read(HEAD_BYTES)
    except OSError:
        return ""
    i = head.find(b'"timestamp"')
    if i < 0:
        return ""
    i = head.find(b":", i + len(b'"timestamp"'))
    if i < 0:
        return ""
    i = head.find(b'"', i)
    if i < 0:
        return ""
    j = head.find(b'"', i + 1)
    if j < 0:
        return ""
    return head[i + 1:j].decode("utf-8", "replace")


def order_by_first_record(items):
    """Returns the same list ordered by the time of the first record."""
    def key(it):
        stamp = it.get("first", _MISSING)
        if stamp is _MISSING:
            stamp = first_stamp(it["path"])
        return (not stamp, stamp)

    return sorted(items, key=key)


def head_sum(path, size=HEAD_BYTES):
    """Returns the hash of the file head as bytes."""
    try:
        with open(path, "rb") as f:
            return hashlib.sha256(f.read(size)).digest()
    except OSError:
        return b""
