#!/usr/bin/env python3

import contextlib
import importlib.util
import io
import json
import os
import pathlib
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


guard = load("context-guard")
guards = load("guards")

# The cases these scripts share with the executor's hook of the same name and
# its package internal/guards, which answer the same stops and read the same
# file the same way.
CASES = json.loads((HERE / "testdata" / "context-guard.json").read_text(encoding="utf-8"))
PLACES = json.loads((HERE / "testdata" / "guards.json").read_text(encoding="utf-8"))


class Clock:
    """The clock of the look at the work in the background: it moves only when
    the look sleeps, and the collector writes its next snapshot at the first sleep."""

    def __init__(self, at, fresh):
        self.at = at
        self.fresh = fresh

    def time(self):
        return self.at

    def sleep(self, seconds):
        self.at += seconds
        if self.fresh is not None:
            self.fresh(self.at)
            self.fresh = None


class TestTheSharedCases(unittest.TestCase):

    def setUp(self):
        keys = ("AACP_STATE_DIR", "XDG_STATE_HOME", "CLAUDE_PROJECT_DIR")
        was = {k: os.environ.get(k) for k in keys}

        def restore():
            for k, v in was.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
        self.addCleanup(restore)

    def test_every_shared_case(self):
        self.assertTrue(CASES["cases"], "the shared cases are empty: the test checks nothing")
        for case in CASES["cases"]:
            with self.subTest(case["what"]):
                self.play(case)

    def play(self, case):
        with tempfile.TemporaryDirectory() as state, tempfile.TemporaryDirectory() as xdg:
            os.environ.pop("CLAUDE_PROJECT_DIR", None)
            os.environ.update({"AACP_STATE_DIR": state, "XDG_STATE_HOME": xdg, **case.get("env", {})})
            home = os.path.join(xdg, "aacpanel")
            os.makedirs(home)
            self.guards(home, case.get("guards", CASES["guards"]))
            self.aged(os.path.join(home, "context-guard"), case.get("sent", {}))
            self.markers(home, case.get("restarting", {}))
            row = {**CASES["row"], **case.get("row", {})}
            for i, stop in enumerate(case["stops"]):
                at = f"stop {i}"
                if "guards" in stop:
                    self.guards(home, stop["guards"])
                if "restarting" in stop:
                    self.markers(home, stop["restarting"])
                stop_row = {**row, **stop.get("row", {})}
                if not case.get("nostate"):
                    self.snapshot(state, stop_row, case)
                payload = {"hook_event_name": "Stop", "session_id": "mine", "cwd": CASES["project"],
                           "stop_hook_active": False, **stop.get("payload", {})}
                text = stop.get("input", json.dumps(payload))

                def fresh(now, stop_row=stop_row):
                    self.snapshot(state, {**stop_row, **case["fresh"]}, case, now)
                clock = Clock(CASES["clock"], fresh if "fresh" in case else None)
                got = self.run_hook(text, clock)

                says = stop["says"]
                if says is None:
                    self.assertIsNone(got, at)
                else:
                    self.assertIsNotNone(got, at)
                    self.assertEqual(got["decision"], "block", at)
                    if "text" in says:
                        self.assertEqual(got["reason"], says["text"], at)
                    for word in says.get("has", []):
                        self.assertIn(word, got["reason"], at)
                if "marks" in stop:
                    folder = os.path.join(home, "context-guard")
                    listed = sorted(os.listdir(folder)) if os.path.isdir(folder) else None
                    self.assertEqual(listed, stop["marks"], at)
                if "restartingLeft" in stop:
                    self.assertEqual(sorted(os.listdir(os.path.join(home, "restarting"))), stop["restartingLeft"], at)

    @staticmethod
    def guards(home, body):
        path = os.path.join(home, "guards.tsv")
        if body is None:
            with contextlib.suppress(FileNotFoundError):
                os.remove(path)
            return
        with open(path, "w", encoding="utf-8") as f:
            f.write(body)

    @staticmethod
    def aged(folder, names):
        """Puts files named by their age into a folder, aged by the wall clock the hook prunes by."""
        if os.path.isdir(folder):
            for name in os.listdir(folder):
                os.remove(os.path.join(folder, name))
        elif names:
            os.makedirs(folder)
        for name, age in names.items():
            path = os.path.join(folder, name)
            open(path, "w").close()
            os.utime(path, (time.time() - age,) * 2)

    def markers(self, home, names):
        self.aged(os.path.join(home, "restarting"), names)

    @staticmethod
    def snapshot(state, row, case, at=None):
        data = {"sessions": [row]}
        if at is not None:
            data["sessionsAt"] = at
        elif "sessionsAt" in case:
            data["sessionsAt"] = CASES["clock"] + case["sessionsAt"]
        path = os.path.join(state, "state.json")
        with open(path + ".tmp", "w", encoding="utf-8") as f:
            json.dump(data, f)
        os.replace(path + ".tmp", path)

    @staticmethod
    def run_hook(text, clock):
        """Runs the guard as the hook runs it; returns its answer, None for nothing."""
        look = guard.background.at_work

        def at_work(session_id, path=None):
            return look(session_id, path, clock=clock.time, sleep=clock.sleep)
        out = io.StringIO()
        try:
            sys.stdin = io.StringIO(text)
            with mock.patch.object(guard.background, "at_work", at_work), contextlib.redirect_stdout(out):
                guard.main()
        finally:
            sys.stdin = sys.__stdin__
        said = out.getvalue().strip()
        return json.loads(said) if said else None


class TestPlaces(unittest.TestCase):

    def test_every_shared_case(self):
        self.assertTrue(PLACES["cases"], "the shared cases are empty: the test checks nothing")
        for case in PLACES["cases"]:
            with self.subTest(case["what"]), tempfile.TemporaryDirectory() as folder:
                name = os.path.join(folder, "guards.tsv")
                if case["file"] is not None:
                    with open(name, "w", encoding="utf-8", newline="") as f:
                        f.write(case["file"])
                for cwd, want in case["dirs"].items():
                    self.assertEqual(guards.of(cwd, name), None if want is None else tuple(want), cwd)


if __name__ == "__main__":
    unittest.main()
