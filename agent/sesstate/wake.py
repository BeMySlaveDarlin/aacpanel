"""Alarms: the session set a prompt of its own to come back at a time.

Two tools set one. A wake-up is the next turn of the session, one at a time: a
new one takes the place of the last. A job of the session's cron is a prompt on
a cron expression, once or over and over, as many as the session sets, each by
the id claude gave it.
"""

import datetime as dt
import time

from .limits import _short

TASK_WAKE = "wake"
TASK_CRON = "cron"
WAKE_ID = "wakeup"

# What a restart does not wait for: an alarm runs nothing until it fires.
ALARMS = (TASK_WAKE, TASK_CRON)


def is_wakeup(record):
    """Reports whether the record is the one an alarm fired with.

    In a console claude names the scheduled task on the record; on the stream
    it leaves only the origin of the turn.
    """
    return (isinstance(record, dict) and record.get("type") == "user"
            and (bool(record.get("scheduledTaskId")) or record.get("turnOrigin") == "scheduled"))


def fired(state, record):
    """Takes off the alarm a firing is of: a job that fires once, or the wake-up.

    A job is found by the id a console names it by, or by its prompt, which is
    all the stream leaves: the firing says the prompt word for word. A job that
    repeats stays. A firing no job owns is the wake-up's.
    """
    job = state.tasks.get(record.get("scheduledTaskId") or "")
    if job is None or job.get("kind") != TASK_CRON:
        said = _short(_prompt_of(record))
        job = next((t for t in state.tasks.values()
                    if t.get("kind") == TASK_CRON and t.get("text") == said), None)
    if job is None:
        state.tasks.pop(WAKE_ID, None)
        return
    if not job.get("repeats"):
        state.tasks.pop(job["id"], None)


def _prompt_of(record):
    content = (record.get("message") or {}).get("content")
    if isinstance(content, list):
        return " ".join(b.get("text", "") for b in content
                        if isinstance(b, dict) and b.get("type") == "text")
    return content if isinstance(content, str) else ""


def scheduled(state, started, result):
    """Adds a job the session put on its cron.

    The schedule is claude's own words for it, the expression itself when it
    has none. A job that fires once on a pinned date is due then; one that
    repeats says how often, since the next of its times is the cron's to know.
    """
    job_id = result.get("id")
    if not job_id:
        return
    data = started["data"]
    cron = str(data.get("cron") or "")
    repeats = result.get("recurring")
    if not isinstance(repeats, bool):
        repeats = data.get("recurring") is not False
    state.tasks[job_id] = {
        "id": job_id, "text": _short(data.get("prompt")), "at": started["at"],
        "kind": TASK_CRON, "schedule": _short(result.get("humanSchedule") or cron),
        "repeats": repeats, "due": "" if repeats else _pinned(cron, started["at"]),
    }


def cancelled(state, result):
    """Takes off a job the session cancelled."""
    job = state.tasks.get(result.get("id") or "")
    if job is not None and job.get("kind") == TASK_CRON:
        state.tasks.pop(job["id"], None)


def _pinned(cron, at):
    """Returns when a job pinned to a minute, an hour, a day and a month fires next, or "".

    Claude reads the expression in the local time of the machine it runs on,
    and so does the collector, which runs beside it. The next match after the
    job was set is this year or, past it, the next.
    """
    fields = cron.split()
    if len(fields) != 5 or fields[4] != "*" or not all(f.isdigit() for f in fields[:4]):
        return ""
    minute, hour, day, month = (int(f) for f in fields[:4])
    if minute > 59 or hour > 23:
        return ""
    try:
        made = dt.datetime.fromisoformat(at.replace("Z", "+00:00")).timestamp()
    except (AttributeError, ValueError):
        return ""
    year = time.localtime(made).tm_year
    for when in (year, year + 1):
        try:
            dt.date(when, month, day)
        except ValueError:
            continue
        moment = time.mktime((when, month, day, hour, minute, 0, 0, 0, -1))
        if moment > made:
            return _iso(moment)
    return ""


def _iso(epoch):
    return dt.datetime.fromtimestamp(epoch, dt.timezone.utc).isoformat().replace("+00:00", "Z")


def _wake(state, started, result, at):
    due = ""
    if isinstance(result, dict) and result.get("scheduledFor"):
        try:
            due = _iso(int(result["scheduledFor"]) / 1000)
        except (TypeError, ValueError, OverflowError, OSError):
            due = ""
    state.tasks[WAKE_ID] = {
        "id": WAKE_ID, "text": started["text"], "at": started["at"] or at,
        "kind": TASK_WAKE, "due": due,
    }
