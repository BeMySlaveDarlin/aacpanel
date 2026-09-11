import unittest

import test_barrier  # noqa: F401
from test_sesstate import Transcript, call, line, result


def wakeup(tool_id, prompt="Watch tick", reason="watching CI", delay=1200,
           at="2026-08-25T10:00:00Z", scheduled_for=1788292320000):
    return (call("ScheduleWakeup", tool_id, at=at, delaySeconds=delay, noop=False,
                 prompt=prompt, reason=reason)
            + result(tool_id, "Next wakeup scheduled for 02:52:00 (in 1200s).",
                     scheduledFor=scheduled_for, clampedDelaySeconds=delay, wasClamped=False))


def fired(prompt="Watch tick", at="2026-08-25T10:20:00Z"):
    return line({"type": "user", "timestamp": at, "isMeta": True, "promptSource": "system",
                 "scheduledTaskId": "bbbb2222", "scheduledFireId": "cccc3333",
                 "message": {"content": prompt}})


class Wakeups(Transcript):
    def test_a_wakeup_counts_as_background_work(self):
        got = self.state(wakeup("toolu_1"))
        self.assertEqual([(t["id"], t["kind"], t["text"], t["due"]) for t in got["tasks"]],
                         [("wakeup", "wake", "watching CI", "2026-09-01T19:52:00Z")])

    def test_firing_removes_the_wakeup(self):
        got = self.state(wakeup("toolu_1") + fired())
        self.assertEqual(got["tasks"], [])

    def test_stop_removes_the_wakeup(self):
        got = self.state(wakeup("toolu_1")
                         + call("ScheduleWakeup", "toolu_2", stop=True)
                         + result("toolu_2", "Dynamic loop stopped."))
        self.assertEqual(got["tasks"], [])

    def test_a_new_call_replaces_the_previous_one(self):
        got = self.state(wakeup("toolu_1", reason="first")
                         + wakeup("toolu_2", reason="second", scheduled_for=1788293280000))
        self.assertEqual([(t["text"], t["due"]) for t in got["tasks"]],
                         [("second", "2026-09-01T20:08:00Z")])


if __name__ == "__main__":
    unittest.main()
