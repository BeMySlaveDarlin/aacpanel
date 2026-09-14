#!/usr/bin/env python3
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
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


if __name__ == "__main__":
    unittest.main()
