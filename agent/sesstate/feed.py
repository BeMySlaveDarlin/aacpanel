"""What is accumulated and how one record is spread across the sources."""

import collections
import os

from .artifacts import (ARTIFACT_URL_RE, DOC_TOOLS, SENT_TOOL, _artifact, _document,
                        _sent, result_text)
from .limits import MAX_ITEMS, _short
from .subagents import AGENT_ID_RE, _lose, _prune_reported_agents
from .tasks import (MAYBE_BACKGROUND, NOTIF_BLOCK_RE, STOPPERS, TASK_AGENT,
                    TASK_ID_KEYS, TASK_KIND_BY_KEY, _notify_tasks, _task, finish)
from .wake import WAKE_ID, _wake, is_wakeup


class State:
    """State of one transcript, accumulated as it is read."""

    def __init__(self):
        self.tasks = {}
        self.agents = {}
        self.pending = {}
        self.task_ids = {}
        self.answered = collections.deque(maxlen=100)
        self.arts = {}
        self.docs = {}
        self.sent = {}
        self.cwd = ""
        self.pos = 0
        self.born = None

    def snapshot(self):
        """Returns what goes outside, ordered by the time work started."""
        tasks = sorted(self.tasks.values(), key=lambda t: t.get("at") or "")
        agents = sorted(self.agents.values(), key=lambda a: a.get("at") or "")
        arts = sorted(self.arts.values(), key=lambda a: a.get("at") or "", reverse=True)
        docs = sorted(self.docs.values(), key=lambda d: d.get("at") or "", reverse=True)
        sent = sorted(self.sent.values(), key=lambda s: s.get("at") or "", reverse=True)
        return {
            "tasks": tasks[:MAX_ITEMS],
            "agents": agents[:MAX_ITEMS],
            "artifacts": [dict(a, title=a.get("title") or a["file"]) for a in arts[:MAX_ITEMS]],
            "docs": [d for d in docs[:MAX_ITEMS] if os.path.isfile(d["path"])],
            "sent": [s for s in sent[:MAX_ITEMS] if os.path.isfile(s["path"])],
        }


def _feed_record(state, record, raw):
    kind = record.get("type")

    if not state.cwd:
        cwd = record.get("cwd")
        if isinstance(cwd, str) and cwd:
            state.cwd = cwd

    at = record.get("timestamp") or ""

    if is_wakeup(record):
        state.tasks.pop(WAKE_ID, None)

    if "<task-notification>" in raw:
        content = record.get("content")
        if not isinstance(content, str):
            content = raw
        for body in NOTIF_BLOCK_RE.findall(content):
            _notify_tasks(state, body, at)

    message = record.get("message") or {}
    blocks = message.get("content")
    if not isinstance(blocks, list):
        blocks = []

    for block in blocks:
        if not isinstance(block, dict):
            continue

        if block.get("type") == "tool_use":
            name = block.get("name") or ""
            data = block.get("input") or {}
            if not isinstance(data, dict):
                data = {}
            if name in STOPPERS:
                finish(state, data.get("task_id") or data.get("shell_id") or "", at)
                continue
            if name == "Artifact":
                _artifact(state, data, block.get("id"), at)
                continue
            if name in DOC_TOOLS:
                _document(state, data, at)
                continue
            if name == SENT_TOOL:
                state.pending[block.get("id")] = {"kind": "sent"}
                continue
            if name == "ScheduleWakeup":
                if data.get("stop"):
                    state.tasks.pop(WAKE_ID, None)
                    continue
                state.pending[block.get("id")] = {
                    "kind": "wake", "at": at,
                    "text": _short(data.get("reason") or data.get("prompt") or "wake-up"),
                }
                continue
            label = _short(data.get("description") or data.get("command") or data.get("prompt"))
            if name == "Agent":
                state.pending[block.get("id")] = {
                    "kind": "agent",
                    "name": _short(data.get("name") or data.get("subagent_type") or "agent"),
                    "text": label,
                    "at": at,
                }
            elif name in MAYBE_BACKGROUND:
                state.pending[block.get("id")] = {
                    "kind": "task", "text": label or name, "at": at,
                    "cmd": _short(data.get("command")),
                    "desc": _short(data.get("description")),
                }
            elif name == "SendMessage":
                agent = state.agents.get(data.get("to"))
                if agent is not None:
                    agent["status"] = "active"
                    state.pending[block.get("id")] = {"kind": "mail", "to": agent["name"]}
            continue

        if block.get("type") != "tool_result":
            continue

        if block.get("tool_use_id") and block["tool_use_id"] not in state.answered:
            state.answered.append(block["tool_use_id"])
        started = state.pending.pop(block.get("tool_use_id"), None)
        if started is None:
            continue
        if started["kind"] == "art":
            found = ARTIFACT_URL_RE.search(result_text(block))
            art = state.arts.get(started["key"])
            if art is not None and found:
                art["url"] = found.group(1)
            continue
        result = record.get("toolUseResult")
        if started["kind"] == "wake":
            _wake(state, started, result, at)
            continue
        if not isinstance(result, dict):
            continue
        if started["kind"] == "sent":
            _sent(state, result, at)
            continue
        if started["kind"] == "mail":
            # The only letter the tool refuses to an agent of this session is one
            # to an agent that is gone.
            if result.get("success") is False:
                _lose(state, started["to"])
            continue
        if started["kind"] == "agent":
            status = result.get("status")
            if status == "async_launched":
                _task(state, block.get("tool_use_id"), result.get("agentId"),
                      started, _short(result.get("description")), TASK_AGENT)
                continue
            if status != "teammate_spawned":
                continue
            found = AGENT_ID_RE.search(raw)
            name = found.group(1) if found else started["name"]
            state.agents[name] = {
                "name": name, "text": started["text"], "at": started["at"],
                "status": "active", "reportedAt": "",
            }
            _prune_reported_agents(state)
            continue
        for key in TASK_ID_KEYS:
            if result.get(key):
                _task(state, block.get("tool_use_id"), result[key], started,
                      kind=TASK_KIND_BY_KEY[key])
                break
