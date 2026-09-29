import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
from test_sesstate import Transcript, call, result  # noqa: E402

AGENT = "a5555555555555555"


class MovedToTheBackground(Transcript):
    """A call the turn waited on and the panel moved to the background is background work like any other.

    The calls start in the foreground — no run_in_background — and the answer
    that comes when they are moved is the one claude 2.1.283 writes: the command
    with its task id and the mark of a move by hand, the subagent as launched
    to work on its own.
    """

    def test_a_command_moved_by_hand_stands_among_the_commands(self):
        got = self.state(
            call("Bash", "toolu_01Fg", command="sleep 30 && echo woke", description="Wait thirty seconds")
            + result("toolu_01Fg",
                     "Command was manually backgrounded by user with ID: b7x2k9q1. "
                     "Output is being written to: /tmp/claude-1000/x/tasks/b7x2k9q1.output.",
                     stdout="", stderr="", interrupted=False,
                     backgroundTaskId="b7x2k9q1", backgroundedByUser=True))
        self.assertEqual([(t["id"], t["kind"], t["line"], t["done"]) for t in got["tasks"]],
                         [("b7x2k9q1", "bash", "sleep 30 && echo woke", False)],
                         "a command moved to the background is not on the chip of the commands")

    def test_a_subagent_moved_by_hand_stands_among_the_agents(self):
        got = self.state(
            call("Agent", "toolu_01Ag", description="probe poem", prompt="write",
                 subagent_type="general-purpose")
            + result("toolu_01Ag", "Async agent launched successfully.",
                     isAsync=True, status="async_launched", agentId=AGENT, description="probe poem",
                     resolvedModel="claude-haiku-4-5", prompt="write",
                     outputFile=f"/tmp/claude-1000/x/tasks/{AGENT}.output", canReadOutputFile=True))
        self.assertEqual([(a["id"], a["status"], a["kind"]) for a in got["agents"]],
                         [(AGENT, "active", "background")],
                         "a subagent moved to the background is not on the chip of the agents")


if __name__ == "__main__":
    unittest.main()
