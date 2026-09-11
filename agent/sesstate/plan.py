"""Session plan: the task_reminder attachment and the TaskCreate/TaskUpdate calls."""

import re

from .limits import _short

TASK_NUMBER_RE = re.compile(r"[Tt]ask #(\d{1,4})\b")

PLAN_DROP = "deleted"
PLAN_STATES = ("pending", "in_progress", "completed")


def _plan_of(attachment):
    out = []
    for item in attachment.get("content") or []:
        if not isinstance(item, dict):
            continue
        status = item.get("status") or "pending"
        text = item.get("activeForm") if status == "in_progress" else item.get("subject")
        text = _short(text or item.get("subject"))
        if text:
            out.append({"text": text, "status": status})
    return out


def plan_created(data):
    """Returns what to remember about a TaskCreate until the answer brings its number."""
    return {
        "subject": _short(data.get("subject")),
        "active": _short(data.get("activeForm")),
    }


def plan_number(text):
    """Returns the item number from a TaskCreate answer, empty when it is not one."""
    found = TASK_NUMBER_RE.search(text or "")
    return found.group(1) if found else ""


def plan_add(state, number, started, at):
    """Adds an item to the plan, in order of appearance."""
    if not number or not (started.get("subject") or started.get("active")):
        return
    if number in state.plan_items:
        return
    state.plan_items[number] = {
        "subject": started.get("subject", ""),
        "active": started.get("active", ""),
        "status": "pending",
        "at": at,
        "n": len(state.plan_items),
    }


def plan_update(state, data):
    """Applies a TaskUpdate: the state change of one plan item."""
    number = str(data.get("taskId") or "").strip()
    if not number:
        return
    status = data.get("status")
    if status == PLAN_DROP:
        state.plan_items.pop(number, None)
        return
    item = state.plan_items.get(number)
    if item is None:
        return
    if status in PLAN_STATES:
        item["status"] = status
    if data.get("description"):
        item["subject"] = _short(data["description"])


def plan_rows(items):
    """Returns the plan for the outside: text and state, in order of appearance."""
    out = []
    for item in sorted(items.values(), key=lambda i: i["n"]):
        text = item["active"] if item["status"] == "in_progress" and item["active"] else item["subject"]
        text = text or item["subject"] or item["active"]
        if text:
            out.append({"text": text, "status": item["status"]})
    return out
