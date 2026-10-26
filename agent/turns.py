"""Whose move follows a conversation's last turn, until the next human prompt.

The executor replaces the declaration whole. Its place and session id must
both match: a new conversation under the same name cannot inherit a request
for the person, and a notification from background work cannot answer one.
"""
import datetime
import json
import os

import sessionfile


def reconcile(rows):
    """Marks a self declaration stalled when the finished snapshot names no wake.

    A child can stand before or after its parent, so declarations marked on
    the way are cleared when a later row proves a child is still expected.
    Only the child's own declaration matters: a child stalled on its own is
    still a live child the parent awaits, without walking its descendants.
    """
    parents = set()
    waiting = {}
    for row in rows:
        turn = row.get("turn") or {}
        parent = (row.get("parent") or {}).get("session")
        if parent and (row.get("status") != "idle" or turn.get("next") != "done"):
            parents.add(parent)
            for declaration in waiting.pop(parent, []):
                declaration.pop("stalled", None)
        if not turn or row.get("agent") == "codex":
            continue
        turn.pop("stalled", None)
        if turn.get("next") != "self" or row.get("status") in ("busy", "waiting"):
            continue
        work = row.get("work") or {}
        if any(work.get(key, 0) > 0 for key in ("tasks", "agents", "wakes", "workflows", "observers", "alarms")):
            continue
        name = row.get("session")
        if name not in parents:
            turn["stalled"] = True
            waiting.setdefault(name, []).append(turn)


def of(config_dir, cwd, name=None, *, sid, prompted):
    """Returns the current declaration for a Claude row, or None when it has expired."""
    file = sessionfile.file_name(config_dir, cwd, name)
    if file is None or not sid:
        return None
    try:
        with open(os.path.join(sessionfile.state_dir("turns"), file), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if (not isinstance(data, dict) or data.get("configDir") != sessionfile.clean(config_dir)
            or data.get("dir") != sessionfile.clean(cwd)
            or sessionfile.name(data.get("name")) != sessionfile.name(name)
            or data.get("sessionId") != sid):
        return None
    at, next_move, what = data.get("at"), data.get("next"), data.get("what", "")
    if (not isinstance(at, int) or isinstance(at, bool) or at <= 0
            or next_move not in ("owner", "self", "done") or not isinstance(what, str)
            or len(what) > 300 or (next_move == "owner" and not what.strip())):
        return None
    if prompted:
        try:
            since = datetime.datetime.fromisoformat(prompted.replace("Z", "+00:00")).timestamp() * 1000
        except (ValueError, TypeError, OverflowError):
            return None
        if at <= since:
            return None
    return {"next": next_move, "what": what, "at": at}
