#!/usr/bin/env python3

import contextlib
import importlib.util
import io
import json
import os
import pathlib
import sys
import tempfile
import threading
import time
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


guard = load("context-guard")
guards = load("guards")

PROJECT = "/srv/proj/Side/service/aacpanel"


class TestGuard(unittest.TestCase):

    def setUp(self):
        self.state_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.state_dir.cleanup)
        self.xdg = tempfile.TemporaryDirectory()
        self.addCleanup(self.xdg.cleanup)
        self.env = {"AACP_STATE_DIR": self.state_dir.name, "XDG_STATE_HOME": self.xdg.name,
                    "CLAUDE_PROJECT_DIR": None}
        self.places(f"{PROJECT}\t80\t1")

    def places(self, *lines):
        os.makedirs(os.path.join(self.xdg.name, "aacpanel"), exist_ok=True)
        with open(os.path.join(self.xdg.name, "aacpanel", "guards.tsv"), "w", encoding="utf-8") as f:
            f.write("".join(line + "\n" for line in lines))

    def snapshot(self, at=None, **row):
        session = {"sessionId": "mine", "session": "probe", "tokens": 840_000, "limit": 1_000_000,
                   "pct": 84.0, "limitKnown": True}
        session.update(row)
        data = {"sessions": [session]}
        if at is not None:
            data["sessionsAt"] = at
        path = os.path.join(self.state_dir.name, "state.json")
        with open(path + ".tmp", "w", encoding="utf-8") as f:
            json.dump(data, f)
        os.replace(path + ".tmp", path)

    def run_hook(self, payload=None, **env):
        payload = {"hook_event_name": "Stop", "session_id": "mine", "cwd": PROJECT,
                   "stop_hook_active": False, **(payload or {})}
        settings = {**self.env, **env}
        was = {k: os.environ.get(k) for k in settings}
        out = io.StringIO()
        try:
            for k, v in settings.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
            sys.stdin = io.StringIO(json.dumps(payload))
            with contextlib.redirect_stdout(out):
                guard.main()
        finally:
            sys.stdin = sys.__stdin__
            for k, v in was.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
        text = out.getvalue().strip()
        return json.loads(text) if text else None

    def test_past_the_cap_the_session_is_told_to_finalize_and_restart(self):
        self.snapshot()
        got = self.run_hook()
        self.assertEqual(got["decision"], "block")
        self.assertIn("84%", got["reason"])
        self.assertIn("has reached the 80%", got["reason"])
        self.assertIn("with session_restart, the tool of the panel's server", got["reason"])
        self.assertIn("mcp__aacpanel__session_restart), without continue", got["reason"])
        self.assertIn("Do not go on with this conversation", got["reason"])
        self.assertNotIn("skill with no flags", got["reason"])
        self.assertIn("do it silently", got["reason"])
        self.assertIn("nor that the next session will continue", got["reason"])

    def test_past_the_cap_the_restart_waits_for_the_work_in_the_background(self):
        self.patch_wait(0.3)
        for work in ({"agents": 1, "tasks": 0}, {"agents": 0, "tasks": 2}, {"workflows": 1}):
            self.snapshot(work=work, at=time.time() - 3)
            self.assertIsNone(self.run_hook(), f"a restart past the cap ends the work at {work}")

    def test_a_wake_up_does_not_hold_the_restart(self):
        self.snapshot(work={"agents": 0, "tasks": 1, "wakes": 1})
        self.assertEqual(self.run_hook()["decision"], "block",
                         "a session that set itself a wake-up was never restarted")

    def test_the_turn_that_heard_the_last_agent_is_done_restarts(self):
        # The snapshot on disk was written before the news; the collector's
        # next one knows the agent is done.
        self.patch_wait(5)
        self.snapshot(work={"agents": 1, "tasks": 0}, at=time.time() - 3)

        def collector():
            time.sleep(0.3)
            self.snapshot(work={"agents": 0, "tasks": 0}, at=time.time())

        t = threading.Thread(target=collector)
        t.start()
        self.addCleanup(t.join)
        got = self.run_hook()
        self.assertIsNotNone(got, "the turn that heard the last agent is done ended without the restart, "
                                  "and nothing asks again until the person writes")
        self.assertEqual(got["decision"], "block")

    def patch_wait(self, seconds):
        was = guard.background.FRESH_WAIT
        guard.background.FRESH_WAIT = seconds
        self.addCleanup(setattr, guard.background, "FRESH_WAIT", was)

    def aacpanel(self, *parts):
        return os.path.join(self.xdg.name, "aacpanel", *parts)

    def restart_marker(self, age=0):
        path = self.aacpanel("restarting", "probe")
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write("2026-01-01T00:00:00Z\n")
        at = time.time() - age
        os.utime(path, (at, at))
        return path

    def test_the_ask_is_sent_once_a_conversation(self):
        self.snapshot()
        self.assertEqual(self.run_hook()["decision"], "block")
        self.assertIsNone(self.run_hook(), "the stop the close of the restart lets through asked for "
                                           "a second restart, and a second session came up")
        self.snapshot(sessionId="next")
        self.assertEqual(self.run_hook({"session_id": "next"})["decision"], "block",
                         "the ask sent in one conversation silenced another")

    def test_no_ask_while_a_restart_of_the_session_is_under_way(self):
        self.snapshot()
        marker = self.restart_marker()
        self.assertIsNone(self.run_hook(), "a session being restarted was asked for another restart")
        os.remove(marker)
        self.assertEqual(self.run_hook()["decision"], "block",
                         "a stop held back by a restart under way counted as the ask sent")

    def test_a_restart_of_another_session_does_not_hold_the_ask(self):
        self.snapshot(session="other")
        self.restart_marker()
        self.assertEqual(self.run_hook()["decision"], "block")

    def test_a_stale_restart_marker_does_not_hold_the_ask(self):
        self.snapshot()
        marker = self.restart_marker(age=guard.RESTART_STALE + 60)
        self.assertEqual(self.run_hook()["decision"], "block",
                         "a marker a restart left behind silenced the guard for good")
        self.assertFalse(os.path.exists(marker), "the stale marker was left in place")

    def test_marks_older_than_a_week_are_pruned(self):
        folder = self.aacpanel("context-guard")
        os.makedirs(folder)
        for name, age in (("gone", guard.SENT_KEEP + 3600), ("recent", 3600)):
            open(os.path.join(folder, name), "w").close()
            os.utime(os.path.join(folder, name), (time.time() - age,) * 2)
        self.snapshot()
        self.assertEqual(self.run_hook()["decision"], "block")
        self.assertEqual(sorted(os.listdir(folder)), ["mine", "recent"])

    def test_under_the_cap_nothing_is_said(self):
        self.snapshot(pct=78.9, tokens=789_000)
        self.assertIsNone(self.run_hook())

    def test_a_turn_ending_just_short_of_the_cap_restarts(self):
        # A session wrapped up for the restart at 795k of a 1M window and ended its
        # turn: below the cap it would not stop again until someone wrote to it.
        self.snapshot(pct=79.5, tokens=795_000)
        got = self.run_hook()
        self.assertIsNotNone(got, "a session that ended its turn at 795k is left standing under the cap")
        self.assertEqual(got["decision"], "block")
        self.assertIn("80%", got["reason"])

    def test_the_cap_is_the_project_setting(self):
        self.snapshot(pct=61.0, tokens=610_000)
        self.assertIsNone(self.run_hook())
        self.places(f"{PROJECT}\t60\t1")
        self.assertEqual(self.run_hook()["decision"], "block")

    def test_without_auto_restart_past_the_cap_nothing_is_said(self):
        self.snapshot(pct=99.0)
        self.places(f"{PROJECT}\t80\t0")
        self.assertIsNone(self.run_hook())

    def test_a_directory_no_place_holds_is_not_guarded(self):
        self.snapshot(pct=99.0)
        self.assertIsNone(self.run_hook({"cwd": "/srv/elsewhere"}))
        self.assertIsNone(self.run_hook({"cwd": PROJECT + "-2"}))

    def test_the_project_directory_wins_over_where_the_agent_went(self):
        self.snapshot()
        got = self.run_hook({"cwd": "/srv/elsewhere"}, CLAUDE_PROJECT_DIR=PROJECT)
        self.assertEqual(got["decision"], "block")
        self.assertIsNone(self.run_hook({"cwd": PROJECT}, CLAUDE_PROJECT_DIR="/srv/elsewhere"))

    def test_a_directory_inside_the_project_is_the_project(self):
        self.snapshot()
        got = self.run_hook({"cwd": PROJECT + "/.claude/worktrees/agent-1"})
        self.assertEqual(got["decision"], "block")

    def test_without_the_guards_file_the_guard_is_off(self):
        self.snapshot(pct=99.0)
        os.remove(os.path.join(self.xdg.name, "aacpanel", "guards.tsv"))
        self.assertIsNone(self.run_hook())

    def test_the_turn_after_a_block_is_not_blocked_again(self):
        self.snapshot(pct=95.0)
        self.assertIsNone(self.run_hook({"stop_hook_active": True}))

    def test_a_window_that_is_not_known_does_not_trigger(self):
        self.snapshot(pct=95.0, limitKnown=False)
        self.assertIsNone(self.run_hook())

    def test_a_session_the_collector_does_not_know_does_not_trigger(self):
        self.snapshot()
        self.assertIsNone(self.run_hook({"session_id": "other"}))

    def test_without_a_snapshot_nothing_is_said(self):
        self.assertIsNone(self.run_hook())

    def test_a_subagent_stop_is_not_the_session(self):
        self.snapshot(pct=95.0)
        self.assertIsNone(self.run_hook({"hook_event_name": "SubagentStop"}))

    def test_a_broken_payload_is_not_a_crash(self):
        self.snapshot()
        os.environ["AACP_STATE_DIR"] = self.state_dir.name
        os.environ["XDG_STATE_HOME"] = self.xdg.name
        try:
            sys.stdin = io.StringIO("not json")
            with contextlib.redirect_stdout(io.StringIO()) as out:
                guard.main()
        finally:
            sys.stdin = sys.__stdin__
            os.environ.pop("AACP_STATE_DIR", None)
            os.environ.pop("XDG_STATE_HOME", None)
        self.assertEqual(out.getvalue(), "")


class TestPlaces(unittest.TestCase):

    def file(self, *lines):
        f = tempfile.NamedTemporaryFile("w", suffix=".tsv", delete=False, encoding="utf-8")
        self.addCleanup(os.remove, f.name)
        f.write("".join(line + "\n" for line in lines))
        f.close()
        return f.name

    def test_the_closest_place_above_wins(self):
        name = self.file("/srv/proj/Globex\t70\t1", "/srv/proj/Globex/shop\t90\t0")
        self.assertEqual(guards.of("/srv/proj/Globex/shop/src", name), (90, False))
        self.assertEqual(guards.of("/srv/proj/Globex/api", name), (70, True))
        self.assertEqual(guards.of("/srv/proj/Globex", name), (70, True))
        self.assertIsNone(guards.of("/srv/proj/GlobexX", name))

    def test_a_broken_line_is_passed_over(self):
        name = self.file("/opt/x\teighty\t1", "/opt/x", "/opt\t75\t0")
        self.assertEqual(guards.of("/opt/x", name), (75, False))

    def test_no_file_is_no_guard(self):
        self.assertIsNone(guards.of("/opt/x", "/nonexistent/guards.tsv"))


if __name__ == "__main__":
    unittest.main()
