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


def codex_threads():
    """Returns the state of every codex thread a live executor follows.

    The executor writes a file for a thread beside the holders' files, with
    no process of its own: the holder it names is the executor, and a file
    whose executor is gone is a thread nobody follows any more.
    """
    try:
        names = sorted(os.listdir(stream_dir()))
    except OSError:
        return []
    out = []
    for name in names:
        if not name.endswith(".json"):
            continue
        data = summary(name[:-len(".json")])
        if data is not None and data.get("agent") == "codex":
            out.append(data)
    return out


def codex_limits():
    """Returns the rate limits of the codex accounts, by contour, as live executors last heard them.

    The executor keeps a file per contour beside the state files of the
    threads; a file outlives its executor, and its time says how old it is.
    """
    root = os.path.join(stream_dir(), "codex-limits")
    try:
        names = sorted(os.listdir(root))
    except OSError:
        return []
    out = []
    for name in names:
        if not name.endswith(".json") or name.startswith("."):
            continue
        try:
            with open(os.path.join(root, name), encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        if isinstance(data, dict) and isinstance(data.get("contour"), str) and data["contour"]:
            out.append(data)
    return out


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


_traits = {}
_traits_key = None


def traits():
    """Returns what the holders learned of the models, by resolved id; empty when nothing yet.

    A property of the model, the same in every account: which efforts it takes and
    whether it has the auto mode. Read again only when the file changes.
    """
    global _traits, _traits_key
    path = os.path.join(kept_dir(), "traits.json")
    try:
        stat = os.stat(path)
        key = (stat.st_mtime_ns, stat.st_size)
    except OSError:
        return {}
    if key != _traits_key:
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            data = None
        models = data.get("models") if isinstance(data, dict) else None
        _traits = models if isinstance(models, dict) else {}
        _traits_key = key
    return _traits


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
