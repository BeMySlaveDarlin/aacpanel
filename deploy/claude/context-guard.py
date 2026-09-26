#!/usr/bin/env python3
"""Context guard: past its cap a session with Auto restart is told to wrap up and restart itself."""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import guards  # noqa: E402


def read_state(path):
    """Returns the collector snapshot, or None when there is none."""
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def fill(state, session_id):
    """Returns the context fill of the session in percent, or None when it is not known well."""
    for s in (state or {}).get("sessions", []):
        if s.get("sessionId") != session_id:
            continue
        pct = s.get("pct")
        if pct is None or not s.get("limit") or not s.get("limitKnown", True):
            return None
        return float(pct)
    return None


def reason(pct, cap):
    """Returns what the model is told instead of stopping."""
    return (f"The context of this session is at {pct:.0f}% of the model window, past the {cap}% "
            "this project set as its context cap, and the project restarts its sessions there. "
            "Take no new work. Put the state of the work on disk the way this project keeps it: "
            "its finalize skill if it has one, otherwise a handoff note for the next session and "
            "a commit of what is done. Then run the restart-session skill with no flags: it starts "
            "a fresh session in this place with the project's parameters. Do not continue this "
            "conversation and do not pass --continue.")


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
    pct = fill(read_state(os.path.join(state_dir, "state.json")), payload.get("session_id") or "")
    if pct is None or pct < cap:
        return
    json.dump({"decision": "block", "reason": reason(pct, cap)}, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
    sys.exit(0)
