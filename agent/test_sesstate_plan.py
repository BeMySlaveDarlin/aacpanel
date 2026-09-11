import unittest

import test_barrier  # noqa: F401
from test_sesstate import Transcript, call, plan, result


class Plan(Transcript):
    def test_the_plan_is_taken_from_the_last_attachment(self):
        got = self.state(
            plan(("first", "doing the first", "in_progress")),
            plan(("first", "doing the first", "completed"),
                 ("second", "doing the second", "in_progress")),
        )
        self.assertEqual([(p["text"], p["status"]) for p in got["plan"]],
                         [("first", "completed"), ("doing the second", "in_progress")])

    def test_a_cleared_plan_does_not_linger_from_the_previous_turn(self):
        got = self.state(plan(("first", "doing the first", "in_progress")), plan())
        self.assertEqual(got["plan"], [])

    def test_an_item_in_progress_is_named_by_its_active_form(self):
        got = self.state(plan(("Write the watchdog", "Writing the watchdog", "in_progress")))
        self.assertEqual(got["plan"][0]["text"], "Writing the watchdog")


if __name__ == "__main__":
    unittest.main()


class PlanFromCalls(Transcript):
    def created(self, tool_id, number, subject, active=""):
        return (call("TaskCreate", tool_id, subject=subject, activeForm=active)
                + result(tool_id, f"Task #{number} created successfully: {subject}"))

    def test_the_plan_is_built_from_calls_when_the_attachment_is_empty(self):
        got = self.state(
            self.created("c1", 1, "Write the watchdog", "Writing the watchdog"),
            self.created("c2", 2, "Run the tests", "Running the tests"),
            call("TaskUpdate", "u1", taskId="1", status="completed"),
            call("TaskUpdate", "u2", taskId="2", status="in_progress"),
        )
        self.assertEqual([(p["text"], p["status"]) for p in got["plan"]],
                         [("Write the watchdog", "completed"), ("Running the tests", "in_progress")])

    def test_the_attachment_wins_over_what_was_built_from_calls(self):
        got = self.state(
            self.created("c1", 1, "from a call"),
            plan(("from the attachment", "doing it", "in_progress")),
        )
        self.assertEqual([p["text"] for p in got["plan"]], ["doing it"])

    def test_a_deleted_item_leaves_the_plan(self):
        got = self.state(
            self.created("c1", 1, "first"),
            self.created("c2", 2, "second"),
            call("TaskUpdate", "u1", taskId="1", status="deleted"),
        )
        self.assertEqual([p["text"] for p in got["plan"]], ["second"])

    def test_without_a_result_an_item_does_not_get_into_the_plan(self):
        got = self.state(call("TaskCreate", "c1", subject="no result"))
        self.assertEqual(got["plan"], [])

    def test_a_result_without_an_item_number_creates_nothing(self):
        got = self.state(
            call("TaskCreate", "c1", subject="no number") + result("c1", "Task not found"),
        )
        self.assertEqual(got["plan"], [])

    def test_an_update_of_an_unknown_number_stays_silent(self):
        got = self.state(
            self.created("c1", 1, "first"),
            call("TaskUpdate", "u9", taskId="99", status="completed"),
        )
        self.assertEqual([(p["text"], p["status"]) for p in got["plan"]],
                         [("first", "pending")])

    def test_a_background_task_does_not_get_into_the_plan(self):
        got = self.state(
            call("Bash", "b1", command="build", run_in_background=True)
            + result("b1", "ok", backgroundTaskId="bg1"),
        )
        self.assertEqual(got["plan"], [])
        self.assertEqual(len(got["tasks"]), 1)

    def test_stopping_a_background_task_does_not_touch_the_plan(self):
        got = self.state(
            self.created("c1", 1, "a plan item"),
            call("Bash", "b1", command="build", run_in_background=True)
            + result("b1", "ok", backgroundTaskId="bg1"),
            call("TaskStop", "s1", task_id="bg1"),
        )
        self.assertEqual([p["text"] for p in got["plan"]], ["a plan item"])
        self.assertTrue(got["tasks"][0]["done"], "the stop did not close the shell")
