import calendar
import json
import os
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import sesstate  # noqa: E402
from collect.live import work_of  # noqa: E402
from test_sesstate import Transcript, background, call, line, result  # noqa: E402
from test_sesstate_tasks import agent_notification, async_agent  # noqa: E402

AGENT = "a3333333333333333"


def stopped_by(mark, at="2026-08-25T10:05:30Z"):
    return line({"type": "user", "timestamp": at, "isSidechain": True,
                 "message": {"role": "user", "content": [{"type": "text", "text": mark}]}})


def request(tokens, model="claude-sonnet-5", at="2026-08-25T10:05:00Z"):
    return line({"type": "assistant", "timestamp": at,
                 "message": {"model": model, "content": [{"type": "text", "text": "ok"}],
                             "usage": {"input_tokens": tokens}}})


class BackgroundAgents(Transcript):
    """An agent the session sent off to work stands among its agents, not its commands."""

    def test_one_at_work_is_an_active_agent_of_the_background_kind(self):
        got = self.state(async_agent("toolu_1", AGENT))
        self.assertEqual(
            [(a["id"], a["name"], a["text"], a["status"], a["kind"]) for a in got["agents"]],
            [(AGENT, "general-purpose", "Run the tests", "active", "background")])

    def test_the_line_is_how_the_screen_of_background_work_names_it(self):
        named = (call("Agent", "toolu_2", at="2026-08-25T10:01:00Z", description="Essay",
                      prompt="write", subagent_type="general-purpose", name="lighthouse-essay",
                      run_in_background=True)
                 + result("toolu_2", "Agent launched.", status="async_launched",
                          agentId="a4444444444444444", description="Essay", isAsync=True))
        got = self.state(async_agent("toolu_1", AGENT), named)
        self.assertEqual({a["id"]: a["line"] for a in got["agents"]},
                         {AGENT: "Run the tests", "a4444444444444444": "@lighthouse-essay"},
                         "the stop aims at the line the screen shows: a teammate by its name, a local agent by what it does")

    def test_one_that_finished_keeps_its_place_with_how_it_ended(self):
        got = self.state(async_agent("toolu_1", AGENT), agent_notification(AGENT))
        self.assertEqual([(a["id"], a["status"], a["doneAt"]) for a in got["agents"]],
                         [(AGENT, "completed", "2026-08-25T10:40:00Z")],
                         "a finished agent left the list: its conversation is still worth reading")

    def test_a_failure_is_told_from_a_finish(self):
        got = self.state(async_agent("toolu_1", AGENT), agent_notification(AGENT, "failed"))
        self.assertEqual(got["agents"][0]["status"], "failed")

    def test_a_stop_by_hand_is_a_stop(self):
        got = self.state(async_agent("toolu_1", AGENT),
                         call("TaskStop", "toolu_2", task_id=AGENT))
        self.assertEqual(got["agents"][0]["status"], "stopped")

    def test_a_letter_to_one_that_is_over_puts_it_back_to_work(self):
        got = self.state(async_agent("toolu_1", AGENT), agent_notification(AGENT),
                         call("SendMessage", "toolu_2", at="2026-08-25T10:50:00Z",
                              to=AGENT, message="one more thing"))
        agent = got["agents"][0]
        self.assertEqual((agent["status"], agent.get("doneAt")), ("active", None))

    def test_a_letter_by_name_puts_it_back_to_work(self):
        named = (call("Agent", "toolu_1", at="2026-08-25T10:01:00Z", description="Review",
                      prompt="review", subagent_type="general-purpose", name="review-302",
                      run_in_background=True)
                 + result("toolu_1", "Agent launched.", status="async_launched",
                          agentId=AGENT, description="Review", isAsync=True))
        letter = call("SendMessage", "toolu_2", at="2026-08-25T10:50:00Z", to="review-302", message="re-check")
        got = self.state(named, agent_notification(AGENT), letter)
        self.assertEqual(got["agents"][0]["status"], "active",
                         "a letter names the agent by its name, and the agent at work again stood as done")
        answered = self.state(named, agent_notification(AGENT), letter,
                              result("toolu_2", '{"success":true}', success=True,
                                     message="Resuming agent review-302", resumedAgentId=AGENT))
        self.assertEqual(answered["agents"][0]["status"], "active")
        self.assertEqual(work_of(answered)["agents"], 1, "the card counts no agent at work while one works")

    def test_a_letter_that_could_not_resume_the_agent_leaves_it_over(self):
        over = self.state(async_agent("toolu_1", AGENT), agent_notification(AGENT))["agents"][0]
        refused = (call("SendMessage", "toolu_2", at="2026-08-25T10:50:00Z", to=AGENT, message="next phase")
                   + result("toolu_2", '{"success":false}', success=False,
                            message="This agent cannot be resumed: its worktree no longer exists"))
        got = self.state(async_agent("toolu_1", AGENT), agent_notification(AGENT), refused)
        agent = got["agents"][0]
        self.assertEqual((agent["status"], agent.get("doneAt")), (over["status"], over.get("doneAt")),
                         "a letter the tool refused left the agent at work for good")
        self.assertEqual(work_of(got)["agents"], 0, "the card counts an agent nobody resumed as at work")

    def test_the_answer_to_a_letter_names_the_agent_it_resumed(self):
        letter = (call("SendMessage", "toolu_2", at="2026-08-25T10:50:00Z", to="someone-else", message="go")
                  + result("toolu_2", '{"success":true}', success=True, message="Resuming agent",
                           resumedAgentId=AGENT))
        got = self.state(async_agent("toolu_1", AGENT), agent_notification(AGENT), letter)
        self.assertEqual(got["agents"][0]["status"], "active")

    def test_one_at_work_stands_above_one_that_is_over(self):
        got = self.state(async_agent("toolu_1", "a1111111111111111", at="2026-08-25T10:00:00Z"),
                         async_agent("toolu_2", "a2222222222222222", at="2026-08-25T09:00:00Z"),
                         agent_notification("a1111111111111111"))
        self.assertEqual([a["id"] for a in got["agents"]],
                         ["a2222222222222222", "a1111111111111111"])

    def test_the_card_of_the_session_counts_only_the_ones_at_work(self):
        got = self.state(async_agent("toolu_1", "a1111111111111111"),
                         async_agent("toolu_2", "a2222222222222222"),
                         agent_notification("a1111111111111111"))
        self.assertEqual(work_of(got)["agents"], 1,
                         "a finished background agent is counted as one still at work")

    def test_over_the_limit_the_finished_give_way_and_the_working_stay(self):
        records = []
        for n in range(sesstate.MAX_ITEMS + 5):
            agent_id = f"a{n:016d}"
            records.append(async_agent(f"toolu_{n}", agent_id,
                                       at=f"2026-08-25T10:{n:02d}:00Z"))
            if n >= 5:
                records.append(agent_notification(agent_id))
        got = self.state(*records)
        self.assertLessEqual(len(got["agents"]), sesstate.MAX_ITEMS)
        self.assertEqual(sum(1 for a in got["agents"] if a["status"] == "active"), 5,
                         "an agent at work was dropped while finished ones stayed")


