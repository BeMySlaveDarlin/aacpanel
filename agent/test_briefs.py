import contextlib
import io
import json
import os
import socket
import sys
import threading
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import briefs  # noqa: E402


@contextlib.contextmanager
def muted():
    said = io.StringIO()
    with contextlib.redirect_stdout(said):
        yield said


DOC = {
    "id": "seven-after-twelve",
    "title": "Seven questions after twelve",
    "eyebrow": "after twelve answers",
    "lede": "Ten of twelve **closed**.",
    "summary": [{"n": "3", "label": "still open"}],
    "lineage": [{"from": "did not close", "to": "**07** the configuration directory"}],
    "sections": [{"title": "Where this came from", "body": "The tail of yesterday."}],
    "questions": [
        {
            "id": "r1",
            "chips": [{"tone": "stuck", "text": "did not close · decision 07"}],
            "title": "The configuration directory",
            "ask": "You picked one directory. **The CLI may not offer that.**",
            "facts": [
                {"text": "`--setting-sources user` reads the directory whole", "src": "stack 5"},
                {"text": "`--restricted` ignores user settings", "src": "help 2.1.270", "flag": True},
            ],
            "kind": "pick",
            "options": [
                {"key": "A", "label": "Curate through `--settings`", "note": "argv carries the role"},
                {"key": "B", "label": "A directory per role", "note": "a copy of the token in each"},
            ],
            "read": ["Option A is what you described, with argv as the carrier."],
            "capture": {"note": {"placeholder": "The fallback"}},
        },
        {
            "id": "r2",
            "title": "Does the pause expire",
            "kind": "pick",
            "options": [
                {"key": "A", "label": "It does not"},
                {"key": "C", "label": "It does not, but it is visible"},
            ],
            "answered": {"pick": "C", "at": "2026-09-15"},
        },
    ],
    "closing": ["Ten of twelve closed for good."],
}


def published(**over):
    return {"sessionId": "s-1", "cwd": "/srv/proj", "doc": dict(DOC, **over)}


class Clean(unittest.TestCase):
    def test_the_document_is_parsed_whole(self):
        got = briefs.clean(published())
        self.assertEqual(got["id"], "seven-after-twelve")
        self.assertEqual(got["sessionId"], "s-1")
        self.assertEqual(got["cwd"], "/srv/proj")
        self.assertEqual(len(got["questions"]), 2)
        self.assertEqual(got["summary"], [{"n": "3", "label": "still open"}])
        self.assertEqual(got["lineage"][0]["from"], "did not close")
        self.assertEqual(got["sections"][0]["body"], ["The tail of yesterday."])
        self.assertEqual(got["closing"], ["Ten of twelve closed for good."])

    def test_a_question_keeps_its_facts_options_and_reading(self):
        q = briefs.clean(published())["questions"][0]
        self.assertEqual(q["n"], "01")
        self.assertEqual(q["chips"], [{"tone": "stuck", "text": "did not close · decision 07"}])
        self.assertEqual(len(q["facts"]), 2)
        self.assertTrue(q["facts"][1]["flag"])
        self.assertEqual(q["facts"][0]["src"], "stack 5")
        self.assertEqual([o["key"] for o in q["options"]], ["A", "B"])
        self.assertEqual(len(q["read"]), 1)
        self.assertEqual(q["capture"]["note"]["placeholder"], "The fallback")

    def test_an_answer_the_agent_already_knew_survives_a_reissue(self):
        q = briefs.clean(published())["questions"][1]
        self.assertEqual(q["answered"]["picks"], ["C"])
        self.assertEqual(q["answered"]["at"], "2026-09-15")

    def test_a_brief_with_no_questions_is_a_report_and_is_taken(self):
        got = briefs.clean(published(questions=[]))
        self.assertEqual(got["questions"], [])
        self.assertEqual(got["title"], DOC["title"])

    def test_the_seat_number_is_given_when_the_document_names_none(self):
        got = briefs.clean(published())
        self.assertEqual([q["n"] for q in got["questions"]], ["01", "02"])

    def test_an_option_with_no_key_is_lettered_in_order(self):
        doc = json.loads(json.dumps(DOC))
        for opt in doc["questions"][0]["options"]:
            opt.pop("key")
        got = briefs.clean(published(questions=doc["questions"][:1]))
        self.assertEqual([o["key"] for o in got["questions"][0]["options"]], ["A", "B"])

    def test_long_text_is_cut_rather_than_refused(self):
        doc = json.loads(json.dumps(DOC))
        doc["questions"][0]["title"] = "x" * (briefs.MAX_TITLE + 400)
        doc["questions"][0]["facts"][0]["text"] = "y" * (briefs.MAX_LINE + 400)
        got = briefs.clean(published(questions=doc["questions"]))
        self.assertEqual(len(got["questions"][0]["title"]), briefs.MAX_TITLE)
        self.assertEqual(len(got["questions"][0]["facts"][0]["text"]), briefs.MAX_LINE)

    def test_a_paragraph_keeps_its_line_breaks(self):
        got = briefs.clean(published(lede="first\n\nsecond"))
        self.assertEqual(got["lede"], "first\n\nsecond")


