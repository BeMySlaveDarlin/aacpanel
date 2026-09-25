#!/usr/bin/env python3
"""Sessions of the panel held on the stream protocol.

A `claude -p` is a run of its own — a one-off question, an SDK reviewer, a
script — unless a holder keeps it: then it is a session of the panel, kept on
the stream instead of in a terminal. The holder's state file names the
conversation and the pid of its claude, and says what a person is waited for
by the names of the tools, never by their text.
"""
import hashlib
import json
import os


def stream_dir():
    """Returns where the holders keep their state files: the owner's runtime directory."""
    base = os.environ.get("XDG_RUNTIME_DIR") or f"/run/user/{os.getuid()}"
    return os.path.join(base, "aacpanel-stream")


def named(sid):
    """Reports whether a conversation id names a file of its own and nothing outside the directory."""
    return isinstance(sid, str) and bool(sid) and "/" not in sid and not sid.startswith(".")


def summary(sid, pid=None):
    """Returns the holder's state of a conversation, or None when no live holder keeps it.

    With a pid, the claude the holder keeps has to be that one: another
    process claiming the conversation is not its session.
    """
    if not named(sid):
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


def kept_dir():
    """Returns where the holders keep what outlives them: what the transcript does not say."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.expanduser("~/.local/state")
    return os.path.join(base, "aacpanel-stream")


def kept(path):
    """Returns a list a holder kept, empty when there is none."""
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return []
    return data if isinstance(data, list) else []


def withdrawn_path(sid):
    """Returns where the holder keeps the fingerprints of the messages taken back."""
    return os.path.join(kept_dir(), "withdrawn", sid + ".json")


def permits_path(sid):
    """Returns where the holder keeps the answers to the permissions of a conversation."""
    return os.path.join(kept_dir(), "permits", sid + ".json")


def permits(sid):
    """Returns the answers a person gave to the permissions of a conversation, by call.

    The transcript has the call and its result and nothing of the question
    between them: on the stream it is a request and a reply. The holder keeps
    no words of the call, only which it was and what was answered.
    """
    if not named(sid):
        return {}
    out = {}
    for entry in kept(permits_path(sid)):
        if (isinstance(entry, dict) and isinstance(entry.get("use"), str) and entry["use"]
                and entry.get("decision") in ("allow", "deny")):
            out[entry["use"]] = entry
    return out


def fingerprint(text):
    """Names a message without its words, the way the holder does."""
    return hashlib.sha256(text.strip().encode("utf-8")).hexdigest()[:32]


def withdrawn(sid):
    """Returns the fingerprints of the messages taken back from the queue, with repeats."""
    if not named(sid):
        return []
    return [x for x in kept(withdrawn_path(sid)) if isinstance(x, str)]


def mark_withdrawn(items, sid):
    """Marks the messages of a person that were taken back before the session read them.

    The transcript writes the same record for a message read and for one taken
    back, and the feed would show it as sent. A message sent twice and taken
    back once has only its first copy marked. A message cut for the feed is not
    matched: its fingerprint is of the whole.
    """
    left = {}
    for mark in withdrawn(sid):
        left[mark] = left.get(mark, 0) + 1
    if not left:
        return items
    out = []
    for item in items:
        if item.get("role") == "me" and not item.get("cut") and isinstance(item.get("text"), str):
            mark = fingerprint(item["text"])
            if left.get(mark):
                left[mark] -= 1
                item = {**item, "state": "withdrawn"}
        out.append(item)
    return out