BORN = calendar.timegm(time.strptime("2026-08-25T10:30:00Z", "%Y-%m-%dT%H:%M:%SZ"))


class Restart(Transcript):
    def born(self, *chunks):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write("".join(chunks))
        return sesstate.read(path, born=BORN).snapshot()

    def test_one_sent_off_before_the_process_was_born_went_with_it(self):
        got = self.born(async_agent("toolu_1", AGENT, at="2026-08-25T10:00:00Z"))
        self.assertEqual([(a["status"], a["doneAt"]) for a in got["agents"]],
                         [("killed", "2026-08-25T10:30:00Z")],
                         "the agent died with the process, and the chip still counts it working")

    def test_one_that_finished_before_the_restart_keeps_its_own_end(self):
        got = self.born(async_agent("toolu_1", AGENT, at="2026-08-25T10:00:00Z"),
                        agent_notification(AGENT))
        self.assertEqual(got["agents"][0]["status"], "completed")


class Files(Transcript):
    """The file of an agent sent off without a name knows its type and its task, not its model."""

    def setUp(self):
        super().setUp()
        self.folder = os.path.join(self.dir.name, "t", "subagents")
        os.makedirs(self.folder, exist_ok=True)

    def files(self, agent_id, *chunks, **meta):
        with open(os.path.join(self.folder, f"agent-{agent_id}.meta.json"), "w",
                  encoding="utf-8") as f:
            json.dump({"agentType": "cx-scout", "description": "Scouting the restore",
                       "toolUseId": "toolu_1", "requestShape": "background", **meta}, f)
        with open(os.path.join(self.folder, f"agent-{agent_id}.jsonl"), "w",
                  encoding="utf-8") as f:
            f.write("".join(chunks) or "{}\n")

    def test_the_model_and_the_context_come_from_its_last_request(self):
        self.files(AGENT, request(35600))
        agent = self.state(async_agent("toolu_1", AGENT))["agents"][0]
        self.assertEqual((agent["model"], agent["tokens"]), ("claude-sonnet-5", 35600))
        self.assertTrue(agent["last"], "the last word of the agent is not known")

    def test_a_model_the_file_names_wins_over_the_request(self):
        self.files(AGENT, request(35600), model="haiku")
        agent = self.state(async_agent("toolu_1", AGENT))["agents"][0]
        self.assertEqual(agent["model"], "haiku")

    def test_its_context_moves_while_the_session_says_nothing(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(async_agent("toolu_1", AGENT))
        self.files(AGENT, request(1000))
        state = sesstate.read(path)
        self.files(AGENT, request(1000), request(52000, at="2026-08-25T10:09:00Z"))
        agent = sesstate.read(path, state).snapshot()["agents"][0]
        self.assertEqual(agent["tokens"], 52000,
                         "the context of the agent froze while the session waited for it")

    def test_its_shell_is_the_work_of_the_session_under_what_it_was_sent_to_do(self):
        self.files(AGENT, background("toolu_9", "b00000009"))
        got = self.state(async_agent("toolu_1", AGENT))
        self.assertEqual([(t["id"], t["agent"]) for t in got["tasks"]],
                         [("b00000009", "Scouting the restore")],
                         "five agents of one type would name their shells alike")

    def talk_at(self, agent_id, when):
        path = os.path.join(self.folder, f"agent-{agent_id}.jsonl")
        os.utime(path, (when, when))

    def test_one_its_file_says_was_stopped_is_stopped_when_it_last_moved(self):
        self.files(AGENT, request(35600), stoppedByUser=True)
        self.talk_at(AGENT, calendar.timegm((2026, 8, 25, 10, 20, 0)))
        got = self.state(async_agent("toolu_1", AGENT))
        self.assertEqual([(a["status"], a["doneAt"]) for a in got["agents"]],
                         [("stopped", "2026-08-25T10:20:00Z")],
                         "claude stopped the agent with the turn and said nothing to the session: "
                         "it still counts as working")
        self.assertEqual(work_of(got)["agents"], 0, "the card of the session counts a stopped agent at work")

    def test_one_whose_conversation_ends_in_a_stop_is_stopped(self):
        for mark in ("[Request interrupted by user]", "[Request interrupted by user for tool use]"):
            self.files(AGENT, request(35600), stopped_by(mark),
                       line({"type": "attachment", "timestamp": "2026-08-25T10:06:00Z"}))
            got = self.state(async_agent("toolu_1", AGENT))
            self.assertEqual(got["agents"][0]["status"], "stopped",
                             f"an agent whose last word is {mark} is taken as working")

    def test_one_given_work_past_the_mark_is_at_work(self):
        self.files(AGENT, request(35600), stopped_by("[Request interrupted by user]"),
                   line({"type": "user", "timestamp": "2026-08-25T10:07:00Z",
                         "message": {"content": "go on"}}),
                   request(36000, at="2026-08-25T10:07:30Z"))
        got = self.state(async_agent("toolu_1", AGENT))
        self.assertEqual(got["agents"][0]["status"], "active",
                         "a stop the agent has written past is taken for its end")

    def test_a_letter_does_not_bring_a_stopped_one_back(self):
        self.files(AGENT, request(35600), stoppedByUser=True)
        got = self.state(async_agent("toolu_1", AGENT),
                         call("SendMessage", "toolu_2", at="2026-08-25T10:50:00Z",
                              to=AGENT, message="carry on"))
        self.assertEqual(got["agents"][0]["status"], "stopped",
                         "claude refuses a letter to an agent it stopped, and the panel put it back to work")

    def test_one_that_finished_keeps_its_end_whatever_its_file_says(self):
        self.files(AGENT, request(35600), stoppedByUser=True)
        got = self.state(async_agent("toolu_1", AGENT), agent_notification(AGENT))
        self.assertEqual(got["agents"][0]["status"], "completed")

    def test_its_file_does_not_stand_in_for_a_teammate_of_the_same_name(self):
        self.files(AGENT, request(35600), agentType="alpha")
        spawned = (call("Agent", "toolu_2", name="alpha", description="audit")
                   + result("toolu_2", "Spawned successfully.\nagent_id: alpha@session-abc",
                            status="teammate_spawned"))
        got = self.state(spawned)
        self.assertEqual([(a["name"], a.get("id")) for a in got["agents"]], [("alpha", None)],
                         "the file of a nameless agent was taken for the teammate")


if __name__ == "__main__":
    unittest.main()
