#!/usr/bin/env python3
"""Context fill of live sessions."""
import datetime as dt
import glob
import json
import os
import re

import archive
import chat
import contours
import held

SESSION_MODELS = os.environ.get("AACP_SESSION_MODELS")


def proc_start(pid):
    """Returns the start time of a process in ticks, from /proc/<pid>/stat."""
    try:
        with open(f"/proc/{pid}/stat") as f:
            return f.read().rsplit(")", 1)[1].split()[19]
    except (OSError, IndexError):
        return None


def _oneshot(pid, sid=None):
    """Says whether a process is a run of its own: a `-p` that no holder keeps."""
    try:
        with open(f"/proc/{pid}/cmdline", "rb") as f:
            cmdline = f.read().decode("utf-8", "replace").replace("\0", " ")
    except OSError:
        return False
    if not re.search(r"(^|\s)(-p|--print)(\s|$)", cmdline):
        return False
    return held.summary(sid, pid) is None


def _boot_time():
    """Returns when the machine was booted, in epoch seconds, or None when it is not told."""
    try:
        with open("/proc/stat") as f:
            for row in f:
                if row.startswith("btime "):
                    return float(row.split()[1])
    except (OSError, IndexError, ValueError):
        return None
    return None


def started_at(pid):
    """Returns when a process was born, in epoch seconds, or None when it is gone.

    The birth is counted from the ticks of /proc/<pid>/stat against the boot
    time, and never from the directory of the process: procfs stamps that
    directory when it builds the inode, at the first look, so a long living
    process nobody has looked at until now would pass for a newborn. Sessions
    lose their background tasks and their agents to such a birth.
    """
    ticks, boot = proc_start(pid), _boot_time()
    if ticks is None or boot is None:
        return None
    try:
        return boot + int(ticks) / os.sysconf("SC_CLK_TCK")
    except (TypeError, ValueError, ZeroDivisionError):
        return None


def _iso(ts):
    return dt.datetime.fromtimestamp(ts).astimezone().isoformat() if ts else None


def live_sessions():
    """Returns the live claude sessions of every contour: name, uuid, directory, transcript."""
    out = []
    files = []
    for root in archive.live_dirs():
        try:
            files.extend(glob.glob(os.path.join(root, "*.json")))
        except OSError:
            continue
    for path in sorted(files):
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        pid, sid, cwd = data.get("pid"), data.get("sessionId"), data.get("cwd") or ""
        if not pid or not sid or not cwd:
            continue
        start = proc_start(pid)
        if start is None or (data.get("procStart") and str(data["procStart"]) != start):
            continue
        if _oneshot(pid, sid) or archive.background(data):
            continue
        name = data.get("name") or os.path.basename(cwd.rstrip("/")) or cwd
        out.append({
            "name": name, "sessionId": sid, "cwd": cwd,
            "transcript": chat.transcript_path(sid),
            "procStartedAt": started_at(pid),
        })
    return out


def session_model_dirs():
    """Returns the directories of the status line snapshots of every contour."""
    return contours.dirs("session-models", SESSION_MODELS)


def status_line(sid):
    """Returns the model and the effort the status line last saw for the session, or None."""
    for d in session_model_dirs():
        try:
            with open(os.path.join(d, f"{sid}.json"), encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        if isinstance(data, dict) and isinstance(data.get("at"), (int, float)):
            return data
    return None


def _epoch(stamp):
    try:
        return dt.datetime.fromisoformat(stamp.replace("Z", "+00:00")).timestamp()
    except (AttributeError, ValueError):
        return None


def _model_and_effort(found, sid):
    """Returns the model and the effort of a live session.

    The transcript learns of a change of model or effort only with the next
    request; the status line snapshot knows at once. The snapshot wins while it
    is fresher than the last request, after that the transcript is the truth.
    """
    model, effort = found.get("model") or "", found.get("effort") or ""
    seen = status_line(sid)
    if not seen:
        return model, effort
    last = _epoch(found.get("lastRequestAt") or "")
    if last is not None and seen["at"] <= last:
        return model, effort
    picked = seen.get("model")
    picked = picked.get("id") if isinstance(picked, dict) else ""
    if "effort" in seen:
        effort = str(seen["effort"] or "")
    return picked or model, effort


def _mode(found):
    mode = found.get("mode") or ""
    if not mode:
        return {}
    return {"mode": mode, "modeAt": found.get("lastAt") or ""}


def _row(live):
    transcript = live["transcript"]
    found = archive.scan(transcript) if transcript else {}
    model, effort = _model_and_effort(found, live["sessionId"])
    limit, known = archive.limit_for(model)
    started = found.get("startedAt") or _iso(live["procStartedAt"])

    if not found.get("lastRequestAt"):
        return {
            "session": live["name"], "sessionId": live["sessionId"], "cwd": live["cwd"],
            "transcript": transcript,
            "tokens": 0, "limit": limit, "pct": 0.0, "limitKnown": known,
            "tokensIn": 0, "tokensOut": 0,
            "stale": False, "noRequests": True, "model": model,
            "effort": effort, "messages": found.get("messages") or 0,
            "compacts": found.get("compacts") or 0, "startedAt": started,
            "lastRequestAt": None,
            **_mode(found),
        }

    total = found.get("tokens") or 0
    if total > limit:
        limit, known = archive.DEFAULT_LIMIT_TOKENS, False
    pct = round(total / limit * 100, 1) if limit else 0.0
    return {
        "session": live["name"], "sessionId": live["sessionId"], "cwd": live["cwd"],
        "transcript": transcript,
        "tokens": total, "limit": limit, "pct": pct, "limitKnown": known,
        "tokensIn": found.get("tokensIn") or 0, "tokensOut": found.get("tokensOut") or 0,
        "stale": bool(found.get("stale")), "model": model,
        "effort": effort, "messages": found.get("messages") or 0,
        "compacts": found.get("compacts") or 0, "startedAt": started,
        "lastRequestAt": found.get("lastRequestAt"),
        **_mode(found),
    }


def sessions():
    """Returns the live sessions as {"sessions": [...], "notes": []}, the fullest first."""
    rows = [_row(live) for live in live_sessions()]
    rows.sort(key=lambda r: -r["pct"])
    return {"sessions": rows, "notes": []}
