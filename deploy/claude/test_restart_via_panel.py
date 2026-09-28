#!/usr/bin/env python3

import contextlib
import http.server
import importlib.util
import io
import json
import os
import pathlib
import tempfile
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


class TestTheBackgroundHoldsTheRestart(unittest.TestCase):

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)
        with open(os.path.join(self.dir.name, "state.json"), "w", encoding="utf-8") as f:
            json.dump({"sessionsAt": time.time() + 60,
                       "sessions": [{"sessionId": "c0ffee", "work": {"agents": 2, "tasks": 1}}]}, f)
        self.panel = Panel()
        self.addCleanup(self.panel.close)
        for k, v in (("AACP_STATE_DIR", self.dir.name), ("CLAUDE_CODE_SESSION_ID", "c0ffee")):
            was = os.environ.get(k)
            os.environ[k] = v
            self.addCleanup(lambda k=k, was=was: os.environ.pop(k, None) if was is None else os.environ.update({k: was}))

    def main(self, *args):
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = restart.main(["--url", self.panel.url, *args])
        return code, out.getvalue()

    def test_work_at_work_is_not_restarted_and_the_panel_is_not_asked(self):
        code, said = self.main()
        self.assertEqual(code, 2, said)
        self.assertIn("WAIT 2 agents and 1 background task of this session are at work", said)
        self.assertEqual(self.panel.got, [], "the panel was asked to restart a session with its agents at work")

    def test_anyway_asks_the_panel(self):
        code, said = self.main("--anyway")
        self.assertEqual(code, 0, said)
        self.assertEqual(len(self.panel.got), 1)


if __name__ == "__main__":
    unittest.main()
