import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import sesstate  # noqa: E402
from test_sesstate import (Transcript, background, call, line,  # noqa: E402
                           notification, result, spawn)


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

    def test_a_completion_notification_removes_the_task(self):
        got = self.state(background("toolu_1", "b00000001"), notification("toolu_1"))
        self.assertEqual(got["tasks"], [])

    def test_a_manually_stopped_task_is_removed_without_a_notification(self):
        got = self.state(background("toolu_1", "b00000001"),
                         call("TaskStop", "toolu_2", task_id="b00000001"))
        self.assertEqual(got["tasks"], [])

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

    def test_a_monitor_event_does_not_remove_the_task(self):
        got = self.state(aacpanel("toolu_1", "b00000001"),
                         monitor_event("b00000001", "ordered=5 bought=6 percent=120"))
        self.assertEqual([t["id"] for t in got["tasks"]], ["b00000001"])

    def test_a_stopped_status_closes_the_task(self):
        got = self.state(background("toolu_1", "b00000001"),
                         notification("toolu_1", "stopped"))
        self.assertEqual(got["tasks"], [])

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

    def test_every_kind_is_removed_by_a_notification(self):
        got = self.state(
            background("toolu_1", "b00000001"), aacpanel("toolu_2", "b00000003"),
            async_agent("toolu_3", "a3333333333333333"),
            notification("toolu_1"), notification("toolu_2"), notification("toolu_3"))
        self.assertEqual(got["tasks"], [])

    def test_every_kind_is_removed_by_a_manual_stop(self):
        got = self.state(
            background("toolu_1", "b00000001"), aacpanel("toolu_2", "b00000003"),
            async_agent("toolu_3", "a3333333333333333"),
            call("TaskStop", "toolu_4", task_id="b00000001"),
            call("TaskStop", "toolu_5", task_id="b00000003"),
            call("TaskStop", "toolu_6", task_id="a3333333333333333"))
        self.assertEqual(got["tasks"], [])

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
