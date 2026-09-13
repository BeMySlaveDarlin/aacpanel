import calendar
import json
import os
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import sesstate  # noqa: E402
from test_sesstate import Transcript, call, line, result, spawn  # noqa: E402


def tool_output(text, tool_id="toolu_9", at="2026-08-25T10:45:00Z"):
    return line({"type": "user", "timestamp": at,
                 "message": {"content": [
                     {"type": "tool_result", "tool_use_id": tool_id, "content": text}]}})


def letters(*names, at="2026-08-25T10:30:00Z"):
    body = "Another Claude session sent a message:\n" + "\n\n".join(
        f'<teammate-message teammate_id="{n}" color="blue">\ndone\n</teammate-message>'
        for n in names)
    return line({"type": "user", "timestamp": at,
                 "message": {"role": "user", "content": body}})


def letter(name, at="2026-08-25T10:30:00Z"):
    return line({"type": "user", "timestamp": at,
                 "message": {"role": "user", "content":
                             "Another Claude session sent a message: "
                             f'<teammate-message teammate_id="{name}" color="blue">'
                             "\nfound three of them\n</teammate-message>"}})


def mail(tool_id, to, delivered=True, at="2026-08-25T10:40:00Z"):
    outcome = ({"success": True, "message": f"Message sent to {to}'s inbox"} if delivered
               else {"success": False,
                     "message": f"No agent named '{to}' is reachable.\n"
                                "Use ListAgents to see everyone you can message."})
    return (call("SendMessage", tool_id, at=at, to=to, message="go on")
            + result(tool_id, json.dumps(outcome), at=at, **outcome))


def terminated(name, at="2026-08-25T11:00:00Z"):
    return line({"type": "user", "timestamp": at,
                 "message": {"role": "user", "content":
                             'Another Claude session sent a message: '
                             '<teammate-message teammate_id="system">\n'
                             '{"type":"teammate_terminated","message":"'
                             f'{name} has shut down."}}\n</teammate-message>'}})


