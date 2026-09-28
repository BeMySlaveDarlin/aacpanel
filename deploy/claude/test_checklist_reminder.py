#!/usr/bin/env python3

import contextlib
import datetime
import importlib.util
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


reminder = load("checklist-reminder")

SESSION = "5a0c7d1e-2b3f-4a5b-8c6d-7e8f9a0b1c2d"
BEFORE = "6b1d8e2f-3c4a-4b5c-9d7e-8f9a0b1c2d3e"
LAB = "/srv/proj/lab"

# The claude running the hook, as the fake /proc shows it.
CLAUDE = 4242
START = "5555"

# The turn under test begins here; the checklist is written a minute before it
# or half a minute into it.
BEGAN = "2026-09-28T10:00:00.000Z"
BEGAN_AT = datetime.datetime(2026, 9, 28, 10, 0, tzinfo=datetime.timezone.utc).timestamp()

WITH_TOOL = ["/srv/bin/claude", "--mcp-config", '{"mcpServers":{}}', "--allowedTools", reminder.TOOL, "-n", "lab"]
BY_HAND = ["/srv/bin/claude", "--resume", SESSION]


def prompt(text, at=BEGAN):
    return {"type": "user", "timestamp": at, "message": {"role": "user", "content": text}}


def call(name, at="2026-09-28T10:00:05.000Z", use="toolu_1"):
    return {"type": "assistant", "timestamp": at,
            "message": {"role": "assistant", "content": [{"type": "tool_use", "id": use, "name": name, "input": {}}]}}


def result(use="toolu_1", at="2026-09-28T10:00:06.000Z"):
    return {"type": "user", "timestamp": at,
            "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": use, "content": "ok"}]}}


def answer(text="done", at="2026-09-28T10:00:40.000Z"):
    return {"type": "assistant", "timestamp": at,
            "message": {"role": "assistant", "content": [{"type": "text", "text": text}]}}


