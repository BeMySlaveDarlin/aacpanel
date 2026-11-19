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
def live(*rows):
    """Stands in for the registry of live sessions the shelf reads."""
    import ctx
    standing = ctx.live_sessions
    ctx.live_sessions = lambda: [dict(r) for r in rows]
    try:
        yield
    finally:
        ctx.live_sessions = standing


@contextlib.contextmanager
def muted():
    said = io.StringIO()
    with contextlib.redirect_stdout(said):
        yield said


DOC = {
    "id": "seven-after-twelve", "title": "Prepare the rollout",
    "task": "Choose the rollout order.", "why": "The release is ready.",
    "context": [{"title": "Dependency", "text": "The migration must finish first."}],
    "decisions": [
        {"id": "order", "title": "The rollout order", "ask": "Which goes first?",
         "options": [
             {"key": "A", "label": "Migration first", "reason": "The code needs it",
              "cost": "One rebuild", "hours": 0.5, "recommended": True},
             {"key": "B", "label": "Code first", "reason": "Earlier deployment",
              "cost": "Temporary downtime", "hours": 1.2}]},
        {"id": "checks", "title": "Choose checks", "multi": True,
         "options": [
             {"label": "API", "reason": "Check the route", "cost": "One run",
              "hours": 0.1, "recommended": True},
             {"label": "Browser", "reason": "Check the screen", "cost": "One run",
              "hours": 0.2, "recommended": True}]},
    ],
}


def published(**over):
    doc = json.loads(json.dumps(DOC))
    doc.update(over)
    return {"sessionId": "s-1", "cwd": "/srv/proj", "doc": doc}


class Clean(unittest.TestCase):
    def test_the_document_is_parsed_whole(self):
        got = briefs.clean(published())
        self.assertEqual(got["id"], DOC["id"])
        self.assertEqual(got["sessionId"], "s-1")
        self.assertEqual(got["cwd"], "/srv/proj")
        self.assertEqual(got["task"], DOC["task"])
        self.assertEqual(got["why"], DOC["why"])
        self.assertEqual(got["context"], DOC["context"])
        self.assertEqual(got["decisions"][0]["options"][0], DOC["decisions"][0]["options"][0])
        self.assertTrue(got["decisions"][1]["multi"])
        self.assertEqual([o["key"] for o in got["decisions"][1]["options"]], ["A", "B"])

    def test_the_directory_is_the_session_s_own_not_the_caller_s(self):
        with live({"sessionId": "s-1", "cwd": "/srv/real-work"}):
            self.assertEqual(briefs.clean(published())["cwd"], "/srv/real-work")

    def test_a_codex_thread_is_placed_where_its_executor_says(self):
        import held
        standing = held.codex_threads
        held.codex_threads = lambda: [{"sessionId": "s-1", "cwd": "/srv/link/proj"}]
        self.addCleanup(setattr, held, "codex_threads", standing)
        with live({"sessionId": "someone-else", "cwd": "/srv/elsewhere"}):
            self.assertEqual(briefs.clean(published())["cwd"], "/srv/link/proj")

    def test_a_session_the_registry_does_not_know_keeps_what_it_sent(self):
        with live({"sessionId": "someone-else", "cwd": "/srv/elsewhere"}):
            self.assertEqual(briefs.clean(published())["cwd"], "/srv/proj")

    def test_an_unnamed_decision_gets_its_number(self):
        payload = published()
        payload["doc"]["decisions"][0].pop("id")
        self.assertEqual(briefs.clean(payload)["decisions"][0]["id"], "1")

    def test_long_text_is_cut_at_the_field_ceiling(self):
        payload = published(title="x" * 500, task="x" * 900, why="x" * 1200)
        payload["doc"]["decisions"][0]["ask"] = "x" * 900
        payload["doc"]["decisions"][0]["options"][0]["cost"] = "x" * 500
        got = briefs.clean(payload)
        self.assertEqual(len(got["title"]), 120)
        self.assertEqual(len(got["task"]), 400)
        self.assertEqual(len(got["why"]), 800)
        self.assertEqual(len(got["decisions"][0]["ask"]), 600)
        self.assertEqual(len(got["decisions"][0]["options"][0]["cost"]), 300)

    def test_a_paragraph_keeps_its_line_breaks(self):
        self.assertEqual(briefs.clean(published(why="first\n\nsecond"))["why"], "first\n\nsecond")


