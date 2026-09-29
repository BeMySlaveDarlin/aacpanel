#!/usr/bin/env python3
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import notes  # noqa: E402
from collect import live  # noqa: E402


class WorkOfASession(unittest.TestCase):
    """The card of a session says what it is doing now.

    A shell that is over stays in the state so its output can still be read,
    and an agent that reported may never speak again; neither is work in
    progress, and counting them told the person about work that was done.
    """


    def test_a_finished_command_is_not_counted(self):
        got = live.work_of({"tasks": [{"done": True}, {"done": True}, {}], "agents": []})
        self.assertEqual(got, {"tasks": 1, "agents": 0})

    def test_a_reported_agent_is_not_counted(self):
        got = live.work_of({"tasks": [], "agents": [{"status": "reported"}, {"status": "active"}]})
        self.assertEqual(got, {"tasks": 0, "agents": 1})

    def test_a_wake_up_is_a_task_that_says_so(self):
        got = live.work_of({"tasks": [{"kind": "wake"}, {"kind": "bash"}, {"kind": "wake", "done": True}],
                            "agents": []})
        self.assertEqual(got, {"tasks": 2, "agents": 0, "wakes": 1},
                         "a restart waits for the background of a session, and a wake-up runs nothing")

    def test_a_job_of_the_cron_is_an_alarm_too(self):
        got = live.work_of({"tasks": [{"kind": "cron"}, {"kind": "wake"}, {"kind": "aacpanel"}],
                            "agents": []})
        self.assertEqual(got, {"tasks": 3, "agents": 0, "wakes": 2},
                         "a restart waited for a prompt on the cron that runs nothing until it fires")

    def test_a_running_workflow_is_work(self):
        got = live.work_of({"tasks": [], "agents": [],
                            "workflows": [{"status": "running"}, {"status": "completed"}]})
        self.assertEqual(got, {"tasks": 0, "agents": 0, "workflows": 1},
                         "a restart ended a workflow at work it did not see")

    def test_all_over_counts_nothing(self):
        got = live.work_of({"tasks": [{"done": True}], "agents": [{"status": "reported"}]})
        self.assertEqual(got, {"tasks": 0, "agents": 0})


class CallOfASession(unittest.TestCase):
    """A call waits on the board for the panel, which takes it from the chat socket.

    The sessions pass keeps the board to the sessions that live; the card of a
    session does not carry the call, since the snapshot is read on a tick and a
    call is for now.
    """

    SESSION = "abcd1234-0000-4000-8000-000000000000"
    GONE = "abcd1234-0000-4000-8000-00000000dead"

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)

        board = notes.Board(os.path.join(self.dir.name, "notes.json"))
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = board
        self.board = board

        rows = {"sessions": [{"session": "aacpanel", "sessionId": self.SESSION, "cwd": "/srv/proj/lab"}]}
        for name, value in (("sessions", lambda: rows), ("session_profiles", dict),
                            ("session_births", dict), ("live_session_waits", dict),
                            ("live_session_status", dict), ("live_session_status_at", dict)):
            holder = live.ctx if name == "sessions" else live
            self.addCleanup(setattr, holder, name, getattr(holder, name))
            setattr(holder, name, value)

    def only(self):
        return live.sessions()["sessions"][0]

    def put(self, session):
        ok, why = self.board.put(notes.clean({"sessionId": session, "text": "need you"}))
        self.assertTrue(ok, why)

    def test_the_call_of_a_live_session_stands_for_the_panel(self):
        self.put(self.SESSION)
        card = self.only()
        self.assertIsNotNone(self.board.of(self.SESSION),
                             "the sessions pass swept the call of a live session before the panel took it")
        self.assertNotIn("note", card, "the card carries a copy of the call that nobody reads")

    def test_the_call_of_a_session_gone_is_swept(self):
        self.put(self.GONE)
        self.only()
        self.assertIsNone(self.board.of(self.GONE),
                          "the call of a session that is gone stands on the board, and the panel pushes it")


if __name__ == "__main__":
    unittest.main()
