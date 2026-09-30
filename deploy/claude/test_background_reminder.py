#!/usr/bin/env python3

import importlib.util
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / "background-reminder.py"


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


reminder = load("background-reminder")

SESSION = "5a0c7d1e-2b3f-4a5b-8c6d-7e8f9a0b1c2d"
T0 = 1_790_000_000.0
MINUTE = 60
HOUR = 60 * MINUTE

SHELL = {"id": "b1", "type": "shell", "status": "running", "description": "npm run dev", "command": "npm run dev"}
MONITOR = {"id": "m1", "type": "monitor", "status": "running", "description": "tail the build log",
           "server": "watch", "tool": "tail"}
AGENT = {"id": "a1", "type": "subagent", "status": "running", "description": "review the diff",
         "agent_type": "general-purpose"}
FLOW = {"id": "w1", "type": "workflow", "status": "running", "description": "audit", "name": "audit"}


class Base(unittest.TestCase):

    def setUp(self):
        self.state = tempfile.TemporaryDirectory()
        self.addCleanup(self.state.cleanup)
        self.env("AACP_STATE_DIR", self.state.name)

    def env(self, key, value):
        was = os.environ.get(key)
        os.environ[key] = value
        self.addCleanup(lambda: os.environ.__setitem__(key, was) if was is not None else os.environ.pop(key, None))

    def marks_file(self):
        return pathlib.Path(self.state.name, "background-reminder", f"{SESSION}.json")

    def stop(self, at, *tasks, **payload):
        payload = {"hook_event_name": "Stop", "session_id": SESSION, "stop_hook_active": False,
                   "background_tasks": list(tasks), **payload}
        return reminder.remind(payload, T0 + at)


class TestTheThreshold(Base):

    def test_a_task_is_named_only_once_it_has_run_an_hour(self):
        self.assertIsNone(self.stop(0, SHELL))
        self.assertIsNone(self.stop(HOUR - 1, SHELL))
        text = self.stop(HOUR, SHELL)
        self.assertIsNotNone(text)
        self.assertIn("shell `b1`, 60 min: npm run dev", text)

    def test_the_hour_counts_from_the_first_stop_that_saw_the_task(self):
        self.assertIsNone(self.stop(0, SHELL))
        self.assertIsNone(self.stop(HOUR - 1, SHELL, MONITOR))
        text = self.stop(HOUR, SHELL, MONITOR)
        self.assertIn("`b1`", text)
        self.assertNotIn("`m1`", text)

    def test_a_task_that_went_away_starts_afresh(self):
        self.stop(0, SHELL)
        self.assertIsNone(self.stop(30 * MINUTE))
        self.assertIsNone(self.stop(HOUR, SHELL))


class TestTheRepeat(Base):

    def test_a_task_is_named_again_only_after_two_hours(self):
        self.stop(0, SHELL)
        self.assertIsNotNone(self.stop(HOUR, SHELL))
        self.assertIsNone(self.stop(HOUR + 1, SHELL))
        self.assertIsNone(self.stop(3 * HOUR - 1, SHELL))
        text = self.stop(3 * HOUR, SHELL)
        self.assertIn("shell `b1`, 180 min", text)

    def test_each_task_keeps_its_own_repeat(self):
        self.stop(0, SHELL)
        self.stop(30 * MINUTE, SHELL, MONITOR)
        self.assertIn("`b1`", self.stop(HOUR, SHELL, MONITOR))
        text = self.stop(90 * MINUTE, SHELL, MONITOR)
        self.assertIn("`m1`", text)
        self.assertNotIn("`b1`", text)


class TestWhatCounts(Base):

    def test_a_subagent_is_never_named(self):
        self.stop(0, AGENT)
        self.assertIsNone(self.stop(5 * HOUR, AGENT))

    def test_a_workflow_is_never_named(self):
        self.stop(0, FLOW)
        self.assertIsNone(self.stop(5 * HOUR, FLOW))

    def test_beside_a_shell_they_stay_out_of_the_list(self):
        self.stop(0, SHELL, AGENT, FLOW)
        text = self.stop(HOUR, SHELL, AGENT, FLOW)
        self.assertIn("`b1`", text)
        self.assertNotIn("`a1`", text)
        self.assertNotIn("`w1`", text)
        self.assertEqual(set(json.loads(self.marks_file().read_text())), {"b1"})

    def test_a_task_of_a_type_not_known_counts(self):
        odd = {"id": "t1", "type": "teammate", "status": "running", "description": "keeps the docs"}
        odder = {"id": "t2", "type": ["x"], "status": "running", "description": "?"}
        self.stop(0, odd, odder)
        text = self.stop(HOUR, odd, odder)
        self.assertIn("teammate `t1`", text)
        self.assertIn("`t2`", text)

    def test_no_background_work_leaves_no_marks(self):
        self.stop(0, SHELL)
        self.assertTrue(self.marks_file().exists())
        self.assertIsNone(self.stop(HOUR))
        self.assertFalse(self.marks_file().exists())
        self.stop(0, SHELL)
        self.assertIsNone(self.stop(HOUR, background_tasks=None))
        self.assertFalse(self.marks_file().exists())

    def test_only_subagents_and_workflows_leave_no_marks(self):
        self.stop(0, SHELL)
        self.assertIsNone(self.stop(HOUR, AGENT, FLOW))
        self.assertFalse(self.marks_file().exists())

    def test_the_wait_loop_without_a_limit_is_called_out(self):
        loop = {"id": "b2", "type": "shell", "status": "running", "description": "wait for ready",
                "command": "until grep -q ready log; do sleep 20; done"}
        bounded = dict(loop, id="b3", command="timeout 600 bash -c 'until grep -q ready log; do sleep 20; done'")
        self.stop(0, loop, bounded)
        lines = self.stop(HOUR, loop, bounded).splitlines()
        self.assertIn("without a time limit", next(l for l in lines if "`b2`" in l))
        self.assertNotIn("without a time limit", next(l for l in lines if "`b3`" in l))


