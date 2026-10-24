"""Requests use the same cards and positions in both agents and the waiting index."""
import json
import os
import unittest

import test_barrier  # noqa: F401
import chat
from chat.waiting import Index, cards
from test_codex import mcp_call


REQUEST_ID = "a" * 32
ARGS = {"title": "Restart the router", "steps": "Run this:\n```sh\nreboot\n```", "blocking": True}
RESULT = "Requested as " + REQUEST_ID + ". End the turn and wait for the person's answer message."


def tool(args=None):
    return {"type": "assistant", "message": {"content": [
        {"type": "tool_use", "name": "mcp__aacpanel__request", "id": "toolu_request", "input": ARGS if args is None else args}]}}


def result(text=RESULT):
    return {"type": "user", "message": {"content": [
        {"type": "tool_result", "tool_use_id": "toolu_request", "content": text}]}}


class Requests(unittest.TestCase):
    def test_claude_draws_only_a_request_the_service_accepted(self):
        pending = {}
        chat.parse(tool(), 0, briefs=pending)
        got = chat.parse(result(), 20, briefs=pending)
        self.assertEqual(len(got), 1)
        self.assertEqual(got[0], dict(ARGS, role="request", id=REQUEST_ID, use="toolu_request", at="", pos=20))
        for args, text in [(ARGS, "Nothing was requested: no session"),
                           ({"id": REQUEST_ID, "withdraw": True}, "Withdrawn request " + REQUEST_ID + ".")]:
            chat.parse(tool(args), 30, briefs=pending)
            self.assertFalse(any(r["role"] == "request" for r in chat.parse(result(text), 40, briefs=pending)))

    def test_codex_draws_the_same_request_card(self):
        rows = chat.parse(mcp_call("aacpanel", "request", ARGS, text=RESULT), 4)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["role"], "request")
        self.assertEqual(rows[0]["id"], REQUEST_ID)
        self.assertEqual(rows[0]["steps"], ARGS["steps"])
        self.assertTrue(rows[0]["blocking"])
        rows = chat.parse(mcp_call("aacpanel", "request", {"id": REQUEST_ID, "withdraw": True},
                                  text="Withdrawn request " + REQUEST_ID + "."), 5)
        self.assertFalse(any(r["role"] == "request" for r in rows))

    def test_the_index_keeps_only_actionable_cards_and_no_secret_template(self):
        index = Index("/unused/conversation.jsonl")
        index.take([{"role": role, "pos": n, "title": role, "template": "not part of the summary"}
                    for n, role in enumerate(("request", "brief", "secret", "call", "asked", "permit", "question", "ai"))])
        self.assertEqual({item["role"] for item in index.items.values()}, {"request", "brief", "secret", "call"})
        self.assertTrue(all("template" not in item for item in index.items.values()))

    def test_the_index_reads_a_result_appended_after_its_call_and_finds_the_feed_row(self):
        with test_barrier.tmp_dir() as directory:
            path = os.path.join(directory, "conversation.jsonl")
            first = json.dumps(tool()) + "\n"
            with open(path, "w", encoding="utf-8") as stream:
                stream.write(first)
            self.assertEqual(cards(path), [])
            with open(path, "a", encoding="utf-8") as stream:
                stream.write(json.dumps(result()) + "\n")
            found = cards(path)
            self.assertEqual(len(found), 1)
            self.assertEqual(found[0]["pos"], len(first.encode()))
            feed = chat.feed(path)["items"]
            row = next(item for item in feed if item["role"] == "request")
            self.assertEqual(found[0]["pos"], row["pos"])
            self.assertEqual(cards(path), found, "a second listing repeated the task")
            with open(path, "w", encoding="utf-8") as stream:
                stream.write(json.dumps({"type": "user", "message": {"content": "new conversation"}}) + "\n")
            self.assertEqual(cards(path), [], "a replaced transcript kept the previous task")


class TextQuestions(unittest.TestCase):
    def test_only_prose_can_be_a_question(self):
        from chat.waiting import question_in
        for text in ["Before. Shall I publish? I can wait.", "[Shall I publish?](https://example.test)",
                     "`x?` and the real question?", "``x`?`` and this?"]:
            with self.subTest(text=text):
                self.assertTrue(question_in(text))
        for text in ["Done.", "Use `x ? a : b`.", "Use ``a`?``.",
                     "```js\nx ? a : b\n```\nDone.", "~~~\nwhy?\n~~~", "```\nwhy?",
                     "    x ? a : b", "https://example.test/path?q=1", "<https://example.test/?q=1>",
                     "[link](/page?q=1)", "[link][one]\n[one]: https://example.test/?q=1"]:
            with self.subTest(text=text):
                self.assertFalse(question_in(text))

    def test_a_candidate_has_the_feed_anchor_and_leaves_on_human_input(self):
        with test_barrier.tmp_dir() as directory:
            path = os.path.join(directory, "conversation.jsonl")
            def append(record):
                with open(path, "a", encoding="utf-8") as stream:
                    stream.write(json.dumps(record) + "\n")
            append({"type": "assistant", "uuid": "message-one", "message": {"content": [
                {"type": "text", "text": "Shall I publish? The checks passed."}]}})
            found = cards(path)
            self.assertEqual(len(found), 1)
            self.assertEqual(found[0]["role"], "question")
            self.assertEqual(found[0]["id"], "message-one")
            self.assertEqual(found[0]["pos"], chat.feed(path)["items"][0]["pos"])
            self.assertEqual(cards(path), found)
            append(result("The tool finished."))
            self.assertEqual(cards(path), found, "a tool result settled the question")
            append({"type": "user", "message": {"content": "Yes, publish."}})
            self.assertEqual(cards(path), [])
            append({"type": "assistant", "uuid": "message-two", "message": {"content": [
                {"type": "text", "text": "Another question?"}]}})
            self.assertEqual(cards(path)[0]["id"], "message-two")
            append({"type": "assistant", "uuid": "message-three", "message": {"content": [
                {"type": "text", "text": "I have the answer."}]}})
            self.assertEqual(cards(path), [], "an older assistant message remained a candidate")

    def test_a_question_past_the_feed_limit_is_still_a_candidate(self):
        from chat.limits import MAX_TEXT
        index = Index("/unused/conversation.jsonl")
        record = {"type": "assistant", "uuid": "long-message", "message": {"content": [
            {"type": "text", "text": "x" * MAX_TEXT + " Shall I publish?"}]}}
        index.inspect(record, chat.parse(record, 42))
        self.assertIsNotNone(index.question)
        self.assertTrue(index.question["cut"])

    def test_codex_and_sidechains_never_supply_a_candidate(self):
        index = Index("/unused/conversation.jsonl")
        item = {"role": "ai", "text": "Publish?", "pos": 1}
        index.inspect({"type": "response_item", "payload": {}}, [item])
        self.assertIsNone(index.question)
        index.inspect({"type": "assistant", "isSidechain": True}, [item])
        self.assertIsNone(index.question)
