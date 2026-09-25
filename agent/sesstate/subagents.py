"""Subagents: whom the session has launched and who of them still holds its turn."""

import json
import os
import re
import threading
import time

import models

from .limits import MAX_ITEMS

# The kind of an agent the session sent off to work with no name of its own.
KIND_BACKGROUND = "background"

AGENT_ID_RE = re.compile(r"agent_id:\s*([^\s@\\\"]+)")


TERMINATED_RE = re.compile(r'teammate_terminated[\\":,\s]*message[\\":\s]*([^"\\]+?) has shut down')


def letter_text(record):
    """Returns the text of a delivered letter, empty when the record carries none."""
    if not isinstance(record, dict) or record.get("type") != "user":
        return ""
    body = (record.get("message") or {}).get("content")
    return body if isinstance(body, str) else ""


def _drop_terminated(state, record):
    text = letter_text(record)
    if not state.agents or "teammate_terminated" not in text:
        return
    for name in TERMINATED_RE.findall(text):
        state.agents.pop(name, None)


def _mark_reported(state, record):
    text = letter_text(record)
    if not state.agents or "teammate_id" not in text:
        return
    at = record.get("timestamp") or ""
    for key, agent in state.agents.items():
        if f'teammate_id="{key}"' in text:
            agent["status"] = "reported"
            agent["reportedAt"] = at
    _prune_reported_agents(state)


def _lose(state, name):
    """Lets an agent go that cannot be reached: one that ever wrote keeps its place as reported."""
    agent = state.agents.get(name)
    if agent is None:
        return
    if not agent.get("reportedAt"):
        state.agents.pop(name, None)
        return
    agent["status"] = "reported"
    _prune_reported_agents(state)


def _lose_older_than(state, born):
    """Lets go every active agent spawned before the process was born: none of them outlived it."""
    if not born:
        return
    since = _stamp(born)[:19]
    gone = [name for name, agent in state.agents.items()
            if agent.get("status") == "active"
            and agent.get("at") and agent["at"][:19] < since]
    for name in gone:
        _lose(state, name)


def _prune_reported_agents(state):
    if len(state.agents) <= MAX_ITEMS:
        return
    reported = sorted(
        (a for a in state.agents.values() if a.get("status") == "reported"),
        key=lambda a: a.get("reportedAt") or "",
    )
    for agent in reported:
        if len(state.agents) <= MAX_ITEMS:
            break
        state.agents.pop(agent["name"], None)


_meta_cache = {}
_meta_lock = threading.Lock()

# The context of an agent is the input of its last request. The request is
# among the last records of its transcript, so the file is read from the end:
# this much at first, and four times more each time the tail held no request —
# a tool result of a few hundred kilobytes can stand between the end and it.
CONTEXT_TAIL = 64 * 1024

_context_cache = {}
_context_lock = threading.Lock()


def _stamp(mtime):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(mtime))


def _request_of(line):
    """Returns (tokens, model) of a record that holds a request, else None."""
    try:
        record = json.loads(line)
    except ValueError:
        return None
    if not isinstance(record, dict):
        return None
    message = record.get("message")
    if not isinstance(message, dict):
        return None
    usage = message.get("usage")
    if not isinstance(usage, dict) or message.get("model") == "<synthetic>":
        return None
    total = 0
    for key in ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"):
        value = usage.get(key)
        if isinstance(value, int):
            total += value
    return total, str(message.get("model") or "")


def last_request(path, size):
    """Returns (tokens, model) of the last request in a transcript, read from its end."""
    span = CONTEXT_TAIL
    while True:
        start = max(0, size - span)
        try:
            with open(path, "rb") as f:
                f.seek(start)
                data = f.read(size - start)
        except OSError:
            return 0, ""
        lines = data.split(b"\n")
        if start > 0:
            # The first piece is the end of a line cut at the seek: a wider
            # read brings it whole.
            lines = lines[1:]
        for line in reversed(lines):
            found = _request_of(line.decode("utf-8", "replace"))
            if found is not None:
                return found
        if start == 0:
            return 0, ""
        span *= 4


