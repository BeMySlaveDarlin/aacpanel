#!/usr/bin/env python3
"""Plan reminder: at the end of a turn a session that keeps a plan is asked once whether it changed.

The plan is the model's own list of steps, kept through the panel's plan tool
and shown to the person. A turn that did work — called tools — and left the
plan as it was may have left it behind: the end of that turn is held once, and
the model is told to update the plan if it changed and otherwise to end the
turn. The turn after the hold is never held again. A plan with nothing left
to do asks nothing, and neither does one another process wrote: the same
conversation resumed without the tool cannot update it.
"""

import datetime
import json
import os
import sys

UNFINISHED = ("pending", "active")

TOOL = "mcp__aacpanel__plan"

# How much of the tail of the transcript is read for the turn. A turn longer
# than this is taken from the oldest record read, which is all the question
# needs: whether the turn called tools, and whether the plan is older.
TAIL = 4 * 1024 * 1024

# How far up the chain of its parents the hook looks for the claude running
# it: claude may start a hook through a shell.
ANCESTORS = 6

PROC = "/proc"


def plans_dir():
    """Returns where the panel's executor keeps the plans."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.join(os.path.expanduser("~"), ".local", "state")
    return os.path.join(base, "aacpanel", "plans")


def read_plan(session_id):
    """Returns the plan of a conversation and when its file was written, or (None, None)."""
    if not session_id or "/" in session_id or session_id.startswith("."):
        return None, None
    path = os.path.join(plans_dir(), session_id + ".json")
    try:
        written = os.stat(path).st_mtime
        with open(path, encoding="utf-8") as f:
            plan = json.load(f)
    except (OSError, ValueError):
        return None, None
    if not isinstance(plan, dict) or plan.get("sessionId") != session_id:
        return None, None
    return plan, written


def unfinished(plan):
    """Says whether a step of the plan is still to do or at work."""
    items = plan.get("items") if isinstance(plan.get("items"), list) else []
    return any(isinstance(it, dict) and it.get("status") in UNFINISHED for it in items)


def _parent(pid):
    try:
        with open(os.path.join(PROC, str(pid), "stat"), encoding="utf-8") as f:
            line = f.read()
        return int(line[line.rindex(")") + 1:].split()[1])
    except (OSError, ValueError, IndexError):
        return None


def ancestors():
    """Returns the processes above this one, its parent first, init left out."""
    out, pid = [], os.getppid()
    while pid and pid > 1 and len(out) < ANCESTORS and pid not in out:
        out.append(pid)
        pid = _parent(pid)
    return out


def _stamp(record):
    at = record.get("timestamp")
    if not isinstance(at, str) or not at:
        return None
    try:
        return datetime.datetime.fromisoformat(at.replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


def _is_prompt(record):
    """Says whether a record starts a turn: a message to the session, not a tool's result."""
    if record.get("type") != "user" or record.get("isMeta") or record.get("isCompactSummary"):
        return False
    content = (record.get("message") or {}).get("content")
    if isinstance(content, str):
        return bool(content.strip())
    if not isinstance(content, list):
        return False
    kinds = {b.get("type") for b in content if isinstance(b, dict)}
    return "text" in kinds and "tool_result" not in kinds


def _called(record):
    if record.get("type") != "assistant":
        return False
    content = (record.get("message") or {}).get("content")
    return isinstance(content, list) and any(isinstance(b, dict) and b.get("type") == "tool_use" for b in content)


def _records(path):
    """Yields the records of the tail of a transcript, newest first."""
    limit = TAIL
    try:
        with open(path, "rb") as f:
            f.seek(0, os.SEEK_END)
            size = f.tell()
            f.seek(max(0, size - limit))
            data = f.read()
    except OSError:
        return
    lines = data.split(b"\n")
    if size > limit:
        lines = lines[1:]
    for raw in reversed(lines):
        if not raw.strip():
            continue
        try:
            record = json.loads(raw)
        except ValueError:
            continue
        if isinstance(record, dict) and not record.get("isSidechain"):
            yield record


def turn(path):
    """Returns (when the turn began, whether it called tools); the time is None when unknown."""
    began, called = None, False
    for record in _records(path):
        at = _stamp(record)
        if at is not None:
            began = at
        if _called(record):
            called = True
        if _is_prompt(record):
            break
    return began, called


def reason():
    """Returns what the model is told instead of stopping."""
    return (f"This session keeps a plan in the panel, and this turn did work without touching it. "
            f"If the plan changed — a step started, ended or was dropped, or the steps themselves "
            f"changed — send it with the plan tool ({TOOL}). If it did not change, end the turn now, "
            f"without a word about this.")


def main():
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        return
    if not isinstance(payload, dict) or payload.get("hook_event_name", "Stop") != "Stop":
        return
    # The turn that follows a hold is the answer to it: it is never held again.
    if payload.get("stop_hook_active"):
        return
    plan, written = read_plan(payload.get("session_id") or "")
    if plan is None or not unfinished(plan):
        return
    if plan.get("pid") not in ancestors():
        return
    began, called = turn(payload.get("transcript_path") or "")
    if not called or began is None or written >= began:
        return
    json.dump({"decision": "block", "reason": reason()}, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
    sys.exit(0)
