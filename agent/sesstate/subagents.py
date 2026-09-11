"""Subagents: whom the session has launched and who of them still holds its turn."""

import json
import os
import re
import threading
import time

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


def _stamp(mtime):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(mtime))


def agent_meta(path):
    """Returns the description, model, color, kind and last activity of subagents by name."""
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
            meta = dict(meta, last=_stamp(os.stat(talk).st_mtime))
        except OSError:
            meta = dict(meta, last="")
        was = out.get(meta["name"])
        if was is None or (was.get("last") or "") <= meta["last"]:
            out[meta["name"]] = meta
    return out
