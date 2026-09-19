import json
import os
import socket
import sys
import threading
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402
import reviews  # noqa: E402


DAY = 24 * 3600

REVIEW = {
    "id": "aacpanel-20260919-081500",
    "session": "11111111-1111-4111-8111-111111111111",
    "cwd": "/srv/proj",
    "base": "main",
    "at": "2026-09-19T08:15:00Z",
    "notes": [
        {"id": "n1", "path": "pkg/env.go", "line": 74,
         "quote": "\tif warn != \"\" {", "text": "this is read twice",
         "at": "2026-09-19T08:14:00Z"},
        {"id": "n2", "path": "pkg/env.go", "line": 91,
         "quote": "\t\treturn nil", "text": "and the error goes where?",
         "at": "2026-09-19T08:14:30Z"},
    ],
}


def stamp(delta=0):
    """The time this many seconds from now, in the shape a reading carries."""
    return time.strftime(reviews.STAMP, time.gmtime(time.time() + delta))


class Clean(unittest.TestCase):
    def test_a_reading_carries_what_the_session_opens_it_for(self):
        review = reviews.clean(REVIEW)
        self.assertEqual(review["id"], "aacpanel-20260919-081500")
        self.assertEqual(review["session"], REVIEW["session"])
        self.assertEqual(review["cwd"], "/srv/proj")
        self.assertEqual(review["base"], "main")
        self.assertEqual([n["line"] for n in review["notes"]], [74, 91])

    def test_a_name_that_the_panel_did_not_mint_is_refused(self):
        # The name becomes the name of a file, and this is the one operation of
        # the viewer that writes: a name is turned away, never repaired into
        # something that would open a neighbouring file.
        for bad in ("../secret", "a/b", "a.json", "a b", "\u0441\u043b\u043e\u0432\u043e", "", None,
                    "x" * (reviews.MAX_ID + 1)):
            with self.assertRaises(reviews.Refused, msg=repr(bad)):
                reviews.clean(dict(REVIEW, id=bad))

    def test_the_quote_keeps_the_indentation_of_the_line(self):
        # The quote is how a note is found again after the branch moves. Two
        # lines that differ only by their indentation are two lines.
        review = reviews.clean(REVIEW)
        self.assertEqual(review["notes"][0]["quote"], "\tif warn != \"\" {")
        self.assertEqual(review["notes"][1]["quote"], "\t\treturn nil")

    def test_a_reading_of_no_conversation_is_refused(self):
        with self.assertRaises(reviews.Refused):
            reviews.clean(dict(REVIEW, session="  "))

    def test_a_note_that_stands_nowhere_is_refused(self):
        for bad in ({"id": "n1", "path": "", "line": 74, "text": "x"},
                    {"id": "n1", "path": "pkg/env.go", "line": 0, "text": "x"},
                    {"id": "n1", "path": "pkg/env.go", "line": -3, "text": "x"},
                    {"id": "n1", "path": "pkg/env.go", "line": "seventy", "text": "x"},
                    {"id": "n1", "path": "pkg/env.go", "line": 74, "text": "   "},
                    {"id": "", "path": "pkg/env.go", "line": 74, "text": "x"}):
            with self.assertRaises(reviews.Refused, msg=repr(bad)):
                reviews.clean(dict(REVIEW, notes=[bad]))

    def test_two_notes_under_one_name_are_refused(self):
        twice = [dict(REVIEW["notes"][0]), dict(REVIEW["notes"][1], id="n1")]
        with self.assertRaises(reviews.Refused):
            reviews.clean(dict(REVIEW, notes=twice))

    def test_more_notes_than_a_reading_holds_are_refused(self):
        many = [dict(REVIEW["notes"][0], id=f"n{i}") for i in range(reviews.MAX_NOTES + 1)]
        with self.assertRaises(reviews.Refused):
            reviews.clean(dict(REVIEW, notes=many))
        fits = many[:reviews.MAX_NOTES]
        self.assertEqual(len(reviews.clean(dict(REVIEW, notes=fits))["notes"]),
                         reviews.MAX_NOTES)

    def test_a_note_past_its_ceiling_is_refused_rather_than_cut(self):
        # The panel holds a note to these numbers before it saves one, so a
        # note past them did not come from the panel. Cutting it here would
        # hand the session a reading that is not what was written.
        with self.assertRaises(reviews.Refused):
            reviews.clean(dict(REVIEW, notes=[dict(REVIEW["notes"][0],
                                                   text="x" * (reviews.MAX_TEXT + 1))]))
        with self.assertRaises(reviews.Refused):
            reviews.clean(dict(REVIEW, notes=[dict(REVIEW["notes"][0],
                                                   quote="x" * (reviews.MAX_QUOTE + 1))]))

    def test_notes_that_are_not_a_list_are_refused(self):
        for bad in ("n1", {"id": "n1"}, 7):
            with self.assertRaises(reviews.Refused, msg=repr(bad)):
                reviews.clean(dict(REVIEW, notes=bad))

    def test_a_time_is_kept_in_one_shape_whatever_shape_it_arrives_in(self):
        # Go prints a time with fractions and with an offset that is not
        # always Z; the sweep compares one shape and knows no others.
        review = reviews.clean(dict(REVIEW, at="2026-09-19T11:15:00.123456789+03:00"))
        self.assertEqual(review["at"], "2026-09-19T08:15:00Z")

    def test_a_reading_that_names_no_time_is_stamped_on_arrival(self):
        for missing in ("", "yesterday", None):
            review = reviews.clean(dict(REVIEW, at=missing))
            self.assertAlmostEqual(reviews.seconds(review["at"]), time.time(), delta=60)


