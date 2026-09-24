import json
import os
import shutil
import subprocess
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import archive  # noqa: E402
import chat  # noqa: E402
import ctx  # noqa: E402
import held  # noqa: E402
import notes  # noqa: E402
from collect import live  # noqa: E402

SID = "55555555-5555-4555-8555-555555555555"


class Runtime(unittest.TestCase):
    """A runtime directory of its own, where a test plays the holder."""

    def setUp(self):
        self.run = test_barrier.tmp_path(prefix="held-")
        self.addCleanup(shutil.rmtree, self.run, True)
        self.old_env = os.environ.get("XDG_RUNTIME_DIR")
        os.environ["XDG_RUNTIME_DIR"] = self.run
        self.addCleanup(self.restore_env)

    def restore_env(self):
        if self.old_env is None:
            os.environ.pop("XDG_RUNTIME_DIR", None)
        else:
            os.environ["XDG_RUNTIME_DIR"] = self.old_env

    def hold(self, sid, pid, holder=None, **extra):
        os.makedirs(held.stream_dir(), exist_ok=True)
        data = {"protocol": 1, "sessionId": sid, "pid": pid,
                "holder": os.getpid() if holder is None else holder, "waiting": []}
        data.update(extra)
        with open(os.path.join(held.stream_dir(), sid + ".json"), "w", encoding="utf-8") as f:
            json.dump(data, f)


class Summary(Runtime):
    def test_a_live_holder_of_that_claude_is_found(self):
        self.hold(SID, 4321)
        self.assertEqual(held.summary(SID, 4321)["pid"], 4321)
        self.assertIsNotNone(held.summary(SID), "without a pid any live holder of the conversation counts")

    def test_another_claude_claiming_the_conversation_is_not_its_session(self):
        self.hold(SID, 4321)
        self.assertIsNone(held.summary(SID, 4322))

    def test_a_file_left_by_a_dead_holder_holds_nothing(self):
        dead = subprocess.Popen(["true"])
        dead.wait()
        self.hold(SID, 4321, holder=dead.pid)
        self.assertIsNone(held.summary(SID, 4321))

    def test_a_name_that_walks_out_of_the_directory_is_refused(self):
        for sid in ("../" + SID, "", None, ".hidden"):
            self.assertIsNone(held.summary(sid))

    def test_what_is_waited_for_is_said_in_the_words_of_a_terminal(self):
        self.assertEqual(held.waiting_for({"waiting": ["AskUserQuestion"]}), "input needed")
        self.assertEqual(held.waiting_for({"waiting": ["Bash"]}), "dialog open")
        self.assertEqual(held.waiting_for({"waiting": ["ExitPlanMode"]}), "dialog open")
        self.assertIsNone(held.waiting_for({"waiting": []}))
        self.assertIsNone(held.waiting_for({}))


class LiveStream(Runtime):
    """A real process whose command line says -p, the way claude on the stream does."""

    def setUp(self):
        super().setUp()
        self.sessions_dir = test_barrier.tmp_path(prefix="held-sessions-")
        self.addCleanup(shutil.rmtree, self.sessions_dir, True)
        self.old_live = archive.LIVE
        archive.LIVE = self.sessions_dir
        self.addCleanup(setattr, archive, "LIVE", self.old_live)
        self.projects_dir = test_barrier.tmp_path(prefix="held-projects-")
        self.addCleanup(shutil.rmtree, self.projects_dir, True)
        self.old_projects = chat.PROJECTS_DIR
        chat.PROJECTS_DIR = self.projects_dir
        self.addCleanup(setattr, chat, "PROJECTS_DIR", self.old_projects)

        self.proc = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)",
                                      "-p", "--input-format", "stream-json", "--session-id", SID])
        self.addCleanup(self.proc.wait)
        self.addCleanup(self.proc.terminate)
        # The kernel fills the command line of a new process a moment after
        # exec returns to its parent; read in that moment it is empty. claude
        # writes its session file long after, so only a test can land there.
        deadline = time.monotonic() + 5
        while b"-p" not in self.cmdline() and time.monotonic() < deadline:
            time.sleep(0.01)
        with open(os.path.join(self.sessions_dir, f"{self.proc.pid}.json"), "w", encoding="utf-8") as f:
            json.dump({"pid": self.proc.pid, "sessionId": SID, "cwd": "/opt/x", "name": "held",
                       "procStart": ctx.proc_start(self.proc.pid), "kind": "interactive"}, f)

    def cmdline(self):
        with open(f"/proc/{self.proc.pid}/cmdline", "rb") as f:
            return f.read()

    def test_a_stream_run_nobody_holds_is_not_a_live_session(self):
        self.assertEqual(ctx.live_sessions(), [],
                         "an SDK reviewer or a script on the stream was put on the list of sessions")

    def test_a_held_stream_run_is_a_live_session(self):
        self.hold(SID, self.proc.pid)
        self.assertEqual([s["name"] for s in ctx.live_sessions()], ["held"])


