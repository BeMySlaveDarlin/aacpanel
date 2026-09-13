"""What the session has made: published artifacts, written documents, files sent to the human."""

import os
import re

from .limits import _prune, _short

ARTIFACT_PUBLISH = ("", "publish")

ARTIFACT_URL_RE = re.compile(r"Published\s+\S+\s+at\s+(https://\S+)")

DOC_TOOLS = ("Write", "Edit", "MultiEdit")

DOC_EXT = (".md", ".markdown", ".txt", ".rst", ".adoc", ".org")

SENT_TOOL = "SendUserFile"


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


def sent_files(result):
    """Returns the files a delivery handed to the human: path, name, size and type.

    The call names the files it wants sent; only the answer says which ones
    went. A delivery that failed carries no attachments, and a file it names
    without a path is nothing to open.
    """
    if not isinstance(result, dict) or not isinstance(result.get("attachments"), list):
        return []
    out = []
    for item in result["attachments"]:
        if not isinstance(item, dict):
            continue
        path = str(item.get("path") or "").strip()
        if not path:
            continue
        entry = {"path": path, "file": os.path.basename(path)}
        size = item.get("size")
        if isinstance(size, int) and not isinstance(size, bool) and size > 0:
            entry["size"] = size
        media = " ".join(str(item.get("media_type") or "").split())
        if media:
            entry["media"] = media
        out.append(entry)
    return out


def _sent(state, result, at):
    for entry in sent_files(result):
        # The reader opens a file only inside the directory of the conversation,
        # and a session may send one from anywhere. The row stays, since the
        # file did reach the human, but it is marked: a tap would end in a refusal.
        if inside(entry["path"], state.cwd) is None:
            entry["outside"] = True
        was = state.sent.get(entry["path"]) or {}
        state.sent[entry["path"]] = {
            **entry,
            "at": at,
            "count": int(was.get("count") or 0) + 1,
        }
    _prune(state.sent)
