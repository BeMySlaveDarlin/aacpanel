#!/usr/bin/env python3
"""Sessions of the panel held on the stream protocol.

A `claude -p` is a run of its own — a one-off question, an SDK reviewer, a
script — unless a holder keeps it: then it is a session of the panel, kept on
the stream instead of in a terminal. The holder's state file names the
conversation and the pid of its claude, and says what a person is waited for
by the names of the tools, never by their text.
"""
import json
import os


def stream_dir():
    """Returns where the holders keep their state files: the owner's runtime directory."""
    base = os.environ.get("XDG_RUNTIME_DIR") or f"/run/user/{os.getuid()}"
    return os.path.join(base, "aacpanel-stream")


def summary(sid, pid=None):
    """Returns the holder's state of a conversation, or None when no live holder keeps it.

    With a pid, the claude the holder keeps has to be that one: another
    process claiming the conversation is not its session.
    """
    if not isinstance(sid, str) or not sid or "/" in sid or sid.startswith("."):
        return None
    try:
        with open(os.path.join(stream_dir(), sid + ".json"), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict) or data.get("sessionId") != sid:
        return None
    if pid and data.get("pid") != pid:
        return None
    holder = data.get("holder")
    if isinstance(holder, bool) or not isinstance(holder, int) or holder <= 0:
        return None
    if not os.path.exists(f"/proc/{holder}"):
        return None
    return data


def waiting_for(data):
    """Says what a held session waits on, in the words claude in a terminal uses for it.

    A question is input the session needs; anything else — a permission for a
    tool, a plan to approve — is a dialog. The screen already knows both.
    """
    tools = data.get("waiting") if isinstance(data, dict) else None
    if not isinstance(tools, list) or not tools:
        return None
    return "input needed" if "AskUserQuestion" in tools else "dialog open"
