"""Background tasks: a command, a Monitor watch and an agent sent off to work."""

import re

NOTIF_BLOCK_RE = re.compile(r"<task-notification>(.*?)</task-notification>", re.S)
NOTIF_USE_RE = re.compile(r"<tool-use-id>([^<]+)</tool-use-id>")
NOTIF_TASK_RE = re.compile(r"<task-id>([^<]+)</task-id>")
NOTIF_STATUS_RE = re.compile(r"<status>([^<]*)</status>")
NOTIF_EVENT_RE = re.compile(r"<event>(.*?)</event>", re.S)

DONE_STATUSES = ("completed", "failed", "killed", "cancelled", "stopped")

AACP_TIMEOUT = "[Monitor timed out"

MAYBE_BACKGROUND = ("Bash", "Monitor")

TASK_ID_KEYS = ("backgroundTaskId", "taskId")

TASK_BASH = "bash"
TASK_MONITOR = "aacpanel"
TASK_AGENT = "agent"

TASK_KIND_BY_KEY = {"backgroundTaskId": TASK_BASH, "taskId": TASK_MONITOR}

STOPPERS = ("TaskStop",)


def _task(state, use, task_id, started, text="", kind=TASK_BASH):
    if not task_id:
        return
    state.task_ids[use] = task_id
    state.tasks[task_id] = {
        "id": task_id, "text": started["text"] or text, "at": started["at"],
        "kind": kind, "line": _screen_line(started, kind),
    }


def _screen_line(started, kind):
    if kind == TASK_BASH:
        return started.get("cmd", "")
    if kind == TASK_MONITOR:
        return started.get("desc", "")
    return ""


def _notify_tasks(state, body, at):
    status = [s.strip() for s in NOTIF_STATUS_RE.findall(body)]
    event = " ".join(NOTIF_EVENT_RE.findall(body))
    targets = [state.task_ids.get(t, t) for t in NOTIF_USE_RE.findall(body)]
    targets += NOTIF_TASK_RE.findall(body)
    if any(s in DONE_STATUSES for s in status) or AACP_TIMEOUT in event:
        for tool_id in NOTIF_USE_RE.findall(body):
            state.task_ids.pop(tool_id, None)
        for task_id in targets:
            state.tasks.pop(task_id, None)
        return
    if not event:
        return
    for task_id in targets:
        task = state.tasks.get(task_id)
        if task is not None:
            task["event"] = at
