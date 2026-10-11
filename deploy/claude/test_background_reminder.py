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

# The cases this script shares with the executor's hook of the same name,
# which answers the same stops the same way.
CASES = json.loads((HERE / "testdata" / "background-reminder.json").read_text(encoding="utf-8"))
SESSION = CASES["session"]
T0 = CASES["t0"]
TASKS = CASES["tasks"]
SHELL = TASKS["shell"]
HOUR = 60 * 60


class Base(unittest.TestCase):

    def setUp(self):
        self.state = self.fresh_state()

    def fresh_state(self):
        state = tempfile.TemporaryDirectory()
        self.addCleanup(state.cleanup)
        self.env("AACP_STATE_DIR", state.name)
        return state

    def env(self, key, value):
        was = os.environ.get(key)
        os.environ[key] = value
        self.addCleanup(lambda: os.environ.__setitem__(key, was) if was is not None else os.environ.pop(key, None))

    def marks_dir(self):
        return pathlib.Path(self.state.name, "background-reminder")

    def marks_file(self):
        return self.marks_dir() / f"{SESSION}.json"

    def stop(self, at, *tasks, **payload):
        payload = {"hook_event_name": "Stop", "session_id": SESSION, "stop_hook_active": False,
                   "background_tasks": list(tasks), **payload}
        return reminder.remind(payload, T0 + at)


class TestTheSharedCases(Base):

    def test_every_shared_case(self):
        self.assertTrue(CASES["cases"], "the shared cases are empty: the test checks nothing")
        for case in CASES["cases"]:
            with self.subTest(case["what"]):
                self.state = self.fresh_state()
                self.play(case)

    def play(self, case):
        if "before" in case:
            self.marks_dir().mkdir(parents=True, exist_ok=True)
            self.marks_file().write_text(case["before"])
        for name, age in case.get("aged", {}).items():
            self.marks_dir().mkdir(parents=True, exist_ok=True)
            aged = self.marks_dir() / name
            aged.write_text("{}")
            # The script prunes by the wall clock.
            os.utime(aged, (time.time() - age,) * 2)
        for i, stop in enumerate(case["stops"]):
            at = f"stop {i} at {stop['at']}"
            text = self.stop(stop["at"], *(TASKS[n] for n in stop["tasks"]), **stop.get("payload", {}))
            says = stop["says"]
            if says is None:
                self.assertIsNone(text, at)
            else:
                self.assertIsNotNone(text, at)
                if "text" in says:
                    self.assertEqual(text, says["text"], at)
                for word in says.get("has", []):
                    self.assertIn(word, text, at)
                for word in says.get("lacks", []):
                    self.assertNotIn(word, text, at)
            if "marks" in stop:
                if stop["marks"] is None:
                    self.assertFalse(self.marks_file().exists(), at)
                else:
                    self.assertEqual(set(json.loads(self.marks_file().read_text())), set(stop["marks"]), at)
            if "exact" in stop:
                self.assertEqual(json.loads(self.marks_file().read_text()), stop["exact"], at)
            listing = stop.get("listing", {})
            if "state" in listing:
                self.assertEqual(sorted(os.listdir(self.state.name)), listing["state"], at)
            if "marks" in listing:
                self.assertEqual(sorted(os.listdir(self.marks_dir())), listing["marks"], at)


class TestTheLoop(Base):

    def test_a_reminder_that_cannot_be_marked_is_not_spoken(self):
        self.stop(0, SHELL)
        # A disk that takes no write: root, which runs the checks in a container, writes past any mode.
        with mock.patch.object(reminder, "save", return_value=False):
            self.assertIsNone(self.stop(HOUR, SHELL))
        self.assertIsNotNone(self.stop(HOUR + 1, SHELL))

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


def run(payload, state, raw=None):
    env = {k: v for k, v in os.environ.items() if not k.startswith("AACP_")}
    env["AACP_STATE_DIR"] = state
    return subprocess.run([sys.executable, str(SCRIPT)], input=raw if raw is not None else json.dumps(payload),
                          capture_output=True, text=True, env=env, timeout=30)


def silent_input(entry):
    """Returns the text of an input of the shared cases the hook answers with nothing."""
    if "raw" in entry:
        return entry["raw"]
    if "payload" in entry:
        return json.dumps(entry["payload"])
    return "[" * entry["deep"]


class TestBrokenInput(Base):

    def test_every_broken_input_exits_zero_without_a_word(self):
        self.assertTrue(CASES["silent"])
        for entry in CASES["silent"]:
            raw = silent_input(entry)
            out = run(None, self.state.name, raw=raw)
            self.assertEqual((out.returncode, out.stdout), (0, ""), raw[:40])

    def test_an_unusable_state_directory_exits_zero_without_a_word(self):
        blocker = pathlib.Path(self.state.name, "file")
        blocker.write_text("")
        out = run({"hook_event_name": "Stop", "session_id": SESSION, "background_tasks": [SHELL]}, str(blocker))
        self.assertEqual((out.returncode, out.stdout), (0, ""))


if __name__ == "__main__":
    unittest.main()
