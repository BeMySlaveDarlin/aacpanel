#!/usr/bin/env python3
"""Context guard: past its cap a session with Auto restart is told to wrap up and restart itself.

Not while its agents, workflows or the commands it sent to the background are
at work: a restart would end them. The news that one of them is done starts a turn of its
own, and the end of that turn asks again.

Not while a restart of the session is under way either, and once a conversation: the
close of a restart lets the session stop once more, and an ask there would bring up a
second session.
"""

import json
import os
import sys
import time
import urllib.parse

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import background  # noqa: E402
import guards  # noqa: E402


# A restart marker older than this was left by a restart that never ended, an
# executor gone down mid-way, and no longer holds the ask back.
RESTART_STALE = 5 * 60

# A mark that the ask was sent is kept this long, past the life of a conversation.
SENT_KEEP = 7 * 24 * 3600


def read_state(path):
    """Returns the collector snapshot, or None when there is none."""
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def row(state, session_id):
    """Returns what the snapshot says of the session, or an empty dict."""
    for s in (state or {}).get("sessions", []):
        if s.get("sessionId") == session_id:
            return s
    return {}


def fill(state, session_id):
    """Returns the context fill of the session in percent, or None when it is not known well."""
    s = row(state, session_id)
    pct = s.get("pct")
    if pct is None or not s.get("limit") or not s.get("limitKnown", True):
        return None
    return float(pct)


def restarting(name):
    """Reports whether a restart of the named session is under way.

    The executor keeps a marker while it restarts a session, named by the
    session. A stale one is cleared.
    """
    if not name:
        return False
    marker = os.path.join(guards.home(), "restarting", urllib.parse.quote(name, safe=""))
    try:
        at = os.stat(marker).st_mtime
    except OSError:
        return False
    if time.time() - at < RESTART_STALE:
        return True
    try:
        os.remove(marker)
    except OSError:
        pass
    return False


def sent_mark(session_id):
    """Returns the file that marks the ask as sent in the conversation."""
    return os.path.join(guards.home(), "context-guard", urllib.parse.quote(session_id, safe=""))


def mark_sent(session_id):
    """Marks the ask as sent in the conversation and prunes the marks past SENT_KEEP.

    A mark that cannot be written leaves the ask to repeat, and the executor
    still refuses a second restart while one is under way.
    """
    mark = sent_mark(session_id)
    folder = os.path.dirname(mark)
    try:
        os.makedirs(folder, mode=0o700, exist_ok=True)
        old = time.time() - SENT_KEEP
        for entry in os.scandir(folder):
            if entry.stat().st_mtime < old:
                os.remove(entry.path)
        with open(mark, "w", encoding="utf-8"):
            pass
    except OSError:
        pass


def reason(pct, cap):
    """Returns what the model is told instead of stopping."""
    return (f"The context of this session is at {pct:.0f}% of the model window, past the {cap}% "
            "this project set as its context cap, and the project restarts its sessions there. "
            "Take no new work. Put the state of the work on disk the way this project keeps it: "
            "its finalize skill if it has one, otherwise a handoff note for the next session and "
            "a commit of what is done. Then restart this session with session_restart, the tool of "
            "the panel's server (mcp__aacpanel__session_restart), without continue: the panel starts "
            "a fresh session in this place with the project's parameters. Do not go on with this "
            "conversation. The restart is automatic: do it silently, and do not tell the person "
            "about the context, the cap or the restart, nor that the next session will continue.")


def main():
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        return
    if not isinstance(payload, dict) or payload.get("hook_event_name", "Stop") != "Stop":
        return
    # The turn that follows a block is the finalization itself: it is never blocked again.
    if payload.get("stop_hook_active"):
        return
    guard = guards.of(guards.where(payload))
    if guard is None:
        return
    cap, restart = guard
    if not restart:
        return
    state_dir = os.environ.get("AACP_STATE_DIR") or "/var/lib/aacpanel"
    session_id = payload.get("session_id") or ""
    state = read_state(os.path.join(state_dir, "state.json"))
    pct = fill(state, session_id)
    if pct is None or pct < cap:
        return
    if any(background.at_work(session_id, os.path.join(state_dir, "state.json"))):
        return
    if restarting(row(state, session_id).get("session")):
        return
    if os.path.exists(sent_mark(session_id)):
        return
    mark_sent(session_id)
    json.dump({"decision": "block", "reason": reason(pct, cap)}, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
    sys.exit(0)
