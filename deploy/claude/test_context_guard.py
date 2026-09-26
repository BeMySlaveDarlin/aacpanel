#!/usr/bin/env python3

import contextlib
import importlib.util
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


guard = load("context-guard")
guards = load("guards")

PROJECT = "/srv/proj/Pets/service/aacpanel"


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

    def snapshot(self, **row):
        session = {"sessionId": "mine", "tokens": 840_000, "limit": 1_000_000,
                   "pct": 84.0, "limitKnown": True}
        session.update(row)
        with open(os.path.join(self.state_dir.name, "state.json"), "w", encoding="utf-8") as f:
            json.dump({"sessions": [session]}, f)

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
        self.assertIn("past the 80%", got["reason"])
        self.assertIn("restart-session", got["reason"])
        self.assertIn("do not pass --continue", got["reason"])

    def test_under_the_cap_nothing_is_said(self):
        self.snapshot(pct=79.9, tokens=799_000)
        self.assertIsNone(self.run_hook())

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
        name = self.file("/srv/proj/Algo\t70\t1", "/srv/proj/Algo/lms\t90\t0")
        self.assertEqual(guards.of("/srv/proj/Algo/lms/src", name), (90, False))
        self.assertEqual(guards.of("/srv/proj/Algo/ai-platform", name), (70, True))
        self.assertEqual(guards.of("/srv/proj/Algo", name), (70, True))
        self.assertIsNone(guards.of("/srv/proj/AlgoX", name))

    def test_a_broken_line_is_passed_over(self):
        name = self.file("/opt/x\teighty\t1", "/opt/x", "/opt\t75\t0")
        self.assertEqual(guards.of("/opt/x", name), (75, False))

    def test_no_file_is_no_guard(self):
        self.assertIsNone(guards.of("/opt/x", "/nonexistent/guards.tsv"))


if __name__ == "__main__":
    unittest.main()
