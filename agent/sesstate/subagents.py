"""Subagents: whom the session has launched and who of them still holds its turn."""

import json
import os
import re
import threading
import time

import models

from .limits import MAX_ITEMS

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
    """Returns the context fields of an agent: tokens, limit, limitKnown."""
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
    return {"tokens": tokens, "limit": limit, "limitKnown": known}


def agent_meta(path):
    """Returns the description, model, color, kind, last activity and context of subagents by name."""
    base = path[: -len(".jsonl")] if path.endswith(".jsonl") else path
    folder = os.path.join(base, "subagents")
    try:
        names = os.listdir(folder)
    except OSError:
        return {}
    out = {}
    for entry in names:
        if not entry.endswith(".meta.json") or not entry.startswith("agent-"):
            continue
        agent_id = entry[len("agent-"): -len(".meta.json")]
        meta_path = os.path.join(folder, entry)
        try:
            stat = os.stat(meta_path)
        except OSError:
            continue
        key = (stat.st_mtime_ns, stat.st_size)
        with _meta_lock:
            hit = _meta_cache.get(meta_path)
        if hit and hit[0] == key:
            meta = hit[1]
        else:
            try:
                with open(meta_path, encoding="utf-8") as f:
                    data = json.load(f)
            except (OSError, ValueError):
                continue
            if not isinstance(data, dict) or not data.get("name"):
                continue
            meta = {
                "name": str(data["name"]),
                "text": str(data.get("description") or ""),
                "model": str(data.get("model") or ""),
                "color": str(data.get("color") or ""),
                "id": agent_id,
                "kind": "teammate" if data.get("taskKind") == "in_process_teammate" else "subagent",
            }
            with _meta_lock:
                _meta_cache[meta_path] = (key, meta)
        talk = meta_path[: -len(".meta.json")] + ".jsonl"
        try:
            talk_stat = os.stat(talk)
        except OSError:
            meta = dict(meta, last="", tokens=0, limit=0, limitKnown=False)
        else:
            meta = dict(meta, last=_stamp(talk_stat.st_mtime),
                        **_context(talk, talk_stat, meta["model"]))
        was = out.get(meta["name"])
        if was is None or (was.get("last") or "") <= meta["last"]:
            out[meta["name"]] = meta
    return out
