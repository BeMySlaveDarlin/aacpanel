"""Alarm: the session scheduled its own next turn and stays silent until then."""

import datetime as dt

TASK_WAKE = "wake"
WAKE_ID = "wakeup"


def is_wakeup(record):
    """Reports whether the record is the one an alarm fired with."""
    return (isinstance(record, dict) and record.get("type") == "user"
            and bool(record.get("scheduledTaskId")))


def _wake(state, started, result, at):
    due = ""
    if isinstance(result, dict) and result.get("scheduledFor"):
        try:
            due = dt.datetime.fromtimestamp(int(result["scheduledFor"]) / 1000,
                                            dt.timezone.utc).isoformat().replace("+00:00", "Z")
        except (TypeError, ValueError, OverflowError, OSError):
            due = ""
    state.tasks[WAKE_ID] = {
        "id": WAKE_ID, "text": started["text"], "at": started["at"] or at,
        "kind": TASK_WAKE, "due": due,
    }
