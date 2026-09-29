"""The checklist of a session: the list of steps the model keeps through the panel's checklist tool.

The executor serves the tool to every session the panel starts and writes one
file a session: told by its place — the config directory of the account and
the directory the session runs in — and by its name, so the sessions of one
directory keep checklists of their own, and a session started again under its
name finds the checklist it left. A session without a name is told by its
place alone. The collector reads the file into the row of the session and into
the state of its conversation, in the small shape the screens take: every step
with its status and, for a step at work or done, since when.
"""
import hashlib
import json
import os
import posixpath

STATUSES = ("pending", "active", "done", "dropped")


def checklists_dir():
    """Returns where the executor keeps the checklists: the owner's state."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.expanduser("~/.local/state")
    return os.path.join(base, "aacpanel", "checklists")


def _clean(path):
    """Returns a path in the form the executor names the file by, or None when it is not absolute.

    The form is Go's filepath.Clean: posixpath keeps a leading double slash,
    Go does not.
    """
    if not isinstance(path, str) or not path.startswith("/"):
        return None
    path = posixpath.normpath(path)
    return "/" + path.lstrip("/") if path.startswith("//") else path


def _name(name):
    return name if isinstance(name, str) and name else ""


def name_of(config_dir, cwd, name=None):
    """Returns the file name of the checklist of a session, or None for a place that is not one.

    The name is a hash of the two paths and the name of the session, or of the
    paths alone for a session without a name: the one the executor computes.
    """
    config_dir, cwd = _clean(config_dir), _clean(cwd)
    if config_dir is None or cwd is None:
        return None
    key = os.fsencode(config_dir) + b"\0" + os.fsencode(cwd)
    if _name(name):
        key += b"\0" + os.fsencode(name)
    return hashlib.sha256(key).hexdigest()[:32] + ".json"


def _text(value):
    return value.strip() if isinstance(value, str) else ""


def _load(path):
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def _read(config_dir, cwd, name):
    """Returns the file of the checklist of a session as it lies, or None when it keeps none."""
    file = name_of(config_dir, cwd, name)
    if file is None:
        return None
    data = _load(os.path.join(checklists_dir(), file))
    if data is None or not _owns(data, config_dir, cwd) or _name(data.get("name")) != _name(name):
        return None
    return data


def _owns(data, config_dir, cwd):
    return data.get("configDir") == _clean(config_dir) and data.get("dir") == _clean(cwd)


def of(config_dir, cwd, name=None, sid=None):
    """Returns the checklist of a live session for the screens, or None when it keeps none.

    A named session without a file of its own shows the checklist of its place
    — the one a server that does not tell the sessions of a place apart keeps
    writing — only while its conversation `sid` sent it last: which session
    sent the checklist of another conversation is not to be told.
    """
    data = _read(config_dir, cwd, name)
    if data is None and _name(name) and sid:
        placed = _read(config_dir, cwd, None)
        data = placed if placed is not None and placed.get("sessionId") == sid else None
    return _shape(data)


def of_conversation(config_dir, cwd, sid):
    """Returns the checklist a conversation that is over left, or None.

    It is the checklist of its place — under whatever name — that the
    conversation sent last: once a later conversation of the session sends the
    checklist, the one before has none.
    """
    if name_of(config_dir, cwd) is None or not sid:
        return None
    root = checklists_dir()
    try:
        files = os.listdir(root)
    except OSError:
        return None
    found = None
    for file in files:
        if not file.endswith(".json"):
            continue
        data = _load(os.path.join(root, file))
        if data is None or not _owns(data, config_dir, cwd) or data.get("sessionId") != sid:
            continue
        if file != name_of(config_dir, cwd, data.get("name")):
            continue
        if found is None or _text(data.get("at")) > _text(found.get("at")):
            found = data
    return _shape(found)


def _shape(data):
    """Returns a checklist as it lies in the shape the screens take, or None when no step of it is drawn."""
    if data is None:
        return None
    items = []
    for item in data.get("items") if isinstance(data.get("items"), list) else []:
        if not isinstance(item, dict):
            continue
        text, status = _text(item.get("text")), item.get("status")
        if not text or status not in STATUSES:
            continue
        step = {"text": text, "status": status}
        since = _text(item.get("since"))
        if since:
            step["since"] = since
        items.append(step)
    if not items:
        return None
    checklist = {"items": items, "at": _text(data.get("at"))}
    note = _text(data.get("note"))
    if note:
        checklist["note"] = note
    return checklist