class TestTheLoop(Base):

    def test_the_round_after_a_reminder_is_silent(self):
        self.stop(0, SHELL)
        self.assertIsNone(self.stop(5 * HOUR, SHELL, stop_hook_active=True))
        self.assertIsNotNone(self.stop(5 * HOUR, SHELL))

    def test_the_round_after_a_reminder_keeps_no_marks(self):
        self.assertIsNone(self.stop(0, SHELL, stop_hook_active=True))
        self.assertFalse(self.marks_file().exists())

    def test_a_reminder_that_cannot_be_marked_is_not_spoken(self):
        self.stop(0, SHELL)
        # A disk that takes no write: root, which runs the checks in a container, writes past any mode.
        with mock.patch.object(reminder, "save", return_value=False):
            self.assertIsNone(self.stop(HOUR, SHELL))
        self.assertIsNotNone(self.stop(HOUR + 1, SHELL))

    def test_another_event_is_left_alone(self):
        self.stop(0, SHELL)
        self.assertIsNone(self.stop(HOUR, SHELL, hook_event_name="SubagentStop"))

    def test_the_reminder_goes_as_context_never_as_a_block(self):
        self.stop(0, SHELL)
        marks = json.loads(self.marks_file().read_text())
        marks["b1"]["first"] = time.time() - 2 * HOUR
        self.marks_file().write_text(json.dumps(marks))
        out = run({"hook_event_name": "Stop", "session_id": SESSION, "background_tasks": [SHELL]},
                  self.state.name)
        self.assertEqual(out.returncode, 0)
        reply = json.loads(out.stdout)
        self.assertNotIn("decision", reply)
        self.assertEqual(reply["hookSpecificOutput"]["hookEventName"], "Stop")
        self.assertIn("`b1`", reply["hookSpecificOutput"]["additionalContext"])


class TestTheMarks(Base):

    def test_broken_marks_start_afresh(self):
        self.marks_file().parent.mkdir(parents=True)
        for raw in ("{", "[]", json.dumps({"b1": "x"}), json.dumps({"b1": {"first": "x", "reminded": 0}})):
            self.marks_file().write_text(raw)
            self.assertIsNone(self.stop(HOUR, SHELL), raw)
            self.assertEqual(json.loads(self.marks_file().read_text()),
                             {"b1": {"first": T0 + HOUR, "reminded": 0}}, raw)

    def test_the_session_id_cannot_leave_the_directory(self):
        self.assertIsNone(self.stop(0, SHELL, session_id="../../x"))
        self.assertEqual(os.listdir(self.state.name), ["background-reminder"])
        self.assertEqual(os.listdir(os.path.join(self.state.name, "background-reminder")), ["x.json"])

    def test_marks_of_conversations_long_over_go(self):
        folder = self.marks_file().parent
        folder.mkdir(parents=True)
        old, recent = folder / "old.json", folder / "recent.json"
        for f in (old, recent):
            f.write_text("{}")
        os.utime(old, (time.time() - 8 * 24 * HOUR,) * 2)
        os.utime(recent, (time.time() - 6 * 24 * HOUR,) * 2)
        self.stop(0, SHELL)
        self.assertFalse(old.exists())
        self.assertTrue(recent.exists())
        self.assertTrue(self.marks_file().exists())


def run(payload, state, raw=None):
    env = {k: v for k, v in os.environ.items() if not k.startswith("AACP_")}
    env["AACP_STATE_DIR"] = state
    return subprocess.run([sys.executable, str(SCRIPT)], input=raw if raw is not None else json.dumps(payload),
                          capture_output=True, text=True, env=env, timeout=30)


class TestBrokenInput(Base):

    def test_every_broken_input_exits_zero_without_a_word(self):
        for raw in ("", "not json", "[1, 2]", '"x"', "null",
                    json.dumps({"session_id": SESSION, "background_tasks": "x"}),
                    json.dumps({"session_id": SESSION, "background_tasks": [1, None, {"type": "shell"}, {"id": 5}]}),
                    json.dumps({"session_id": {"a": 1}, "background_tasks": [SHELL]}),
                    json.dumps({"background_tasks": [SHELL]}),
                    # Deeper than the JSON reader goes: it fails with no ValueError.
                    "[" * 100_000):
            out = run(None, self.state.name, raw=raw)
            self.assertEqual((out.returncode, out.stdout), (0, ""), raw)

    def test_an_unusable_state_directory_exits_zero_without_a_word(self):
        blocker = pathlib.Path(self.state.name, "file")
        blocker.write_text("")
        out = run({"hook_event_name": "Stop", "session_id": SESSION, "background_tasks": [SHELL]}, str(blocker))
        self.assertEqual((out.returncode, out.stdout), (0, ""))


if __name__ == "__main__":
    unittest.main()
