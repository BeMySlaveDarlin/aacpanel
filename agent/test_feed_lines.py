"""What a terminal prints beside the conversation reaches the feed too."""
import json
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402
from chat import spots  # noqa: E402

AT = "2026-09-26T10:00:00Z"
CWD = "/srv/proj"

AGENT_DONE = """<task-notification>
<task-id>a515a204cce27a85c</task-id>
<tool-use-id>toolu_agent</tool-use-id>
<status>completed</status>
<summary>Agent "Commit the notes" finished</summary>
<usage><subagent_tokens>33108</subagent_tokens><tool_uses>6</tool_uses><duration_ms>24233</duration_ms></usage>
</task-notification>"""

COMMAND_DONE = """<task-notification>
<task-id>bg3me3whb</task-id>
<tool-use-id>toolu_make</tool-use-id>
<status>failed</status>
<summary>Background command "make check" failed with exit code 2</summary>
</task-notification>"""


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def system(subtype, **fields):
    return {"type": "system", "subtype": subtype, "timestamp": AT, "cwd": CWD, **fields}


def hook(kind, name, **fields):
    return {"type": "attachment", "timestamp": AT, "cwd": CWD,
            "attachment": {"type": kind, "hookName": name, **fields}}


def call(use, command):
    return {"type": "assistant", "timestamp": AT, "cwd": CWD,
            "message": {"content": [{"type": "tool_use", "id": use, "name": "Bash",
                                     "input": {"command": command}}]}}


def result(use):
    return {"type": "user", "timestamp": AT, "cwd": CWD,
            "message": {"content": [{"type": "tool_result", "tool_use_id": use, "content": "ok"}]}}


class Records(unittest.TestCase):
    def parse(self, record):
        return chat.records.parse(record, 7)

    def test_an_agent_done_says_how_long_it_ran_and_what_it_spent(self):
        got = chat.harness.service(AGENT_DONE, AT, 7)
        self.assertEqual(got, [{"role": "taskdone", "use": "toolu_agent", "status": "completed",
                                "summary": 'Agent "Commit the notes" finished',
                                "ms": 24233, "tokens": 33108, "at": AT, "pos": 7}])

    def test_a_command_done_says_neither(self):
        got = chat.harness.service(COMMAND_DONE, AT, 7)[0]
        self.assertEqual((got["status"], got["use"]), ("failed", "toolu_make"))
        self.assertNotIn("ms", got)
        self.assertNotIn("tokens", got)

    def test_a_turn_says_how_long_it_took(self):
        self.assertEqual(self.parse(system("turn_duration", durationMs=169000, messageCount=40)),
                         [{"role": "turn", "ms": 169000, "at": AT, "pos": 7}])
        for bad in (0, -5, True, "169000", None):
            self.assertEqual(self.parse(system("turn_duration", durationMs=bad)), [], bad)

    def test_what_claude_says_beside_the_conversation_is_a_line(self):
        cases = [
            (system("away_summary", content="Working on the panel; the tree is clean."),
             {"from": "while you were away", "level": "info"}),
            (system("informational", level="warning", content="Unknown command: /storage"),
             {"level": "warn"}),
            (system("informational", level="notice", content="agents-md: AGENTS.md loaded"),
             {"level": "info"}),
            (system("model_refusal_fallback", content="The safeguards flagged this message."),
             {"from": "safeguards", "level": "crit"}),
            (system("model_refusal_no_fallback", content=""),
             {"from": "safeguards", "level": "crit", "text": chat.notices.REFUSED}),
        ]
        for record, want in cases:
            got = self.parse(record)
            self.assertEqual(len(got), 1, record)
            self.assertEqual(got[0]["role"], "notice")
            for key, value in want.items():
                self.assertEqual(got[0].get(key), value, (record["subtype"], key))
            self.assertEqual(bool(got[0].get("from")), "from" in want, record["subtype"])

    def test_a_system_record_the_terminal_keeps_to_itself_is_not_a_line(self):
        for subtype in ("stop_hook_summary", "bridge_status", "compact_boundary"):
            self.assertEqual(self.parse(system(subtype, content="x")), [], subtype)

    def test_a_hook_message_is_a_call_of_the_hook_kind(self):
        got = self.parse(hook("hook_system_message", "PreToolUse:Bash",
                              content="harness: the budget ran out,\nskipped 2 handlers"))
        self.assertEqual(got, [{"role": "tool", "name": "PreToolUse:Bash hook", "kind": "hook",
                                "arg": "harness: the budget ran out, ⏎ skipped 2 handlers",
                                "at": AT, "use": "", "pos": 7, "index": 0}])

    def test_a_blocking_hook_error_is_left_to_the_letter_it_comes_back_as(self):
        blocked = hook("hook_blocking_error", "Stop",
                       blockingError={"blockingError": "apply the simplification skill"})
        self.assertEqual(self.parse(blocked), [])

    def test_an_attachment_that_is_nothing_to_the_person_stays_out(self):
        for kind in ("hook_success", "total_tokens_reminder", "hook_additional_context", "date"):
            self.assertEqual(self.parse(hook(kind, "SessionStart")), [], kind)


class Runs(unittest.TestCase):
    def setUp(self):
        chat.tail.PIECES.forget()
        self.root = test_barrier.tmp_path(prefix="chat-lines-")
        self.addCleanup(shutil.rmtree, self.root, True)
        self.path = os.path.join(self.root, "44444444-4444-4444-4444-444444444444.jsonl")

    def feed(self, *records):
        with open(self.path, "w", encoding="utf-8") as f:
            f.write("".join(line(r) for r in records))
        return chat.feed(self.path)["items"]

    def test_hooks_add_to_a_badge_of_the_run_they_arrive_in(self):
        items = self.feed(call("t1", "make"),
                          hook("hook_system_message", "PreToolUse:Bash", content="budget ran out"),
                          result("t1"),
                          call("t2", "make check"),
                          hook("hook_system_message", "PreToolUse:Bash", content="budget ran out"),
                          result("t2"))
        self.assertEqual([(i["role"], i.get("kind"), len(i.get("calls", []))) for i in items],
                         [("tools", "bash", 2), ("tools", "hook", 2)])
        self.assertEqual(len({i["run"] for i in items}), 1, "the hooks split the run")

    def test_a_line_inside_a_run_does_not_end_it(self):
        items = self.feed(call("t1", "make"), result("t1"),
                          system("informational", level="warning", content="Unknown command: /x"),
                          call("t2", "make check"), result("t2"))
        self.assertEqual([i["role"] for i in items], ["tools", "notice"])
        self.assertEqual(len(items[0]["calls"]), 2)

    def test_a_hook_opens_with_its_words_as_the_result(self):
        self.feed(call("t1", "make"),
                  hook("hook_non_blocking_error", "PostToolUse:Bash", stderr="exit status 3"))
        with open(self.path, "rb") as f:
            f.readline()
            pos = f.tell()
        got = spots.call(self.path, pos, 0)
        self.assertEqual((got["tool"], got["result"], got["failed"]),
                         ("PostToolUse:Bash hook", "exit status 3", True))
        self.assertIsNone(spots.call(self.path, pos, 1))


if __name__ == "__main__":
    unittest.main()
