import json
import os
import socket
import sys
import threading
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import pages  # noqa: E402


PAGE = {
    "session": "11111111-1111-4111-8111-111111111111",
    "cwd": "/srv/proj",
    "path": "/home/u/.cache/scratch/roadmap.html",
    "title": "The roadmap of the contour",
    "desc": "what is done and what follows what",
    "icon": "🗺",
    "url": "https://claude.ai/public/artifacts/ab12cd34",
    "html": "<!doctype html><title>Roadmap</title><p>hello",
}


class Clean(unittest.TestCase):
    def test_a_page_is_named_by_where_it_was_published(self):
        # The address survives republishing, so a second publish of the same
        # document lands on the copy it replaces instead of piling up.
        page = pages.clean(PAGE)
        self.assertEqual(page["id"], "ab12cd34")
        again = pages.clean(dict(PAGE, title="The roadmap, again"))
        self.assertEqual(again["id"], page["id"])

    def test_a_page_without_an_address_is_named_by_its_own_file(self):
        page = pages.clean(dict(PAGE, url=""))
        self.assertTrue(page["id"].startswith("f-"))
        other = pages.clean(dict(PAGE, url="", path="/home/u/other.html"))
        self.assertNotEqual(page["id"], other["id"])

    def test_the_copy_carries_what_the_card_shows(self):
        page = pages.clean(PAGE)
        self.assertEqual(page["file"], "roadmap.html")
        self.assertEqual(page["title"], "The roadmap of the contour")
        self.assertEqual(page["desc"], "what is done and what follows what")
        self.assertEqual(page["bytes"], len(PAGE["html"].encode("utf-8")))

    def test_a_page_of_nobody_is_refused(self):
        # Without a session there is no conversation to show the page under.
        with self.assertRaises(pages.Refused):
            pages.clean(dict(PAGE, session=""))

    def test_an_empty_page_is_refused(self):
        with self.assertRaises(pages.Refused):
            pages.clean(dict(PAGE, html="   "))

    def test_a_page_past_the_ceiling_is_left_to_its_link(self):
        with self.assertRaises(pages.Refused):
            pages.clean(dict(PAGE, html="x" * (pages.MAX_HTML + 1)))

    def test_an_address_that_is_not_one_is_dropped(self):
        # A card offers the link as a second way in. A "javascript:" line in
        # that field would be a way in of another kind.
        page = pages.clean(dict(PAGE, url="javascript:alert(1)"))
        self.assertEqual(page["url"], "")
        self.assertTrue(page["id"].startswith("f-"))


class ShelfKeeps(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.shelf = pages.Shelf(self.dir.name)

    def tearDown(self):
        self.dir.cleanup()

    def test_a_page_is_read_back_whole(self):
        ok, why = self.shelf.put(pages.clean(PAGE))
        self.assertTrue(ok, why)
        got = self.shelf.of("ab12cd34")
        self.assertEqual(got["html"], PAGE["html"])
        self.assertEqual(got["versions"], 1)

    def test_publishing_again_counts_and_keeps_the_first_time(self):
        self.shelf.put(pages.clean(PAGE))
        self.shelf.put(pages.clean(dict(PAGE, html="<!doctype html><p>second")))
        got = self.shelf.of("ab12cd34")
        self.assertEqual(got["versions"], 2)
        self.assertEqual(got["html"], "<!doctype html><p>second")
        self.assertTrue(got.get("firstAt"))

    def test_a_name_that_is_not_a_name_opens_nothing(self):
        # The name arrives in a path. A walk out of the shelf has to end here,
        # not in a neighbouring directory.
        self.shelf.put(pages.clean(PAGE))
        for bad in ("../secret", "ab12cd34/../ab12cd34", "AB12CD34", ""):
            self.assertIsNone(self.shelf.of(bad), bad)

    def test_the_card_leaves_the_page_behind(self):
        # A shelf of twenty pages would be megabytes of html if the list
        # carried the documents.
        self.shelf.put(pages.clean(PAGE))
        card = self.shelf.cards()[0]
        self.assertNotIn("html", card)
        self.assertEqual(card["title"], "The roadmap of the contour")
        self.assertEqual(card["bytes"], len(PAGE["html"].encode("utf-8")))

    def test_the_shelf_of_one_session_holds_only_its_own(self):
        self.shelf.put(pages.clean(PAGE))
        self.shelf.put(pages.clean(dict(PAGE, url="https://claude.ai/x/other", session="22222222-2222-4222-8222-222222222222")))
        mine = self.shelf.cards(session=PAGE["session"])
        self.assertEqual([c["id"] for c in mine], ["ab12cd34"])

    def test_the_sweep_keeps_the_newest_and_drops_the_rest(self):
        keep = pages.MAX_PAGES
        for i in range(keep + 3):
            self.shelf.put(pages.clean(dict(PAGE, url=f"https://claude.ai/x/page-{i:03d}")))
        gone = self.shelf.sweep()
        self.assertEqual(len(gone), 3)
        self.assertEqual(len(self.shelf.cards()), keep)


class OverTheSocket(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.shelf = pages.Shelf(self.dir.name)

    def tearDown(self):
        self.dir.cleanup()

    def talk(self, payload):
        here, there = socket.socketpair()
        thread = threading.Thread(target=pages.handle, args=(there, self.shelf))
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

    def test_a_publish_is_taken_and_named(self):
        reply = self.talk(PAGE)
        self.assertTrue(reply["ok"], reply)
        self.assertEqual(reply["id"], "ab12cd34")
        self.assertEqual(self.shelf.of("ab12cd34")["html"], PAGE["html"])

    def test_a_refusal_says_why(self):
        reply = self.talk(dict(PAGE, html=""))
        self.assertFalse(reply["ok"])
        self.assertIn("html", reply["error"])


if __name__ == "__main__":
    unittest.main()
