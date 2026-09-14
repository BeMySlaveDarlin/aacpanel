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

    def test_all_over_counts_nothing(self):
        got = live.work_of({"tasks": [{"done": True}], "agents": [{"status": "reported"}]})
        self.assertEqual(got, {"tasks": 0, "agents": 0})


class CallOnTheCard(unittest.TestCase):
    """A call a session made reaches the snapshot, which is all the panel reads."""

    SESSION = "abcd1234-0000-4000-8000-000000000000"

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

    def test_a_session_that_called_carries_its_line(self):
        self.board.put({"sessionId": self.SESSION, "text": "need you", "at": "2026-09-08T03:00:00Z"})
        got = self.only()
        self.assertEqual(got.get("note"), {"text": "need you", "at": "2026-09-08T03:00:00Z"},
                         "the call is not on the card the panel reads, so no push is ever made")

    def test_a_session_that_did_not_call_carries_nothing(self):
        self.assertNotIn("note", self.only(),
                         "an empty call on every card makes the panel push about silence")


if __name__ == "__main__":
    unittest.main()
