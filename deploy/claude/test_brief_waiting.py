#!/usr/bin/env python3
import contextlib
import importlib.util
import io
import json
import os
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("brief_waiting", os.path.join(HERE, "brief-waiting.py"))
brief_waiting = importlib.util.module_from_spec(spec)
spec.loader.exec_module(brief_waiting)


@contextlib.contextmanager
def said():
    out = io.StringIO()
    with contextlib.redirect_stdout(out):
        yield out


class Panel:
    """A panel answering one canned shelf on the loopback."""

    def __init__(self, body, status=200):
        payload = json.dumps(body).encode("utf-8")

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *args):
                pass

        self.server = HTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=lambda: self.server.serve_forever(poll_interval=0.01), daemon=True)
        self.thread.start()

    @property
    def url(self):
        return f"http://127.0.0.1:{self.server.server_address[1]}"

    def close(self):
        self.server.shutdown()
        self.server.server_close()


def card(**over):
    base = {"id": "what-is-left", "title": "What is left to decide", "cwd": "/srv/proj",
            "at": "2026-09-16T03:46:00Z", "questions": 5, "answered": 5, "sent": False}
    base.update(over)
    return base


class Waiting(unittest.TestCase):
    def test_the_hook_and_server_say_the_same_line(self):
        with open(os.path.join(HERE, "testdata", "brief-waiting.json"), encoding="utf-8") as f:
            cases = json.load(f)
        for case in cases:
            with self.subTest(case["name"]):
                got = brief_waiting.line(brief_waiting.waiting(case["cards"], case["cwd"]))
                self.assertEqual(got, case["line"])

    def test_the_shelf_is_read_from_the_panel(self):
        panel = Panel({"briefs": [card()]})
        self.addCleanup(panel.close)
        self.assertEqual(len(brief_waiting.shelf(panel.url)), 1)

    def test_a_panel_that_is_not_there_leaves_the_session_alone(self):
        """A session must not start with an error about a service it did not ask for."""
        with said() as out:
            code = brief_waiting.main(["http://127.0.0.1:9"])
        self.assertEqual(code, 0)
        self.assertEqual(out.getvalue(), "")

    def test_a_panel_answering_rubbish_is_not_a_crash(self):
        panel = Panel({"nothing": "here"})
        self.addCleanup(panel.close)
        self.assertEqual(brief_waiting.shelf(panel.url), [])


if __name__ == "__main__":
    unittest.main()
