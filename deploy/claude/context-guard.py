#!/usr/bin/env python3
"""Context guard: past the threshold a session is told to finalize and restart itself."""

import json
import os
import sys

SETTING = "AACP_FINALIZE_AT"


def threshold():
    """Returns the percentage the project set, or None when the guard is off here."""
    raw = os.environ.get(SETTING, "").strip()
    if not raw:
        return None
    try:
        value = int(raw)
    except ValueError:
        return None
    return value if 0 < value < 100 else None


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


def reason(pct, at):
    """Returns what the model is told instead of stopping."""
    return (f"The context of this session is at {pct:.0f}% of the model window, past the {at}% "
            "this project set as the point to finalize. Take no new work. Put the state of the "
            "work on disk the way this project keeps it: its finalize skill if it has one, "
            "otherwise a handoff note for the next session and a commit of what is done. Then "
            "run the restart-session skill with no flags: it starts a fresh session in this "
            "place with a clean context. Do not continue this conversation and do not pass "
            "--continue.")


def main():
    at = threshold()
    if at is None:
        return
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        return
    if not isinstance(payload, dict) or payload.get("hook_event_name", "Stop") != "Stop":
        return
    # The turn that follows a block is the finalization itself: it is never blocked again.
    if payload.get("stop_hook_active"):
        return
    state_dir = os.environ.get("AACP_STATE_DIR") or "/var/lib/aacpanel"
    pct = fill(read_state(os.path.join(state_dir, "state.json")), payload.get("session_id") or "")
    if pct is None or pct < at:
        return
    json.dump({"decision": "block", "reason": reason(pct, at)}, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
    sys.exit(0)
