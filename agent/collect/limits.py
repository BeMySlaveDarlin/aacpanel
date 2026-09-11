"""Claude subscription limits: the snapshots written by statusLine."""
import json
import os
import time

import contours

import agent


def limits_paths():
    """Returns the limit snapshots of every contour, the personal one first."""
    return contours.files("rate-limits.json", agent.LIMITS_PATH)


def limits_sources():
    """Returns triples of contour name, its config directory and its snapshot path."""
    if agent.LIMITS_PATH:
        return [(contours.PERSONAL, contours.HOME, agent.LIMITS_PATH)]
    return [(name, d, os.path.join(d, "rate-limits.json")) for name, d in contours.profiles()]


def limits():
    """Returns the claude subscription limits from the snapshots statusLine writes."""
    found = []
    for name, config_dir, path in limits_sources():
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        if not isinstance(data, dict):
            continue
        at = data.get("at")
        data["ageSec"] = max(0, int(time.time() - at)) if isinstance(at, (int, float)) else None
        data["profile"] = name
        data["configDir"] = config_dir
        found.append(data)
    if not found:
        return None
    head = dict(found[0])
    head["contours"] = found
    return head
