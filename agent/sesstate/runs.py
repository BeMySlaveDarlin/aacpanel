"""The calls of a session that may have started a run of codex exec.

A run of codex exec a session started leaves no word of its thread in the
transcript: the session runs a command, and codex makes a thread of its own in
the database of its home. What the transcript has is the call — when it went
out and from what directory, what the session called it, whether it went to
the background and when it came back — and the run is found by it in that
database (codex_archive.runs). A call sent to the background comes back when
its task ends, not with the answer that it went there. The command itself is
not kept: it carries the words of the work. Kept of it are the directories it
names, for a run that works somewhere else than the session, and whether it
resumes a thread, which makes none.
"""

import datetime
import os
import re

from .artifacts import result_text
from .limits import _short
from .tasks import DONE_STATUSES, NOTIF_STATUS_RE, NOTIF_TASK_RE, NOTIF_USE_RE, left_behind

TOOL = "Bash"
CODEX = "codex"

# How many calls a state keeps, the newest: a command that greps codex's files
# names codex too, and a long session makes hundreds of those.
MAX_CALLS = 400

# The words of a command, and how many of the directories among them are kept.
WORDS_RE = re.compile(r"[\s'\"`;|&<>(){}=,]+")
MAX_PATHS = 16
MAX_PATH = 1024

UUID_RE = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
RESUME_RE = re.compile(r"\bresume\b")


def ms(stamp):
    """Returns a stamp of the transcript in milliseconds, 0 when it cannot be read."""
    if not isinstance(stamp, str) or not stamp:
        return 0
    try:
        return int(datetime.datetime.fromisoformat(stamp.replace("Z", "+00:00")).timestamp() * 1000)
    except ValueError:
        return 0


def named_paths(command):
    """Returns the directories and files a command names by their full path."""
    out = []
    for word in WORDS_RE.split(command):
        if word.startswith("~/"):
            word = os.path.expanduser(word)
        if not word.startswith("/") or len(word) > MAX_PATH:
            continue
        word = word.rstrip("/.:") or "/"
        if word not in out:
            out.append(word)
            if len(out) >= MAX_PATHS:
                break
    return out


def called(state, tool, data, use_id, record, at):
    """Keeps a call of the shell whose command names codex."""
    command = data.get("command")
    if tool != TOOL or not isinstance(command, str) or CODEX not in command or not use_id:
        return
    went = ms(at)
    if not went:
        return
    state.codex[use_id] = {
        "id": use_id, "at": went, "back": 0,
        "cwd": record.get("cwd") or state.cwd,
        "text": _short(data.get("description")),
        "bg": data.get("run_in_background") is True,
        "paths": named_paths(command),
        "resume": bool(RESUME_RE.search(command)) and bool(UUID_RE.search(command)),
    }
    while len(state.codex) > MAX_CALLS:
        state.codex.pop(next(iter(state.codex)))


def answered(state, use_id, at, result, block):
    """Notes when a kept call came back, or that it went to the background and is still out."""
    call = state.codex.get(use_id)
    if call is None or call["back"]:
        return
    left = left_behind(result, result_text(block))
    if left:
        call["bg"], call["task"] = True, left[0]
        return
    if not call["bg"]:
        call["back"] = ms(at)


def notified(state, body, at):
    """Notes when a kept call sent to the background came back: its task ended."""
    if not state.codex or not any(s.strip() in DONE_STATUSES for s in NOTIF_STATUS_RE.findall(body)):
        return
    uses, tasks = set(NOTIF_USE_RE.findall(body)), set(NOTIF_TASK_RE.findall(body))
    for call in state.codex.values():
        if not call["back"] and (call["id"] in uses or call.get("task") in tasks):
            call["back"] = ms(at)
