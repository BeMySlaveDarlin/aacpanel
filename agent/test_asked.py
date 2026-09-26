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
import asked  # noqa: E402


@contextlib.contextmanager
def muted():
    said = io.StringIO()
    with contextlib.redirect_stdout(said):
        yield said

PROBE = {
    "session_id": "66666666-6666-4666-8666-666666666666",
    "transcript_path": "/home/u/.claude/projects/x/66666666.jsonl",
    "cwd": "/srv/proj/Beta/service/aacpanel",
    "hook_event_name": "PreToolUse",
    "tool_name": "AskUserQuestion",
    "tool_use_id": "toolu_01AAAAAAAAAAAAAAAAAAAAAA",
    "tool_input": {
        "questions": [{
            "question": "Fix it now or after the rollout?",
            "header": "Order",
            "options": [
                {"label": "Now", "description": "The rollout will wait"},
                {"label": "After", "description": "We roll out first"},
            ],
            "multiSelect": False,
        }],
    },
}


class Clean(unittest.TestCase):
    def test_the_question_is_parsed_whole(self):
        got = asked.clean(PROBE)
        self.assertEqual(got["sessionId"], PROBE["session_id"])
        self.assertEqual(got["toolUseId"], PROBE["tool_use_id"])
        q = got["questions"][0]
        self.assertEqual(q["text"], "Fix it now or after the rollout?")
        self.assertEqual(q["header"], "Order")
        self.assertEqual([o["label"] for o in q["options"]], ["Now", "After"])
        self.assertEqual(q["options"][0]["description"], "The rollout will wait")
        self.assertFalse(q["multi"])

    def test_a_multi_select_survives_the_parse(self):
        probe = json.loads(json.dumps(PROBE))
        probe["tool_input"]["questions"][0]["multiSelect"] = True
        self.assertTrue(asked.clean(probe)["questions"][0]["multi"])

    def test_the_preview_of_an_option_keeps_its_spaces(self):
        probe = json.loads(json.dumps(PROBE))
        layout = "+-----+\n|  A  |\n+-----+"
        probe["tool_input"]["questions"][0]["options"][1]["preview"] = layout
        got = asked.clean(probe)["questions"][0]["options"]
        self.assertEqual(got[1]["preview"], layout)
        self.assertNotIn("preview", got[0])

    def test_the_preview_is_cut_at_whole_lines(self):
        probe = json.loads(json.dumps(PROBE))
        probe["tool_input"]["questions"][0]["options"][0]["preview"] = "\n".join(
            "line %d" % n for n in range(asked.MAX_PREVIEW_LINES + 20))
        got = asked.clean(probe)["questions"][0]["options"][0]["preview"]
        self.assertEqual(len(got.split("\n")), asked.MAX_PREVIEW_LINES)
        self.assertTrue(got.endswith("line %d" % (asked.MAX_PREVIEW_LINES - 1)))

    def test_input_with_no_question_is_rejected(self):
        self.assertIsNone(asked.clean({"session_id": "x", "tool_input": {}}))
        self.assertIsNone(asked.clean({"tool_input": PROBE["tool_input"]}))
        self.assertIsNone(asked.clean({"session_id": "x", "tool_input": {"questions": "not a list"}}))

    def test_a_question_with_no_text_is_not_shown(self):
        probe = json.loads(json.dumps(PROBE))
        probe["tool_input"]["questions"][0]["question"] = "   "
        self.assertIsNone(asked.clean(probe))


