#!/usr/bin/env python3

import contextlib
import importlib.util
import io
import json
import os
import pathlib
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


background = load("background")

# The cases this script shares with the panel's session_restart tool, which
# reads the same snapshot the same way.
CASES = json.loads((HERE / "testdata" / "background.json").read_text(encoding="utf-8"))


def snapshot(path, work=None, at=None, session="mine"):
    row = {"sessionId": session}
    if work is not None:
        row["work"] = work
    data = {"sessions": [row]}
    if at is not None:
        data["sessionsAt"] = at
    tmp = f"{path}.tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f)
    os.replace(tmp, path)


class TestWhatIsAtWork(unittest.TestCase):

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)
        self.path = os.path.join(self.dir.name, "state.json")

    def test_the_shared_cases_of_what_is_at_work(self):
        self.assertTrue(CASES["work"], "the shared cases are empty: the test checks nothing")
        for case in CASES["work"]:
            with self.subTest(case["what"]):
                with open(self.path, "w", encoding="utf-8") as f:
                    json.dump(case["state"], f)
                self.assertEqual(background.of(background.read_state(self.path), case["session"]),
                                 tuple(case["work"]))

    def test_nothing_at_work_is_answered_at_once(self):
        snapshot(self.path, {"agents": 0, "tasks": 0}, at=1.0)
        slept = []
        got = background.at_work("mine", self.path, wait=5, sleep=slept.append)
        self.assertEqual(got, (0, 0, 0))
        self.assertEqual(slept, [], "a session with nothing at work waited for the collector")

    def test_work_seen_is_looked_at_again_in_a_snapshot_written_after_the_look(self):
        # The turn began with the news that the agent is done; the snapshot on
        # disk was written before that and still counts it.
        snapshot(self.path, {"agents": 1}, at=time.time() - 3)

        def collector():
            time.sleep(0.3)
            snapshot(self.path, {"agents": 0}, at=time.time())

        t = threading.Thread(target=collector)
        t.start()
        self.addCleanup(t.join)
        self.assertEqual(background.at_work("mine", self.path, wait=3), (0, 0, 0),
                         "a snapshot older than the look kept an agent that is done at work")

    def test_work_that_goes_on_is_work_after_the_wait(self):
        snapshot(self.path, {"agents": 1}, at=time.time() - 3)
        start = time.time()
        self.assertEqual(background.at_work("mine", self.path, wait=0.4), (1, 0, 0))
        self.assertLess(time.time() - start, 2, "the look waited past its limit")

    def test_the_words_say_what_is_at_work(self):
        self.assertTrue(CASES["words"], "the shared cases are empty: the test checks nothing")
        for case in CASES["words"]:
            agents, tasks, flows = case["work"]
            with self.subTest(case["words"]):
                self.assertEqual(background.words(agents, tasks, flows), case["words"])
        line = background.wait_line(0, 3)
        self.assertIn("3 background tasks of this session are at work, and a restart ends them", line)
        self.assertIn("1 agent of this session is at work, and a restart ends it", background.wait_line(1, 0))
        self.assertIn("Do not restart now", line)
        self.assertIn("--anyway", line)


class TestCommand(unittest.TestCase):

    def test_outside_a_session_nothing_is_said(self):
        was = os.environ.pop("CLAUDE_CODE_SESSION_ID", None)
        self.addCleanup(lambda: was is not None and os.environ.update(CLAUDE_CODE_SESSION_ID=was))
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(background.main(), 0)
        self.assertEqual(out.getvalue(), "")


if __name__ == "__main__":
    unittest.main()
