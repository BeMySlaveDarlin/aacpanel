import calendar
import os
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import sesstate  # noqa: E402
from test_sesstate import (Transcript, background, call, line,  # noqa: E402
                           notification, result, spawn)
from test_sesstate_wake import wakeup  # noqa: E402


def orphan_summary(*task_ids, status="stopped"):
    ids = "".join(f"<task-id>{t}</task-id>\n" for t in task_ids)
    return line({"type": "user", "timestamp": "2026-08-25T11:00:00Z",
                 "content": f"<task-notification>\n{ids}"
                            f"<task-id>__orphan_summary__:shell</task-id>\n"
                            f"<status>{status}</status>\n"
                            f"<summary>{len(task_ids)} background shell command "
                            f"task(s) from the previous session have no completion "
                            f"record. They have been marked stopped.</summary>\n"
                            f"</task-notification>"})


def monitor_event(task_id, event, description="Watching the build",
                  at="2026-08-25T10:20:00Z"):
    return line({"type": "user", "timestamp": at,
                 "content": f"<task-notification>\n<task-id>{task_id}</task-id>\n"
                            f'<summary>Monitor event: "{description}"</summary>\n'
                            f"<event>{event}</event>\n</task-notification>"})


def agent_notification(task_id, status="completed"):
    return line({"type": "user", "timestamp": "2026-08-25T10:40:00Z",
                 "content": f"<task-notification>\n<task-id>{task_id}</task-id>\n"
                            f"<output-file>/srv/proj/tasks/{task_id}.output</output-file>\n"
                            f"<status>{status}</status>\n"
                            f'<summary>Agent "Test run" completed</summary>\n'
                            f"</task-notification>"})


def timed_out(tool_id, task_id, description="Catching a flaky check",
              at="2026-08-25T10:00:00Z"):
    return (call("Bash", tool_id, at=at, command="./stage-check.sh",
                 description=description)
            + result(tool_id,
                     "Command did not complete within its 300s timeout and was "
                     f"moved to the background (ID: {task_id}).",
                     backgroundTaskId=task_id, timedOutAfterMs=300000))


def aacpanel(tool_id, task_id, description="Watching the build",
            at="2026-08-25T10:00:00Z", persistent=True):
    return (call("Monitor", tool_id, at=at, command="make watch",
                 description=description, persistent=persistent)
            + result(tool_id, "ok", taskId=task_id, timeoutMs=0,
                     persistent=persistent))


def async_agent(tool_id, agent_id, description="Run the tests",
                at="2026-08-25T10:00:00Z"):
    return (call("Agent", tool_id, at=at, description=description,
                 prompt="run go test", subagent_type="general-purpose",
                 run_in_background=True)
            + result(tool_id, "Agent launched.", status="async_launched",
                     agentId=agent_id, description=description, isAsync=True,
                     canReadOutputFile=True,
                     outputFile=f"/tmp/claude-1000/x/tasks/{agent_id}.output"))


