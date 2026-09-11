#!/usr/bin/env python3
"""Context fill of live sessions."""
import datetime as dt
import glob
import json
import os
import re

import archive
import chat


def proc_start(pid):
    """Returns the start time of a process in ticks, from /proc/<pid>/stat."""
    try:
        with open(f"/proc/{pid}/stat") as f:
            return f.read().rsplit(")", 1)[1].split()[19]
    except (OSError, IndexError):
        return None


def _oneshot(pid):
    try:
        with open(f"/proc/{pid}/cmdline", "rb") as f:
            cmdline = f.read().decode("utf-8", "replace").replace("\0", " ")
    except OSError:
        return False
    return bool(re.search(r"(^|\s)(-p|--print)(\s|$)", cmdline))


def _started_at(pid):
    try:
        return os.stat(f"/proc/{pid}").st_mtime
    except OSError:
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
        if _oneshot(pid) or archive.background(data):
            continue
        name = data.get("name") or os.path.basename(cwd.rstrip("/")) or cwd
        out.append({
            "name": name, "sessionId": sid, "cwd": cwd,
            "transcript": chat.transcript_path(sid),
            "procStartedAt": _started_at(pid),
        })
    return out


def _mode(found):
    mode = found.get("mode") or ""
    if not mode:
        return {}
    return {"mode": mode, "modeAt": found.get("lastAt") or ""}


def _row(live):
    transcript = live["transcript"]
    found = archive.scan(transcript) if transcript else {}
    model = found.get("model") or ""
    limit, known = archive.limit_for(model)
    started = found.get("startedAt") or _iso(live["procStartedAt"])

    if not found.get("lastRequestAt"):
        return {
            "session": live["name"], "sessionId": live["sessionId"], "cwd": live["cwd"],
            "transcript": transcript,
            "tokens": 0, "limit": limit, "pct": 0.0, "limitKnown": known,
            "stale": False, "noRequests": True, "model": model,
            "effort": found.get("effort") or "", "messages": found.get("messages") or 0,
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
        "stale": bool(found.get("stale")), "model": model,
        "effort": found.get("effort") or "", "messages": found.get("messages") or 0,
        "compacts": found.get("compacts") or 0, "startedAt": started,
        "lastRequestAt": found.get("lastRequestAt"),
        **_mode(found),
    }


def sessions():
    """Returns the live sessions as {"sessions": [...], "notes": []}, the fullest first."""
    rows = [_row(live) for live in live_sessions()]
    rows.sort(key=lambda r: -r["pct"])
    return {"sessions": rows, "notes": []}
