#!/usr/bin/env python3
"""What of a session is still at work in the background, as the panel's collector sees it.

A restart ends the session and everything it runs: its agents, its workflows
and the commands and watchers it sent to the background. The context guard and a restart asked
for by hand both wait while any of that is at work. A wake-up the session set
itself runs nothing and is not waited for.

As a command, for the session it runs in: exit 0 when nothing of it is at work,
exit 2 with the words of what is.
"""

import json
import os
import sys
import time

# The collector reads the sessions every six seconds and writes them out on
# its next five-second tick, so a snapshot on disk can be eleven seconds old.
# A turn that began with the news that the last agent is done can end before
# the snapshot has read that news: work the snapshot shows is looked at again
# in one read after the look began, or in the last one when none comes in time.
FRESH_WAIT = 15.0
STEP = 0.5


def state_path():
    """Returns the snapshot the collector writes."""
    return os.path.join(os.environ.get("AACP_STATE_DIR") or "/var/lib/aacpanel", "state.json")


def read_state(path=None):
    """Returns the collector snapshot, or None when there is none."""
    try:
        with open(path or state_path(), encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def _count(v):
    return v if isinstance(v, int) and not isinstance(v, bool) and v > 0 else 0


def of(state, session_id):
    """Returns (agents, tasks, workflows) of the session at work in the snapshot, wake-ups left out."""
    for s in (state or {}).get("sessions") or []:
        if not isinstance(s, dict) or s.get("sessionId") != session_id:
            continue
        work = s.get("work") if isinstance(s.get("work"), dict) else {}
        tasks = _count(work.get("tasks")) - _count(work.get("wakes"))
        return _count(work.get("agents")), max(tasks, 0), _count(work.get("workflows"))
    return 0, 0, 0


def _written(state):
    at = (state or {}).get("sessionsAt")
    return at if isinstance(at, (int, float)) and not isinstance(at, bool) else None


def at_work(session_id, path=None, wait=None, clock=time.time, sleep=time.sleep):
    """Returns (agents, tasks, workflows) of the session still at work."""
    since = clock()
    state = read_state(path)
    work = of(state, session_id)
    if not any(work):
        return work
    end = since + (FRESH_WAIT if wait is None else wait)
    while clock() < end:
        sleep(STEP)
        state = read_state(path)
        written = _written(state)
        if written is not None and written > since:
            return of(state, session_id)
    return work


def _plural(n, one, many):
    return f"{n} {one if n == 1 else many}"


def words(agents, tasks, flows=0):
    """Returns the work in words: 2 agents and 1 background task."""
    parts = []
    if agents:
        parts.append(_plural(agents, "agent", "agents"))
    if flows:
        parts.append(_plural(flows, "workflow", "workflows"))
    if tasks:
        parts.append(_plural(tasks, "background task", "background tasks"))
    if len(parts) < 2:
        return "".join(parts)
    return ", ".join(parts[:-1]) + " and " + parts[-1]


def wait_line(agents, tasks, flows=0):
    """Returns what a restart that waits says to the session asking for it."""
    one = agents + tasks + flows == 1
    return (f"{words(agents, tasks, flows)} of this session {'is' if one else 'are'} at work, and a restart "
            f"ends {'it' if one else 'them'} with the session. Do not restart now. A restart you started on your own: end the "
            "turn, and it is asked for again once they are done. A restart the person asked "
            "for: tell them what is at work, and restart with --anyway only when they say so.")


def main():
    session = os.environ.get("CLAUDE_CODE_SESSION_ID", "")
    if not session:
        return 0
    work = at_work(session)
    if any(work):
        print(f"WAIT {wait_line(*work)}")
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
