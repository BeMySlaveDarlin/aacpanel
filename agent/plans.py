"""The plan of a session: the list of steps the model keeps through the panel's plan tool.

The executor serves the tool to every session the panel starts and writes one
file a conversation. The collector reads it into the row of the session and
into the state of the conversation, in the small shape the screens take:
every step with its status and, for a step at work or done, since when.
"""
import json
import os

STATUSES = ("pending", "active", "done", "dropped")


def plans_dir():
    """Returns where the executor keeps the plans: the owner's state."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.expanduser("~/.local/state")
    return os.path.join(base, "aacpanel", "plans")


def _named(sid):
    return isinstance(sid, str) and bool(sid) and "/" not in sid and not sid.startswith(".")


def _text(value):
    return value.strip() if isinstance(value, str) else ""


def of(sid):
    """Returns the plan of a conversation for the screens, or None when it keeps none."""
    if not _named(sid):
        return None
    try:
        with open(os.path.join(plans_dir(), sid + ".json"), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict) or data.get("sessionId") != sid:
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