class Store(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.book = asked.Book(os.path.join(self.dir.name, "asked.json"))

    def test_the_question_is_remembered_and_handed_back(self):
        ask = asked.clean(PROBE)
        self.book.put(ask)
        self.assertEqual(self.book.of(ask["sessionId"])["toolUseId"], ask["toolUseId"])

    def test_the_question_survives_a_restart_of_the_agent(self):
        self.book.put(asked.clean(PROBE))
        again = asked.Book(self.book.path)
        self.assertIsNotNone(again.of(PROBE["session_id"]))

    def test_a_second_question_displaces_the_first(self):
        self.book.put(asked.clean(PROBE))
        second = asked.clean(PROBE)
        second["toolUseId"] = "toolu_second"
        self.book.put(second)
        self.assertEqual(self.book.of(PROBE["session_id"])["toolUseId"], "toolu_second")

    def test_an_answer_clears_the_question(self):
        ask = asked.clean(PROBE)
        self.book.put(ask)
        self.assertTrue(self.book.answered(ask["sessionId"], {ask["toolUseId"]}))
        self.assertIsNone(self.book.of(ask["sessionId"]))

    def test_someone_elses_answer_does_not_clear_the_question(self):
        ask = asked.clean(PROBE)
        self.book.put(ask)
        self.assertFalse(self.book.answered(ask["sessionId"], {"toolu_other"}))
        self.assertIsNotNone(self.book.of(ask["sessionId"]))

    def test_a_turn_that_ended_after_the_question_clears_it(self):
        # The hook reported a call the conversation never made: nothing will
        # answer it, and the next end of a turn is what says so.
        ask = asked.clean(PROBE)
        ask["at"] = "2026-09-26T15:42:30Z"
        self.book.put(ask)
        self.assertTrue(self.book.answered(ask["sessionId"], set(), "2026-09-26T15:47:07.105Z"))
        self.assertIsNone(self.book.of(ask["sessionId"]))

    def test_a_turn_end_before_the_question_keeps_it(self):
        ask = asked.clean(PROBE)
        ask["at"] = "2026-09-26T15:42:30Z"
        self.book.put(ask)
        for ended in ("", "2026-09-26T15:42:27.300Z", "2026-09-26T15:42:30.900Z", "not a time"):
            self.assertFalse(self.book.answered(ask["sessionId"], set(), ended),
                             f"the question went with a turn end at {ended!r}")
        self.assertIsNotNone(self.book.of(ask["sessionId"]))

    def test_the_question_of_a_dead_session_is_forgotten(self):
        self.book.put(asked.clean(PROBE))
        with muted():
            self.assertEqual(self.book.sweep(alive=set()), [PROBE["session_id"]])
        self.assertIsNone(self.book.of(PROBE["session_id"]))

    def test_a_live_session_keeps_its_question(self):
        self.book.put(asked.clean(PROBE))
        self.assertEqual(self.book.sweep(alive={PROBE["session_id"]}), [])
        self.assertIsNotNone(self.book.of(PROBE["session_id"]))

    def test_a_stale_question_is_forgotten(self):
        ask = asked.clean(PROBE)
        old = time.gmtime(time.time() - asked.MAX_AGE - 3600)
        ask["at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", old)
        self.book.put(ask)
        with muted() as said:
            self.assertEqual(self.book.sweep(alive={ask["sessionId"]}), [ask["sessionId"]])
        self.assertIn("older than a day", said.getvalue())

    def test_the_age_of_the_question_is_counted_from_utc(self):
        os.environ["TZ"] = "Asia/Bangkok"
        time.tzset()
        self.addCleanup(time.tzset)
        self.addCleanup(os.environ.pop, "TZ", None)
        ask = asked.clean(PROBE)
        half = time.gmtime(time.time() - asked.MAX_AGE // 2)
        ask["at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", half)
        self.book.put(ask)
        self.assertEqual(self.book.sweep(alive={ask["sessionId"]}), [])
        self.assertIsNotNone(self.book.of(ask["sessionId"]))


class Disk(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)

    def unwritable_store(self):
        path = os.path.join(self.dir.name, "asked.json")
        os.mkdir(path)
        return asked.Book(path)

    def test_an_unsaved_question_does_not_stay_in_memory(self):
        book = self.unwritable_store()
        with muted() as said:
            self.assertFalse(book.put(asked.clean(PROBE)))
        self.assertIsNone(book.of(PROBE["session_id"]))
        self.assertIn(book.path, said.getvalue())

    def test_the_hook_learns_that_the_question_was_not_saved(self):
        book = self.unwritable_store()
        ours, theirs = socket.socketpair()
        with theirs, muted():
            theirs.sendall(json.dumps(PROBE).encode("utf-8"))
            theirs.shutdown(socket.SHUT_WR)
            asked.handle(ours, book)
            reply = json.loads(theirs.recv(4096).decode("utf-8"))
        self.assertFalse(reply["ok"])
        self.assertIn("was not saved", reply["error"])

    def test_a_failed_clearing_leaves_the_question_for_a_retry(self):
        book = asked.Book(os.path.join(self.dir.name, "asked.json"))
        ask = asked.clean(PROBE)
        book.put(ask)
        book._write = lambda: False
        self.assertFalse(book.answered(ask["sessionId"], {ask["toolUseId"]}))
        self.assertIsNotNone(book.of(ask["sessionId"]))
        with open(book.path, encoding="utf-8") as f:
            self.assertIn(ask["sessionId"], json.load(f))

    def test_a_failed_sweep_leaves_the_question_for_a_retry(self):
        book = asked.Book(os.path.join(self.dir.name, "asked.json"))
        book.put(asked.clean(PROBE))
        book._write = lambda: False
        with muted():
            self.assertEqual(book.sweep(alive=set()), [])
        self.assertIsNotNone(book.of(PROBE["session_id"]))


class Together(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.book = asked.Book(os.path.join(self.dir.name, "asked.json"))

    def ask(self, use, session=PROBE["session_id"]):
        got = asked.clean(PROBE)
        got["toolUseId"] = use
        got["sessionId"] = session
        return got

    def test_the_clearing_does_not_touch_the_next_question(self):
        ready = threading.Event()

        class Slow(dict):
            def get(self, key, default=None):
                ready.set()
                time.sleep(0.03)
                return dict.get(self, key, default)

        first = self.ask("toolu_first")
        self.book._asks = Slow({first["sessionId"]: first})

        def asking():
            ready.wait(1)
            self.book.put(self.ask("toolu_second"))

        thread = threading.Thread(target=asking)
        thread.start()
        self.assertTrue(self.book.answered(first["sessionId"], {"toolu_first"}))
        thread.join(2)

        left = self.book.of(first["sessionId"])
        self.assertIsNotNone(left, "the question that had just been asked was cleared")
        self.assertEqual(left["toolUseId"], "toolu_second")

    def wedge(self, at=1):
        opened = threading.Event()
        walks = [0]

        class Wedged(dict):
            def items(self):
                view = dict.items(self)

                def walk():
                    for n, pair in enumerate(view):
                        if n == 0:
                            walks[0] += 1
                            if walks[0] == at:
                                opened.set()
                                time.sleep(0.03)
                        yield pair

                return walk()

        self.book._asks = Wedged({
            "s1": self.ask("toolu_1", session="s1"),
            "s2": self.ask("toolu_2", session="s2"),
        })
        return opened

    def test_writing_the_store_does_not_break_on_a_neighbours_edit(self):
        opened = self.wedge()
        failures = []

        def asking():
            opened.wait(1)
            self.book.put(self.ask("toolu_3", session="s3"))

        thread = threading.Thread(target=asking)
        thread.start()
        try:
            self.book.put(self.ask("toolu_4", session="s4"))
        except Exception as e:  # noqa: BLE001
            failures.append("%s: %s" % (type(e).__name__, e))
        thread.join(2)

        self.assertEqual(failures, [], "writing the store fell over on an edit from a neighbouring thread")
        with open(self.book.path, encoding="utf-8") as f:
            json.load(f)

    def test_the_sweep_survives_a_neighbours_question(self):
        opened = self.wedge(at=2)
        failures = []

        def asking():
            opened.wait(1)
            self.book.put(self.ask("toolu_3", session="s3"))

        thread = threading.Thread(target=asking)
        thread.start()
        try:
            with muted():
                self.book.sweep(alive={"s1"})
        except Exception as e:  # noqa: BLE001
            failures.append("%s: %s" % (type(e).__name__, e))
        thread.join(2)

        self.assertEqual(failures, [], "the sweep fell over on an edit from a neighbouring thread")
        self.assertIsNotNone(self.book.of("s3"), "the question that arrived during the sweep is lost")


class Socket(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.book = asked.Book(os.path.join(self.dir.name, "asked.json"))
        self.path = os.path.join(self.dir.name, "ask.sock")
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.bind(self.path)
        self.sock.listen(2)
        self.addCleanup(self.sock.close)

    def send(self, payload):
        def serve():
            conn, _ = self.sock.accept()
            asked.handle(conn, self.book)

        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        client.settimeout(5)
        client.connect(self.path)
        client.sendall(payload)
        client.shutdown(socket.SHUT_WR)
        reply = client.recv(4096)
        client.close()
        thread.join(timeout=5)
        return json.loads(reply.decode("utf-8"))

    def test_the_question_arrives_over_the_socket(self):
        got = self.send(json.dumps(PROBE).encode("utf-8"))
        self.assertTrue(got["ok"], got)
        self.assertIsNotNone(self.book.of(PROBE["session_id"]))

    def test_garbage_does_not_break_the_receiving(self):
        got = self.send("not json".encode("utf-8"))
        self.assertFalse(got["ok"])
        self.assertIn("was not parsed", got["error"])

    def test_a_message_with_no_question_is_rejected_with_a_reason(self):
        got = self.send(json.dumps({"session_id": "x", "tool_input": {}}).encode("utf-8"))
        self.assertFalse(got["ok"])
        self.assertIn("no question", got["error"])


class Hook(unittest.TestCase):
    def run_hook(self, payload, socket_path):
        import subprocess
        env = dict(os.environ, AACP_ASK_SOCKET=socket_path)
        hook = os.path.join(os.path.dirname(os.path.abspath(__file__)), "ask-hook.py")
        return subprocess.run([sys.executable, hook], input=json.dumps(payload),
                              capture_output=True, text=True, timeout=20, env=env)

    def test_with_no_socket_the_hook_stays_quiet_and_does_not_interfere(self):
        res = self.run_hook(PROBE, "/nonexistent/ask.sock")
        self.assertEqual(res.returncode, 0)
        self.assertEqual(res.stdout.strip(), "")

    def test_another_tool_is_left_alone(self):
        res = self.run_hook({"tool_name": "Bash", "tool_input": {}}, "/nonexistent/ask.sock")
        self.assertEqual(res.returncode, 0)

    def test_the_question_goes_into_the_socket(self):
        dirname = test_barrier.tmp_path()
        self.addCleanup(lambda: __import__("shutil").rmtree(dirname, ignore_errors=True))
        path = os.path.join(dirname, "ask.sock")
        book = asked.Book(os.path.join(dirname, "asked.json"))
        srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        srv.bind(path)
        srv.listen(1)
        self.addCleanup(srv.close)

        def serve():
            conn, _ = srv.accept()
            asked.handle(conn, book)

        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        res = self.run_hook(PROBE, path)
        thread.join(timeout=10)
        self.assertEqual(res.returncode, 0)
        self.assertIsNotNone(book.of(PROBE["session_id"]))


if __name__ == "__main__":
    unittest.main()
