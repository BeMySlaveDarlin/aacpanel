"""Background tasks: a command, a Monitor watch and an agent sent off to work."""

import re

from .limits import MAX_ITEMS
from .subagents import _stamp

NOTIF_BLOCK_RE = re.compile(r"<task-notification>(.*?)</task-notification>", re.S)
NOTIF_USE_RE = re.compile(r"<tool-use-id>([^<]+)</tool-use-id>")
NOTIF_TASK_RE = re.compile(r"<task-id>([^<]+)</task-id>")
NOTIF_STATUS_RE = re.compile(r"<status>([^<]*)</status>")
NOTIF_EVENT_RE = re.compile(r"<event>(.*?)</event>", re.S)

DONE_STATUSES = ("completed", "failed", "killed", "cancelled", "stopped")

# The end of a watch arrives as an ordinary event: the notification of a
# monitor carries no status, and its closing line is the only sign the watch
# is over. The wording of that line belongs to the harness and has moved
# before, so the end is read by the shape of the line — a bracketed word from
# the monitor that either names the end or asks for a re-arm. Miss it and the
# watch never leaves the list: a session that re-arms every half hour shows
# dozens of live watches over two.
MONITOR_OVER_RE = re.compile(
    r"\[Monitor\b[^]]*?(?:\btimed out\b|\bexpired\b|\bre-arm\b)", re.I)

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
    # A shell of a subagent stands in the same list as the session's own: the
    # screen of the session counts it among the ones it holds. The name of the
    # agent is what tells them apart — three identical waits with nobody to
    # attribute them to are one wait shown three times to the reader.
    state.tasks[task_id] = {
        "id": task_id, "text": started["text"] or text, "at": started["at"],
        "kind": kind, "line": _screen_line(started, kind), "done": False,
        "agent": started.get("agent", ""),
    }
    # The state holds no more than the list that goes out: over the limit the
    # shells that finished first give way, the same ones the snapshot cuts.
    _prune_done_tasks(state)


def finish(state, task_id, at):
    """Closes a task: a shell stays in the list, everything else leaves it.

    The session keeps a finished shell around — its output is still readable,
    and the screen of the session counts it among the ones it has. A watch and
    an agent sent off to work have nothing to come back to, so they go.
    """
    task = state.tasks.get(task_id)
    if task is None:
        return
    if task.get("kind") != TASK_BASH:
        state.tasks.pop(task_id, None)
        return
    task["done"] = True
    task["doneAt"] = at or task.get("doneAt") or ""
    _prune_done_tasks(state)


def _finish_older_than(state, born):
    """Closes every open task started before the process was born: none of them outlived it.

    A shell, a watch and an agent sent off to work live in the process that
    started them, and so does an alarm the session set for itself: the
    schedule is kept in the memory of the process, and a new one knows
    nothing of it. The birth is the latest moment any of them could still
    have been alive, and the only end the transcript gives for them.
    """
    if not born:
        return
    since = _stamp(born)
    gone = [task["id"] for task in state.tasks.values()
            if not task.get("done") and task.get("at") and task["at"][:19] < since[:19]]
    for task_id in gone:
        finish(state, task_id, since)


def _prune_done_tasks(state):
    if len(state.tasks) <= MAX_ITEMS:
        return
    done = sorted(
        (t for t in state.tasks.values() if t.get("done")),
        key=lambda t: t.get("doneAt") or "",
    )
    for task in done:
        if len(state.tasks) <= MAX_ITEMS:
            break
        state.tasks.pop(task["id"], None)


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
    if any(s in DONE_STATUSES for s in status) or MONITOR_OVER_RE.search(event):
        for tool_id in NOTIF_USE_RE.findall(body):
            state.task_ids.pop(tool_id, None)
        for task_id in targets:
            finish(state, task_id, at)
        return
    if not event:
        return
    for task_id in targets:
        task = state.tasks.get(task_id)
        if task is not None:
            task["event"] = at
