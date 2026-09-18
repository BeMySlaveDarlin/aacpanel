import calendar
import json
import os
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import sesstate  # noqa: E402
from test_sesstate import Transcript, call, line, result  # noqa: E402

SCRIPT = ("export const meta = {\n"
          "  name: 'review-changes',\n"
          "  description: 'Review the diff across dimensions',\n"
          "  phases: [{ title: 'Review' }, { title: 'Verify' }],\n"
          "}\n")


def launch(tool_id="toolu_w1", run="wf_1a2b3c4d5e6", task="w7yz", at="2026-09-18T10:00:00Z",
           folder="", script_path="/p/scripts/review-wf_1a2b3c4d5e6.js"):
    return (call("Workflow", tool_id, at=at, script=SCRIPT)
            + result(tool_id,
                     f"Workflow launched in background. Task ID: {task}",
                     at=at,
                     status="async_launched", taskId=task, runId=run,
                     summary="Review the diff across dimensions",
                     transcriptDir=folder, scriptPath=script_path))


def over(tool_id="toolu_w1", task="w7yz", status="completed", agents=6, tokens=120000,
         calls=44, at="2026-09-18T10:40:00Z"):
    return line({"type": "queue-operation", "operation": "enqueue", "timestamp": at,
                 "content": f"<task-notification>\n<task-id>{task}</task-id>\n"
                            f"<tool-use-id>{tool_id}</tool-use-id>\n"
                            f"<status>{status}</status>\n"
                            f"<summary>Dynamic workflow \"review-changes\" {status}</summary>\n"
                            f"<usage><agent_count>{agents}</agent_count>"
                            f"<subagent_tokens>{tokens}</subagent_tokens>"
                            f"<tool_uses>{calls}</tool_uses></usage>\n"
                            "</task-notification>"})


class Runs(Transcript):
    def flows(self, *chunks):
        return self.state(*chunks)["workflows"]

    def test_a_launched_run_stands_on_its_own(self):
        flows = self.flows(launch())
        self.assertEqual(len(flows), 1)
        flow = flows[0]
        self.assertEqual(flow["id"], "wf_1a2b3c4d5e6")
        self.assertEqual(flow["status"], "running")
        # The name is the one the script gives itself; the summary is what the
        # session said it was for.
        self.assertEqual(flow["name"], "review-changes")
        self.assertEqual(flow["text"], "Review the diff across dimensions")

    # The phases of a run in flight come out of the script it was launched
    # with: the harness writes nothing else about a run until it ends.
    def test_a_live_run_carries_the_phases_of_its_script(self):
        flow = self.flows(launch())[0]
        self.assertEqual([p["title"] for p in flow["phases"]], ["Review", "Verify"])

    def test_a_run_is_no_background_job_and_no_subagent(self):
        snap = self.state(launch())
        self.assertEqual(snap["tasks"], [])
        self.assertEqual(snap["agents"], [])

    def test_the_notification_closes_the_run_and_says_what_it_cost(self):
        flow = self.flows(launch() + over())[0]
        self.assertEqual(flow["status"], "completed")
        self.assertEqual(flow["agents"], 6)
        self.assertEqual(flow["tokens"], 120000)
        self.assertEqual(flow["calls"], 44)
        self.assertEqual(flow["doneAt"], "2026-09-18T10:40:00Z")

    def test_a_failed_run_says_so(self):
        flow = self.flows(launch() + over(status="failed"))[0]
        self.assertEqual(flow["status"], "failed")

    def test_the_live_run_stands_above_the_finished_one(self):
        first = launch(tool_id="toolu_w1", run="wf_one", task="w1") + over(tool_id="toolu_w1", task="w1")
        second = launch(tool_id="toolu_w2", run="wf_two", task="w2",
                        at="2026-09-18T11:00:00Z", script_path="/p/scripts/two.js")
        flows = self.flows(first + second)
        self.assertEqual([f["id"] for f in flows], ["wf_two", "wf_one"])


class OnDisk(Transcript):
    def snapshot_file(self, run, body):
        folder = os.path.join(self.dir.name, "t", "workflows")
        os.makedirs(folder, exist_ok=True)
        with open(os.path.join(folder, run + ".json"), "w", encoding="utf-8") as f:
            json.dump(body, f)

    def agents_of(self, run, count):
        folder = os.path.join(self.dir.name, "t", "subagents", "workflows", run)
        os.makedirs(folder, exist_ok=True)
        for n in range(count):
            with open(os.path.join(folder, f"agent-a{n}.jsonl"), "w", encoding="utf-8") as f:
                f.write("{}\n")
        return folder

    # While a run is going the harness writes no snapshot of it: the transcripts
    # of its agents are the only thing that says how far it has got.
    def test_a_running_workflow_is_counted_by_its_agents(self):
        folder = self.agents_of("wf_live", 4)
        flow = self.state(launch(run="wf_live", folder=folder))["workflows"][0]
        self.assertEqual(flow["status"], "running")
        self.assertEqual(flow["agents"], 4)

    def test_the_snapshot_carries_the_phases_the_log_and_the_result(self):
        self.snapshot_file("wf_done", {
            "runId": "wf_done", "workflowName": "review-changes", "status": "completed",
            "durationMs": 743000, "agentCount": 9, "totalTokens": 250000, "totalToolCalls": 61,
            "phases": [{"title": "Review", "detail": "five dimensions"},
                       {"title": "Verify", "detail": "adversarial"}],
            "logs": ["[stall] agent \"review:bugs\" stalled after 339s — retrying (1/5)"],
            "result": {"confirmed": 3},
        })
        flow = self.state(launch(run="wf_done"))["workflows"][0]
        self.assertEqual(flow["status"], "completed")
        self.assertEqual(flow["ms"], 743000)
        self.assertEqual([p["title"] for p in flow["phases"]], ["Review", "Verify"])
        self.assertEqual(flow["phases"][0]["detail"], "five dimensions")
        self.assertIn("stalled", flow["logs"][0])
        self.assertIn("confirmed", flow["result"])
        self.assertEqual(flow["agents"], 9)

    # A run that ended while nobody was reading still ended: the snapshot is
    # written when it is over, and it is the word on that.
    def test_a_snapshot_without_a_notification_still_ends_the_run(self):
        self.snapshot_file("wf_quiet", {"runId": "wf_quiet", "status": "killed",
                                        "workflowName": "review-changes"})
        flow = self.state(launch(run="wf_quiet"))["workflows"][0]
        self.assertEqual(flow["status"], "killed")


class Birth(Transcript):
    def test_a_run_of_the_previous_process_is_over(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(launch(at="2026-09-18T10:00:00Z"))
        # The process was born after the launch: nothing of that run survived it.
        born = calendar.timegm(time.strptime("2026-09-18T12:40:00Z", "%Y-%m-%dT%H:%M:%SZ"))
        flow = sesstate.read(path, born=born).snapshot()["workflows"][0]
        self.assertEqual(flow["status"], "killed")


if __name__ == "__main__":
    unittest.main()
