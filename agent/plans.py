"""The plan of a session: the list of steps the model keeps through the panel's plan tool.

The executor serves the tool to every session the panel starts and writes one
file a place: the config directory of the account and the directory the
session runs in, so a session started again there finds the plan it left.
The collector reads it into the row of a session of the place and into the
state of its conversation, in the small shape the screens take: every step
with its status and, for a step at work or done, since when.
"""
import hashlib
import json
import os
import posixpath

STATUSES = ("pending", "active", "done", "dropped")


def plans_dir():
    """Returns where the executor keeps the plans: the owner's state."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.expanduser("~/.local/state")
    return os.path.join(base, "aacpanel", "plans")


def _clean(path):
    """Returns a path in the form the executor names the file by, or None when it is not absolute.

    The form is Go's filepath.Clean: posixpath keeps a leading double slash,
    Go does not.
    """
    if not isinstance(path, str) or not path.startswith("/"):
        return None
    path = posixpath.normpath(path)
    return "/" + path.lstrip("/") if path.startswith("//") else path


def name_of(config_dir, cwd):
    """Returns the file name of the plan of a place, or None for a place that is not one.

    The name is a hash of the two paths, the one the executor computes.
    """
    config_dir, cwd = _clean(config_dir), _clean(cwd)
    if config_dir is None or cwd is None:
        return None
    digest = hashlib.sha256(os.fsencode(config_dir) + b"\0" + os.fsencode(cwd)).hexdigest()
    return digest[:32] + ".json"


def _text(value):
    return value.strip() if isinstance(value, str) else ""


def of(config_dir, cwd, sid=None):
    """Returns the plan of a place for the screens, or None when it keeps none.

    With a conversation named, the plan is its only while that conversation
    sent it last: the feed of a conversation that is over shows the plan it
    left, and not one a later session of the place keeps.
    """
    name = name_of(config_dir, cwd)
    if name is None:
        return None
    try:
        with open(os.path.join(plans_dir(), name), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict) or data.get("configDir") != _clean(config_dir) or data.get("dir") != _clean(cwd):
        return None
    if sid is not None and data.get("sessionId") != sid:
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
    plan = {"items": items, "at": _text(data.get("at"))}
    note = _text(data.get("note"))
    if note:
        plan["note"] = note
    return plan
