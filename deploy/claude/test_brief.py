#!/usr/bin/env python3
import contextlib
import io
import json
import os
import socket
import sys
import tempfile
import threading
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import brief  # noqa: E402


@contextlib.contextmanager
def said():
    out = io.StringIO()
    with contextlib.redirect_stdout(out):
        yield out


@contextlib.contextmanager
def session(value):
    was = os.environ.get("CLAUDE_CODE_SESSION_ID")
    if value is None:
        os.environ.pop("CLAUDE_CODE_SESSION_ID", None)
    else:
        os.environ["CLAUDE_CODE_SESSION_ID"] = value
    try:
        yield
    finally:
        if was is None:
            os.environ.pop("CLAUDE_CODE_SESSION_ID", None)
        else:
            os.environ["CLAUDE_CODE_SESSION_ID"] = was


DOC = {
    "id": "two-questions",
    "title": "Two questions before the rollout",
    "lede": "Both close today.",
    "questions": [
        {"id": "q1", "title": "The order of the two", "kind": "pick",
         "options": [{"key": "A", "label": "Now"}, {"key": "B", "label": "After"}]},
        {"id": "q2", "title": "What this rests on", "kind": "none"},
    ],
}


class Collector:
    """A stand-in for the collector: takes one document and answers what it is told."""

    def __init__(self, path, reply):
        self.reply = reply
        self.got = None
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.bind(path)
        self.sock.listen(1)
        self.thread = threading.Thread(target=self._serve, daemon=True)
        self.thread.start()

    def _serve(self):
        try:
            conn, _ = self.sock.accept()
        except OSError:
            return
        with conn:
            raw = b""
            while True:
                chunk = conn.recv(64 * 1024)
                if not chunk:
                    break
                raw += chunk
            try:
                self.got = json.loads(raw.decode("utf-8"))
            except ValueError:
                self.got = {"unparsed": raw.decode("utf-8", "replace")}
            conn.sendall(json.dumps(self.reply).encode("utf-8"))

    def close(self):
        self.sock.close()


class Publish(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)
        self.path = os.path.join(self.dir.name, "brief.sock")

    def collector(self, reply):
        c = Collector(self.path, reply)
        self.addCleanup(c.close)
        return c

    def doc_file(self, name="brief.json", body=None):
        path = os.path.join(self.dir.name, name)
        text = body if body is not None else json.dumps(DOC, ensure_ascii=False)
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        return path

    def run_cli(self, args):
        with said() as out:
            code = brief.main(args + ["--socket", self.path])
        return code, out.getvalue()

    def test_the_document_and_the_session_reach_the_collector(self):
        c = self.collector({"ok": True, "id": "two-questions"})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli([self.doc_file()])
        self.assertEqual(code, 0, out)
        c.thread.join(5)
        self.assertEqual(c.got["doc"]["title"], DOC["title"])
        self.assertEqual(len(c.got["doc"]["questions"]), 2)
        self.assertEqual(c.got["sessionId"], "11111111-1111-4111-8111-111111111111")
        self.assertEqual(c.got["cwd"], os.getcwd())
        self.assertIn("two-questions", out)

    def test_the_session_is_told_not_to_wait_for_the_answers(self):
        self.collector({"ok": True, "id": "two-questions"})
        with session("11111111-1111-4111-8111-111111111111"):
            _, out = self.run_cli([self.doc_file()])
        self.assertIn("do not wait", out,
                      "a session that waits for a brief stands for as long as the person reads it")

    def test_a_brief_that_asks_nothing_promises_no_answers(self):
        self.collector({"ok": True, "id": "report"})
        report = dict(DOC, questions=[{"id": "q1", "title": "What happened", "kind": "none"}])
        with session("11111111-1111-4111-8111-111111111111"):
            _, out = self.run_cli([self.doc_file(body=json.dumps(report))])
        self.assertNotIn("do not wait", out)

    def test_a_refusal_is_told_out_loud(self):
        self.collector({"ok": False, "error": "the id 'two-questions' is taken by a brief from /srv/other"})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli([self.doc_file()])
        self.assertEqual(code, 1)
        self.assertIn("/srv/other", out,
                      "the session thinks the brief is published while nothing was stored")

    def test_nothing_is_published_without_a_session(self):
        c = self.collector({"ok": True})
        with session(None):
            code, out = self.run_cli([self.doc_file()])
        self.assertEqual(code, 2)
        self.assertIsNone(c.got, "a brief went out naming nobody: the answer has nowhere to return")

    def test_check_reads_the_document_and_sends_nothing(self):
        c = self.collector({"ok": True})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli([self.doc_file(), "--check"])
        self.assertEqual(code, 0)
        self.assertIsNone(c.got)
        self.assertIn("2 questions", out)
        self.assertIn("1 of them ask", out)

    def test_a_document_that_is_not_an_object_is_refused_before_the_socket(self):
        c = self.collector({"ok": True})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli([self.doc_file(body="[1, 2, 3]")])
        self.assertEqual(code, 2)
        self.assertIsNone(c.got)
        self.assertIn("an object", out)

    def test_a_missing_file_is_named(self):
        code, out = self.run_cli([os.path.join(self.dir.name, "nowhere.json")])
        self.assertEqual(code, 2)
        self.assertIn("not read", out)

    def test_yaml_is_read_where_the_machine_can_read_it(self):
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("this machine has no pyyaml")
        body = "id: two-questions\ntitle: Two questions before the rollout\nquestions: []\n"
        c = self.collector({"ok": True, "id": "two-questions"})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli([self.doc_file(name="brief.yaml", body=body)])
        self.assertEqual(code, 0, out)
        c.thread.join(5)
        self.assertEqual(c.got["doc"]["title"], "Two questions before the rollout")

    def test_broken_yaml_is_named_and_not_thrown(self):
        """A colon in an unquoted title is the usual mistake, and a traceback
        is a poor way of saying "put quotes around it"."""
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("this machine has no pyyaml")
        body = "id: tails\ntitle: Tails of a brief: the card and the push\nquestions: []\n"
        code, out = self.run_cli([self.doc_file(name="brief.yaml", body=body)])
        self.assertEqual(code, 2)
        self.assertIn("not valid yaml", out)
        self.assertNotIn("Traceback", out)

    def test_a_document_over_the_ceiling_is_refused_before_the_socket(self):
        c = self.collector({"ok": True})
        big = dict(DOC, lede="x" * (brief.MAX_BYTES + 1024))
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli([self.doc_file(body=json.dumps(big))])
        self.assertEqual(code, 1)
        self.assertIsNone(c.got)
        self.assertIn("longer than", out)


if __name__ == "__main__":
    unittest.main()
