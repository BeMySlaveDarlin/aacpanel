"""The card of permissions: what a person answered before a call ran."""
import json
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402
import held  # noqa: E402

UUID = "66666666-6666-4666-8666-666666666666"
CWD = "/srv/proj"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def call(*uses, at="2026-09-26T10:00:00Z"):
    """Returns an answer of the model that makes these calls: (id, tool, input) each."""
    return {"type": "assistant", "timestamp": at, "cwd": CWD,
            "message": {"content": [{"type": "tool_use", "id": use, "name": name, "input": data}
                                    for use, name, data in uses]}}


def result(*uses, text="ok", at="2026-09-26T10:00:05Z"):
    """Returns the record that brings the results of these calls."""
    return {"type": "user", "timestamp": at, "cwd": CWD,
            "message": {"content": [{"type": "tool_result", "tool_use_id": use, "content": text}
                                    for use in uses]}}


class Permits(unittest.TestCase):
    def setUp(self):
        chat.tail.PIECES.forget()
        self.root = os.path.realpath(test_barrier.tmp_path(prefix="chat-permit-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.addCleanup(os.environ.pop, "XDG_STATE_HOME", None)
        os.environ["XDG_STATE_HOME"] = os.path.join(self.root, "state")
        self.path = os.path.join(self.root, f"{UUID}.jsonl")

    def keep(self, *answers):
        """Plays the holder: keeps the answers a person gave, as it writes them."""
        path = held.permits_path(UUID)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump(list(answers), f)

    def write(self, *records, mode="w"):
        with open(self.path, mode, encoding="utf-8") as f:
            f.write("".join(line(r) for r in records))

    def cards(self):
        return [i for i in chat.feed(self.path)["items"] if i["role"] == "permitted"]

    def test_an_answered_permission_stands_by_the_result_of_its_call(self):
        self.keep({"use": "t1", "tool": "Bash", "decision": "allow"})
        self.write(call(("t1", "Bash", {"command": "git push origin main"})), result("t1"))
        items = chat.feed(self.path)["items"]
        self.assertEqual([i["role"] for i in items], ["tools", "permitted"])
        self.assertEqual(items[1]["rows"], [{"tool": "Bash", "subject": "git push origin main",
                                              "decision": "allow"}])
        self.assertEqual(items[1]["at"], "2026-09-26T10:00:05Z")

    def test_a_call_nobody_was_asked_about_has_no_card(self):
        self.keep({"use": "t9", "tool": "Bash", "decision": "allow"})
        self.write(call(("t1", "Read", {"file_path": "/srv/proj/a.go"})), result("t1"))
        self.assertEqual(self.cards(), [])

    def test_a_session_off_the_stream_has_no_cards(self):
        self.write(call(("t1", "Bash", {"command": "ls"})), result("t1"))
        self.assertEqual(self.cards(), [])

    def test_an_answer_for_good_and_a_refusal_say_so(self):
        self.keep({"use": "t1", "tool": "Edit", "decision": "allow", "lasting": True},
                  {"use": "t2", "tool": "Bash", "decision": "deny", "lasting": True})
        self.write(call(("t1", "Edit", {"file_path": "/srv/proj/a.go"})), result("t1"),
                   call(("t2", "Bash", {"command": "rm -rf build"})), result("t2", text="refused"))
        rows = [card["rows"] for card in self.cards()]
        self.assertEqual(rows, [[{"tool": "Edit", "subject": "/srv/proj/a.go", "decision": "allow",
                                  "lasting": True}],
                                [{"tool": "Bash", "subject": "rm -rf build", "decision": "deny"}]])

    def test_calls_answered_together_are_one_card(self):
        # The feed keeps one card of a kind per record: two would take each
        # other's place.
        self.keep({"use": "t1", "tool": "Bash", "decision": "allow"},
                  {"use": "t2", "tool": "WebFetch", "decision": "deny"})
        self.write(call(("t1", "Bash", {"command": "make"}), ("t2", "WebFetch", {"url": "https://x.io"})),
                   result("t1", "t2"))
        cards = self.cards()
        self.assertEqual(len(cards), 1)
        self.assertEqual([(r["tool"], r["decision"]) for r in cards[0]["rows"]],
                         [("Bash", "allow"), ("WebFetch", "deny")])

    def test_the_answer_stands_before_what_the_result_brought(self):
        self.keep({"use": "t1", "tool": "Artifact", "decision": "allow"})
        self.write(call(("t1", "Artifact", {"action": "list"})),
                   result("t1", text="Published page at https://claude.ai/code/artifact/abc"))
        items = chat.feed(self.path)["items"]
        self.assertEqual([i["role"] for i in items], ["tools", "permitted", "artifactlink"])

    def test_a_call_before_the_piece_is_named_by_the_holder(self):
        answer = {"use": "t1", "tool": "Bash", "decision": "allow"}
        items = chat.records.parse(result("t1"), 7, calls={}, permits={"t1": answer})
        self.assertEqual(items, [{"role": "permitted", "rows": [{"tool": "Bash", "decision": "allow"}],
                                  "at": "2026-09-26T10:00:05Z", "pos": 7}])

    def test_a_result_written_after_the_length_was_taken_waits(self):
        # The holder keeps an answer before claude has it, so a result within
        # the length taken has its answer on the disk by the time the answers
        # are read. A result past that length is not: it waits for the next
        # read, or its card would be lost with the piece that remembers it.
        self.write(call(("t1", "Bash", {"command": "make"})))
        size = os.path.getsize(self.path)
        self.write(result("t1"), mode="a")
        piece = chat.tail.Piece.of(self.path, size, chat.tail.FIRST_SPAN)
        self.assertEqual([i["role"] for _, items in piece.rows for i in items], ["tool"])
        self.keep({"use": "t1", "tool": "Bash", "decision": "allow"})
        piece.read_on(os.path.getsize(self.path))
        self.assertEqual([i["role"] for _, items in piece.rows for i in items], ["tool", "permitted"])


class Kept(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.addCleanup(os.environ.pop, "XDG_STATE_HOME", None)
        os.environ["XDG_STATE_HOME"] = self.dir.name

    def test_only_answers_the_holder_writes_are_read(self):
        path = held.permits_path(UUID)
        os.makedirs(os.path.dirname(path))
        with open(path, "w", encoding="utf-8") as f:
            json.dump([{"use": "t1", "tool": "Bash", "decision": "allow"},
                       {"use": "t2", "tool": "Bash", "decision": "maybe"},
                       {"use": "", "tool": "Bash", "decision": "deny"},
                       "t3"], f)
        self.assertEqual(list(held.permits(UUID)), ["t1"])

    def test_a_name_that_walks_out_of_the_directory_is_refused(self):
        self.assertEqual(held.permits("../" + UUID), {})

    def test_no_file_is_no_answers(self):
        self.assertEqual(held.permits(UUID), {})


if __name__ == "__main__":
    unittest.main()
