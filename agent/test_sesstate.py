import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import sesstate  # noqa: E402


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def plan(*items):
    return line({
        "type": "attachment",
        "attachment": {
            "type": "task_reminder",
            "itemCount": len(items),
            "content": [{"id": str(n), "subject": subject, "activeForm": active,
                         "status": status}
                        for n, (subject, active, status) in enumerate(items, 1)],
        },
    })


def call(tool, tool_id, at="2026-08-25T10:00:00Z", **data):
    return line({"type": "assistant", "timestamp": at,
                 "message": {"content": [
                     {"type": "tool_use", "id": tool_id, "name": tool, "input": data}]}})


def result(tool_id, text="ok", at="2026-08-25T10:00:01Z", **outcome):
    return line({"type": "user", "timestamp": at,
                 "message": {"content": [
                     {"type": "tool_result", "tool_use_id": tool_id, "content": text}]},
                 "toolUseResult": outcome})


def notification(tool_id, status="completed"):
    return line({"type": "queue-operation", "operation": "enqueue",
                 "content": f"<task-notification>\n<task-id>b1</task-id>\n"
                            f"<tool-use-id>{tool_id}</tool-use-id>\n"
                            f"<status>{status}</status>\n"
                            f"<summary>Background command done</summary>\n"
                            f"</task-notification>"})


def spawn(tool_id, name, at="2026-08-25T10:00:00Z", description="audit"):
    return (call("Agent", tool_id, at=at, name=name, description=description)
            + result(tool_id, f"Spawned successfully.\nagent_id: {name}@session-abc\nname: {name}",
                     status="teammate_spawned"))


def background(tool_id, task_id, description="Waiting for CI", at="2026-08-25T10:00:00Z"):
    return (call("Bash", tool_id, at=at, command="sleep 600", description=description,
                 run_in_background=True)
            + result(tool_id, f"Command running in background with ID: {task_id}.",
                     backgroundTaskId=task_id))


class Transcript(unittest.TestCase):
    def state(self, *chunks):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write("".join(chunks))
        return sesstate.read(path).snapshot()

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)


class Incremental(Transcript):
    def test_the_appended_tail_equals_a_full_pass(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        head = background("toolu_1", "b1") + plan(("first", "doing it", "in_progress"))
        tail = spawn("toolu_2", "alpha") + notification("toolu_1")
        with open(path, "w", encoding="utf-8") as f:
            f.write(head)
        state = sesstate.read(path)
        with open(path, "a", encoding="utf-8") as f:
            f.write(tail)
        grown = sesstate.read(path, state).snapshot()

        whole = os.path.join(self.dir.name, "whole.jsonl")
        with open(whole, "w", encoding="utf-8") as f:
            f.write(head + tail)
        self.assertEqual(grown, sesstate.read(whole).snapshot())

    def test_an_unfinished_line_is_read_to_the_end_later(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        whole = notification("toolu_1")
        cut = len(whole) // 2
        with open(path, "w", encoding="utf-8") as f:
            f.write(background("toolu_1", "b1"))
            f.write(whole[:cut])
        state = sesstate.read(path)
        self.assertEqual(len(state.snapshot()["tasks"]), 1)
        with open(path, "a", encoding="utf-8") as f:
            f.write(whole[cut:])
        self.assertTrue(sesstate.read(path, state).snapshot()["tasks"][0]["done"],
                        "the second half of the line was not read: the shell is still open")

    def test_a_file_that_got_shorter_is_parsed_from_scratch(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(background("toolu_1", "b1") + background("toolu_2", "b2"))
        state = sesstate.read(path)
        self.assertEqual(len(state.snapshot()["tasks"]), 2)
        with open(path, "w", encoding="utf-8") as f:
            f.write(plan(("a new conversation", "starting", "in_progress")))
        fresh = sesstate.read(path, state)
        self.assertEqual(fresh.snapshot()["tasks"], [])
        self.assertEqual(len(fresh.snapshot()["plan"]), 1)


class Cache(Transcript):
    def test_forgotten_sessions_do_not_pile_up(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(background("toolu_1", "b1"))
        cache = sesstate.Cache()
        self.assertEqual(len(cache.of(path)["tasks"]), 1)
        cache.forget(keep=set())
        self.assertEqual(cache._states, {})

    def test_a_missing_transcript_does_not_break_the_collection(self):
        self.assertIsNone(sesstate.Cache().of(os.path.join(self.dir.name, "missing.jsonl")))


if __name__ == "__main__":
    unittest.main()


class Answered(Transcript):
    def test_an_answer_is_still_seen_after_a_second_read(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(call("AskUserQuestion", "toolu_1"))
            f.write(result("toolu_1"))

        first = sesstate.read(path)
        self.assertIn("toolu_1", set(first.answered), "the answer is not noticed on the first read")

        second = sesstate.read(path, first)
        self.assertIn("toolu_1", set(second.answered),
                      "the answer is forgotten between reads — the question on the phone hangs forever")

    def test_only_a_limited_number_is_remembered(self):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            for n in range(150):
                f.write(result("toolu_%d" % n))
        state = sesstate.read(path)
        self.assertEqual(len(state.answered), 100)
        self.assertIn("toolu_149", set(state.answered))
