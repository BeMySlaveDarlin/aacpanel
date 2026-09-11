"""What the session has made: published artifacts and written documents."""

import os
import re

from .limits import _prune, _short

ARTIFACT_PUBLISH = ("", "publish")

ARTIFACT_URL_RE = re.compile(r"Published\s+\S+\s+at\s+(https://\S+)")

DOC_TOOLS = ("Write", "Edit", "MultiEdit")

DOC_EXT = (".md", ".markdown", ".txt", ".rst", ".adoc", ".org")


def result_text(block):
    """Returns the answer text of a tool call, stored either as a string or as blocks."""
    body = block.get("content")
    if isinstance(body, str):
        return body
    if isinstance(body, list):
        return " ".join(x.get("text", "") for x in body if isinstance(x, dict))
    return ""


def artifact_fields(data):
    """Returns what an artifact is made of: path, file name, title, description, icon."""
    if not isinstance(data, dict):
        return None
    if str(data.get("action") or "").strip() not in ARTIFACT_PUBLISH:
        return None
    path = str(data.get("file_path") or "").strip()
    name = os.path.basename(path)
    title = _short(data.get("title"))
    if not title and not name:
        return None
    out = {"path": path, "file": name}
    if title:
        out["title"] = title
    for key, field in (("description", "desc"), ("label", "label"), ("note", "note")):
        value = _short(data.get(key))
        if value:
            out[field] = value
    icon = " ".join(str(data.get("favicon") or "").split())[:8]
    if icon:
        out["icon"] = icon
    return out


def inside(path_in_repo, cwd):
    """Returns the absolute path inside the session directory, or None when it lies outside."""
    if not cwd or not path_in_repo:
        return None
    raw = os.path.expanduser(str(path_in_repo).strip())
    full = raw if os.path.isabs(raw) else os.path.join(cwd, raw)
    try:
        real = os.path.realpath(full)
        root = os.path.realpath(cwd)
    except OSError:
        return None
    try:
        if os.path.commonpath([real, root]) != root:
            return None
    except ValueError:
        return None
    return real


def _artifact(state, data, use, at):
    fields = artifact_fields(data)
    if not fields:
        return
    key = fields["path"] or fields.get("title") or ""
    if not key:
        return
    was = state.arts.get(key) or {}
    art = dict(was, **fields)
    art["at"] = at
    art["count"] = int(was.get("count") or 0) + 1
    state.arts[key] = art
    if use:
        state.pending[use] = {"kind": "art", "key": key}
    _prune(state.arts)


def _document(state, data, at):
    path = str(data.get("file_path") or "").strip()
    if not path or not path.lower().endswith(DOC_EXT):
        return
    real = inside(path, state.cwd)
    if not real:
        return
    was = state.docs.get(real) or {}
    where = os.path.relpath(os.path.dirname(real), os.path.realpath(state.cwd))
    state.docs[real] = {
        "path": real,
        "file": os.path.basename(real),
        "dir": "" if where == "." else where,
        "at": at,
        "count": int(was.get("count") or 0) + 1,
    }
    _prune(state.docs)