class Leaving(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.session = "1a2b3c4d-0000-0000-0000-000000000000"
        self.path = os.path.join(self.dir.name, self.session + ".jsonl")
        self.teams = os.path.join(self.dir.name, "teams")
        self.old, sesstate.TEAMS_DIR = sesstate.TEAMS_DIR, self.teams
        self.addCleanup(lambda: setattr(sesstate, "TEAMS_DIR", self.old))
        sesstate._team_cache.clear()

    def write(self, *chunks):
        with open(self.path, "w", encoding="utf-8") as f:
            f.write("".join(chunks))
        return sesstate.read(self.path).snapshot()

    def team(self, *names):
        d = os.path.join(self.teams, "session-" + self.session[:8])
        os.makedirs(d, exist_ok=True)
        members = [{"name": "team-lead"}] + [{"name": n} for n in names]
        with open(os.path.join(d, "config.json"), "w", encoding="utf-8") as f:
            json.dump({"members": members}, f)

    def test_a_shutdown_removes_the_agent(self):
        got = self.write(spawn("toolu_1", "alpha"), spawn("toolu_2", "beta"),
                         terminated("alpha"))
        self.assertEqual([a["name"] for a in got["agents"]], ["beta"])

    def test_a_shutdown_removes_even_one_that_reported(self):
        got = self.write(spawn("toolu_1", "alpha"), letter("alpha"),
                         terminated("alpha"))
        self.assertEqual(got["agents"], [])

    def test_one_dropped_from_the_team_disappears_with_no_trace_in_the_transcript(self):
        self.team("beta")
        got = self.write(spawn("toolu_1", "alpha"), spawn("toolu_2", "beta"))
        self.assertEqual([a["name"] for a in got["agents"]], ["beta"])

    def test_without_a_team_roster_the_list_stays_as_it_is(self):
        got = self.write(spawn("toolu_1", "alpha"))
        self.assertEqual([a["name"] for a in got["agents"]], ["alpha"])

    def test_the_roster_is_reread_when_it_changes(self):
        self.team("alpha", "beta")
        state = sesstate.read(self.path) if os.path.exists(self.path) else None
        got = self.write(spawn("toolu_1", "alpha"), spawn("toolu_2", "beta"))
        self.assertEqual(len(got["agents"]), 2)
        self.team("beta")
        again = sesstate.read(self.path).snapshot()
        self.assertEqual([a["name"] for a in again["agents"]], ["beta"])


class Agents(Transcript):
    def test_a_spawned_agent_is_visible(self):
        got = self.state(spawn("toolu_1", "alpha"))
        self.assertEqual([(a["name"], a["status"]) for a in got["agents"]],
                         [("alpha", "active")])

    def test_a_respawn_of_the_same_name_is_visible_again(self):
        got = self.state(spawn("toolu_1", "gamma"), letter("gamma"),
                         spawn("toolu_2", "gamma", at="2026-08-25T12:00:00Z"))
        self.assertEqual([(a["name"], a["at"], a["status"]) for a in got["agents"]],
                         [("gamma", "2026-08-25T12:00:00Z", "active")])

    def test_a_message_from_an_agent_does_not_remove_it_but_marks_it_reported(self):
        got = self.state(spawn("toolu_1", "alpha"), letter("alpha"))
        self.assertEqual([(a["name"], a["status"], a["reportedAt"]) for a in got["agents"]],
                         [("alpha", "reported", "2026-08-25T10:30:00Z")])

    def test_two_messages_in_a_row_do_not_remove_the_agent(self):
        got = self.state(spawn("toolu_1", "alpha"), letter("alpha"),
                         letter("alpha"))
        self.assertEqual([a["name"] for a in got["agents"]], ["alpha"])
        self.assertEqual(got["agents"][0]["status"], "reported")

    def test_a_message_to_the_agent_puts_it_back_among_the_active_ones(self):
        got = self.state(
            spawn("toolu_1", "alpha"), letter("alpha"),
            call("SendMessage", "toolu_2", at="2026-08-25T10:40:00Z", to="alpha"),
        )
        self.assertEqual([(a["name"], a["status"], a["reportedAt"]) for a in got["agents"]],
                         [("alpha", "active", "2026-08-25T10:30:00Z")])

    def test_a_delivered_message_keeps_the_agent_active(self):
        got = self.state(spawn("toolu_1", "alpha"), letter("alpha"),
                         mail("toolu_2", "alpha"))
        self.assertEqual([(a["name"], a["status"]) for a in got["agents"]],
                         [("alpha", "active")])

    def test_a_message_that_found_nobody_returns_the_agent_to_reported(self):
        got = self.state(spawn("toolu_1", "alpha"), letter("alpha"),
                         mail("toolu_2", "alpha", delivered=False))
        self.assertEqual([(a["name"], a["status"], a["reportedAt"]) for a in got["agents"]],
                         [("alpha", "reported", "2026-08-25T10:30:00Z")])

    def test_a_message_that_found_nobody_forgets_an_agent_that_never_wrote(self):
        got = self.state(spawn("toolu_1", "alpha"),
                         mail("toolu_2", "alpha", delivered=False))
        self.assertEqual(got["agents"], [])

    def test_a_message_with_no_verdict_leaves_the_agent_active(self):
        got = self.state(spawn("toolu_1", "alpha"), letter("alpha"),
                         call("SendMessage", "toolu_2", to="alpha")
                         + result("toolu_2", "Message sent"))
        self.assertEqual(got["agents"][0]["status"], "active")

    def test_a_refusal_for_another_session_does_not_touch_the_agents(self):
        got = self.state(spawn("toolu_1", "alpha"), letter("alpha"),
                         mail("toolu_2", "docs", delivered=False))
        self.assertEqual([(a["name"], a["status"]) for a in got["agents"]],
                         [("alpha", "reported")])

    def test_a_message_to_another_session_does_not_touch_the_agents(self):
        got = self.state(
            spawn("toolu_1", "alpha"), letter("alpha"),
            call("SendMessage", "toolu_2", to="docs"),
        )
        self.assertEqual(got["agents"][0]["status"], "reported")

    def test_a_message_from_one_agent_does_not_touch_another(self):
        got = self.state(spawn("toolu_1", "alpha"), spawn("toolu_2", "gamma"),
                         letter("gamma"))
        by_name = {a["name"]: a["status"] for a in got["agents"]}
        self.assertEqual(by_name, {"alpha": "active", "gamma": "reported"})

    def test_a_batch_of_messages_marks_every_sender(self):
        got = self.state(spawn("toolu_1", "alpha"),
                         spawn("toolu_2", "gamma"),
                         letters("alpha", "gamma"))
        self.assertEqual({a["name"]: a["status"] for a in got["agents"]},
                         {"alpha": "reported", "gamma": "reported"})

    def test_tool_output_does_not_count_as_a_message(self):
        got = self.state(spawn("toolu_1", "alpha"),
                         tool_output('530: if f\'teammate_id="alpha"\' in raw:'))
        self.assertEqual([(a["name"], a["status"]) for a in got["agents"]],
                         [("alpha", "active")])

    def test_tool_output_does_not_remove_an_agent(self):
        got = self.state(
            spawn("toolu_1", "alpha"),
            tool_output('44: `{"type": "teammate_terminated", "message": '
                        '"alpha has shut down."}`'))
        self.assertEqual([a["name"] for a in got["agents"]], ["alpha"])

    def test_an_agent_launch_that_failed_is_not_shown(self):
        got = self.state(call("Agent", "toolu_1", name="alpha")
                         + result("toolu_1", "no such agent type"))
        self.assertEqual(got["agents"], [])


BORN = calendar.timegm(time.strptime("2026-08-25T10:30:00Z", "%Y-%m-%dT%H:%M:%SZ"))


class Restart(Transcript):
    def born(self, born, *chunks):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write("".join(chunks))
        return sesstate.read(path, born=born).snapshot()

    def test_an_agent_spawned_before_the_process_was_born_did_not_outlive_it(self):
        got = self.born(BORN, spawn("toolu_1", "alpha", at="2026-08-25T10:00:00Z"))
        self.assertEqual(got["agents"], [])

    def test_one_that_wrote_before_the_restart_stays_reported(self):
        got = self.born(BORN, spawn("toolu_1", "alpha", at="2026-08-25T10:00:00Z"),
                        letter("alpha", at="2026-08-25T10:10:00Z"),
                        call("SendMessage", "toolu_2", at="2026-08-25T10:20:00Z", to="alpha"))
        self.assertEqual([(a["name"], a["status"], a["reportedAt"]) for a in got["agents"]],
                         [("alpha", "reported", "2026-08-25T10:10:00Z")])

    def test_an_agent_spawned_after_the_birth_is_working(self):
        got = self.born(BORN, spawn("toolu_1", "alpha", at="2026-08-25T11:00:00Z"))
        self.assertEqual([(a["name"], a["status"]) for a in got["agents"]],
                         [("alpha", "active")])

    def test_a_namesake_spawned_after_the_restart_is_working(self):
        got = self.born(BORN, spawn("toolu_1", "alpha", at="2026-08-25T10:00:00Z"),
                        spawn("toolu_2", "alpha", at="2026-08-25T11:00:00Z"))
        self.assertEqual([(a["name"], a["at"], a["status"]) for a in got["agents"]],
                         [("alpha", "2026-08-25T11:00:00Z", "active")])

    def test_without_a_birth_nothing_is_lost(self):
        got = self.born(None, spawn("toolu_1", "alpha", at="2026-08-25T10:00:00Z"))
        self.assertEqual([(a["name"], a["status"]) for a in got["agents"]],
                         [("alpha", "active")])

    def test_the_birth_is_remembered_by_a_read_that_does_not_name_it(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        open(path, "w", encoding="utf-8").close()
        state = sesstate.read(path, born=BORN)
        with open(path, "a", encoding="utf-8") as f:
            f.write(spawn("toolu_1", "alpha", at="2026-08-25T10:00:00Z"))
        got = sesstate.read(path, state).snapshot()
        self.assertEqual(got["agents"], [])

    def test_a_birth_told_after_the_reading_lets_go_what_was_read_before_it(self):
        # The birth may reach the state after the transcript was read, and the
        # file need not grow in between.
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(spawn("toolu_1", "alpha", at="2026-08-25T10:00:00Z"))
        state = sesstate.read(path)
        self.assertEqual([a["status"] for a in state.snapshot()["agents"]], ["active"])
        got = sesstate.read(path, state, born=BORN).snapshot()
        self.assertEqual(got["agents"], [],
                         "a birth told to a read that found nothing new changed nothing")


class AgentPruning(Transcript):
    def spawn_and_report(self, n, minute_offset=0):
        chunks = []
        for i in range(n):
            name = f"a{i}"
            at = f"2026-08-25T{10 + (i + minute_offset) // 60:02d}:{(i + minute_offset) % 60:02d}:00Z"
            chunks.append(spawn(f"toolu_{i}", name, at=at))
            chunks.append(letter(name, at=at))
        return chunks

    def test_reported_agents_over_the_limit_are_forgotten_oldest_first(self):
        over = sesstate.MAX_ITEMS + 5
        got = self.state(*self.spawn_and_report(over))
        self.assertEqual(len(got["agents"]), sesstate.MAX_ITEMS)
        names = {a["name"] for a in got["agents"]}
        self.assertNotIn("a0", names)
        self.assertIn(f"a{over - 1}", names)

    def test_an_active_agent_is_not_forgotten_even_over_the_limit(self):
        chunks = self.spawn_and_report(sesstate.MAX_ITEMS + 5)
        chunks.append(spawn("toolu_last", "still-active",
                            at="2026-08-25T12:00:00Z"))
        got = self.state(*chunks)
        by_name = {a["name"]: a["status"] for a in got["agents"]}
        self.assertEqual(by_name.get("still-active"), "active")


if __name__ == "__main__":
    unittest.main()


class AgentMeta(Transcript):
    def meta(self, agent_id, name, when, **extra):
        folder = os.path.join(self.dir.name, "t", "subagents")
        os.makedirs(folder, exist_ok=True)
        with open(os.path.join(folder, f"agent-{agent_id}.meta.json"), "w",
                  encoding="utf-8") as f:
            json.dump({"name": name, "description": "reconnaissance",
                       "model": "opus[1m]", "color": "blue", **extra}, f)
        talk = os.path.join(folder, f"agent-{agent_id}.jsonl")
        with open(talk, "w", encoding="utf-8") as f:
            f.write("{}\n")
        os.utime(talk, (when, when))

    def test_the_feed_address_and_the_kind_reach_the_panel(self):
        self.meta("aalpha-0123456789abcdef", "alpha", 1_700_000_000)
        got = self.state(spawn("toolu_1", "alpha"))
        self.assertEqual([(a["id"], a["kind"]) for a in got["agents"]],
                         [("aalpha-0123456789abcdef", "subagent")])

    def test_a_teammate_is_told_apart_from_a_subagent(self):
        self.meta("adelta-0123456789abcdef", "delta", 1_700_000_000,
                  taskKind="in_process_teammate")
        got = self.state(spawn("toolu_1", "delta"))
        self.assertEqual(got["agents"][0]["kind"], "teammate")

    def test_of_two_namesakes_the_fresher_one_is_taken(self):
        self.meta("aalpha-1111111111111111", "alpha", 1_700_000_000)
        self.meta("aalpha-2222222222222222", "alpha", 1_700_009_000)
        got = self.state(spawn("toolu_1", "alpha"))
        self.assertEqual(got["agents"][0]["id"], "aalpha-2222222222222222")
