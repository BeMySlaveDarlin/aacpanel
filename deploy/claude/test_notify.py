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

import notify  # noqa: E402


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


class Collector:
    """A stand-in for the collector: takes one call and answers what it is told."""

    def __init__(self, path, reply):
        self.path = path
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
            raw = conn.recv(8192)
            try:
                self.got = json.loads(raw.decode("utf-8"))
            except ValueError:
                self.got = {"unparsed": raw.decode("utf-8", "replace")}
            conn.sendall(json.dumps(self.reply).encode("utf-8"))

    def close(self):
        self.sock.close()


class Call(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)
        self.path = os.path.join(self.dir.name, "notify.sock")

    def collector(self, reply):
        c = Collector(self.path, reply)
        self.addCleanup(c.close)
        return c

    def run_cli(self, args):
        with said() as out:
            code = notify.main(args + ["--socket", self.path])
        return code, out.getvalue()

    def test_the_line_and_the_caller_reach_the_collector(self):
        c = self.collector({"ok": True})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli(["stuck", "on", "the", "migration"])
        self.assertEqual(code, 0, out)
        c.thread.join(5)
        self.assertEqual(c.got["text"], "stuck on the migration")
        self.assertEqual(c.got["sessionId"], "11111111-1111-4111-8111-111111111111")
        self.assertIn("OK", out)

    def test_a_refusal_is_told_out_loud(self):
        self.collector({"ok": False, "error": "one call a minute, 40 s left"})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli(["come", "here"])
        self.assertEqual(code, 1)
        self.assertIn("40 s left", out,
                      "the session thinks it called the person while nothing went out")

    def test_nothing_is_sent_without_a_session(self):
        c = self.collector({"ok": True})
        with session(None):
            code, out = self.run_cli(["come", "here"])
        self.assertEqual(code, 2)
        self.assertIsNone(c.got, "a call went out naming nobody: the push opens no session")

    def test_an_empty_line_is_refused_before_the_socket(self):
        c = self.collector({"ok": True})
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli(["   "])
        self.assertEqual(code, 2)
        self.assertIsNone(c.got, "a push with nothing in it reached the phone")

    def test_a_silent_collector_is_not_mistaken_for_success(self):
        with session("11111111-1111-4111-8111-111111111111"):
            code, out = self.run_cli(["come", "here"])
        self.assertEqual(code, 1)
        self.assertIn("STOP", out)
        self.assertIn("nobody was called", out,
                      "the session is not told to say it in the conversation instead")


if __name__ == "__main__":
    unittest.main()