class Tasks(Transcript):
    def test_a_background_task_is_visible_until_it_ends(self):
        got = self.state(background("toolu_1", "b00000001"))
        self.assertEqual([(t["id"], t["text"]) for t in got["tasks"]],
                         [("b00000001", "Waiting for CI")])

    def test_a_completion_notification_closes_the_shell_without_dropping_it(self):
        got = self.state(background("toolu_1", "b00000001"), notification("toolu_1"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"],
                         "a finished shell left the list: the session still holds it "
                         "and its output is still readable")
        self.assertTrue(got["tasks"][0]["done"], "the shell is in the list but not marked finished")

    def test_a_manually_stopped_shell_is_closed_without_a_notification(self):
        got = self.state(background("toolu_1", "b00000001"),
                         call("TaskStop", "toolu_2", task_id="b00000001"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"])
        self.assertTrue(got["tasks"][0]["done"], "a stop by hand left the shell open")

    def test_a_call_without_a_background_id_does_not_count_as_a_task(self):
        got = self.state(call("Bash", "toolu_1", command="ls", run_in_background=True)
                         + result("toolu_1", "files"))
        self.assertEqual(got["tasks"], [])

    def test_a_progress_notification_does_not_remove_a_live_monitor(self):
        got = self.state(background("toolu_1", "b00000001"), notification("toolu_1", "running"))
        self.assertEqual(len(got["tasks"]), 1)


class Notifications(Transcript):
    def test_a_notification_without_a_call_removes_the_task_by_its_id(self):
        got = self.state(aacpanel("toolu_1", "b00000002"),
                         orphan_summary("beapubqvz", "b00000002"))
        self.assertEqual(got["tasks"], [])

    def test_a_summary_about_other_tasks_does_not_touch_ours(self):
        got = self.state(aacpanel("toolu_1", "b00000001"),
                         orphan_summary("beapubqvz"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"])

    def test_an_agent_completion_removes_the_task_by_its_id(self):
        got = self.state(async_agent("toolu_1", "a2222222222222222"),
                         agent_notification("a2222222222222222"))
        self.assertEqual(got["tasks"], [])

    def test_a_monitor_timeout_removes_the_task(self):
        got = self.state(aacpanel("toolu_1", "b00000002"),
                         monitor_event("b00000002", "[Monitor timed out — re-arm if needed.]"))
        self.assertEqual(got["tasks"], [])

    def test_an_expired_monitor_leaves_the_list(self):
        got = self.state(aacpanel("toolu_1", "b00000002"),
                         monitor_event("b00000002",
                                       "[Monitor expired after 30m with no events delivered. "
                                       "Re-arm it if you still need the watch — and widen the "
                                       "filter if silence was unexpected.]"))
        self.assertEqual(got["tasks"], [])

    def test_a_monitor_that_delivered_and_then_expired_leaves_the_list(self):
        got = self.state(aacpanel("toolu_1", "b00000002"),
                         monitor_event("b00000002", "CI=running threads=0"),
                         monitor_event("b00000002",
                                       "[Monitor expired after 30m with 1 event delivered. "
                                       "Re-arm it if you still need the watch.]",
                                       at="2026-08-25T10:50:00Z"))
        self.assertEqual(got["tasks"], [])

    def test_a_watch_re_armed_every_half_hour_keeps_one_live_row(self):
        marks = []
        for n in range(1, 6):
            at = "2026-08-25T%02d:00:00Z" % (9 + n)
            over = "2026-08-25T%02d:30:00Z" % (9 + n)
            marks.append(aacpanel("toolu_%d" % n, "b0000000%d" % n, at=at))
            if n < 5:
                marks.append(monitor_event("b0000000%d" % n,
                                           "[Monitor expired after 30m with no events "
                                           "delivered. Re-arm it if you still need the "
                                           "watch.]", at=over))
        got = self.state(*marks)
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000005"])

    def test_a_monitor_event_does_not_remove_the_task(self):
        got = self.state(aacpanel("toolu_1", "b00000001"),
                         monitor_event("b00000001", "ordered=5 bought=6 percent=120"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"])

    def test_a_stopped_status_closes_the_task(self):
        got = self.state(background("toolu_1", "b00000001"),
                         notification("toolu_1", "stopped"))
        self.assertTrue(got["tasks"][0]["done"], "the status stopped left the shell open")

    def test_a_monitor_event_is_remembered_by_its_time(self):
        got = self.state(aacpanel("toolu_1", "b00000001"),
                         monitor_event("b00000001", "CI=running threads=0"))
        self.assertEqual(got["tasks"][0]["event"], "2026-08-25T10:20:00Z")

    def test_a_task_without_events_has_no_mark(self):
        got = self.state(aacpanel("toolu_1", "b00000001"))
        self.assertNotIn("event", got["tasks"][0])

    def test_the_mark_is_updated_by_the_last_event(self):
        got = self.state(aacpanel("toolu_1", "b00000001"),
                         monitor_event("b00000001", "CI=running threads=0"),
                         monitor_event("b00000001", "CI=success threads=0",
                                       at="2026-08-25T11:30:00Z"))
        self.assertEqual(got["tasks"][0]["event"], "2026-08-25T11:30:00Z")

    def test_an_event_of_another_task_sets_no_mark(self):
        got = self.state(aacpanel("toolu_1", "b00000001"),
                         monitor_event("b00000002", "CI=running threads=0"))
        self.assertNotIn("event", got["tasks"][0])

    def test_a_finishing_event_does_not_replace_the_mark_with_the_end(self):
        got = self.state(aacpanel("toolu_1", "b00000001"),
                         monitor_event("b00000001", "[Monitor timed out — re-arm if needed.]"))
        self.assertEqual(got["tasks"], [])

    def test_another_task_named_in_a_notification_does_not_touch_the_rest(self):
        got = self.state(background("toolu_1", "b00000001"),
                         aacpanel("toolu_2", "b00000002", at="2026-08-25T10:05:00Z"),
                         orphan_summary("b00000002"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"])


class Kinds(Transcript):
    def test_a_command_is_recognised_by_its_own_key(self):
        got = self.state(background("toolu_1", "b00000001"))
        self.assertEqual([(t["id"], t["text"]) for t in got["tasks"]],
                         [("b00000001", "Waiting for CI")])

    def test_a_monitor_is_recognised_by_its_own_key(self):
        got = self.state(aacpanel("toolu_1", "b00000003"))
        self.assertEqual([(t["id"], t["text"]) for t in got["tasks"]],
                         [("b00000003", "Watching the build")])

    def test_a_monitor_creates_a_task_even_without_persistent(self):
        got = self.state(aacpanel("toolu_1", "b00000004", persistent=False))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000004"])

    def test_a_background_agent_is_recognised_by_its_status(self):
        got = self.state(async_agent("toolu_1", "a3333333333333333"))
        self.assertEqual([(t["id"], t["text"]) for t in got["tasks"]],
                         [("a3333333333333333", "Run the tests")])

    def test_a_background_agent_does_not_count_as_a_subagent(self):
        got = self.state(async_agent("toolu_1", "a3333333333333333"))
        self.assertEqual(got["agents"], [])

    def test_a_subagent_does_not_count_as_a_background_task(self):
        got = self.state(spawn("toolu_1", "alpha"))
        self.assertEqual(got["tasks"], [])
        self.assertEqual([a["name"] for a in got["agents"]], ["alpha"])

    def test_a_failed_agent_launch_creates_nothing(self):
        got = self.state(call("Agent", "toolu_1", description="audit")
                         + result("toolu_1", "no such agent type", status="error"))
        self.assertEqual((got["tasks"], got["agents"]), ([], []))

    def test_a_command_pushed_to_the_background_by_a_timeout_is_visible_without_the_flag(self):
        got = self.state(timed_out("toolu_1", "b00000005"))
        self.assertEqual([(t["id"], t["text"]) for t in got["tasks"]],
                         [("b00000005", "Catching a flaky check")])

    def test_a_notification_leaves_only_the_shell(self):
        got = self.state(
            background("toolu_1", "b00000001"), aacpanel("toolu_2", "b00000003"),
            async_agent("toolu_3", "a3333333333333333"),
            notification("toolu_1"), notification("toolu_2"), notification("toolu_3"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"],
                         "a watch and an agent have nothing to come back to, they leave; "
                         "a shell stays")

    def test_a_running_shell_is_not_marked_finished(self):
        got = self.state(background("toolu_1", "b00000001"))
        self.assertFalse(got["tasks"][0]["done"],
                         "a shell that nobody closed is shown as finished")

    def test_finished_shells_give_way_to_live_ones(self):
        records = []
        for n in range(sesstate.MAX_ITEMS + 5):
            use, task = f"toolu_{n}", f"b{n:08d}"
            records.append(background(use, task))
            if n < sesstate.MAX_ITEMS:
                records.append(notification(use))
        got = self.state(*records)
        self.assertLessEqual(len(got["tasks"]), sesstate.MAX_ITEMS,
                             "the list of shells grows without a ceiling")
        live = [t["id"] for t in got["tasks"] if not t["done"]]
        self.assertEqual(len(live), 5,
                         "a live shell was dropped while finished ones stayed")

    def test_a_manual_stop_leaves_only_the_shell(self):
        got = self.state(
            background("toolu_1", "b00000001"), aacpanel("toolu_2", "b00000003"),
            async_agent("toolu_3", "a3333333333333333"),
            call("TaskStop", "toolu_4", task_id="b00000001"),
            call("TaskStop", "toolu_5", task_id="b00000003"),
            call("TaskStop", "toolu_6", task_id="a3333333333333333"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"])

    def test_the_three_kinds_are_counted_together(self):
        got = self.state(background("toolu_1", "b00000001"),
                         aacpanel("toolu_2", "b00000003"),
                         async_agent("toolu_3", "a3333333333333333"))
        self.assertEqual([t["id"] for t in got["tasks"]],
                         ["b00000001", "b00000003", "a3333333333333333"])


class TaskKinds(Transcript):
    def kinds(self, *chunks):
        return {t["id"]: t.get("kind") for t in self.state(*chunks)["tasks"]}

    def test_the_three_kinds_are_told_apart(self):
        got = self.kinds(
            call("Bash", "tool-1", command="make check", run_in_background=True),
            result("tool-1", backgroundTaskId="bqqq1"),
            call("Monitor", "tool-2", command="docker logs -f aacpanel"),
            result("tool-2", taskId="bqqq2"),
            call("Agent", "tool-3", description="stand build"),
            result("tool-3", status="async_launched", agentId="bqqq3",
                   description="stand build"),
        )
        self.assertEqual(got, {
            "bqqq1": sesstate.TASK_BASH,
            "bqqq2": sesstate.TASK_MONITOR,
            "bqqq3": sesstate.TASK_AGENT,
        })

    def test_the_counter_stays_shared(self):
        got = self.kinds(
            call("Bash", "tool-1", command="make check", run_in_background=True),
            result("tool-1", backgroundTaskId="bqqq1"),
            call("Monitor", "tool-2", command="docker logs -f aacpanel"),
            result("tool-2", taskId="bqqq2"),
        )
        self.assertEqual(len(got), 2, "the kinds ended up in different lists")


if __name__ == "__main__":
    unittest.main()


class ScreenLine(Transcript):
    def test_bash_line_is_the_command(self):
        got = self.state(background("tool-1", "b00000001"))["tasks"][0]
        self.assertEqual(got["text"], "Waiting for CI")
        self.assertEqual(got["line"], "sleep 600")

    def test_monitor_line_is_the_description(self):
        got = self.state(aacpanel("tool-1", "b00000002"))["tasks"][0]
        self.assertEqual(got["line"], "Watching the build")

    def test_background_agent_has_no_line(self):
        got = self.state(async_agent("tool-1", "a1"))["tasks"][0]
        self.assertEqual(got["line"], "")


BORN = calendar.timegm(time.strptime("2026-08-25T10:30:00Z", "%Y-%m-%dT%H:%M:%SZ"))
BEFORE = "2026-08-25T10:00:00Z"
AFTER = "2026-08-25T11:00:00Z"


class Restart(Transcript):
    """A restart of the process takes the background work of the old one with it."""

    def born(self, born, *chunks):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write("".join(chunks))
        return sesstate.read(path, born=born).snapshot()

    def test_a_shell_started_before_the_process_was_born_is_finished(self):
        got = self.born(BORN, background("toolu_1", "b00000001", at=BEFORE))
        self.assertEqual([(t["id"], t["done"], t["doneAt"]) for t in got["tasks"]],
                         [("b00000001", True, "2026-08-25T10:30:00Z")],
                         "the shell died with the process, and the chip still counts it running")

    def test_a_watch_and_an_agent_started_before_the_birth_are_gone(self):
        got = self.born(BORN, aacpanel("toolu_1", "b00000002", at=BEFORE),
                        async_agent("toolu_2", "a3333333333333333", at=BEFORE))
        self.assertEqual(got["tasks"], [],
                         "a watch and an agent have nothing to come back to after a restart")

    def test_work_started_after_the_birth_is_running(self):
        got = self.born(BORN, background("toolu_1", "b00000001", at=AFTER),
                        aacpanel("toolu_2", "b00000002", at=AFTER),
                        async_agent("toolu_3", "a3333333333333333", at=AFTER))
        self.assertEqual([(t["id"], t.get("done")) for t in got["tasks"]],
                         [("b00000001", False), ("b00000002", False),
                          ("a3333333333333333", False)])

    def test_a_shell_closed_before_the_restart_keeps_its_own_end(self):
        got = self.born(BORN, background("toolu_1", "b00000001", at=BEFORE),
                        call("TaskStop", "toolu_2", at="2026-08-25T10:10:00Z",
                             task_id="b00000001"))
        self.assertEqual([(t["done"], t["doneAt"]) for t in got["tasks"]],
                         [(True, "2026-08-25T10:10:00Z")],
                         "the restart rewrote the end of a shell that had ended on its own")

    def test_an_alarm_set_before_the_birth_does_not_ring_in_the_new_process(self):
        # The schedule lives in the memory of the process that set it, and
        # nothing is written down for the next one to pick up.
        got = self.born(BORN, wakeup("toolu_1", at=BEFORE))
        self.assertEqual(got["tasks"], [])

    def test_an_alarm_set_after_the_birth_is_kept(self):
        got = self.born(BORN, wakeup("toolu_1", at=AFTER))
        self.assertEqual([t["kind"] for t in got["tasks"]], [sesstate.TASK_WAKE])

    def test_without_a_birth_nothing_is_finished(self):
        got = self.born(None, background("toolu_1", "b00000001", at=BEFORE),
                        wakeup("toolu_2", at=BEFORE))
        self.assertEqual([(t["id"], t.get("done")) for t in got["tasks"]],
                         [("b00000001", False), (sesstate.WAKE_ID, None)])

    def test_the_birth_is_remembered_by_a_read_that_does_not_name_it(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        open(path, "w", encoding="utf-8").close()
        state = sesstate.read(path, born=BORN)
        with open(path, "a", encoding="utf-8") as f:
            f.write(background("toolu_1", "b00000001", at=BEFORE))
        got = sesstate.read(path, state).snapshot()
        self.assertEqual([(t["id"], t["done"]) for t in got["tasks"]], [("b00000001", True)])

    def test_a_birth_told_after_the_reading_finishes_what_was_read_before_it(self):
        # The birth may reach the state after the transcript was read, and the
        # file need not grow in between.
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(background("toolu_1", "b00000001", at=BEFORE))
        state = sesstate.read(path)
        self.assertFalse(state.snapshot()["tasks"][0]["done"])
        got = sesstate.read(path, state, born=BORN).snapshot()
        self.assertTrue(got["tasks"][0]["done"],
                        "a birth told to a read that found nothing new changed nothing")

    def test_a_shell_finished_by_the_restart_gives_way_like_any_finished_one(self):
        records = [background(f"toolu_{n}", f"b{n:08d}", at=BEFORE)
                   for n in range(sesstate.MAX_ITEMS)]
        records += [background(f"toolu_{n}", f"b{n:08d}", at=AFTER)
                    for n in range(sesstate.MAX_ITEMS, sesstate.MAX_ITEMS + 5)]
        got = self.born(BORN, *records)
        self.assertLessEqual(len(got["tasks"]), sesstate.MAX_ITEMS)
        self.assertEqual(len([t for t in got["tasks"] if not t["done"]]), 5,
                         "a live shell was dropped while ones the restart finished stayed")


class Order(Transcript):
    def stop(self, tool_id, task_id, at):
        return call("TaskStop", tool_id, at=at, task_id=task_id)

    def test_a_running_shell_stands_above_a_finished_one_that_started_earlier(self):
        got = self.state(background("toolu_1", "b00000001", at="2026-08-25T10:00:00Z"),
                         background("toolu_2", "b00000002", at="2026-08-25T10:05:00Z"),
                         self.stop("toolu_3", "b00000001", "2026-08-25T10:10:00Z"))
        self.assertEqual([(t["id"], t["done"]) for t in got["tasks"]],
                         [("b00000002", False), ("b00000001", True)],
                         "a shell still running stands under one that is over")

    def test_among_the_running_the_one_started_last_stands_first(self):
        got = self.state(background("toolu_1", "b00000001", at="2026-08-25T10:00:00Z"),
                         background("toolu_2", "b00000002", at="2026-08-25T10:05:00Z"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000002", "b00000001"])

    def test_an_event_lifts_a_running_shell_above_one_started_later(self):
        got = self.state(background("toolu_1", "b00000001", at="2026-08-25T10:00:00Z"),
                         background("toolu_2", "b00000002", at="2026-08-25T10:05:00Z"),
                         monitor_event("b00000001", "build passed", at="2026-08-25T10:20:00Z"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001", "b00000002"],
                         "the order goes by the start, not by the last thing heard")

    def test_among_the_finished_the_one_that_ended_last_stands_first(self):
        got = self.state(background("toolu_1", "b00000001", at="2026-08-25T10:00:00Z"),
                         background("toolu_2", "b00000002", at="2026-08-25T10:05:00Z"),
                         self.stop("toolu_3", "b00000001", "2026-08-25T10:10:00Z"),
                         self.stop("toolu_4", "b00000002", "2026-08-25T10:30:00Z"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000002", "b00000001"])

    def test_the_cut_takes_the_finished_ones_not_the_running(self):
        records = [background(f"toolu_{n}", f"b{n:08d}", at=f"2026-08-25T10:{n:02d}:00Z")
                   for n in range(sesstate.MAX_ITEMS + 5)]
        got = self.state(*records)
        self.assertEqual(len(got["tasks"]), sesstate.MAX_ITEMS)
        self.assertEqual(got["tasks"][0]["id"], f"b{sesstate.MAX_ITEMS + 4:08d}",
                         "the shell opened last fell off the edge")
