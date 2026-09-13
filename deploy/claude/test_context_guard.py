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


class TestGuard(unittest.TestCase):

    def setUp(self):
        self.state_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.state_dir.cleanup)
        self.env = {"AACP_STATE_DIR": self.state_dir.name, guard.SETTING: "80"}

    def snapshot(self, **row):
        session = {"sessionId": "mine", "tokens": 840_000, "limit": 1_000_000,
                   "pct": 84.0, "limitKnown": True}
        session.update(row)
        with open(os.path.join(self.state_dir.name, "state.json"), "w", encoding="utf-8") as f:
            json.dump({"sessions": [session]}, f)

    def run_hook(self, payload=None, **env):
        payload = {"hook_event_name": "Stop", "session_id": "mine",
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

    def test_past_the_threshold_the_session_is_told_to_finalize_and_restart(self):
        self.snapshot()
        got = self.run_hook()
        self.assertEqual(got["decision"], "block")
        self.assertIn("84%", got["reason"])
        self.assertIn("past the 80%", got["reason"])
        self.assertIn("restart-session", got["reason"])
        self.assertIn("do not pass --continue", got["reason"])

    def test_under_the_threshold_nothing_is_said(self):
        self.snapshot(pct=79.9, tokens=799_000)
        self.assertIsNone(self.run_hook())

    def test_the_threshold_is_the_project_setting(self):
        self.snapshot(pct=61.0, tokens=610_000)
        self.assertIsNone(self.run_hook())
        self.assertEqual(self.run_hook(**{guard.SETTING: "60"})["decision"], "block")

    def test_without_the_setting_the_guard_is_off(self):
        self.snapshot(pct=99.0)
        self.assertIsNone(self.run_hook(**{guard.SETTING: None}))
        self.assertIsNone(self.run_hook(**{guard.SETTING: ""}))

    def test_a_setting_that_is_not_a_percentage_keeps_the_guard_off(self):
        self.snapshot(pct=99.0)
        for raw in ("yes", "0", "100", "150", "-5", "80%"):
            self.assertIsNone(self.run_hook(**{guard.SETTING: raw}), raw)

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
        was = os.environ.get(guard.SETTING)
        os.environ[guard.SETTING] = "80"
        os.environ["AACP_STATE_DIR"] = self.state_dir.name
        try:
            sys.stdin = io.StringIO("not json")
            with contextlib.redirect_stdout(io.StringIO()) as out:
                guard.main()
        finally:
            sys.stdin = sys.__stdin__
            os.environ.pop("AACP_STATE_DIR", None)
            if was is None:
                os.environ.pop(guard.SETTING, None)
        self.assertEqual(out.getvalue(), "")


if __name__ == "__main__":
    unittest.main()