class ShelfKeeps(unittest.TestCase):
    def setUp(self):
        # The shelf sits one directory down, so that the run has a place a walk
        # out of it would land in.
        self.dir = test_barrier.tmp_dir()
        self.store = os.path.join(self.dir.name, "reviews")
        self.shelf = reviews.Shelf(self.store)

    def tearDown(self):
        self.dir.cleanup()

    def test_a_reading_is_read_back_whole_from_where_it_was_put(self):
        where, why = self.shelf.put(reviews.clean(REVIEW))
        self.assertTrue(where, why)
        self.assertEqual(where, os.path.join(self.store, "aacpanel-20260919-081500.json"))
        with open(where, encoding="utf-8") as f:
            on_disk = json.load(f)
        self.assertEqual(on_disk, self.shelf.of("aacpanel-20260919-081500"))
        self.assertEqual(on_disk["notes"][0]["text"], "this is read twice")

    def test_a_name_that_is_not_a_name_opens_nothing(self):
        # The name arrives from outside. A file of the same shape one directory
        # up is exactly what a walk out of the shelf would find, so there is one.
        outside = os.path.join(self.dir.name, "secret.json")
        with open(outside, "w", encoding="utf-8") as f:
            json.dump(reviews.clean(dict(REVIEW, id="secret")), f)

        self.shelf.put(reviews.clean(REVIEW))
        for bad in ("../secret", "x/../secret", "../secret.json",
                    "aacpanel-20260919-081500.json", " aacpanel-20260919-081500", ""):
            self.assertIsNone(self.shelf.of(bad), bad)
        self.assertIsNotNone(self.shelf.of("aacpanel-20260919-081500"))

    def test_the_card_leaves_the_notes_behind(self):
        # A list of readings is opened to see what is in hand. Carrying the
        # notes would make it as expensive as opening every one of them.
        self.shelf.put(reviews.clean(REVIEW))
        card = self.shelf.cards()[0]
        self.assertEqual(card["notes"], 2)
        self.assertEqual(card["session"], REVIEW["session"])
        self.assertEqual(card["at"], "2026-09-19T08:15:00Z")
        self.assertNotIn("quote", json.dumps(card))
        self.assertNotIn("this is read twice", json.dumps(card))

    def test_the_shelf_of_one_session_holds_only_its_own(self):
        self.shelf.put(reviews.clean(REVIEW))
        self.shelf.put(reviews.clean(dict(REVIEW, id="other-20260919-090000",
                                          session="22222222-2222-4222-8222-222222222222")))
        mine = self.shelf.cards(session=REVIEW["session"])
        self.assertEqual([c["id"] for c in mine], ["aacpanel-20260919-081500"])

    def test_a_second_send_of_one_reading_lands_on_the_same_file(self):
        first, _ = self.shelf.put(reviews.clean(REVIEW))
        again, _ = self.shelf.put(reviews.clean(dict(REVIEW, notes=REVIEW["notes"][:1])))
        self.assertEqual(first, again)
        self.assertEqual(len(self.shelf.cards()), 1)

    def test_a_reading_sent_a_month_ago_is_swept(self):
        self.shelf.put(reviews.clean(dict(REVIEW, at=stamp(-(reviews.SENT_AGE + DAY)))))
        self.shelf.put(reviews.clean(dict(REVIEW, id="fresh-20260919-090000", at=stamp())))
        gone = self.shelf.sweep()
        self.assertEqual(gone, ["aacpanel-20260919-081500"])
        self.assertEqual([c["id"] for c in self.shelf.cards()], ["fresh-20260919-090000"])

    def test_an_old_file_goes_whatever_it_says_about_itself(self):
        # The stamp inside is what the file claims. A file claiming to have
        # been sent tomorrow would sit on the shelf for ever if the age of the
        # file itself were not a rule of its own.
        where, _ = self.shelf.put(reviews.clean(dict(REVIEW, at=stamp(365 * DAY))))
        old = time.time() - (reviews.MAX_AGE + DAY)
        os.utime(where, (old, old))
        self.assertEqual(self.shelf.sweep(), ["aacpanel-20260919-081500"])
        self.assertEqual(self.shelf.cards(), [])

    def test_the_sweep_keeps_the_newest_and_drops_the_rest(self):
        keep = reviews.MAX_REVIEWS
        for i in range(keep + 3):
            self.shelf.put(reviews.clean(dict(REVIEW, id=f"reading-{i:03d}", at=stamp())))
        gone = self.shelf.sweep()
        self.assertEqual(len(gone), 3)
        self.assertEqual(len(self.shelf.cards()), keep)

    def test_the_sweep_drops_what_does_not_fit_in_the_room(self):
        was = reviews.MAX_STORE
        reviews.MAX_STORE = 2 * 1024
        self.addCleanup(setattr, reviews, "MAX_STORE", was)
        for i in range(8):
            self.shelf.put(reviews.clean(dict(REVIEW, id=f"reading-{i:03d}", at=stamp())))
            os.utime(os.path.join(self.store, f"reading-{i:03d}.json"),
                     (time.time() + i, time.time() + i))
        self.shelf.sweep()
        left = [c["id"] for c in self.shelf.cards()]
        self.assertLess(len(left), 8)
        room = sum(os.path.getsize(os.path.join(self.store, name + ".json")) for name in left)
        self.assertLessEqual(room, reviews.MAX_STORE)
        # What is kept is the newest of them, not whichever the directory listed first.
        self.assertEqual(set(left), {f"reading-{i:03d}" for i in range(8 - len(left), 8)})