class Refusals(unittest.TestCase):
    def refuse(self, payload):
        with self.assertRaises(briefs.Refused) as caught:
            briefs.clean(payload)
        return str(caught.exception)

    def test_a_brief_without_a_session_has_nowhere_to_send_the_answer(self):
        payload = published()
        payload.pop("sessionId")
        self.assertIn("nowhere to send the answer", self.refuse(payload))

    def test_a_brief_without_a_title_is_refused(self):
        self.assertIn("a title", self.refuse(published(title="")))

    def test_an_id_that_is_not_a_slug_is_refused(self):
        self.assertIn("lowercase latin", self.refuse(published(id="Seven Questions")))

    def test_a_choice_question_without_options_is_refused(self):
        doc = json.loads(json.dumps(DOC))
        doc["questions"][0]["options"] = []
        said = self.refuse(published(questions=doc["questions"][:1]))
        self.assertIn("carries no options", said)
        self.assertIn("\"text\"", said)

    def test_a_question_that_asks_nothing_carries_no_options(self):
        doc = json.loads(json.dumps(DOC))
        doc["questions"][0]["kind"] = "none"
        self.assertIn("nowhere to go", self.refuse(published(questions=doc["questions"][:1])))

    def test_an_unknown_kind_is_refused(self):
        doc = json.loads(json.dumps(DOC))
        doc["questions"][0]["kind"] = "slider"
        self.assertIn("is unknown", self.refuse(published(questions=doc["questions"][:1])))

    def test_two_questions_under_one_id_are_refused(self):
        doc = json.loads(json.dumps(DOC))
        doc["questions"][1]["id"] = "r1"
        self.assertIn("two questions carry the id", self.refuse(published(questions=doc["questions"])))

    def test_two_options_under_one_key_are_refused(self):
        doc = json.loads(json.dumps(DOC))
        doc["questions"][0]["options"][1]["key"] = "A"
        self.assertIn("two options carry the key", self.refuse(published(questions=doc["questions"][:1])))

    def test_an_answer_naming_an_option_that_is_not_there_is_refused(self):
        doc = json.loads(json.dumps(DOC))
        doc["questions"][1]["answered"] = {"pick": "Z"}
        said = self.refuse(published(questions=doc["questions"]))
        self.assertIn("which the question does not offer", said)

    def test_more_questions_than_the_ceiling_are_refused(self):
        one = DOC["questions"][0]
        many = [dict(one, id=f"q{i}") for i in range(briefs.MAX_QUESTIONS + 1)]
        self.assertIn(f"ceiling is {briefs.MAX_QUESTIONS}", self.refuse(published(questions=many)))

    def test_questions_that_are_not_a_list_are_refused(self):
        self.assertIn("come as a list", self.refuse(published(questions={"r1": {}})))


