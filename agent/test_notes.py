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
import notes  # noqa: E402


@contextlib.contextmanager
def muted():
    said = io.StringIO()
    with contextlib.redirect_stdout(said):
        yield said


SESSION = "66666666-6666-4666-8666-666666666666"
OTHER = "77777777-7777-4777-8777-777777777777"


def stamp(seconds):
    return time.strftime(notes.STAMP, time.gmtime(seconds))


class Clean(unittest.TestCase):
    def test_a_call_carries_a_session_and_a_word(self):
        self.assertIsNone(notes.clean({"text": "come here"}), "a call with no caller was taken")
        self.assertIsNone(notes.clean({"sessionId": SESSION}), "a call with nothing to say was taken")
        self.assertIsNone(notes.clean({"sessionId": SESSION, "text": "   "}))

    def test_the_line_is_squeezed_and_cut(self):
        note = notes.clean({"sessionId": SESSION, "text": " stuck   on\nthe migration "})
        self.assertEqual(note["text"], "stuck on the migration",
                         "the line reaches a phone as it was typed, newlines and all")
        long = notes.clean({"sessionId": SESSION, "text": "x" * 1000})
        self.assertEqual(len(long["text"]), notes.MAX_TEXT)

    def test_the_call_is_stamped(self):
        note = notes.clean({"sessionId": SESSION, "text": "come here"})
        self.assertIsNotNone(notes._seconds(note["at"]),
                             "the stamp is unreadable, and the panel keys a push by it")


class Rate(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.board = notes.Board(os.path.join(self.dir.name, "notes.json"))

    def note(self, session, text, at):
        return {"sessionId": session, "text": text, "at": stamp(at)}

    def test_a_call_is_taken(self):
        ok, why = self.board.put(self.note(SESSION, "come here", 1000), now=1000)
        self.assertTrue(ok, why)
        self.assertEqual(self.board.of(SESSION)["text"], "come here")

    def test_a_second_call_inside_the_minute_is_refused(self):
        self.board.put(self.note(SESSION, "come here", 1000), now=1000)
        ok, why = self.board.put(self.note(SESSION, "really, come here", 1030), now=1030)
        self.assertFalse(ok, "a session can call twice in half a minute: a phone buzzes on a loop")
        self.assertIn("30 s left", why, f"the refusal does not say how long is left: {why!r}")
        self.assertEqual(self.board.of(SESSION)["text"], "come here",
                         "the refused call took the place of the standing one")

    def test_after_the_minute_the_session_may_call_again(self):
        self.board.put(self.note(SESSION, "come here", 1000), now=1000)
        ok, why = self.board.put(self.note(SESSION, "still here", 1061), now=1061)
        self.assertTrue(ok, why)
        self.assertEqual(self.board.of(SESSION)["text"], "still here")

    def test_the_minute_is_counted_for_each_session_apart(self):
        self.board.put(self.note(SESSION, "come here", 1000), now=1000)
        ok, why = self.board.put(self.note(OTHER, "and here", 1001), now=1001)
        self.assertTrue(ok, "one session calling silenced another: %s" % why)

    def test_a_call_survives_a_restart_of_the_collector(self):
        self.board.put(self.note(SESSION, "come here", 1000), now=1000)
        again = notes.Board(self.board.path)
        self.assertEqual(again.of(SESSION)["text"], "come here")


class Sweep(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.board = notes.Board(os.path.join(self.dir.name, "notes.json"))

    def put(self, session, at):
        self.board.put({"sessionId": session, "text": "come here", "at": stamp(at)}, now=at)

    def test_the_call_of_a_dead_session_is_forgotten(self):
        self.put(SESSION, 1000)
        self.assertEqual(self.board.sweep({OTHER}, now=1010), [SESSION])
        self.assertIsNone(self.board.of(SESSION))

    def test_a_call_nobody_came_for_goes_stale(self):
        self.put(SESSION, 1000)
        self.assertEqual(self.board.sweep({SESSION}, now=1000 + notes.MAX_AGE + 1), [SESSION])
        self.assertIsNone(self.board.of(SESSION))

    def test_a_fresh_call_of_a_live_session_stays(self):
        self.put(SESSION, 1000)
        self.assertEqual(self.board.sweep({SESSION}, now=1030), [])
        self.assertIsNotNone(self.board.of(SESSION),
                             "the call was swept before the panel looked: the push is lost")

    def test_a_call_stays_long_enough_for_the_panel_to_look(self):
        self.assertGreater(notes.MAX_AGE, 60,
                           "the panel reads the snapshot every twenty seconds; a window this "
                           "narrow drops calls it never had a chance to carry")


class Socket(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.board = notes.Board(os.path.join(self.dir.name, "notes.json"))

    def serve(self, payload):
        here, there = socket.socketpair()
        got = {}

        def run():
            with muted():
                notes.handle(there, self.board)

        thread = threading.Thread(target=run)
        thread.start()
        here.sendall(payload)
        here.shutdown(socket.SHUT_WR)
        raw = here.recv(4096)
        thread.join(5)
        here.close()
        got.update(json.loads(raw.decode("utf-8")))
        return got

    def test_a_call_over_the_socket_reaches_the_board(self):
        reply = self.serve(json.dumps({"sessionId": SESSION, "text": "come here"}).encode())
        self.assertTrue(reply.get("ok"), reply)
        self.assertEqual(self.board.of(SESSION)["text"], "come here")

    def test_rubbish_is_answered_not_swallowed(self):
        reply = self.serve(b"{not json")
        self.assertFalse(reply.get("ok"))
        self.assertTrue(reply.get("error"), "the caller is told nothing and thinks it was called")

    def test_an_empty_call_is_refused_with_a_reason(self):
        reply = self.serve(json.dumps({"sessionId": SESSION, "text": ""}).encode())
        self.assertFalse(reply.get("ok"))
        self.assertIn("word", reply.get("error", ""))


if __name__ == "__main__":
    unittest.main()
