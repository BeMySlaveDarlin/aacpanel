"""Availability probes: whether a port listens, and how fast it answered."""
import socket
import time

import agent


def port_probes():
    """Returns the availability of local ports."""
    out = []
    for item in agent.PROBE_PORTS.split(","):
        item = item.strip()
        if not item or "=" not in item:
            continue
        name, _, target = item.partition("=")
        host, _, port = target.rpartition(":")
        result = {
            "name": name,
            "kind": "tcp",
            "target": target,
            "intervalSec": int(agent.PROBES_EVERY),
            "timeoutSec": int(agent.PROBE_TIMEOUT),
            "latencyMs": None,
            "error": None,
        }
        try:
            port = int(port)
        except ValueError:
            out.append({**result, "ok": False, "outcome": "config", "error": f"cannot parse the port in {target!r}"})
            continue

        started = time.monotonic()
        try:
            with socket.create_connection((host, port), timeout=agent.PROBE_TIMEOUT):
                pass
            outcome, ok, err = "ok", True, None
        except socket.timeout:
            outcome, ok, err = "timeout", False, f"no answer in {agent.PROBE_TIMEOUT:g}s"
        except OSError as e:
            outcome, ok, err = "network", False, str(e)[:200]
        result["latencyMs"] = int((time.monotonic() - started) * 1000)
        out.append({**result, "ok": ok, "outcome": outcome, "error": err})
    return out
