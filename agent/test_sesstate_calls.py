import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import sesstate  # noqa: E402
from sesstate import calls  # noqa: E402
from test_sesstate import Transcript, call, line, result  # noqa: E402


def turn_end(at="2026-08-25T10:00:05Z"):
    return line({"type": "system", "subtype": "turn_duration", "timestamp": at})


class Head(unittest.TestCase):
    """What a card says a call runs: a few words, never the whole input."""

    def test_a_command_is_said_by_its_first_words(self):
        self.assertEqual(calls.head("Bash", {"command": "tar -xf archive.tgz -C /srv"}), "tar -xf archive.tgz")

    def test_a_command_stops_at_the_first_word_shell_makes_something_of(self):
        for command, want in (('git commit -m "the token is x"', "git commit -m"),
                              ("curl -H 'Authorization: Bearer secret' https://x", "curl -H"),
                              ("echo $TOKEN", "echo"),
                              ("ls >out.txt", "ls"),
                              ("docker run --env=KEY=v img", "docker run")):
            self.assertEqual(calls.head("Bash", {"command": command}), want, command)

    def test_the_assignments_before_a_command_are_left_out(self):
        self.assertEqual(calls.head("Bash", {"command": "TOKEN=secret OTHER=x make deploy"}), "make deploy")

    def test_a_command_of_many_lines_is_said_by_its_first(self):
        self.assertEqual(calls.head("Bash", {"command": "cd /srv\nrm -rf build"}), "cd /srv")

    def test_a_long_word_is_cut(self):
        got = calls.head("Bash", {"command": "/" + "a" * 80})
        self.assertEqual(len(got), calls.HEAD_MAX)
        self.assertTrue(got.endswith("…"))

    def test_a_file_is_said_by_its_name(self):
        self.assertEqual(calls.head("Edit", {"file_path": "/srv/app/web/src/ui.js", "old_string": "a"}), "ui.js")
        self.assertEqual(calls.head("NotebookEdit", {"notebook_path": "/srv/n/plot.ipynb"}), "plot.ipynb")

    def test_a_page_is_said_by_its_host(self):
        self.assertEqual(calls.head("WebFetch", {"url": "https://api.example.com/v1?key=secret"}), "api.example.com")

    def test_a_tool_the_card_knows_nothing_of_says_nothing(self):
        self.assertEqual(calls.head("mcp__panel__secret_ask", {"name": "token", "text": "the key"}), "")
        self.assertEqual(calls.head("Bash", "not a dict"), "")


class OpenCalls(Transcript):
    """The calls a dialog can be about: written, and with no result yet."""

    def read(self, *chunks):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write("".join(chunks))
        return sesstate.read(path)

    def test_a_call_without_its_result_is_open_and_one_with_it_is_not(self):
        state = self.read(call("Read", "toolu_1", file_path="/srv/a.py"), result("toolu_1"),
                          call("Bash", "toolu_2", command="make check"))
        self.assertEqual(list(state.open_calls), ["toolu_2"])
        self.assertEqual(calls.asking(state, "Bash", "toolu_2")["head"], "make check")

    def test_a_permission_names_its_call_and_without_an_id_the_newest_of_its_tool(self):
        state = self.read(call("Bash", "toolu_1", command="sleep 1000"),
                          call("Edit", "toolu_2", file_path="/srv/ui.js"),
                          call("Bash", "toolu_3", command="git push origin"))
        self.assertEqual(calls.asking(state, "Bash", "toolu_1")["head"], "sleep 1000")
        self.assertEqual(calls.asking(state, "Bash", "")["head"], "git push origin")
        self.assertIsNone(calls.asking(state, "Write", ""))

    def test_a_dialog_in_the_console_is_about_the_newest_call(self):
        state = self.read(call("Bash", "toolu_1", command="sleep 1000"), call("Edit", "toolu_2", file_path="/srv/ui.js"))
        self.assertEqual(calls.dialog(state)["head"], "ui.js")

    def test_a_dialog_while_an_agent_works_is_not_about_the_agent(self):
        state = self.read(call("Agent", "toolu_1", description="Audit", prompt="go"))
        self.assertIsNone(calls.dialog(state), "the dialog is about a call of the agent, which this transcript lacks")

    def test_the_end_of_a_turn_lets_its_calls_go(self):
        state = self.read(call("Bash", "toolu_1", command="make"), turn_end())
        self.assertEqual(state.open_calls, {})
        self.assertIsNone(calls.dialog(state))

    def test_a_call_from_before_the_process_started_is_let_go(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(call("Bash", "toolu_1", at="2026-08-25T10:00:00Z", command="make")
                    + call("Edit", "toolu_2", at="2026-08-25T11:00:00Z", file_path="/srv/a.py"))
        born = 1787653200  # 2026-08-25T10:20:00Z
        state = sesstate.read(path, born=born)
        self.assertEqual(list(state.open_calls), ["toolu_2"])

    def test_a_call_of_an_agent_outlives_the_turn_of_the_session(self):
        folder = os.path.join(self.dir.name, "t", "subagents")
        os.makedirs(folder, exist_ok=True)
        with open(os.path.join(folder, "agent-aalpha-0123456789abcdef.meta.json"), "w", encoding="utf-8") as f:
            json.dump({"name": "alpha", "description": "audit", "model": "opus", "color": "blue"}, f)
        with open(os.path.join(folder, "agent-aalpha-0123456789abcdef.jsonl"), "w", encoding="utf-8") as f:
            f.write(call("Read", "toolu_read", at="2026-08-25T10:00:30Z", file_path="/srv/a.py")
                    + result("toolu_read", at="2026-08-25T10:00:31Z")
                    + call("Bash", "toolu_sub", at="2026-08-25T10:01:00Z", command="npm publish"))
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(call("Agent", "toolu_1", description="audit", prompt="go", run_in_background=True)
                    + result("toolu_1", "launched", status="async_launched", agentId="aalpha-0123456789abcdef"))
        state = sesstate.read(path)
        with open(path, "a", encoding="utf-8") as f:
            f.write(turn_end())
        state = sesstate.read(path, state)
        self.assertEqual((calls.asking(state, "Bash", "toolu_sub") or {}).get("head"), "npm publish",
                         "the agent works on after the turn of the session, and its call asks on")
        self.assertIsNone(calls.dialog(state), "the session's own turn is over")
        self.assertNotIn("toolu_read", state.open_calls, "the call of the agent that came back is not open")

    def test_the_open_calls_are_bounded(self):
        state = self.read(*[call("Bash", f"toolu_{i}", command="true") for i in range(calls.MAX_OPEN + 5)])
        self.assertEqual(len(state.open_calls), calls.MAX_OPEN)
        self.assertIn(f"toolu_{calls.MAX_OPEN + 4}", state.open_calls)
        self.assertNotIn("toolu_0", state.open_calls)


if __name__ == "__main__":
    unittest.main()
