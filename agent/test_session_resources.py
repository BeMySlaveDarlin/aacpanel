import json
import os
import pathlib
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import agent  # noqa: E402
import ctx  # noqa: E402
from collect import resources  # noqa: E402

SLICE = "/user.slice/user-1000.slice/user@1000.service/aacp.slice/aacp-agents.slice"
UNIT = SLICE + "/run-p4321-i5678.service"


class SessionResources(unittest.TestCase):
    def setUp(self):
        self.root = pathlib.Path(self.enterContext(test_barrier.tmp_dir(prefix="session-resources-")))
        self.proc = self.root / "proc"
        self.cgroup = self.root / "cgroup"
        self.files = self.root / "sessions"
        self.files.mkdir()
        self.enterContext(patch.object(ctx, "PROC", str(self.proc)))
        self.enterContext(patch.object(agent, "CLAUDE_SESSIONS", str(self.files)))
        self.enterContext(patch.object(resources, "CGROUP", str(self.cgroup)))
        self.enterContext(patch.object(resources, "_previous", {}))
        self.enterContext(patch.object(ctx, "sessions", self.snapshot))
        self.enterContext(patch.object(ctx, "codex_sessions", return_value=[]))
        self.clock = self.enterContext(patch.object(resources.time, "monotonic", return_value=100))
        self.rows = []

    def write(self, path, text):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")

    def process(self, group=UNIT, pid=4321, kind="claude"):
        sid = f"session-{pid}"
        self.write(self.proc / str(pid) / "stat", f"{pid} (claude) S 1 " + "0 " * 17 + "5678 0 0\n")
        self.write(self.proc / str(pid) / "cgroup", f"0::{group}\n")
        self.write(self.files / f"{pid}.json", json.dumps({
            "pid": pid, "procStart": "5678", "sessionId": sid, "name": sid,
        }))
        self.rows.append({"session": sid, "sessionId": sid, "agent": kind})
        self.counters(group)
        return sid

    def counters(self, group=UNIT, mem=512 * 1024**2, used=900_000_000):
        root = self.cgroup / group.lstrip("/")
        self.write(root / "memory.current", f"{mem}\n")
        self.write(root / "cpu.stat", f"usage_usec {used}\nuser_usec 123\nsystem_usec 456\n")
        return root

    def snapshot(self):
        return {"sessions": [dict(row) for row in self.rows], "notes": []}

    def collect(self):
        return {row["sessionId"]: row for row in agent.sessions()["sessions"]}

    def test_memory_and_cpu_deltas_reach_the_session_snapshot(self):
        sid = self.process()
        self.assertEqual(self.collect()[sid]["res"], {"mem": 512 * 1024**2})
        self.clock.return_value = 104
        self.counters(mem=640 * 1024**2, used=904_160_000)
        self.assertEqual(self.collect()[sid]["res"], {"mem": 640 * 1024**2, "cpu": 104.0})
        self.clock.return_value = 106
        self.counters(used=904_226_666)
        self.assertEqual(self.collect()[sid]["res"], {"mem": 512 * 1024**2, "cpu": 3.3})
        self.clock.return_value = 108
        self.assertEqual(self.collect()[sid]["res"]["cpu"], 0.0)

    def test_only_a_sessions_own_unit_has_resource_readings(self):
        cases = [
            (UNIT, "claude", True),
            (SLICE + "/terminal.scope", "claude", True),
            (SLICE, "claude", False),
            ("/user.slice/user-1000.slice/user@1000.service", "claude", False),
            (SLICE + "/aacp-codex-personal.service", "claude", False),
            (SLICE + "/aacp-codex-personal.service", "codex", False),
            ("/user.slice/app.slice/run-p4321-i5678.service", "claude", False),
            (UNIT, "codex", False),
        ]
        expected = {}
        for i, (group, kind, own) in enumerate(cases):
            expected[self.process(group, pid=4321 + i, kind=kind)] = own
        rows = self.collect()
        for sid, own in expected.items():
            with self.subTest(session=sid):
                self.assertEqual("res" in rows[sid], own, rows[sid])

    def test_unavailable_or_invalid_counters_leave_the_session_without_readings(self):
        for i, (file, text) in enumerate([
            ("memory.current", None),
            ("cpu.stat", None),
            ("cpu.stat", "usage_usec invalid\n"),
            ("cpu.stat", "user_usec 123\n"),
            ("memory.current", "-1\n"),
        ]):
            group = SLICE + f"/run-p{i}-i1.service"
            sid = self.process(group, pid=5000 + i)
            path = self.cgroup / group.lstrip("/") / file
            if text is None:
                path.unlink()
            else:
                path.write_text(text, encoding="utf-8")
        for row in self.collect().values():
            self.assertNotIn("res", row)
        (self.proc / "5000" / "cgroup").unlink()
        self.assertNotIn("res", self.collect()["session-5000"])

    def test_a_reset_counter_or_reappearing_unit_starts_with_a_baseline(self):
        sid = self.process()
        self.collect()
        self.clock.return_value = 102
        self.counters(used=10)
        self.assertEqual(self.collect()[sid]["res"], {"mem": 512 * 1024**2})
        self.clock.return_value = 104
        self.counters(mem=0, used=20_010)
        self.assertEqual(self.collect()[sid]["res"], {"mem": 0, "cpu": 1.0})
        (self.files / "4321.json").unlink()
        self.rows.clear()
        self.assertEqual(self.collect(), {})
        self.clock.return_value = 106
        sid = self.process()
        self.assertEqual(self.collect()[sid]["res"], {"mem": 512 * 1024**2})


if __name__ == "__main__":
    unittest.main()
