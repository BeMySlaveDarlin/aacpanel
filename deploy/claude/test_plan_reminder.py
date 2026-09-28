#!/usr/bin/env python3

import contextlib
import datetime
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


reminder = load("plan-reminder")

SESSION = "5a0c7d1e-2b3f-4a5b-8c6d-7e8f9a0b1c2d"

# The turn under test begins here; the plan is written a minute before it or
# half a minute into it.
BEGAN = "2026-09-28T10:00:00.000Z"
BEGAN_AT = datetime.datetime(2026, 9, 28, 10, 0, tzinfo=datetime.timezone.utc).timestamp()


def prompt(text, at=BEGAN):
    return {"type": "user", "timestamp": at, "message": {"role": "user", "content": text}}


def call(name, at="2026-09-28T10:00:05.000Z", use="toolu_1"):
    return {"type": "assistant", "timestamp": at,
            "message": {"role": "assistant", "content": [{"type": "tool_use", "id": use, "name": name, "input": {}}]}}


def result(use="toolu_1", at="2026-09-28T10:00:06.000Z"):
    return {"type": "user", "timestamp": at,
            "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": use, "content": "ok"}]}}


def answer(text="done", at="2026-09-28T10:00:40.000Z"):
    return {"type": "assistant", "timestamp": at,
            "message": {"role": "assistant", "content": [{"type": "text", "text": text}]}}


class TestReminder(unittest.TestCase):

    def setUp(self):
        self.xdg = tempfile.TemporaryDirectory()
        self.addCleanup(self.xdg.cleanup)
        self.transcript = os.path.join(self.xdg.name, "transcript.jsonl")
        # The claude that runs the hook is above it; here the test is that
        # process, and its parent stands for claude.
        self.claude = os.getppid()

    def plan(self, *statuses, pid=None, written=BEGAN_AT - 60):
        root = os.path.join(self.xdg.name, "aacpanel", "plans")
        os.makedirs(root, exist_ok=True)
        path = os.path.join(root, SESSION + ".json")
        items = [{"text": f"step {i}", "status": s} for i, s in enumerate(statuses)]
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"sessionId": SESSION, "pid": self.claude if pid is None else pid,
                       "at": "2026-09-28T09:59:00Z", "items": items}, f)
        os.utime(path, (written, written))

    def turn(self, *records):
        with open(self.transcript, "w", encoding="utf-8") as f:
            for r in records:
                f.write(json.dumps(r) + "\n")

    def run_hook(self, payload=None):
        payload = {"hook_event_name": "Stop", "session_id": SESSION, "transcript_path": self.transcript,
                   "stop_hook_active": False, **(payload or {})}
        was = os.environ.get("XDG_STATE_HOME")
        out = io.StringIO()
        try:
            os.environ["XDG_STATE_HOME"] = self.xdg.name
            sys.stdin = io.StringIO(json.dumps(payload))
            with contextlib.redirect_stdout(out):
                reminder.main()
        finally:
            sys.stdin = sys.__stdin__
            if was is None:
                os.environ.pop("XDG_STATE_HOME", None)
            else:
                os.environ["XDG_STATE_HOME"] = was
        text = out.getvalue().strip()
        return json.loads(text) if text else None

    def worked(self):
        self.turn(prompt("earlier"), answer(at="2026-09-28T09:50:00.000Z"),
                  prompt("fix the build"), call("Bash"), result(), call("Edit", use="toolu_2"),
                  result("toolu_2"), answer())

    def test_a_turn_that_worked_past_an_unfinished_plan_is_held_once(self):
        self.plan("done", "active", "pending")
        self.worked()
        got = self.run_hook()
        self.assertEqual(got and got["decision"], "block")
        self.assertIn("If the plan changed", got["reason"])
        self.assertIn(reminder.TOOL, got["reason"])
        self.assertIn("If it did not change, end the turn now", got["reason"])
        self.assertIsNone(self.run_hook({"stop_hook_active": True}),
                          "the turn that answers the hold was held again: a loop of reminders")

    def test_a_plan_touched_in_the_turn_is_not_asked_about(self):
        self.plan("done", "active", written=BEGAN_AT + 30)
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_turn_that_called_no_tool_is_not_asked(self):
        self.plan("active", "pending")
        self.turn(prompt("earlier"), call("Bash", at="2026-09-28T09:50:00.000Z"),
                  result(at="2026-09-28T09:50:01.000Z"), prompt("what do you think?"), answer())
        self.assertIsNone(self.run_hook(), "a question answered in words was held for the plan")

    def test_a_plan_with_nothing_left_to_do_asks_nothing(self):
        self.plan("done", "dropped", "done")
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_without_a_plan_nothing_is_said(self):
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_plan_another_process_wrote_asks_nothing(self):
        # The conversation resumed by hand, outside the panel, has no plan tool.
        self.plan("active", pid=os.getpid())
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_result_of_a_tool_is_not_the_start_of_the_turn(self):
        # The plan was written after the last tool result but before the prompt:
        # a hook that took the result for a prompt would find the plan fresh.
        self.plan("active", written=BEGAN_AT + 3)
        self.turn(prompt("go"), call("Bash", at="2026-09-28T10:00:05.000Z"),
                  result(at="2026-09-28T10:00:06.000Z"), answer(at="2026-09-28T10:00:07.000Z"))
        self.assertIsNone(self.run_hook(), "the plan written inside the turn was taken for an old one")
        self.plan("active", written=BEGAN_AT - 1)
        self.assertEqual(self.run_hook()["decision"], "block",
                         "the turn was taken from the tool's result and its call was missed")

    def test_a_turn_longer_than_the_tail_is_taken_from_what_was_read(self):
        # The tail holds the last call, its result and the answer, and not the
        # prompt: the turn is taken from the call, and the plan is older.
        self.plan("active")
        was = reminder.TAIL
        self.addCleanup(setattr, reminder, "TAIL", was)
        self.worked()
        reminder.TAIL = 500
        self.assertEqual(reminder.turn(self.transcript), (BEGAN_AT + 5, True))
        self.assertEqual(self.run_hook()["decision"], "block")
        reminder.TAIL = 200
        self.assertIsNone(self.run_hook(), "a tail with no call in it was taken for a turn that worked")

    def test_a_subagent_stop_is_not_the_session(self):
        self.plan("active")
        self.worked()
        self.assertIsNone(self.run_hook({"hook_event_name": "SubagentStop"}))

    def test_a_transcript_that_is_not_there_asks_nothing(self):
        self.plan("active")
        self.assertIsNone(self.run_hook({"transcript_path": os.path.join(self.xdg.name, "none.jsonl")}))

    def test_a_broken_payload_is_not_a_crash(self):
        try:
            sys.stdin = io.StringIO("not json")
            with contextlib.redirect_stdout(io.StringIO()) as out:
                reminder.main()
        finally:
            sys.stdin = sys.__stdin__
        self.assertEqual(out.getvalue(), "")


if __name__ == "__main__":
    unittest.main()
