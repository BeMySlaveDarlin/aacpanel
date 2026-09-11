"""Threads with their own pace and snapshots taken under a lock."""
import sys
import time

import chat
import models

import agent

from .live import sessions
from .probes import port_probes


def sessions_worker(shared, lock):
    """Collects sessions in a thread of its own."""
    while True:
        try:
            chat.observe_names()
        except Exception as e:  # noqa: BLE001
            print(f"aacpanel-agent: the names of the sessions: {e}", file=sys.stderr, flush=True)
        try:
            data = sessions()
        except Exception as e:  # noqa: BLE001
            data = {"sessions": [], "notes": [f"the session collector crashed: {e}"]}
            print(f"aacpanel-agent: the sessions: {e}", file=sys.stderr, flush=True)
        with lock:
            shared["at"] = int(time.time())
            shared["sessions"] = data.get("sessions", [])
            shared["notes"] = data.get("notes", [])
        time.sleep(agent.SESSIONS_EVERY)


def probe_worker(shared, lock):
    """Runs the probes in a thread of its own."""
    while True:
        try:
            items = port_probes()
        except Exception as e:  # noqa: BLE001
            items = []
            print(f"aacpanel-agent: the probes: {e}", file=sys.stderr, flush=True)
        with lock:
            shared["at"] = int(time.time())
            shared["items"] = items
        time.sleep(agent.PROBES_EVERY)


def models_worker(shared, lock):
    """Refreshes the model catalog in a thread of its own."""
    while True:
        try:
            data = models.catalog()
        except Exception as e:  # noqa: BLE001
            data = None
            print(f"aacpanel-agent: the catalogue of models: {e}", file=sys.stderr, flush=True)
        if data is not None:
            with lock:
                shared["catalog"] = data
        time.sleep(agent.MODELS_EVERY)


def models_snapshot(shared, lock):
    """Returns the last model catalog, or None while it has not been collected yet."""
    with lock:
        return shared["catalog"]


def probes_snapshot(shared, lock):
    """Returns the last probe results together with their own timestamp."""
    with lock:
        return {"at": shared["at"], "items": list(shared["items"])}
