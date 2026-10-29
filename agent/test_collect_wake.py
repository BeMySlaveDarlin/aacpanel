import os
import sys
import threading
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import agent  # noqa: E402
from collect import loop  # noqa: E402


class SnapshotWake(unittest.TestCase):
    def test_a_session_pass_publishes_without_waiting_for_the_tick(self):
        publish = threading.Event()
        threads = []
        now = [100.0]
        states = []
        spans = []
        cpu = iter(((0, 0), (2, 4), (3, 8)))
        net = iter(({"eth0": (0, 0)}, {"eth0": (100, 0)}, {"eth0": (150, 0)}))

        def thread(**args):
            threads.append(args)
            return SimpleNamespace(start=lambda: None)

        def wait(timeout):
            self.assertEqual(timeout, agent.INTERVAL)
            self.assertIs(threads[0]["target"], loop.sessions_worker)
            shared, _, event = threads[0]["args"]
            self.assertIs(event, publish, "the session reader cannot wake the publisher")
            shared["sessions"] = [{"session": "lab", "pinnedAt": "stamp"}]
            now[0] += 0.5 if not states else 1.0
            publish.set()
            return True

        def write(state):
            self.assertFalse(publish.is_set(), "the next pass would spin on an old wake")
            states.append(state)
            if len(states) == 2:
                raise StopIteration

        def procs(prev, cur, elapsed):
            spans.append(elapsed)
            return {}

        replacements = {
            "threading": SimpleNamespace(Event=lambda: publish, Lock=threading.Lock, Thread=thread),
            "time": SimpleNamespace(monotonic=lambda: now[0], time=lambda: now[0]),
            "projects": SimpleNamespace(scan=lambda: []),
            "cpu_jiffies": lambda: next(cpu),
            "net_counters": lambda: next(net),
            "net_link": lambda name: {},
            "proc_sample": lambda: {},
            "proc_top": procs,
            "read": lambda path: "0 0 0" if path == "/proc/loadavg" else "1000",
            "temperatures": lambda: {},
            "mem_info": lambda: {},
            "disks": lambda: [],
            "limits": lambda: {},
            "contours": SimpleNamespace(described=lambda: []),
            "models_snapshot": lambda *args: None,
            "probes_snapshot": lambda *args: {},
            "held": SimpleNamespace(codex_releases=lambda: [], programs=lambda: []),
            "write_state": write,
        }
        with patch.multiple(loop, **replacements), patch.object(publish, "wait", wait):
            with self.assertRaises(StopIteration):
                loop.main()
        self.assertEqual(spans, [0.5, 1.0])
        self.assertEqual([s["host"]["cpuPct"] for s in states], [50.0, 25.0])
        self.assertEqual([s["host"]["net"][0]["rxRate"] for s in states], [200, 50])
        self.assertEqual(states[0]["sessions"], [{"session": "lab", "pinnedAt": "stamp"}])