class OverTheSocket(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.shelf = reviews.Shelf(self.dir.name)

    def tearDown(self):
        self.dir.cleanup()

    def talk(self, payload):
        here, there = socket.socketpair()
        thread = threading.Thread(target=reviews.handle, args=(there, self.shelf))
        thread.start()
        here.sendall(json.dumps(payload).encode("utf-8"))
        here.shutdown(socket.SHUT_WR)
        chunks = []
        while True:
            chunk = here.recv(64 * 1024)
            if not chunk:
                break
            chunks.append(chunk)
        here.close()
        thread.join(5)
        return json.loads(b"".join(chunks).decode("utf-8"))

    def test_a_reading_is_taken_and_the_answer_says_where_it_lies(self):
        # The panel writes down the path it is given and hands it to the
        # session: an answer without one leaves the session with nothing to open.
        reply = self.talk(REVIEW)
        self.assertTrue(reply["ok"], reply)
        self.assertEqual(reply["id"], "aacpanel-20260919-081500")
        self.assertEqual(reply["path"], os.path.join(self.dir.name, "aacpanel-20260919-081500.json"))
        self.assertTrue(os.path.exists(reply["path"]))
        self.assertEqual(reply["notes"], 2)

    def test_a_refusal_says_why_and_writes_nothing(self):
        reply = self.talk(dict(REVIEW, id="../escape"))
        self.assertFalse(reply["ok"])
        self.assertIn("name of a reading", reply["error"])
        self.assertEqual(os.listdir(self.dir.name), [])

    def test_a_request_that_is_not_a_reading_is_answered_rather_than_dropped(self):
        for bad in ("[]", "not json", ""):
            here, there = socket.socketpair()
            thread = threading.Thread(target=reviews.handle, args=(there, self.shelf))
            thread.start()
            here.sendall(bad.encode("utf-8"))
            here.shutdown(socket.SHUT_WR)
            got = here.recv(64 * 1024)
            here.close()
            thread.join(5)
            reply = json.loads(got.decode("utf-8"))
            self.assertFalse(reply["ok"], bad)
            self.assertTrue(reply["error"])

    def test_a_request_longer_than_the_cap_is_cut_off(self):
        was = reviews.MAX_REQUEST
        reviews.MAX_REQUEST = 4 * 1024
        self.addCleanup(setattr, reviews, "MAX_REQUEST", was)
        fat = dict(REVIEW, notes=[dict(REVIEW["notes"][0], id=f"n{i}", text="x" * 200)
                                  for i in range(30)])
        reply = self.talk(fat)
        self.assertFalse(reply["ok"])
        self.assertEqual(os.listdir(self.dir.name), [])


class ThroughTheChat(unittest.TestCase):
    """The panel asks for readings where it asks for everything else."""

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.was, reviews.SHELF = reviews.SHELF, reviews.Shelf(self.dir.name)
        self.addCleanup(setattr, reviews, "SHELF", self.was)
        self.addCleanup(self.dir.cleanup)
        reviews.SHELF.put(reviews.clean(REVIEW))

    def test_the_list_answers_with_cards(self):
        reply = chat.answer({"reviews": {}})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual([c["id"] for c in reply["reviews"]], ["aacpanel-20260919-081500"])
        self.assertEqual(reply["reviews"][0]["notes"], 2)

    def test_the_list_of_one_session_holds_only_its_own(self):
        reply = chat.answer({"reviews": {"session": "22222222-2222-4222-8222-222222222222"}})
        self.assertEqual(reply["reviews"], [])

    def test_one_reading_comes_back_whole(self):
        reply = chat.answer({"review": "aacpanel-20260919-081500"})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual(reply["review"]["notes"][0]["quote"], "\tif warn != \"\" {")

    def test_a_reading_that_is_not_there_is_said_to_be_missing(self):
        reply = chat.answer({"review": "aacpanel-20260101-000000"})
        self.assertFalse(reply["ok"])
        self.assertIn("no reading", reply["error"])


if __name__ == "__main__":
    unittest.main()
