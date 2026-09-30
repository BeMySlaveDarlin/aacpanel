#!/usr/bin/env python3
"""Background reminder: at the end of a turn a session is reminded of background work it left running long.

Claude passes the Stop hook background_tasks, everything of the session still
at work. That there is background work is no reason to speak: a session may
be waiting for a build in earnest. Age is. The hook notes when it first saw
each task and names only those running past FIRST_REMIND, each at most once
in REPEAT_REMIND. Subagents and workflows are left out: they end by
themselves and wake the session when done, so a session that waits for them
is right to. What is forgotten are shell commands, monitors and the like — a
wait loop with no time limit among them never ends at all.

The reminder asks for no answer: the session stops what is no longer needed
or goes on. An answer asked for at every end of a turn would be noise in
every reply that has background work at all.

The reminder goes as additional context of the Stop event, which lets the
turn go on for a round; the stop that follows that round comes with
stop_hook_active, and the hook is silent then. Blocking the stop instead, or
exiting with 2, would hold every end of a turn while the work runs. A task is
named once in REPEAT_REMIND even without stop_hook_active, and the reminder
is spoken only once its mark is on the disk: a mark that cannot be written
would repeat it at every stop.

Not for SubagentStop: background_tasks names no owner of a task, and a
subagent would see itself and its neighbours in the list and stop what is
not its own.

The marks live in the state directory, a file a conversation; a file untouched
for KEEP goes. Any failure exits 0 without a word: the session matters more
than the reminder.
"""

import json
import os
import re
import sys
import tempfile
import time

FIRST_REMIND = 60 * 60     # how long a task runs before it is first named
REPEAT_REMIND = 120 * 60   # and how long before it is named again

# Subagents and workflows end by themselves and wake the session when done.
SELF_ENDING = {"subagent", "workflow"}

# A file of marks untouched this long belongs to a conversation that is over.
KEEP = 7 * 24 * 3600

# A wait loop without a time limit never ends by itself, and that is worth a word of its own.
WAIT_LOOP = re.compile(r"\b(until|while)\b")
HAS_LIMIT = re.compile(r"\btimeout\b|\bSECONDS\b|deadline|--max-time")

LIST_LIMIT = 15
FIELD_LIMIT = 140


def marks_dir():
    """Returns the directory of the marks: under the state directory of the install."""
    return os.path.join(os.environ.get("AACP_STATE_DIR") or "/var/lib/aacpanel", "background-reminder")


def clip(text):
    text = " ".join(str(text).split())
    return text if len(text) <= FIELD_LIMIT else text[:FIELD_LIMIT - 1] + "…"


def describe(task, age):
    """Returns the line of a task in the reminder."""
    kind = task.get("type") or "?"
    what = task.get("description") or task.get("command") or task.get("name") or ""
    extra = task.get("agent_type") or task.get("server") or ""
    line = (f"- {kind}{f' ({extra})' if extra else ''} `{task['id']}`, "
            f"{int(age // 60)} min: {clip(what)}")
    command = task.get("command")
    if isinstance(command, str) and WAIT_LOOP.search(command) and not HAS_LIMIT.search(command):
        line += " — a wait loop without a time limit, it will not end by itself"
    return line


def number(v):
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def load(path):
    """Returns the marks of a conversation: when each task was first seen and last named."""
    try:
        with open(path, encoding="utf-8") as f:
            marks = json.load(f)
    except (OSError, ValueError):
        return {}
    return marks if isinstance(marks, dict) else {}


def save(path, marks):
    """Writes the marks and prunes the files past KEEP; says whether the marks are on the disk."""
    folder = os.path.dirname(path)
    try:
        os.makedirs(folder, mode=0o700, exist_ok=True)
        fd, tmp = tempfile.mkstemp(dir=folder)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(marks, f)
        os.replace(tmp, path)
    except OSError:
        return False
    old = time.time() - KEEP
    try:
        for entry in os.scandir(folder):
            if entry.stat().st_mtime < old:
                os.remove(entry.path)
    except OSError:
        pass
    return True


def forget(path):
    try:
        os.remove(path)
    except OSError:
        pass


def remind(payload, now):
    """Returns the reminder for a stop, or None when there is nothing to say."""
    if not isinstance(payload, dict) or payload.get("hook_event_name", "Stop") != "Stop":
        return None
    # The round after a reminder is the answer to it: it is never reminded again.
    if payload.get("stop_hook_active"):
        return None
    session = payload.get("session_id")
    session = re.sub(r"[^A-Za-z0-9_-]", "", session) if isinstance(session, str) else ""
    if not session:
        return None
    path = os.path.join(marks_dir(), f"{session}.json")
    listed = payload.get("background_tasks")
    tasks = [t for t in (listed if isinstance(listed, list) else [])
             if isinstance(t, dict) and isinstance(t.get("id"), str) and t["id"]
             and not (isinstance(t.get("type"), str) and t["type"] in SELF_ENDING)]
    if not tasks:
        forget(path)
        return None

    seen = load(path)
    marks, due = {}, []
    for task in tasks:
        mark = seen.get(task["id"])
        if not isinstance(mark, dict) or not number(mark.get("first")) or not number(mark.get("reminded")):
            mark = {"first": now, "reminded": 0}
        age = now - mark["first"]
        if age >= FIRST_REMIND and now - mark["reminded"] >= REPEAT_REMIND:
            due.append((task, age))
            mark["reminded"] = now
        # A task no longer listed is not carried over.
        marks[task["id"]] = mark
    if not save(path, marks) or not due:
        return None

    lines = [describe(t, age) for t, age in due[:LIST_LIMIT]]
    if len(due) > LIST_LIMIT:
        lines.append(f"- … and {len(due) - LIST_LIMIT} more")
    return ("Background work of this session has been running a long time:\n"
            + "\n".join(lines)
            + "\n\nIf any of it has done its job or is no longer needed, stop it with TaskStop by its id. "
            f"If it is still needed, go on; no answer is needed. Each task is named at most once "
            f"in {REPEAT_REMIND // 3600} hours.")


def main():
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        return
    text = remind(payload, time.time())
    if text is None:
        return
    json.dump({"hookSpecificOutput": {"hookEventName": "Stop", "additionalContext": text}},
              sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    try:
        main()
    except Exception as err:  # noqa: BLE001 — a failure here would be an error under every turn of the session
        print(f"background-reminder: {err!r}", file=sys.stderr)
    sys.exit(0)
