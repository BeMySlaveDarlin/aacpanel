"""Read the public Claude Code background-session catalogue, once per collection interval."""
import json
import os
import subprocess
import threading
import time

TIMEOUT = 3
INTERVAL = float(os.environ.get("AACP_SESSIONS_INTERVAL", "6"))
_cache = {}
_lock = threading.Lock()


def program():
    native = os.path.expanduser("~/.local/bin/claude")
    return native if os.access(native, os.X_OK) else None


def catalogue(config):
    with _lock:
        now = time.monotonic()
        cached = _cache.get(config)
        if cached and now - cached[0] < INTERVAL:
            return cached[1]
        rows = []
        binary = program()
        if binary:
            env = {k: v for k, v in os.environ.items() if not k.startswith("CLAUDE")}
            env["CLAUDE_CONFIG_DIR"] = config
            try:
                result = subprocess.run([binary, "agents", "--json"], env=env,
                                        capture_output=True, text=True, timeout=TIMEOUT, check=True)
                parsed = json.loads(result.stdout)
                if isinstance(parsed, list):
                    rows = [r for r in parsed if isinstance(r, dict) and r.get("kind") == "background"]
            except (OSError, subprocess.SubprocessError, ValueError):
                pass
        _cache[config] = (time.monotonic(), rows)
        return rows


def row(live):
    found = next((r for r in catalogue(live["configDir"])
                  if r.get("pid") == live["pid"] and r.get("sessionId") == live["sessionId"]), {})
    state, status = found.get("state"), found.get("status")
    if state == "blocked":
        status = "waiting"
    elif state == "working" or status == "busy":
        status = "busy"
    elif status != "idle":
        status = "unknown"
    return {"transport": "bg", "outside": True, "status": status,
            **({"waitingFor": "input in Claude Code"} if status == "waiting" else {})}
