"""Workflow runs: a script the session launched and the agents it drives.

A run is neither a background shell nor a subagent, and it is kept apart from
both. It has phases of its own, a script behind it and dozens of agents inside
that never report to the session: counted among the subagents it would bury
them, counted among the background jobs it would say nothing but that
something is running.

What the harness leaves on disk is read here as well as the transcript. The
launch and the end arrive in the transcript, but everything the run knows
about itself — the phases, the log of agents that stalled and were retried,
the result — is written to a snapshot beside it, and only once the run is
over. While it runs, the transcripts of its agents are the only sign of
progress there is.
"""

import json
import os
import re
import threading

from .limits import MAX_ITEMS, _short

WORKFLOW_TOOL = "Workflow"

# The statuses a run ends with. Anything else is a run still going.
FLOW_DONE = ("completed", "failed", "killed", "cancelled", "stopped")

# The name of a run, taken out of the script it was launched with: the launch
# result carries the summary but not the name, and the name is what the person
# gave the workflow.
META_NAME_RE = re.compile(r"""\bname\s*:\s*['"]([^'"]{1,80})['"]""")

# The phases of a run, taken out of the same script. A workflow writes no
# snapshot of itself until it is over, so without this a run in flight could
# say nothing about what it is going through — only how many agents it has
# started. The titles are read, the details are not: a detail runs to a line
# of prose and is worth nothing beside a title the eye can follow.
META_PHASES_RE = re.compile(r"\bphases\s*:\s*\[(.{0,2000}?)\]", re.S)
PHASE_TITLE_RE = re.compile(r"""\btitle\s*:\s*['"]([^'"]{1,80})['"]""")

NOTIF_USE_RE = re.compile(r"<tool-use-id>([^<]+)</tool-use-id>")
NOTIF_TASK_RE = re.compile(r"<task-id>([^<]+)</task-id>")
NOTIF_STATUS_RE = re.compile(r"<status>([^<]*)</status>")
NOTIF_AGENTS_RE = re.compile(r"<agent_count>(\d+)</agent_count>")
NOTIF_TOKENS_RE = re.compile(r"<subagent_tokens>(\d+)</subagent_tokens>")
NOTIF_CALLS_RE = re.compile(r"<tool_uses>(\d+)</tool_uses>")

# How much of the log and of the result goes out: a run of ninety agents
# writes a log of every stall and a result of whatever the script returned,
# and neither is read on a phone in full.
MAX_LOGS = 12
MAX_RESULT = 1200

_snap_cache = {}
_snap_lock = threading.Lock()


def started_of(data, at):
    """What a launch leaves behind until its result comes."""
    script = (data.get("script") if isinstance(data, dict) else "") or ""
    found = META_NAME_RE.search(script)
    return {
        "kind": "flow",
        "at": at,
        "name": _short(found.group(1)) if found else "",
        "text": _short(data.get("name") or ""),
        "phases": _phases_of_script(script),
    }


def _phases_of_script(script):
    """The phases a script declares, as far as reading the literal goes."""
    block = META_PHASES_RE.search(script)
    if not block:
        return []
    return [{"title": _short(title), "detail": ""}
            for title in PHASE_TITLE_RE.findall(block.group(1))[:MAX_ITEMS]]


def launched(state, use, result, started):
    """Registers a run the session has just sent off."""
    run_id = result.get("runId")
    if not run_id:
        return
    task_id = result.get("taskId") or run_id
    state.flow_ids[task_id] = run_id
    if use:
        state.flow_ids[use] = run_id
    state.flows[run_id] = {
        "id": run_id,
        "task": task_id,
        "name": started.get("name") or started.get("text") or "",
        "text": _short(result.get("summary")),
        "at": started.get("at") or "",
        "status": "running",
        "doneAt": "",
        "dir": result.get("transcriptDir") or "",
        "script": result.get("scriptPath") or "",
        "agents": 0,
        "tokens": 0,
        "calls": 0,
        "phases": started.get("phases") or [],
        "logs": [],
        "result": "",
        "ms": 0,
    }
    _prune_done_flows(state)


def notified(state, body, at):
    """Closes a run whose notification has arrived, or notes that it is alive."""
    if not state.flows:
        return
    targets = NOTIF_USE_RE.findall(body) + NOTIF_TASK_RE.findall(body)
    ended = [s.strip() for s in NOTIF_STATUS_RE.findall(body) if s.strip() in FLOW_DONE]
    for target in targets:
        run_id = state.flow_ids.get(target, target)
        flow = state.flows.get(run_id)
        if flow is None:
            continue
        if not ended:
            flow["event"] = at
            continue
        finish(state, run_id, ended[0], at)
        for regexp, key in ((NOTIF_AGENTS_RE, "agents"), (NOTIF_TOKENS_RE, "tokens"),
                            (NOTIF_CALLS_RE, "calls")):
            found = regexp.search(body)
            if found:
                flow[key] = int(found.group(1))


