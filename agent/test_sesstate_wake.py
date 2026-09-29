import calendar
import os
import time
import unittest

import test_barrier  # noqa: F401
import sesstate
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

    def test_firing_with_no_task_id_removes_the_wakeup(self):
        # A wakeup of the session fires with the origin of the turn and no task.
        firing = line({"type": "user", "timestamp": "2026-08-25T10:20:00Z", "isMeta": True,
                       "promptSource": "system", "turnOrigin": "scheduled",
                       "message": {"content": "Watch tick"}})
        got = self.state(wakeup("toolu_1") + firing)
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


HOURLY = "Hourly check of the shop deploy: errors, latency, the queue. Report in two lines."
ONCE = "Check that the shop cache was rebuilt after the fix and tell the owner in one line."


def job(tool_id, job_id, cron, prompt, recurring, human=None, at="2026-09-29T13:28:14Z"):
    """A job put on the cron, the way claude answers it with a structured result."""
    kind = "recurring job" if recurring else "one-shot task"
    human = human or cron
    return (call("CronCreate", tool_id, at=at, cron=cron, recurring=recurring, prompt=prompt)
            + result(tool_id, f"Scheduled {kind} {job_id} ({human}). Session-only (not written "
                              "to disk, dies when Claude exits).",
                     at=at, id=job_id, humanSchedule=human, recurring=recurring, durable=False))


def job_fired(prompt, job_id=None, at="2026-09-29T14:07:00Z"):
    """A firing: a console names the job on the record, the stream leaves only the origin."""
    record = {"type": "user", "timestamp": at, "isMeta": True, "promptSource": "system",
              "turnOrigin": "scheduled", "message": {"role": "user", "content": prompt}}
    if job_id:
        record["scheduledTaskId"] = job_id
    return line(record)


class Cron(Transcript):
    """A job of the session's cron is an alarm: a prompt that comes back on a schedule."""

    def setUp(self):
        super().setUp()
        # claude reads a cron expression in the local time of its machine.
        was = os.environ.get("TZ")
        os.environ["TZ"] = "Etc/GMT-7"
        time.tzset()
        self.addCleanup(self.untz, was)

    @staticmethod
    def untz(was):
        if was is None:
            os.environ.pop("TZ", None)
        else:
            os.environ["TZ"] = was
        time.tzset()

    def test_a_job_that_repeats_says_its_schedule(self):
        got = self.state(job("toolu_1", "78b049ff", "13 * * * *", HOURLY, True,
                             human="Every hour at :13"))
        self.assertEqual([(t["id"], t["kind"], t["text"], t["schedule"], t["repeats"], t["due"])
                          for t in got["tasks"]],
                         [("78b049ff", sesstate.TASK_CRON, HOURLY, "Every hour at :13", True, "")])

    def test_a_job_that_fires_once_is_due_on_its_pinned_date(self):
        got = self.state(job("toolu_1", "6a61ac3b", "7 21 29 9 *", ONCE, False))
        self.assertEqual([(t["schedule"], t["repeats"], t["due"]) for t in got["tasks"]],
                         [("7 21 29 9 *", False, "2026-09-29T14:07:00Z")],
                         "21:07 of the machine at UTC+7 is 14:07 UTC")

    def test_a_pinned_date_already_past_is_next_year(self):
        got = self.state(job("toolu_1", "6a61ac3b", "7 21 1 1 *", ONCE, False))
        self.assertEqual(got["tasks"][0]["due"], "2027-01-01T14:07:00Z")

    def test_a_job_that_fires_once_leaves_when_a_console_names_it(self):
        got = self.state(job("toolu_1", "6a61ac3b", "7 21 29 9 *", ONCE, False),
                         job_fired(ONCE, job_id="6a61ac3b"))
        self.assertEqual(got["tasks"], [])

    def test_a_job_that_fires_once_leaves_when_the_stream_says_its_prompt(self):
        got = self.state(job("toolu_1", "6a61ac3b", "7 21 29 9 *", ONCE, False),
                         job("toolu_2", "78b049ff", "13 * * * *", HOURLY, True,
                             human="Every hour at :13"),
                         job_fired(ONCE))
        self.assertEqual([t["id"] for t in got["tasks"]], ["78b049ff"],
                         "the firing of one job took off the other, or neither")

    def test_a_job_that_repeats_stays_after_it_fires(self):
        got = self.state(job("toolu_1", "78b049ff", "13 * * * *", HOURLY, True),
                         job_fired(HOURLY, job_id="78b049ff"), job_fired(HOURLY))
        self.assertEqual([t["id"] for t in got["tasks"]], ["78b049ff"])

    def test_a_cancelled_job_leaves(self):
        got = self.state(job("toolu_1", "78b049ff", "13 * * * *", HOURLY, True),
                         call("CronDelete", "toolu_2", at="2026-09-29T14:15:20Z", id="78b049ff"),
                         result("toolu_2", "Cancelled job 78b049ff.", at="2026-09-29T14:15:20Z",
                                id="78b049ff"))
        self.assertEqual(got["tasks"], [])

    def test_the_firing_of_a_job_leaves_the_wakeup_standing(self):
        got = self.state(wakeup("toolu_1", at="2026-09-29T13:00:00Z"),
                         job("toolu_2", "6a61ac3b", "7 21 29 9 *", ONCE, False),
                         job_fired(ONCE, job_id="6a61ac3b"))
        self.assertEqual([(t["id"], t["kind"]) for t in got["tasks"]],
                         [("wakeup", sesstate.TASK_WAKE)],
                         "the firing of a job of the cron was taken for the wake-up")

    def test_a_job_dies_with_the_process(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(job("toolu_1", "78b049ff", "13 * * * *", HOURLY, True))
        born = calendar.timegm(time.strptime("2026-09-29T15:00:00Z", "%Y-%m-%dT%H:%M:%SZ"))
        self.assertEqual(sesstate.read(path, born=born).snapshot()["tasks"], [],
                         "a job only this session knew outlived the process that kept it")


if __name__ == "__main__":
    unittest.main()
