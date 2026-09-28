#!/usr/bin/env python3
"""Checklist reminder: at the end of a turn a session that keeps a checklist is asked once whether it changed.

The checklist is the model's own list of steps, kept through the panel's
checklist tool and shown to the person. It belongs to the place the session
works in — the config directory of the account and the directory claude runs
in — so a session started again there, afresh or going on with its
conversation, finds it. A turn that did work — called tools — and left the
checklist as it was may have left it behind: the end of that turn is held
once, and the model is told to update the checklist if it changed, to clear
it if it no longer applies, and otherwise to end the turn. The turn after the
hold is never held again.

A checklist with nothing left to do asks nothing, and neither does a session
that has no checklist tool: a claude started by hand in the same place cannot
update the checklist. A session has the tool when it sent the checklist
itself, or when it was started with the tool allowed, as the panel starts
every session — which is how a session started again after a restart is asked
about the checklist the one before it left.
"""

import datetime
import hashlib
import json
import os
import posixpath
import re
import sys

UNFINISHED = ("pending", "active")

TOOL = "mcp__aacpanel__checklist"

# How much of the tail of the transcript is read for the turn. A turn longer
# than this is taken from the oldest record read, which is all the question
# needs: whether the turn called tools, and whether the checklist is older.
TAIL = 4 * 1024 * 1024

# How far up the chain of its parents the hook looks for the claude running
# it: claude may start a hook through a shell.
ANCESTORS = 6

PROC = "/proc"


def checklists_dir():
    """Returns where the panel's executor keeps the checklists."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.join(os.path.expanduser("~"), ".local", "state")
    return os.path.join(base, "aacpanel", "checklists")


def _clean(path):
    """Returns a path in the form the executor names a checklist by, or None when it is not absolute."""
    if not isinstance(path, str) or not path.startswith("/"):
        return None
    path = posixpath.normpath(path)
    return "/" + path.lstrip("/") if path.startswith("//") else path


def checklist_name(config_dir, cwd):
    """Returns the file name of the checklist of a place: the hash of its two paths the executor computes."""
    config_dir, cwd = _clean(config_dir), _clean(cwd)
    if config_dir is None or cwd is None:
        return None
    return hashlib.sha256(os.fsencode(config_dir) + b"\0" + os.fsencode(cwd)).hexdigest()[:32] + ".json"


def read_checklist(config_dir, cwd):
    """Returns the checklist of a place and when its file was written, or (None, None)."""
    name = checklist_name(config_dir, cwd)
    if name is None:
        return None, None
    path = os.path.join(checklists_dir(), name)
    try:
        written = os.stat(path).st_mtime
        with open(path, encoding="utf-8") as f:
            found = json.load(f)
    except (OSError, ValueError):
        return None, None
    if not isinstance(found, dict) or found.get("configDir") != _clean(config_dir) or found.get("dir") != _clean(cwd):
        return None, None
    return found, written


def unfinished(checklist):
    """Says whether a step of the checklist is still to do or at work."""
    items = checklist.get("items") if isinstance(checklist.get("items"), list) else []
    return any(isinstance(it, dict) and it.get("status") in UNFINISHED for it in items)


def _stat(pid):
    try:
        with open(os.path.join(PROC, str(pid), "stat"), encoding="utf-8") as f:
            line = f.read()
        return line[line.rindex(")") + 1:].split()
    except (OSError, ValueError):
        return []


def _parent(pid):
    fields = _stat(pid)
    try:
        return int(fields[1])
    except (IndexError, ValueError):
        return None


def ancestors():
    """Returns the processes above this one, its parent first, init left out."""
    out, pid = [], os.getppid()
    while pid and pid > 1 and len(out) < ANCESTORS and pid not in out:
        out.append(pid)
        pid = _parent(pid)
    return out


def config_dir():
    """Returns the config directory of the claude running the hook: the one it passes on, or its default."""
    return os.environ.get("CLAUDE_CONFIG_DIR") or os.path.join(os.path.expanduser("~"), ".claude")


def session_of(pid):
    """Returns the file claude keeps of a process of its, or None when the file is not that process's."""
    try:
        with open(os.path.join(config_dir(), "sessions", f"{pid}.json"), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict) or data.get("pid") != pid or data.get("kind") not in (None, "", "interactive"):
        return None
    fields = _stat(pid)
    if len(fields) < 20 or not data.get("procStart") or str(data["procStart"]) != fields[19]:
        return None
    return data


def claude(session_id):
    """Returns the pid of the claude running the hook and the file it keeps of itself, or (None, None).

    It is the nearest process above the hook whose file names the
    conversation the hook is for: a claude run inside the work of a session
    has no file of its own, and the session above it is not the one whose
    turn ends.
    """
    for pid in ancestors():
        data = session_of(pid)
        if data is not None:
            return (pid, data) if data.get("sessionId") == session_id else (None, None)
    return None, None


def has_tool(pid):
    """Says whether a claude process was started with the checklist tool allowed, as the panel starts every session."""
    try:
        with open(os.path.join(PROC, str(pid), "cmdline"), "rb") as f:
            args = f.read().split(b"\0")
    except OSError:
        return False
    word = TOOL.encode()
    return any(word in re.split(rb"[\s,=]+", arg) for arg in args)


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
    return (f"The panel shows the person a checklist of the work in this place, and this turn did work "
            f"without touching it. If the checklist changed — a step started, ended or was dropped, or the "
            f"steps themselves changed — send it with the checklist tool ({TOOL}); if it no longer applies, "
            f"clear it with an empty list. If it did not change, end the turn now, without a word about this.")


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
    pid, session = claude(payload.get("session_id") or "")
    if pid is None:
        return
    checklist, written = read_checklist(config_dir(), session.get("cwd"))
    if checklist is None or not unfinished(checklist):
        return
    if checklist.get("pid") != pid and not has_tool(pid):
        return
    began, called = turn(payload.get("transcript_path") or "")
    if not called or began is None or written >= began:
        return
    json.dump({"decision": "block", "reason": reason()}, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
    sys.exit(0)