def finish(state, run_id, status, at):
    """Marks a run as over: it keeps its place, the reading of it is the point."""
    flow = state.flows.get(run_id)
    if flow is None:
        return
    flow["status"] = status or "completed"
    flow["doneAt"] = at or flow.get("doneAt") or ""
    state.flow_ids = {t: r for t, r in state.flow_ids.items() if r != run_id}
    _prune_done_flows(state)


def finish_older_than(state, born):
    """Closes every run started before the process was born: none of them outlived it.

    A run lives in the process that launched it — the script, the agents and
    the schedule are all in its memory — so a session that came back to a new
    process has no run going, whatever the transcript last said about it.
    """
    if not born:
        return
    for flow in list(state.flows.values()):
        if flow["status"] == "running" and flow.get("at") and flow["at"][:19] < born[:19]:
            finish(state, flow["id"], "killed", born)


def _prune_done_flows(state):
    if len(state.flows) <= MAX_ITEMS:
        return
    done = sorted((f for f in state.flows.values() if f["status"] != "running"),
                  key=lambda f: f.get("doneAt") or "")
    for flow in done:
        if len(state.flows) <= MAX_ITEMS:
            break
        state.flows.pop(flow["id"], None)


def fill(state, path):
    """Fills the runs with what the harness left on disk beside the transcript."""
    base = path[: -len(".jsonl")] if path.endswith(".jsonl") else path
    for flow in state.flows.values():
        _from_agents(flow)
        _from_snapshot(flow, os.path.join(base, "workflows", flow["id"] + ".json"))


def _from_agents(flow):
    """Counts the agents of a run and when one of them last wrote.

    This is the whole of what a running workflow says about itself: the
    snapshot with the phases and the log is written when it is over.
    """
    folder = flow.get("dir")
    if not folder:
        return
    try:
        names = os.listdir(folder)
    except OSError:
        return
    live = 0
    last = 0.0
    for name in names:
        if not name.startswith("agent-") or not name.endswith(".jsonl"):
            continue
        live += 1
        try:
            mtime = os.stat(os.path.join(folder, name)).st_mtime
        except OSError:
            continue
        last = max(last, mtime)
    if live > flow.get("agents", 0):
        flow["agents"] = live
    if last:
        flow["last"] = last


def _from_snapshot(flow, snap_path):
    """Reads the snapshot of a finished run: phases, log, result and duration."""
    try:
        stat = os.stat(snap_path)
    except OSError:
        return
    key = (stat.st_mtime_ns, stat.st_size)
    with _snap_lock:
        hit = _snap_cache.get(snap_path)
    if hit and hit[0] == key:
        data = hit[1]
    else:
        data = _read_snapshot(snap_path)
        with _snap_lock:
            _snap_cache[snap_path] = (key, data)
            if len(_snap_cache) > MAX_ITEMS * 4:
                _snap_cache.clear()
                _snap_cache[snap_path] = (key, data)
    if data is None:
        return
    flow.update(data)
    # The snapshot is written when the run is over, so it is also the word on
    # whether it is: a transcript that never got the notification — the panel
    # started reading after it, or the session died holding it — otherwise
    # leaves the run running for ever.
    if flow["status"] == "running" and data.get("status"):
        flow["status"] = data["status"]


def _read_snapshot(snap_path):
    try:
        with open(snap_path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    out = {
        "name": _short(data.get("workflowName")) or "",
        "status": str(data.get("status") or ""),
        "ms": data.get("durationMs") if isinstance(data.get("durationMs"), int) else 0,
        "phases": _phases(data.get("phases")),
        "logs": [_short(line) for line in (data.get("logs") or [])[-MAX_LOGS:]
                 if isinstance(line, str)],
        "result": _result(data.get("result")),
    }
    for key, field in (("agentCount", "agents"), ("totalTokens", "tokens"),
                       ("totalToolCalls", "calls")):
        value = data.get(key)
        if isinstance(value, int) and value:
            out[field] = value
    if not out["name"]:
        del out["name"]
    if not out["status"]:
        del out["status"]
    return out


def _phases(phases):
    if not isinstance(phases, list):
        return []
    out = []
    for phase in phases[:MAX_ITEMS]:
        if not isinstance(phase, dict):
            continue
        title = _short(phase.get("title"))
        if not title:
            continue
        out.append({"title": title, "detail": _short(phase.get("detail"))})
    return out


def _result(result):
    """The result of a run as one readable piece: a script returns anything at all."""
    if result is None or result == "":
        return ""
    if isinstance(result, str):
        text = result
    else:
        try:
            text = json.dumps(result, ensure_ascii=False, indent=1)
        except (TypeError, ValueError):
            text = str(result)
    text = text.strip()
    return text[:MAX_RESULT] if len(text) > MAX_RESULT else text


def seen(flow):
    """When the run was last heard from, for the order of the list."""
    return max(flow.get("at") or "", flow.get("event") or "", flow.get("doneAt") or "")


def outside(flow):
    """The run as the panel gets it: without what only the reading here needs."""
    out = {k: v for k, v in flow.items() if k != "last" and v not in ("", 0, [], None)}
    out.setdefault("id", flow["id"])
    out.setdefault("status", flow.get("status") or "running")
    return out