class ShelfStore(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir(prefix="briefs-")
        self.addCleanup(self.dir.cleanup)
        self.shelf = briefs.Shelf(os.path.join(self.dir.name, "briefs"))

    def put(self, **over):
        brief = briefs.clean(published(**over))
        ok, why = self.shelf.put(brief)
        self.assertTrue(ok, why)
        return brief

    def test_a_brief_is_read_back_whole(self):
        self.put()
        got = self.shelf.of("seven-after-twelve")
        self.assertEqual(got["title"], DOC["title"])
        self.assertEqual(len(got["questions"]), 2)

    def test_a_card_says_what_is_inside_without_opening_it(self):
        self.put()
        card = self.shelf.cards()[0]
        self.assertEqual(card["id"], "seven-after-twelve")
        self.assertEqual(card["questions"], 2)
        self.assertEqual(card["sessionId"], "s-1")
        self.assertNotIn("lede", card)

    def test_a_card_counts_only_what_asks_something(self):
        """A block of kind "none" is a section with a number on it. Counting it
        promises an answer on the card, on the phone and in the progress."""
        self.shelf.put(briefs.clean(published(questions=[
            {"id": "r1", "title": "The directory", "kind": "pick",
             "options": [{"key": "A", "label": "one"}, {"key": "B", "label": "two"}]},
            {"id": "r2", "title": "What this rests on", "kind": "none"},
        ])))
        self.assertEqual(self.shelf.cards()[0]["questions"], 1)

    def test_cards_can_be_asked_for_one_session(self):
        self.put()
        self.assertEqual(len(self.shelf.cards(session="s-1")), 1)
        self.assertEqual(self.shelf.cards(session="s-2"), [])

    def test_the_same_id_from_the_same_project_is_an_update(self):
        self.put()
        self.put(title="Seven questions, second pass")
        self.assertEqual(len(self.shelf.cards()), 1)
        self.assertEqual(self.shelf.of("seven-after-twelve")["title"], "Seven questions, second pass")

    def test_a_reissue_is_marked_and_keeps_when_it_first_went_out(self):
        """The person comes back to a text that changed under what they decided.
        A silent replacement is the one thing they cannot check."""
        self.put()
        first = self.shelf.of("seven-after-twelve")
        self.assertNotIn("reissuedAt", first)

        self.put(title="Seven questions, second pass")
        again = self.shelf.of("seven-after-twelve")
        self.assertEqual(again["firstAt"], first["at"])
        self.assertEqual(again["reissuedAt"], again["at"])

    def test_a_third_pass_still_points_at_the_first_publication(self):
        self.put()
        first = self.shelf.of("seven-after-twelve")["at"]
        self.put(title="second")
        self.put(title="third")
        self.assertEqual(self.shelf.of("seven-after-twelve")["firstAt"], first)

    def test_the_same_id_from_another_project_is_refused_rather_than_overwritten(self):
        self.put()
        other = briefs.clean({"sessionId": "s-2", "cwd": "/srv/other", "doc": DOC})
        ok, why = self.shelf.put(other)
        self.assertFalse(ok)
        self.assertIn("/srv/proj", why)
        self.assertEqual(self.shelf.of("seven-after-twelve")["sessionId"], "s-1")

    def test_a_name_that_is_not_a_slug_reads_nothing_from_disk(self):
        self.put()
        self.assertIsNone(self.shelf.of("../seven-after-twelve"))
        self.assertIsNone(self.shelf.of(""))

    def test_a_brief_older_than_the_ceiling_is_swept(self):
        brief = self.put()
        brief["at"] = time.strftime(briefs.STAMP, time.gmtime(time.time() - briefs.MAX_AGE - 60))
        with open(os.path.join(self.shelf.path, brief["id"] + ".json"), "w", encoding="utf-8") as f:
            json.dump(brief, f)
        self.assertEqual(self.shelf.sweep(), [brief["id"]])
        self.assertIsNone(self.shelf.of(brief["id"]))

    def test_the_sweep_leaves_a_brief_whose_session_is_long_gone(self):
        # A brief is answered after the conversation that wrote it has ended.
        # Sweeping by a live session would take it from under the reader.
        self.put()
        self.assertEqual(self.shelf.sweep(), [])
        self.assertIsNotNone(self.shelf.of("seven-after-twelve"))

    def test_over_the_ceiling_the_oldest_go(self):
        now = time.time()
        os.makedirs(self.shelf.path, exist_ok=True)
        for i in range(briefs.MAX_BRIEFS + 3):
            brief = briefs.clean(published(id=f"brief-{i:03d}"))
            brief["at"] = time.strftime(briefs.STAMP, time.gmtime(now - i * 60))
            with open(os.path.join(self.shelf.path, brief["id"] + ".json"), "w", encoding="utf-8") as f:
                json.dump(brief, f)
        gone = self.shelf.sweep(now=now)
        self.assertEqual(len(gone), 3)
        self.assertEqual(len(self.shelf.cards()), briefs.MAX_BRIEFS)
        self.assertIsNotNone(self.shelf.of("brief-000"))


class Socket(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir(prefix="briefs-sock-")
        self.addCleanup(self.dir.cleanup)
        self.shelf = briefs.Shelf(os.path.join(self.dir.name, "briefs"))

    def serve(self, payload):
        left, right = socket.socketpair()
        done = threading.Thread(target=briefs.handle, args=(right, self.shelf))
        done.start()
        left.sendall(json.dumps(payload).encode("utf-8"))
        left.shutdown(socket.SHUT_WR)
        raw = left.recv(8192)
        done.join(5)
        left.close()
        return json.loads(raw.decode("utf-8"))

    def test_a_published_brief_comes_back_by_its_id(self):
        reply = self.serve(published())
        self.assertEqual(reply, {"ok": True, "id": "seven-after-twelve"})
        self.assertIsNotNone(self.shelf.of("seven-after-twelve"))

    def test_a_refusal_says_what_to_fix(self):
        reply = self.serve(published(title=""))
        self.assertFalse(reply["ok"])
        self.assertIn("a title", reply["error"])

    def test_a_document_arriving_in_several_packets_is_read_whole(self):
        # One recv takes 64 KB; a brief of this size arrives in several and
        # has to be put back together before it is parsed.
        payload = published(lede="word " * 20000)
        self.assertGreater(len(json.dumps(payload)), 64 * 1024)
        self.assertLess(len(json.dumps(payload)), briefs.MAX_REQUEST)
        self.assertEqual(self.serve(payload)["ok"], True)
        self.assertEqual(self.shelf.of("seven-after-twelve")["title"], DOC["title"])

    def test_a_document_over_the_request_ceiling_is_refused(self):
        payload = published(lede="x" * (briefs.MAX_REQUEST + 1024))
        reply = self.serve(payload)
        self.assertFalse(reply["ok"])
        self.assertIn("longer than", reply["error"])

    def test_what_is_not_json_is_refused_without_bringing_the_thread_down(self):
        left, right = socket.socketpair()
        done = threading.Thread(target=briefs.handle, args=(right, self.shelf))
        done.start()
        left.sendall(b"{not json")
        left.shutdown(socket.SHUT_WR)
        reply = json.loads(left.recv(8192).decode("utf-8"))
        done.join(5)
        left.close()
        self.assertFalse(reply["ok"])
        self.assertIn("not parsed", reply["error"])


class Handles(unittest.TestCase):
    """The two handles the feed socket answers: the shelf and one brief."""

    def setUp(self):
        from chat import server
        self.server = server
        self.dir = test_barrier.tmp_dir(prefix="briefs-handle-")
        self.addCleanup(self.dir.cleanup)
        shelf = briefs.Shelf(os.path.join(self.dir.name, "briefs"))
        standing = briefs.SHELF
        briefs.SHELF = shelf
        self.addCleanup(setattr, briefs, "SHELF", standing)
        self.shelf = shelf
        ok, why = shelf.put(briefs.clean(published()))
        self.assertTrue(ok, why)

    def test_the_shelf_comes_back_as_cards(self):
        reply = self.server.answer({"briefs": {}})
        self.assertTrue(reply["ok"])
        self.assertEqual([c["id"] for c in reply["briefs"]], ["seven-after-twelve"])

    def test_the_shelf_can_be_asked_for_one_session(self):
        self.assertEqual(len(self.server.answer({"briefs": {"session": "s-1"}})["briefs"]), 1)
        self.assertEqual(self.server.answer({"briefs": {"session": "s-9"}})["briefs"], [])

    def test_one_brief_comes_back_whole(self):
        reply = self.server.answer({"brief": "seven-after-twelve"})
        self.assertTrue(reply["ok"])
        self.assertEqual(len(reply["brief"]["questions"]), 2)
        self.assertEqual(reply["brief"]["title"], DOC["title"])

    def test_a_brief_that_is_not_there_says_so(self):
        reply = self.server.answer({"brief": "never-published"})
        self.assertFalse(reply["ok"])
        self.assertIn("never published", reply["error"])


if __name__ == "__main__":
    unittest.main()