class TestReminder(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.xdg = os.path.join(self.tmp.name, "state")
        self.config = os.path.join(self.tmp.name, "claude")
        self.proc = os.path.join(self.tmp.name, "proc")
        self.transcript = os.path.join(self.tmp.name, "transcript.jsonl")
        for name, value in (("PROC", self.proc), ("ancestors", lambda: [self.shell, CLAUDE])):
            self.addCleanup(setattr, reminder, name, getattr(reminder, name))
            setattr(reminder, name, value)
        # A shell stands between claude and the hook, and has no file of a session.
        self.shell = 4300
        self.process(self.shell, CLAUDE, "900", ["sh", "-c", "hook"])
        self.claude(WITH_TOOL)

    def process(self, pid, parent, start, args):
        root = os.path.join(self.proc, str(pid))
        os.makedirs(root, exist_ok=True)
        fields = ["S", str(parent)] + ["0"] * 17 + [start]
        with open(os.path.join(root, "stat"), "w", encoding="utf-8") as f:
            f.write(f"{pid} (claude) {' '.join(fields)}\n")
        with open(os.path.join(root, "cmdline"), "wb") as f:
            f.write(b"\0".join(a.encode() for a in args) + b"\0")

    def claude(self, args, session=SESSION, start=START, cwd=LAB):
        """Puts the claude running the hook in place: its process and the file it keeps of itself."""
        self.process(CLAUDE, 1, START, args)
        os.makedirs(os.path.join(self.config, "sessions"), exist_ok=True)
        with open(os.path.join(self.config, "sessions", f"{CLAUDE}.json"), "w", encoding="utf-8") as f:
            json.dump({"pid": CLAUDE, "sessionId": session, "cwd": cwd, "procStart": start,
                       "kind": "interactive"}, f)

    def checklist(self, *statuses, pid=CLAUDE, session=SESSION, written=BEGAN_AT - 60, cwd=LAB):
        root = os.path.join(self.xdg, "aacpanel", "checklists")
        os.makedirs(root, exist_ok=True)
        path = os.path.join(root, reminder.checklist_name(self.config, cwd))
        items = [{"text": f"step {i}", "status": s} for i, s in enumerate(statuses)]
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"configDir": self.config, "dir": cwd, "sessionId": session, "pid": pid,
                       "at": "2026-09-28T09:59:00Z", "items": items}, f)
        os.utime(path, (written, written))

    def turn(self, *records):
        with open(self.transcript, "w", encoding="utf-8") as f:
            for r in records:
                f.write(json.dumps(r) + "\n")

    def run_hook(self, payload=None):
        payload = {"hook_event_name": "Stop", "session_id": SESSION, "transcript_path": self.transcript,
                   "stop_hook_active": False, **(payload or {})}
        was = {k: os.environ.get(k) for k in ("XDG_STATE_HOME", "CLAUDE_CONFIG_DIR")}
        out = io.StringIO()
        try:
            os.environ["XDG_STATE_HOME"] = self.xdg
            os.environ["CLAUDE_CONFIG_DIR"] = self.config
            sys.stdin = io.StringIO(json.dumps(payload))
            with contextlib.redirect_stdout(out):
                reminder.main()
        finally:
            sys.stdin = sys.__stdin__
            for k, v in was.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
        text = out.getvalue().strip()
        return json.loads(text) if text else None

    def worked(self):
        self.turn(prompt("earlier"), answer(at="2026-09-28T09:50:00.000Z"),
                  prompt("fix the build"), call("Bash"), result(), call("Edit", use="toolu_2"),
                  result("toolu_2"), answer())

    def test_a_turn_that_worked_past_an_unfinished_checklist_is_held_once(self):
        self.checklist("done", "active", "pending")
        self.worked()
        got = self.run_hook()
        self.assertEqual(got and got["decision"], "block")
        self.assertIn("If the checklist changed", got["reason"])
        self.assertIn(reminder.TOOL, got["reason"])
        self.assertIn("clear it with an empty list", got["reason"])
        self.assertIn("If it did not change, end the turn now", got["reason"])
        self.assertIsNone(self.run_hook({"stop_hook_active": True}),
                          "the turn that answers the hold was held again: a loop of reminders")

    def test_a_session_started_again_in_the_place_is_asked_about_the_checklist_it_found(self):
        # The checklist was sent by the process before the restart, in another
        # conversation; this one has the tool from the panel.
        self.checklist("done", "active", pid=CLAUDE + 1, session=BEFORE)
        self.worked()
        got = self.run_hook()
        self.assertEqual(got and got["decision"], "block",
                         "a session started again with the tool is never asked about the checklist of its place")

    def test_a_session_without_the_tool_is_not_asked_about_a_checklist_it_did_not_send(self):
        # A claude started by hand in the same place cannot update the
        # checklist.
        self.claude(BY_HAND)
        self.checklist("active", pid=CLAUDE + 1, session=BEFORE)
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_session_that_sent_the_checklist_is_asked_whatever_it_was_started_with(self):
        self.claude(BY_HAND)
        self.checklist("active")
        self.worked()
        self.assertEqual(self.run_hook()["decision"], "block")

    def test_the_tool_is_told_by_the_arguments_of_claude(self):
        for args, has in ((WITH_TOOL, True), (BY_HAND, False),
                          (["claude", "--allowedTools=Bash," + reminder.TOOL], True),
                          (["claude", "--allowedTools", reminder.TOOL + "_old"], False),
                          (["claude", reminder.TOOL + "x"], False)):
            self.claude(args)
            self.assertEqual(reminder.has_tool(CLAUDE), has, args)

    def test_a_checklist_of_another_place_asks_nothing(self):
        self.checklist("active", cwd="/srv/proj/other")
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_file_that_names_another_place_is_not_the_checklist_whatever_its_name(self):
        self.checklist("active")
        path = os.path.join(self.xdg, "aacpanel", "checklists", reminder.checklist_name(self.config, LAB))
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        with open(path, "w", encoding="utf-8") as f:
            json.dump({**data, "dir": "/srv/proj/other"}, f)
        os.utime(path, (BEGAN_AT - 60, BEGAN_AT - 60))
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_claude_whose_file_names_another_conversation_is_not_the_one_whose_turn_ends(self):
        # A claude run inside the work of a session has no file of its own:
        # the nearest file above it is the session's, of another conversation.
        self.claude(WITH_TOOL, session=BEFORE)
        self.checklist("active")
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_file_a_dead_process_with_the_same_pid_left_is_not_the_claude(self):
        self.claude(WITH_TOOL, start="1234")
        self.checklist("active")
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_checklist_touched_in_the_turn_is_not_asked_about(self):
        self.checklist("done", "active", written=BEGAN_AT + 30)
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_turn_that_called_no_tool_is_not_asked(self):
        self.checklist("active", "pending")
        self.turn(prompt("earlier"), call("Bash", at="2026-09-28T09:50:00.000Z"),
                  result(at="2026-09-28T09:50:01.000Z"), prompt("what do you think?"), answer())
        self.assertIsNone(self.run_hook(), "a question answered in words was held for the checklist")

    def test_a_checklist_with_nothing_left_to_do_asks_nothing(self):
        self.checklist("done", "dropped", "done")
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_without_a_checklist_nothing_is_said(self):
        self.worked()
        self.assertIsNone(self.run_hook())

    def test_a_result_of_a_tool_is_not_the_start_of_the_turn(self):
        # The checklist was written after the last tool result but before the
        # prompt: a hook that took the result for a prompt would find the
        # checklist fresh.
        self.checklist("active", written=BEGAN_AT + 3)
        self.turn(prompt("go"), call("Bash", at="2026-09-28T10:00:05.000Z"),
                  result(at="2026-09-28T10:00:06.000Z"), answer(at="2026-09-28T10:00:07.000Z"))
        self.assertIsNone(self.run_hook(), "the checklist written inside the turn was taken for an old one")
        self.checklist("active", written=BEGAN_AT - 1)
        self.assertEqual(self.run_hook()["decision"], "block",
                         "the turn was taken from the tool's result and its call was missed")

    def test_a_turn_longer_than_the_tail_is_taken_from_what_was_read(self):
        # The tail holds the last call, its result and the answer, and not the
        # prompt: the turn is taken from the call, and the checklist is older.
        self.checklist("active")
        was = reminder.TAIL
        self.addCleanup(setattr, reminder, "TAIL", was)
        self.worked()
        reminder.TAIL = 500
        self.assertEqual(reminder.turn(self.transcript), (BEGAN_AT + 5, True))
        self.assertEqual(self.run_hook()["decision"], "block")
        reminder.TAIL = 200
        self.assertIsNone(self.run_hook(), "a tail with no call in it was taken for a turn that worked")

    def test_a_subagent_stop_is_not_the_session(self):
        self.checklist("active")
        self.worked()
        self.assertIsNone(self.run_hook({"hook_event_name": "SubagentStop"}))

    def test_a_transcript_that_is_not_there_asks_nothing(self):
        self.checklist("active")
        self.assertIsNone(self.run_hook({"transcript_path": os.path.join(self.tmp.name, "none.jsonl")}))

    def test_a_broken_payload_is_not_a_crash(self):
        try:
            sys.stdin = io.StringIO("not json")
            with contextlib.redirect_stdout(io.StringIO()) as out:
                reminder.main()
        finally:
            sys.stdin = sys.__stdin__
        self.assertEqual(out.getvalue(), "")


class TestPlace(unittest.TestCase):

    def test_the_name_of_the_checklist_is_the_one_the_executor_computes(self):
        # Pinned beside the executor's own test of the same place.
        self.assertEqual(reminder.checklist_name("/srv/claude", LAB), "d2ba143628e62863dae2533062199cf6.json")
        self.assertEqual(reminder.checklist_name("/srv/claude/", LAB + "/"), reminder.checklist_name("/srv/claude", LAB))
        self.assertIsNone(reminder.checklist_name("/srv/claude", "proj/lab"))

    def test_the_ancestors_are_read_up_the_chain_of_parents(self):
        with tempfile.TemporaryDirectory() as proc:
            for pid, parent in ((os.getppid(), 777), (777, 1)):
                os.makedirs(os.path.join(proc, str(pid)))
                with open(os.path.join(proc, str(pid), "stat"), "w", encoding="utf-8") as f:
                    f.write(f"{pid} (a (b) c) S {parent} 0 0\n")
            self.addCleanup(setattr, reminder, "PROC", reminder.PROC)
            reminder.PROC = proc
            self.assertEqual(reminder.ancestors(), [os.getppid(), 777])


if __name__ == "__main__":
    unittest.main()
