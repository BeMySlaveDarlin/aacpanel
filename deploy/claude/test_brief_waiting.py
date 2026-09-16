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
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
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
    def test_an_answered_brief_of_this_project_is_named(self):
        got = brief_waiting.line(brief_waiting.waiting([card()], "/srv/proj"))
        self.assertIn("What is left to decide", got)
        self.assertIn("5 of 5", got)
        self.assertIn("Briefs", got)

    def test_a_brief_already_sent_is_not_news(self):
        self.assertEqual(brief_waiting.waiting([card(sent=True)], "/srv/proj"), [])

    def test_an_untouched_brief_is_not_waiting_on_anybody(self):
        """Nobody has answered it, so there is nothing for this session to carry."""
        self.assertEqual(brief_waiting.waiting([card(answered=0)], "/srv/proj"), [])

    def test_a_brief_of_another_project_stays_there(self):
        self.assertEqual(brief_waiting.waiting([card(cwd="/srv/other")], "/srv/proj"), [])

    def test_more_than_three_are_counted_rather_than_listed(self):
        cards = [card(id=f"b{i}", title=f"Brief {i}") for i in range(5)]
        got = brief_waiting.line(brief_waiting.waiting(cards, "/srv/proj"))
        self.assertIn("and 2 more", got)

    def test_nothing_waiting_says_nothing_at_all(self):
        self.assertEqual(brief_waiting.line([]), "")

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
