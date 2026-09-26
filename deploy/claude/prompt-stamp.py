#!/usr/bin/env python3
"""Prompt stamp: date, context, limits and machine load for the model."""

import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import guards  # noqa: E402

BATCH_INTERVAL = 600
BATCH_STEP = 10

STALE_SEC = 120


def read_state(path):
    """Returns the collector snapshot, or None when there is none."""
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def human_bytes(value):
    """Formats bytes in gigabytes."""
    return f"{value / 1024 ** 3:.0f}G"


def line_context(state, session_id, cap=guards.CAP_DEFAULT):
    """Returns the context fill line of this session, with where its cap falls in its window."""
    for s in state.get("sessions", []):
        if s.get("sessionId") != session_id:
            continue
        pct = s.get("pct")
        tokens, limit = s.get("tokens"), s.get("limit")
        if pct is None or not limit:
            return None
        note = f"finalize from {round(limit * cap / 100 / 1000)}k"
        if not s.get("limitKnown", True):
            note += " · the model window is not exact"
        return f"Context:  {round(tokens / 1000)}k/{round(limit / 1000)}k ({pct:.0f}%) · {note}"
    return None


def line_limits(state, config_dir):
    """Returns the subscription limits line of the account this session runs under."""
    limits = state.get("limits") or {}
    want = os.path.basename(config_dir.rstrip("/")) if config_dir else ""
    rows = limits.get("contours") or [limits]
    row = None
    for candidate in rows:
        if want and candidate.get("profile") and want.endswith(candidate["profile"]):
            row = candidate
            break
    if row is None:
        row = rows[0] if rows else {}

    five = (row.get("fiveHour") or {}).get("pct")
    week = (row.get("sevenDay") or {}).get("pct")
    if five is None and week is None:
        return None
    age = row.get("ageSec")
    said = f"Limits:   5h {five}% · week {week}%"
    if isinstance(age, (int, float)) and age > STALE_SEC:
        said += f" (the snapshot is {round(age / 60)} min old)"
    return said


def line_resources(state):
    """Returns the machine load line."""
    host = state.get("host") or {}
    parts = []
    if host.get("cpuPct") is not None:
        parts.append(f"CPU {host['cpuPct']:.0f}%")
    mem = host.get("mem") or {}
    if mem.get("pct") is not None:
        parts.append(f"RAM {mem['pct']:.0f}%")
    if not parts:
        return None
    return "Load:     " + " · ".join(parts)


DISK_MIN_BYTES = 10 * 1024 ** 3


def line_disks(state):
    """Returns the disk usage line."""
    disks = [d for d in ((state.get("host") or {}).get("disks") or [])
             if (d.get("total") or 0) >= DISK_MIN_BYTES]
    disks.sort(key=lambda d: d.get("total", 0), reverse=True)
    said = [f"{d.get('mount', '?')} {human_bytes(d.get('used', 0))}/{human_bytes(d['total'])}"
            for d in disks[:3]]
    if not said:
        return None
    return "          Disks " + " · ".join(said)


def line_alarms(state):
    """Returns the alarms line, empty when there is nothing to say."""
    alarms = []
    host = state.get("host") or {}
    for d in (host.get("disks") or []):
        pct = d.get("pct")
        if isinstance(pct, (int, float)) and pct >= 90:
            alarms.append(f"DISK {d.get('mount', '?')} {pct:.0f}%")
    mem = host.get("mem") or {}
    if isinstance(mem.get("pct"), (int, float)) and mem["pct"] >= 90:
        alarms.append(f"MEMORY {mem['pct']:.0f}%")
    if not alarms:
        return None
    return "Alarms:   " + " · ".join(alarms)


def stamp(state, session_id, config_dir, with_date, cap=guards.CAP_DEFAULT):
    """Returns every line of the stamp, top to bottom."""
    lines = []
    if with_date:
        lines.append(time.strftime("[%a %d.%m.%Y %H:%M %z]"))
    if state is None:
        if with_date:
            lines.append("The host snapshot is unavailable: the aacpanel collector writes no state")
        return lines

    age = time.time() - (state.get("at") or 0)
    for line in (line_context(state, session_id, cap), line_limits(state, config_dir),
                 line_resources(state), line_disks(state), line_alarms(state)):
        if line:
            lines.append(line)
    if age > STALE_SEC and len(lines) > (1 if with_date else 0):
        lines.append(f"          the snapshot is {round(age / 60)} min old — is the collector stopped?")
    return lines


def throttle(state_dir, session_id, pct, alarms):
    """Reports whether the stamp should speak on this turn."""
    path = os.path.join(state_dir, f"stamp-{session_id or 'common'}.state")
    now = int(time.time())
    step = -1 if pct is None else int(pct) // BATCH_STEP
    mark = f"{step}:{alarms or ''}"

    last_ts, last_mark = 0, ""
    try:
        with open(path, encoding="utf-8") as f:
            raw = f.read().split("\n", 1)
            last_ts = int(raw[0])
            last_mark = raw[1] if len(raw) > 1 else ""
    except (OSError, ValueError, IndexError):
        pass

    if now - last_ts < BATCH_INTERVAL and mark == last_mark:
        return False
    try:
        tmp = path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            f.write(f"{now}\n{mark}")
        os.replace(tmp, path)
    except OSError:
        pass
    return True


def main():
    event = sys.argv[1] if len(sys.argv) > 1 else "UserPromptSubmit"
    state_dir = os.environ.get("AACP_STATE_DIR") or "/var/lib/aacpanel"

    session_id, payload = "", {}
    try:
        payload = json.load(sys.stdin)
        session_id = payload.get("session_id") or ""
    except (ValueError, OSError, AttributeError):
        pass

    # The cap of the project the session works in, as the panel's map says it.
    guard = guards.of(guards.where(payload))
    cap = guard[0] if guard else guards.CAP_DEFAULT

    state = read_state(os.path.join(state_dir, "state.json"))
    config_dir = os.environ.get("CLAUDE_CONFIG_DIR") or ""
    lines = stamp(state, session_id, config_dir, with_date=event == "UserPromptSubmit", cap=cap)

    if event != "UserPromptSubmit":
        pct = None
        for s in (state or {}).get("sessions", []):
            if s.get("sessionId") == session_id:
                pct = s.get("pct")
        if not throttle(state_dir, session_id, pct, line_alarms(state or {})):
            return
        lines = [l for l in lines if not l.startswith("[")]

    if not lines:
        return
    json.dump({"hookSpecificOutput": {
        "hookEventName": event,
        "additionalContext": "\n".join(lines),
    }}, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
