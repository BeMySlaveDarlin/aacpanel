"""Subscription limits: claude's from the snapshots written by statusLine, codex's from the executor."""
import json
import os
import sys
import time

import contours
import held
import paths

import agent

from .forecast import History

# The window codex calls weekly, in minutes. Codex names its windows by how
# long they last and nothing else, and the week is the one the panel shows
# beside the seven days of claude.
CODEX_WEEK_MINUTES = 7 * 24 * 60
CODEX_FIVE_HOUR_MINUTES = 5 * 60


def limits_paths():
    """Returns the limit snapshots of every contour, the personal one first."""
    return contours.files("rate-limits.json", agent.LIMITS_PATH)


def limits_sources():
    """Returns triples of contour name, its config directory and its snapshot path."""
    if agent.LIMITS_PATH:
        return [(contours.PERSONAL, contours.HOME, agent.LIMITS_PATH)]
    return [(name, d, os.path.join(d, "rate-limits.json")) for name, d in contours.profiles()]


def _number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def current(window, now):
    """Returns a window of claude's limits as it stands at now.

    A window whose reset has come has started over: the share the snapshot
    holds is of the window that ended, so until a new snapshot the window
    stands at nothing used, with no reset known.
    """
    if isinstance(window, dict) and _number(window.get("resetsAt")) and window["resetsAt"] <= now:
        return {"pct": 0, "resetsAt": None}
    return window


def codex_block(data, now=None):
    """Returns the limits of a codex account as the snapshot carries them.

    Every window codex told goes in "windows"; the five-hour and weekly ones
    also have the names of claude's windows when codex told them.
    """
    windows = []
    for key in ("primary", "secondary"):
        w = data.get(key)
        if not isinstance(w, dict) or not _number(w.get("usedPercent")):
            continue
        windows.append({
            "pct": w["usedPercent"],
            "resetsAt": w.get("resetsAt") if _number(w.get("resetsAt")) else None,
            "windowMins": w.get("windowDurationMins") if _number(w.get("windowDurationMins")) else None,
        })
    at = data.get("at") if _number(data.get("at")) else None
    block = {
        "at": at,
        "ageSec": max(0, int((now or time.time()) - at)) if at else None,
        "codexHome": data.get("codexHome") if isinstance(data.get("codexHome"), str) else "",
        "plan": data.get("plan") if isinstance(data.get("plan"), str) else "",
        "windows": windows,
    }
    for key, minutes in (("fiveHour", CODEX_FIVE_HOUR_MINUTES), ("sevenDay", CODEX_WEEK_MINUTES)):
        window = next((w for w in windows if w["windowMins"] == minutes), None)
        if window:
            block[key] = {"pct": window["pct"], "resetsAt": window["resetsAt"]}
    if isinstance(data.get("reached"), str) and data["reached"]:
        block["reached"] = data["reached"]
    return block


def limits():
    """Returns the subscription limits of every contour: claude's, and codex's where its account told them.

    A contour's row carries codex's limits under "codex"; a contour with codex
    limits and no snapshot of claude's gets a row of its own with codex's alone.
    """
    found = []
    now = time.time()
    for name, config_dir, path in limits_sources():
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        if not isinstance(data, dict):
            continue
        at = data.get("at")
        data["ageSec"] = max(0, int(now - at)) if isinstance(at, (int, float)) else None
        for key in ("fiveHour", "sevenDay"):
            if key in data:
                data[key] = current(data[key], now)
        data["profile"] = name
        data["configDir"] = config_dir
        found.append(data)
    codex = {data["contour"]: data for data in held.codex_limits()}
    for row in found:
        if row["profile"] in codex:
            row["codex"] = codex_block(codex.pop(row["profile"]))
    accounts = dict(contours.profiles())
    for contour, data in codex.items():
        row = {"profile": contour, "codex": codex_block(data)}
        if accounts.get(contour):
            row["configDir"] = accounts[contour]
        found.append(row)
    history = History(paths.state("limits-history.json"), now)
    for row in found:
        profile = row["profile"]
        for key in ("fiveHour", "sevenDay"):
            history.window((profile, "claude", key), row.get(key), row.get("at"))
        block = row.get("codex")
        if block:
            for i, window in enumerate(block["windows"]):
                key = str(window["windowMins"]) if window["windowMins"] else f"unknown-{i}"
                history.window((profile, "codex", key), window, block["at"])
            for key, minutes in (("fiveHour", CODEX_FIVE_HOUR_MINUTES), ("sevenDay", CODEX_WEEK_MINUTES)):
                window = next((w for w in block["windows"] if w["windowMins"] == minutes), None)
                if window and "forecast" in window:
                    block[key]["forecast"] = dict(window["forecast"])
    try:
        history.save()
    except OSError as e:
        print(f"aacpanel-agent: could not write limit history to {history.path}: {e}", file=sys.stderr, flush=True)
    if not found:
        return None
    head = dict(found[0])
    head["contours"] = found
    return head
