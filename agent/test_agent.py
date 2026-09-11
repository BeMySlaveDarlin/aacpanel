import json
import os
import shutil
import subprocess
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import agent  # noqa: E402
import asked  # noqa: E402
import ctx  # noqa: E402
import paths  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))

UUID_A = "11111111-1111-4111-8111-111111111111"

class NetLink(unittest.TestCase):
    def test_the_state_and_the_address_are_read_from_a_live_interface(self):
        live = [name for name in agent.net_counters()
                if agent.net_link(name)["addr"]]
        if not live:
            self.skipTest("the machine has no interface with an IPv4 address")

        link = agent.net_link(live[0])
        self.assertIn(link["state"], ("up", "down", "dormant", "unknown"))
        self.assertRegex(link["addr"], r"^\d+\.\d+\.\d+\.\d+$")
        self.assertGreaterEqual(link["speed"], 0)

    def test_a_missing_interface_does_not_break_the_collector(self):
        link = agent.net_link("no-such-interface")
        self.assertEqual(link["addr"], "")
        self.assertEqual(link["speed"], 0)

    def test_the_wifi_speed_does_not_turn_into_minus_one(self):
        for name in agent.net_counters():
            self.assertGreaterEqual(agent.net_link(name)["speed"], 0, name)


class LiveSessionWaits(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_path()
        self.addCleanup(shutil.rmtree, self.dir, ignore_errors=True)
        self._was = agent.CLAUDE_SESSIONS
        agent.CLAUDE_SESSIONS = self.dir
        self.addCleanup(setattr, agent, "CLAUDE_SESSIONS", self._was)
        self.pid = os.getpid()
        self.start = ctx.proc_start(self.pid)

    def write(self, name, file=None, **fields):
        data = {"pid": self.pid, "procStart": self.start, "name": name,
                "sessionId": f"uuid-{file or name}"}
        data.update(fields)
        with open(os.path.join(self.dir, f"{self.pid}-{file or name}.json"), "w") as f:
            json.dump(data, f)

    def test_a_waiting_session_names_the_reason(self):
        self.write("site", status="waiting", waitingFor="input needed")
        self.assertEqual(agent.live_session_waits(), {"site": "input needed"})

    def test_a_busy_session_is_not_waiting(self):
        self.write("aacpanel", status="busy")
        self.assertEqual(agent.live_session_waits(), {})

    def test_a_dead_file_does_not_count(self):
        self.write("site", status="waiting", waitingFor="input needed",
                   procStart="0")
        self.assertEqual(agent.live_session_waits(), {})

    def test_a_daemon_fork_does_not_shadow_its_namesake(self):
        self.write("site", status="waiting", waitingFor="input needed")
        self.write("site", file="site-fork", status="idle", kind="bg")
        self.assertEqual(agent.live_session_waits(), {"site": "input needed"})
        self.assertEqual(agent.live_session_status(), {"site": "waiting"})

    def test_a_reason_without_a_name_is_not_invented(self):
        self.write("site", status="waiting")
        self.assertEqual(agent.live_session_waits(), {})
        self.assertEqual(agent.live_session_status(), {"site": "waiting"})

    def test_all_five_reasons_arrive_as_they_are(self):
        reasons = ["dialog open", "input needed", "sandbox request",
                   "goal proposal", "worker request"]
        for i, reason in enumerate(reasons):
            self.write(f"s{i}", status="waiting", waitingFor=reason)
        self.assertEqual(agent.live_session_waits(),
                         {f"s{i}": r for i, r in enumerate(reasons)})

    def test_an_unknown_reason_is_not_dropped(self):
        self.write("site", status="waiting", waitingFor="brand new thing")
        self.assertEqual(agent.live_session_waits(), {"site": "brand new thing"})

    def test_a_reason_that_is_not_a_string_is_dropped(self):
        self.write("site", status="waiting", waitingFor={"kind": "dialog"})
        self.write("aacpanel", status="waiting", waitingFor="   ")
        self.assertEqual(agent.live_session_waits(), {})

    def test_the_time_of_the_status_change_is_read_next_to_the_status(self):
        self.write("site", status="waiting", waitingFor="input needed",
                   statusUpdatedAt=1788795235163)
        self.assertEqual(agent.live_session_status_at(), {"site": 1788795235163})

    def test_a_time_that_is_not_a_number_is_not_invented(self):
        self.write("a", status="waiting")
        self.write("b", status="waiting", statusUpdatedAt="yesterday")
        self.write("c", status="waiting", statusUpdatedAt=True)
        self.write("d", status="waiting", statusUpdatedAt=0)
        self.write("site", status="waiting", statusUpdatedAt=1788795235163)
        self.write("site", file="site-2", status="waiting")
        self.assertEqual(agent.live_session_status_at(), {"site": 1788795235163})




class Sessions(unittest.TestCase):
    def setUp(self):
        self.addCleanup(setattr, agent, "CLAUDE_SESSIONS", agent.CLAUDE_SESSIONS)
        self.root = test_barrier.tmp_path(prefix="sessions-")
        self.addCleanup(shutil.rmtree, self.root, True)
        agent.CLAUDE_SESSIONS = os.path.join(self.root, "sessions")
        os.makedirs(agent.CLAUDE_SESSIONS)
        self.addCleanup(setattr, agent.ctx, "sessions", agent.ctx.sessions)
        self.addCleanup(setattr, asked, "BOOK", asked.BOOK)
        asked.BOOK = asked.Book(os.path.join(self.root, "asked.json"))

    def rows(self, *rows):
        agent.ctx.sessions = lambda: {"sessions": list(rows), "notes": []}

    def test_no_external_process_is_started_for_the_sessions(self):
        self.addCleanup(setattr, subprocess, "run", subprocess.run)

        def deny(*a, **k):
            raise AssertionError(f"the sessions start an external process again: {a[:1]}")

        subprocess.run = deny
        self.rows({"session": "aacpanel", "sessionId": UUID_A, "pct": 12.0})
        self.assertEqual(agent.sessions()["sessions"][0]["pct"], 12.0)

    def test_the_sweep_of_the_questions_does_not_touch_the_store_of_the_machine(self):
        self.assertNotEqual(asked.BOOK.path, paths.state("asked.json"),
                            "the run writes to the question store of the machine")
        asked.BOOK.put({"sessionId": UUID_A, "toolUseId": "toolu_live",
                        "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                        "questions": [{"text": "?"}]})
        self.rows({"session": "aacpanel", "sessionId": UUID_A, "pct": 1.0})
        agent.sessions()
        self.assertIsNotNone(asked.BOOK.of(UUID_A), "the sweep dropped the question of a live session")

    def test_a_failed_calculation_travels_as_a_note(self):
        def boom():
            raise OSError("the transcripts are not readable")

        agent.ctx.sessions = boom
        data = agent.sessions()
        self.assertEqual(data["sessions"], [])
        self.assertTrue(data["notes"], "the failure to parse went unnoticed")

    def test_the_session_state_is_read_from_the_transcript_in_the_row(self):
        seen = []

        class Cache:
            def state(self, path):
                seen.append(path)
                return None

            def forget(self, alive):
                pass

        self.addCleanup(setattr, agent, "SESSION_STATE", agent.SESSION_STATE)
        agent.SESSION_STATE = Cache()
        path = f"/home/x/.claude/projects/-opt-p/{UUID_A}.jsonl"
        self.rows({"session": "aacpanel", "sessionId": UUID_A, "transcript": path})
        agent.sessions()
        self.assertEqual(seen, [path])

    def test_the_path_to_the_transcript_does_not_leak_outside(self):
        self.rows({"session": "aacpanel", "sessionId": UUID_A,
                   "transcript": f"/home/x/.claude/projects/-opt-p/{UUID_A}.jsonl"})
        row = agent.sessions()["sessions"][0]
        self.assertNotIn("transcript", row)
        self.assertEqual(row["sessionId"], UUID_A)

    def test_the_permission_mode_travels_in_the_row_as_it_is(self):
        self.rows({"session": "aacpanel", "sessionId": UUID_A,
                   "mode": "bypassPermissions", "modeAt": "2026-09-01T00:00:00Z"})
        row = agent.sessions()["sessions"][0]
        self.assertEqual(row["mode"], "bypassPermissions")
        self.assertEqual(row["modeAt"], "2026-09-01T00:00:00Z")

    def test_an_unknown_mode_stays_unknown(self):
        self.rows({"session": "aacpanel", "sessionId": UUID_A})
        self.assertNotIn("mode", agent.sessions()["sessions"][0])

    def test_the_time_of_the_status_change_travels_in_the_row_next_to_the_status(self):
        pid = os.getpid()
        with open(os.path.join(agent.CLAUDE_SESSIONS, f"{pid}.json"), "w") as f:
            json.dump({"pid": pid, "procStart": agent.ctx.proc_start(pid),
                       "name": "aacpanel", "sessionId": UUID_A,
                       "status": "waiting", "waitingFor": "input needed",
                       "statusUpdatedAt": 1788795235163}, f)
        self.rows({"session": "aacpanel", "sessionId": UUID_A})
        row = agent.sessions()["sessions"][0]
        self.assertEqual(row["status"], "waiting")
        self.assertEqual(row["statusUpdatedAt"], 1788795235163)
