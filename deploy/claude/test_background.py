#!/usr/bin/env python3

import contextlib
import importlib.util
import io
import json
import os
import pathlib
import subprocess
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

    def test_agents_and_background_commands_are_at_work(self):
        snapshot(self.path, {"agents": 2, "tasks": 1})
        self.assertEqual(background.of(background.read_state(self.path), "mine"), (2, 1, 0))

    def test_a_wake_up_is_no_work(self):
        snapshot(self.path, {"agents": 0, "tasks": 2, "wakes": 1})
        self.assertEqual(background.of(background.read_state(self.path), "mine"), (0, 1, 0),
                         "a wake-up runs nothing, and a loop that sets them would never restart")

    def test_a_running_workflow_is_work(self):
        snapshot(self.path, {"agents": 0, "tasks": 0, "workflows": 1})
        self.assertEqual(background.of(background.read_state(self.path), "mine"), (0, 0, 1))

    def test_another_session_or_none_is_nothing_at_work(self):
        snapshot(self.path, {"agents": 3}, session="other")
        self.assertEqual(background.of(background.read_state(self.path), "mine"), (0, 0, 0))
        self.assertEqual(background.of(None, "mine"), (0, 0, 0))

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
        self.assertEqual(background.words(2, 1), "2 agents and 1 background task")
        self.assertEqual(background.words(1, 0), "1 agent")
        self.assertEqual(background.words(1, 2, 1), "1 agent, 1 workflow and 2 background tasks")
        line = background.wait_line(0, 3)
        self.assertIn("3 background tasks of this session are at work, and a restart ends them", line)
        self.assertIn("1 agent of this session is at work, and a restart ends it", background.wait_line(1, 0))
        self.assertIn("Do not restart now", line)
        self.assertIn("--anyway", line)


class TestRestartScriptWaits(unittest.TestCase):
    """restart-session.sh stops before the panel and before tmux while work goes on."""

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)
        self.state = os.path.join(self.dir.name, "state")
        os.makedirs(self.state)
        # A tmux that records what it was asked and finds no pane: a script that
        # got past the check ends there and restarts nothing real.
        bin_dir = os.path.join(self.dir.name, "bin")
        os.makedirs(bin_dir)
        self.calls = os.path.join(self.dir.name, "tmux-calls")
        fake = os.path.join(bin_dir, "tmux")
        with open(fake, "w", encoding="utf-8") as f:
            f.write(f'#!/bin/sh\necho "$@" >> {self.calls}\nexit 1\n')
        os.chmod(fake, 0o755)
        self.env = {**os.environ, "PATH": f"{bin_dir}:/usr/bin:/bin", "AACP_STATE_DIR": self.state,
                    "CLAUDE_CODE_SESSION_ID": "mine", "AACP_PANEL_URL": "http://127.0.0.1:9"}

    def run_script(self, *args):
        return subprocess.run([str(HERE / "restart-session.sh"), *args], env=self.env,
                              capture_output=True, text=True, timeout=30)

    def test_work_at_work_stops_the_restart_the_old_way_included(self):
        snapshot(os.path.join(self.state, "state.json"), {"agents": 1, "tasks": 0}, at=time.time() + 60)
        for args in ((), ("--continue",)):
            got = self.run_script(*args)
            self.assertEqual(got.returncode, 2, f"{args}: {got.stdout}{got.stderr}")
            self.assertIn("WAIT 1 agent of this session is at work", got.stdout)
        self.assertFalse(os.path.exists(self.calls), "the script went on to tmux with an agent at work")

    def test_anyway_goes_on(self):
        snapshot(os.path.join(self.state, "state.json"), {"agents": 1, "tasks": 0}, at=time.time() + 60)
        got = self.run_script("--anyway")
        self.assertNotEqual(got.returncode, 2, got.stdout + got.stderr)
        self.assertNotIn("WAIT", got.stdout)


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
