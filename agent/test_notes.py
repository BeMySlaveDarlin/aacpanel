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
                           "a panel that is restarting takes the standing calls when it is back; "
                           "a window this narrow drops calls it never had a chance to carry")


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


class Wake(unittest.TestCase):
    """The panel waits on the chat socket for the next call, and a call answers it."""

    # As long as the panel waits for a call before it asks again.
    PANEL_WAIT = 8

    def setUp(self):
        from chat import server
        self.server = server
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.board = notes.Board(os.path.join(self.dir.name, "notes.json"))
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = self.board

    def call(self, session, text):
        here, there = socket.socketpair()
        with here:
            here.sendall(json.dumps({"sessionId": session, "text": text}).encode())
            here.shutdown(socket.SHUT_WR)
            with muted():
                notes.handle(there, self.board)
            return json.loads(here.recv(4096).decode("utf-8"))

    def wait_in_thread(self, after):
        got = {}

        def run():
            got["reply"] = self.server.answer({"notes": {"after": after, "wait": self.PANEL_WAIT}})
            got["at"] = time.monotonic()

        thread = threading.Thread(target=run, daemon=True)
        thread.start()
        return thread, got

    def test_a_call_answers_the_waiting_panel_at_once(self):
        seq = self.server.answer({"notes": {"wait": 0}})["seq"]
        thread, got = self.wait_in_thread(seq)
        time.sleep(0.3)
        self.assertNotIn("reply", got, "the collector answered with no call made: the panel would ask in a loop")

        made = time.monotonic()
        self.assertTrue(self.call(SESSION, "stuck on the migration").get("ok"))
        thread.join(self.PANEL_WAIT + 2)

        self.assertIn("reply", got, "the waiting request never came back")
        took = got["at"] - made
        self.assertLess(took, 1.0,
                        f"the call reached the panel {took:.1f} s after the collector took it: "
                        "the request waited out its time instead of being answered by the call")
        self.assertTrue(got["reply"]["ok"], got["reply"])
        self.assertEqual([(n["sessionId"], n["text"]) for n in got["reply"]["notes"]],
                         [(SESSION, "stuck on the migration")])
        self.assertGreater(got["reply"]["seq"], seq, "the count did not move, and the next wait would not wait")

    def test_a_panel_behind_the_count_is_answered_at_once(self):
        self.assertTrue(self.call(SESSION, "come here").get("ok"))
        started = time.monotonic()
        reply = self.server.answer({"notes": {"wait": self.PANEL_WAIT}})
        self.assertLess(time.monotonic() - started, 1.0,
                        "a panel that has just started waited for a call that is already standing")
        self.assertEqual([n["text"] for n in reply["notes"]], ["come here"])

    def test_an_idle_board_answers_when_the_wait_is_out(self):
        seq, _ = self.board.wait(None, 0)
        started = time.monotonic()
        again, standing = self.board.wait(seq, 0.2)
        self.assertGreaterEqual(time.monotonic() - started, 0.2)
        self.assertEqual((again, standing), (seq, []))

    def test_a_refused_call_wakes_nobody(self):
        self.assertTrue(self.call(SESSION, "come here").get("ok"))
        seq, _ = self.board.wait(None, 0)
        self.assertFalse(self.call(SESSION, "come here again").get("ok"))
        again, standing = self.board.wait(seq, 0.2)
        self.assertEqual(again, seq, "a call the collector refused counts as taken")
        self.assertEqual([n["text"] for n in standing], ["come here"])

    def test_the_wait_is_capped(self):
        self.addCleanup(setattr, notes, "MAX_WAIT", notes.MAX_WAIT)
        notes.MAX_WAIT = 0.2
        seq, _ = self.board.wait(None, 0)
        started = time.monotonic()
        self.board.wait(seq, 3600)
        self.assertLess(time.monotonic() - started, 2, "a request asking for an hour holds a thread for an hour")

    def test_a_request_that_is_not_a_number_is_answered_at_once(self):
        started = time.monotonic()
        reply = self.server.answer({"notes": {"after": True, "wait": "forever"}})
        self.assertLess(time.monotonic() - started, 1.0)
        self.assertTrue(reply["ok"], reply)
        self.assertEqual(reply["notes"], [])


if __name__ == "__main__":
    unittest.main()
