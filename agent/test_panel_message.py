"""Panel instructions stay distinct from the person's prompts on every feed path."""
import json
import os
import unittest

import test_barrier

import chat
from chat.search import search


BODY = "Save the handoff.\n\nCall session_ready and end the turn."
PROMPT = f'<panel-message from="context restart">\n{BODY}\n</panel-message>'


def prompt(text=PROMPT, **fields):
    return {"type": "user", "message": {"content": text}, **fields}


def queued(text=PROMPT):
    return {"type": "queue-operation", "operation": "enqueue", "content": text}


def attachment(text=PROMPT):
    return {"type": "attachment", "attachment": {"type": "queued_command", "prompt": text}}


def pasted(text=PROMPT):
    return f'<pasted_content id="one">\n{text}\n</pasted_content id="one">'


class PanelMessages(unittest.TestCase):
    def assert_panel(self, items, pos=10, body=BODY, sender="context restart"):
        self.assertEqual([(i["role"], i.get("source"), i.get("from"), i["text"], i["pos"]) for i in items],
                         [("mail", "panel", sender, body, pos)])

    def transcript(self, records):
        directory = test_barrier.tmp_dir()
        self.addCleanup(directory.cleanup)
        path = os.path.join(directory.name, "panel.jsonl")
        with open(path, "w", encoding="utf-8") as stream:
            for record in records:
                stream.write(json.dumps(record) + "\n")
        return path

    def test_a_whole_prompt_is_a_panel_message_on_every_input_path(self):
        for record in (prompt(), prompt(" \n" + PROMPT + "\n "),
                       prompt([{"type": "text", "text": PROMPT}]),
                       prompt(promptSource="queued"), prompt(promptSource="sdk"),
                       prompt(pasted()), queued(), queued(pasted()), attachment(),
                       attachment([{"type": "text", "text": PROMPT}]),
                       {"type": "system", "subtype": "local_command", "content": PROMPT}):
            with self.subTest(record=record):
                self.assert_panel(chat.parse(record, 10, chat.Pending()))

    def test_the_sender_comes_from_the_tag(self):
        text = '<panel-message from="maintenance">\nSave the handoff.\n</panel-message>'
        self.assert_panel(chat.parse(prompt(text), 10), body="Save the handoff.", sender="maintenance")

    def test_the_body_is_not_interpreted_as_a_harness_insert(self):
        body = "<system-reminder>Save the handoff.</system-reminder>"
        text = f'<panel-message from="context restart">\n{body}\n</panel-message>'
        self.assert_panel(chat.parse(prompt(text), 10), body=body)

    def test_a_tag_inside_the_persons_words_stays_a_human_prompt(self):
        for text in ("Explain this tag:\n" + PROMPT, PROMPT + "\nIs this correct?",
                     "```\n" + PROMPT + "\n```", PROMPT + "\n" + PROMPT,
                     PROMPT.removesuffix("</panel-message>")):
            for record in (prompt(text), prompt(text, promptSource="sdk"), queued(text),
                           attachment(text), prompt(pasted(text))):
                with self.subTest(record=record):
                    items = chat.parse(record, 10, chat.Pending())
                    self.assertEqual([(i["role"], i["text"]) for i in items], [("me", text.strip())])

    def test_the_queue_draws_the_card_before_the_prompt_is_recorded(self):
        for record in (queued(), attachment()):
            with self.subTest(record=record):
                pending = chat.Pending()
                self.assert_panel(chat.parse(record, 10, pending))
                self.assertEqual(chat.parse(attachment(), 20, pending), [])
                self.assertEqual(chat.parse(prompt(promptSource="queued"), 30, pending), [])
                self.assertEqual(pending.waiting, [], "the panel message left a human bubble in the queue")

    def test_delivery_does_not_draw_a_second_card(self):
        for source in ("queued", "sdk"):
            for remove in (False, True):
                with self.subTest(source=source, remove=remove):
                    pending = chat.Pending()
                    self.assert_panel(chat.parse(queued(), 10, pending))
                    if remove:
                        self.assertEqual(chat.parse({"type": "queue-operation", "operation": "dequeue"}, 20, pending), [])
                    self.assertEqual(chat.parse(prompt(promptSource=source), 30, pending), [])

    def test_a_cloned_queue_keeps_the_card_already_drawn(self):
        pending = chat.Pending()
        self.assert_panel(chat.parse(queued(), 10, pending))
        self.assertEqual(chat.parse(prompt(promptSource="sdk"), 30, pending.clone()), [])

    def test_the_tmux_paste_is_one_card_through_enqueue_and_delivery(self):
        path = self.transcript([queued(pasted()), attachment(pasted()),
                                {"type": "queue-operation", "operation": "dequeue"},
                                prompt(pasted(), promptSource="queued")])
        self.assert_panel(chat.feed(path)["items"], pos=0)

    def test_search_finds_the_body_once_as_a_letter(self):
        path = self.transcript([queued(), attachment(), prompt(promptSource="sdk")])
        found = search(path, "handoff")
        self.assertEqual(found["total"], 1)
        self.assertEqual([(m["role"], m["pos"]) for m in found["matches"]], [("letter", 0)])
        self.assertEqual(search(path, "panel-message")["total"], 0)
        self.assertEqual(search(path, "context restart")["total"], 0)

    def test_an_alarm_letter_keeps_its_own_kind(self):
        text = '<cross-session-message from-name="alarm ci">Check CI.</cross-session-message>'
        for record in (prompt(text), queued(text), attachment(text), prompt(pasted(text))):
            with self.subTest(record=record):
                items = chat.parse(record, 10, chat.Pending())
                self.assertEqual([(i["role"], i["source"], i["from"], i["text"]) for i in items],
                                 [("mail", "alarm", "ci", "Check CI.")])