class Refusals(unittest.TestCase):
    def refuse(self, payload):
        with self.assertRaises(briefs.Refused) as caught:
            briefs.clean(payload)
        return str(caught.exception)

    def test_a_brief_without_a_session_has_nowhere_to_send_the_answer(self):
        payload = published()
        payload.pop("sessionId")
        self.assertIn("nowhere to send the answer", self.refuse(payload))

    def test_required_header_fields(self):
        for field in ("title", "task", "why"):
            with self.subTest(field=field):
                self.assertIn(field, self.refuse(published(**{field: ""})))

    def test_an_id_that_is_not_a_slug_is_refused(self):
        self.assertIn("lowercase latin", self.refuse(published(id="Seven Questions")))

    def test_old_fields_are_refused_at_every_level(self):
        for field in briefs.OLD_FIELDS:
            for place in ("doc", "decision", "option"):
                with self.subTest(field=field, place=place):
                    payload = published()
                    doc = payload["doc"]
                    at = {"doc": doc, "decision": doc["decisions"][0],
                          "option": doc["decisions"][0]["options"][0]}[place]
                    at[field] = []
                    self.assertEqual(self.refuse(payload), briefs.OLD_FORM)

    def test_recommendations_are_required_and_single_unless_multi(self):
        for multi, marks in ((False, [False, False]), (False, [True, True]), (True, [False, False])):
            payload = published()
            decision = payload["doc"]["decisions"][0]
            decision["multi"] = multi
            for option, mark in zip(decision["options"], marks):
                option["recommended"] = mark
            self.assertIn("recommend", self.refuse(payload))

    def test_option_fields_and_hours_are_required(self):
        for field in ("label", "reason", "cost", "hours"):
            payload = published()
            payload["doc"]["decisions"][0]["options"][0].pop(field)
            self.assertIn(field, self.refuse(payload))
        for hours in (None, True, "1", 0, 0.09, 1001, 10**400, float("inf"), float("nan")):
            payload = published()
            payload["doc"]["decisions"][0]["options"][0]["hours"] = hours
            self.assertIn("hours", self.refuse(payload))

    def test_duplicate_decisions_and_option_keys_are_refused(self):
        payload = published()
        payload["doc"]["decisions"][1]["id"] = "order"
        self.assertIn("two decisions", self.refuse(payload))
        payload = published()
        payload["doc"]["decisions"][0]["options"][1]["key"] = "A"
        self.assertIn("two options", self.refuse(payload))

    def test_list_bounds_are_enforced(self):
        for value in (None, {}, [], [dict(DOC["decisions"][0], id=str(i)) for i in range(101)]):
            self.assertIn("decisions must be", self.refuse(published(decisions=value)))
        self.assertIn("context must be", self.refuse(published(context=[{}] * 7)))
        for size in (0, 1, 9):
            payload = published()
            payload["doc"]["decisions"][0]["options"] = [{}] * size
            self.assertIn("options must be", self.refuse(payload))


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
        self.assertEqual(len(got["decisions"]), 2)

    def test_a_card_says_what_is_inside_without_opening_it(self):
        self.put()
        card = self.shelf.cards()[0]
        self.assertEqual(card["id"], "seven-after-twelve")
        self.assertEqual(card["questions"], 2)
        self.assertEqual(card["decisions"], 2)
        self.assertEqual(card["sessionId"], "s-1")
        self.assertNotIn("lede", card)

    def test_a_card_counts_only_what_asks_something(self):
        """A block of kind "none" is a section with a number on it. Counting it
        promises an answer on the card, on the phone and in the progress."""
        old = {"id": "legacy", "title": "Legacy", "questions": [
            {"id": "one", "kind": "pick"}, {"id": "reading", "kind": "none"}]}
        self.shelf.put(old)
        self.assertEqual(self.shelf.cards()[0]["questions"], 1)
        self.assertEqual(self.shelf.of("legacy"), old)

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

    def test_a_card_names_the_publication_it_stands_for(self):
        """The panel keeps the answers of one publication. A brief removed and
        published again under the same name is another document, and the card
        is how the shelf tells the two apart without opening either."""
        self.put()
        first = self.shelf.of("seven-after-twelve")["at"]
        self.assertEqual(self.shelf.cards()[0]["firstAt"], "")
        self.put(title="second")
        self.assertEqual(self.shelf.cards()[0]["firstAt"], first)

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
        self.assertIn("title", reply["error"])

    def test_a_document_arriving_in_several_packets_is_read_whole(self):
        # One recv takes 64 KB; a brief of this size arrives in several and
        # has to be put back together before it is parsed.
        payload = published(why="word " * 20000)
        self.assertGreater(len(json.dumps(payload)), 64 * 1024)
        self.assertLess(len(json.dumps(payload)), briefs.MAX_REQUEST)
        self.assertEqual(self.serve(payload)["ok"], True)
        self.assertEqual(self.shelf.of("seven-after-twelve")["title"], DOC["title"])

    def test_a_document_over_the_request_ceiling_is_refused(self):
        payload = published(why="x" * (briefs.MAX_REQUEST + 1024))
        reply = self.serve(payload)
        self.assertFalse(reply["ok"])
        self.assertIn("longer than", reply["error"])

    def test_a_removal_is_held_to_the_directory_of_the_session_asking(self):
        with live({"sessionId": "s-1", "cwd": "/srv/proj"}):
            self.serve(published())
            # The shell stands in the repository of the panel, the session
            # works in /srv/proj, and the brief is the session's to remove.
            reply = self.serve({"drop": "seven-after-twelve", "sessionId": "s-1", "cwd": "/opt/panel"})
        self.assertTrue(reply["ok"], reply)
        self.assertIsNone(self.shelf.of("seven-after-twelve"))

    def test_a_session_working_elsewhere_removes_nothing(self):
        with live({"sessionId": "s-1", "cwd": "/srv/proj"}, {"sessionId": "s-2", "cwd": "/srv/other"}):
            self.serve(published())
            reply = self.serve({"drop": "seven-after-twelve", "sessionId": "s-2", "cwd": "/srv/proj"})
        self.assertFalse(reply["ok"])
        self.assertIn("/srv/proj", reply["error"])
        self.assertIsNotNone(self.shelf.of("seven-after-twelve"))

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

    def test_the_guide_import_reads_only_the_fixed_file(self):
        config = os.environ.get("XDG_CONFIG_HOME")
        os.environ["XDG_CONFIG_HOME"] = self.dir.name
        def restore():
            if config is None:
                os.environ.pop("XDG_CONFIG_HOME", None)
            else:
                os.environ["XDG_CONFIG_HOME"] = config
        self.addCleanup(restore)
        request = {"briefGuide": True, "file": "/not-the-guide"}
        self.assertEqual(self.server.answer(request), {"ok": True, "briefGuide": ""})
        folder = os.path.join(self.dir.name, "aacpanel")
        os.mkdir(folder)
        path = os.path.join(folder, "brief-guide.md")
        with open(path, "w", encoding="utf-8") as f:
            f.write("Use concrete examples.\nName the cost.")
        self.assertEqual(self.server.answer(request), {
            "ok": True, "briefGuide": "Use concrete examples.\nName the cost."})
        with open(path, "wb") as f:
            f.write(b"\xff")
        self.assertFalse(self.server.answer(request)["ok"])
        os.unlink(path)
        os.mkdir(path)
        self.assertFalse(self.server.answer(request)["ok"])

    def test_the_shelf_can_be_asked_for_one_session(self):
        self.assertEqual(len(self.server.answer({"briefs": {"session": "s-1"}})["briefs"]), 1)
        self.assertEqual(self.server.answer({"briefs": {"session": "s-9"}})["briefs"], [])

    def test_one_brief_comes_back_whole(self):
        reply = self.server.answer({"brief": "seven-after-twelve"})
        self.assertTrue(reply["ok"])
        self.assertEqual(len(reply["brief"]["decisions"]), 2)
        self.assertEqual(reply["brief"]["title"], DOC["title"])

    def test_a_brief_that_is_not_there_says_so(self):
        reply = self.server.answer({"brief": "never-published"})
        self.assertFalse(reply["ok"])
        self.assertIn("never published", reply["error"])