def _context(talk, stat, fallback_model):
    """Returns the context fields of an agent: tokens, limit, limitKnown, and the model it runs on."""
    key = (stat.st_mtime_ns, stat.st_size)
    with _context_lock:
        hit = _context_cache.get(talk)
    if hit and hit[0] == key:
        tokens, model = hit[1]
    else:
        tokens, model = last_request(talk, stat.st_size)
        with _context_lock:
            _context_cache[talk] = (key, (tokens, model))
    limit, known = models.limit_for(model or fallback_model)
    if tokens > limit:
        limit, known = models.DEFAULT_LIMIT_TOKENS, False
    return {"tokens": tokens, "limit": limit, "limitKnown": known, "ran": model}


def _meta_files(path):
    """Yields (id, meta file) of every subagent a session has ever started."""
    base = path[: -len(".jsonl")] if path.endswith(".jsonl") else path
    folder = os.path.join(base, "subagents")
    try:
        names = os.listdir(folder)
    except OSError:
        return
    for entry in names:
        if not entry.endswith(".meta.json") or not entry.startswith("agent-"):
            continue
        yield entry[len("agent-"): -len(".meta.json")], os.path.join(folder, entry)


def _meta_of(agent_id, meta_path):
    """Returns what the meta file of a subagent says, or None when it says nothing."""
    try:
        stat = os.stat(meta_path)
    except OSError:
        return None
    key = (stat.st_mtime_ns, stat.st_size)
    with _meta_lock:
        hit = _meta_cache.get(meta_path)
    if hit and hit[0] == key:
        return hit[1]
    try:
        with open(meta_path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    # An agent sent off without a name of its own is known by its type: the
    # session cannot write to it, and only its id tells it from its namesakes.
    if data.get("name"):
        name = str(data["name"])
        kind = "teammate" if data.get("taskKind") == "in_process_teammate" else "subagent"
    elif data.get("agentType") or data.get("description"):
        name = str(data.get("agentType") or "agent")
        kind = KIND_BACKGROUND
    else:
        return None
    meta = {
        "name": name,
        "text": str(data.get("description") or ""),
        "model": str(data.get("model") or ""),
        "color": str(data.get("color") or ""),
        "id": agent_id,
        "kind": kind,
    }
    with _meta_lock:
        _meta_cache[meta_path] = (key, meta)
    return meta


def talks(path):
    """Returns (name, transcript) of every subagent of a session.

    Namesakes are both returned: two agents called the same are two agents,
    and the work one of them left running is not the other's. An agent with
    no name of its own goes by what it was sent to do: its type is shared by
    every agent of that type the session sent off.
    """
    out = []
    for agent_id, meta_path in _meta_files(path):
        meta = _meta_of(agent_id, meta_path)
        if meta is None:
            continue
        who = meta["text"] if meta["kind"] == KIND_BACKGROUND and meta["text"] else meta["name"]
        out.append((who, _talk_of(meta_path)))
    return out


def _talk_of(meta_path):
    return meta_path[: -len(".meta.json")] + ".jsonl"


def _metas(path):
    """Yields what the files of every subagent say, with its last activity and context."""
    for agent_id, meta_path in _meta_files(path):
        meta = _meta_of(agent_id, meta_path)
        if meta is None:
            continue
        talk = _talk_of(meta_path)
        try:
            talk_stat = os.stat(talk)
        except OSError:
            yield dict(meta, last="", tokens=0, limit=0, limitKnown=False, ran="")
            continue
        yield dict(meta, last=_stamp(talk_stat.st_mtime),
                   **_context(talk, talk_stat, meta["model"]))


def agent_meta(path):
    """Returns the description, model, color, kind, last activity and context of subagents by name.

    An agent without a name of its own is left out: it is found by its id.
    """
    out = {}
    for meta in _metas(path):
        if meta["kind"] == KIND_BACKGROUND:
            continue
        meta.pop("ran")
        was = out.get(meta["name"])
        if was is None or (was.get("last") or "") <= meta["last"]:
            out[meta["name"]] = meta
    return out


def background_meta(path):
    """Returns the model, last activity and context of every subagent by its id.

    The file of an agent sent off without a name says nothing of its model: the
    type it was sent as does, so the model is the one its last request ran on.
    """
    out = {}
    for meta in _metas(path):
        meta["model"] = meta["model"] or meta["ran"]
        meta.pop("ran")
        out[meta["id"]] = meta
    return out