class OnTheCard(Runtime):
    """What the holder knows reaches the snapshot the screen reads."""

    def setUp(self):
        super().setUp()
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = notes.Board(os.path.join(self.dir.name, "notes.json"))
        rows = {"sessions": [{"session": "held", "sessionId": SID, "cwd": "/srv/proj/lab"}]}
        for name, value in (("sessions", lambda: rows), ("session_profiles", dict),
                            ("session_births", dict), ("live_session_waits", dict),
                            ("live_session_status", lambda: {"held": "busy"}), ("live_session_status_at", dict)):
            owner = live.ctx if name == "sessions" else live
            self.addCleanup(setattr, owner, name, getattr(owner, name))
            setattr(owner, name, value)

    def card(self):
        return live.sessions()["sessions"][0]

    def test_a_permission_waiting_with_the_holder_is_a_dialog_on_the_card(self):
        self.hold(SID, 1, waiting=["Bash"], mode="plan")
        got = self.card()
        self.assertEqual(got["transport"], "stream")
        self.assertEqual(got["status"], "waiting", "claude said busy; the holder knows a person is waited for")
        self.assertEqual(got["waitingFor"], "dialog open")
        self.assertEqual(got["mode"], "plan")

    def test_the_effort_picked_in_the_feed_is_on_the_card(self):
        self.hold(SID, 1, effort="max")
        self.assertEqual(self.card()["effort"], "max",
                         "the transcript of claude -p does not carry the effort; the holder does")

    def test_a_question_is_input_needed(self):
        self.hold(SID, 1, waiting=["AskUserQuestion"])
        self.assertEqual(self.card()["waitingFor"], "input needed")

    def test_a_held_session_waiting_for_nothing_keeps_what_claude_said(self):
        self.hold(SID, 1)
        got = self.card()
        self.assertEqual((got["transport"], got["status"]), ("stream", "busy"))
        self.assertNotIn("waitingFor", got)

    def test_a_session_in_a_terminal_is_not_marked(self):
        self.assertNotIn("transport", self.card())


class Withdrawn(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.addCleanup(os.environ.pop, "XDG_STATE_HOME", None)
        os.environ["XDG_STATE_HOME"] = self.dir.name

    def withdraw(self, *texts):
        path = held.withdrawn_path(SID)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump([held.fingerprint(t) for t in texts], f)

    def test_a_message_taken_back_is_not_shown_as_sent(self):
        self.withdraw("drop the table")
        items = [{"role": "me", "text": "run the tests", "pos": 1},
                 {"role": "me", "text": "drop the table", "pos": 2},
                 {"role": "ai", "text": "drop the table", "pos": 3}]
        got = held.mark_withdrawn(items, SID)
        self.assertEqual([i.get("state") for i in got], [None, "withdrawn", None])
        self.assertNotIn("state", items[1], "the items of the tail are changed in place")

    def test_sent_twice_and_taken_back_once_marks_one(self):
        self.withdraw("again")
        items = [{"role": "me", "text": "again", "pos": 1}, {"role": "me", "text": "again", "pos": 2}]
        got = held.mark_withdrawn(items, SID)
        self.assertEqual([i.get("state") for i in got], ["withdrawn", None])

    def test_the_fingerprint_is_the_holders(self):
        # The holder writes it in Go; the same constant stands in its test.
        self.assertEqual(held.fingerprint("  slow  "), "5e0cf7bd1dfa3831788b0cf6dedcdd22")

    def test_nothing_taken_back_changes_nothing(self):
        items = [{"role": "me", "text": "hi", "pos": 1}]
        self.assertIs(held.mark_withdrawn(items, SID), items)


if __name__ == "__main__":
    unittest.main()