if __name__ == "__main__":
    unittest.main()


class Removing(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.shelf = briefs.Shelf(self.dir.name)
        self.shelf.put(briefs.clean(published()))

    def tearDown(self):
        self.dir.cleanup()

    def test_a_brief_is_taken_off_the_shelf(self):
        ok, why = self.shelf.drop(DOC["id"])
        self.assertTrue(ok, why)
        self.assertIsNone(self.shelf.of(DOC["id"]))

    def test_a_session_removes_only_the_documents_of_its_own_directory(self):
        # A brief belongs to the work it was written for, and a session
        # carrying on other work is not the one to put it down.
        ok, why = self.shelf.drop(DOC["id"], "/srv/elsewhere")
        self.assertFalse(ok)
        self.assertIn("/srv/proj", why)
        self.assertIsNotNone(self.shelf.of(DOC["id"]))

    def test_a_session_of_the_same_directory_removes_it(self):
        ok, why = self.shelf.drop(DOC["id"], "/srv/proj")
        self.assertTrue(ok, why)

    def test_a_name_that_is_not_a_name_removes_nothing(self):
        for bad in ("../secret", "", "NOT-A-NAME"):
            ok, why = self.shelf.drop(bad)
            self.assertFalse(ok, bad)
        self.assertIsNotNone(self.shelf.of(DOC["id"]))

    def test_removing_what_is_already_gone_says_so(self):
        self.shelf.drop(DOC["id"])
        ok, why = self.shelf.drop(DOC["id"])
        self.assertFalse(ok)
        self.assertIn("no brief", why)
