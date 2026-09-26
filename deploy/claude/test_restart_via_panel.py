#!/usr/bin/env python3

import http.server
import importlib.util
import json
import pathlib
import threading
import time
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


restart = load("restart-via-panel")


class Panel:
    """A panel on a local port that answers the action as it is told."""

    def __init__(self, status=200, body=b'{"ok":true,"detail":"restarted"}', delay=0.0):
        self.got = []
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length") or 0)
                outer.got.append((self.path, json.loads(self.rfile.read(length))))
                time.sleep(delay)
                try:
                    self.send_response(status)
                    self.end_headers()
                    self.wfile.write(body)
                except OSError:
                    pass

            def log_message(self, *args):
                pass

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class TestRestartViaPanel(unittest.TestCase):

    def panel(self, **kw):
        p = Panel(**kw)
        self.addCleanup(p.close)
        return p

    def test_the_session_is_named_by_its_conversation(self):
        p = self.panel()
        taken, _ = restart.ask(p.url, "c0ffee")
        self.assertTrue(taken)
        self.assertEqual(p.got, [("/api/actions", {"kind": "session.restart", "params": {"conversation": "c0ffee"}})])

    def test_a_refusal_hands_the_restart_back(self):
        p = self.panel(status=400, body=b"no live session runs conversation c0ffee")
        taken, what = restart.ask(p.url, "c0ffee")
        self.assertFalse(taken)
        self.assertIn("no live session", what)

    def test_no_answer_in_time_is_a_restart_under_way(self):
        p = self.panel(delay=1.0)
        taken, what = restart.ask(p.url, "c0ffee", wait=0.2)
        self.assertTrue(taken, what)

    def test_no_panel_hands_the_restart_back(self):
        p = self.panel()
        url = p.url
        p.close()
        taken, what = restart.ask(url, "c0ffee")
        self.assertFalse(taken)
        self.assertIn("does not answer", what)


if __name__ == "__main__":
    unittest.main()
