import base64
import contextlib
import importlib.util
import io
import json
import os
import socket
import sys
import tempfile
import threading
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))

spec = importlib.util.spec_from_file_location("artifact_copy", os.path.join(HERE, "artifact-copy.py"))
copy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(copy)

# The cases this script shares with the executor's hook of the same name,
# which sends the same bytes on the same payload.
with open(os.path.join(HERE, "testdata", "artifact-copy.json"), encoding="utf-8") as f:
    CASES = json.load(f)["cases"]


class TestTheSharedCases(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = tmp.name
        self.socket = os.path.join(self.dir, "page.sock")
        self.got = []
        srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        srv.bind(self.socket)
        srv.listen(1)
        thread = threading.Thread(target=self.collect, args=(srv,), daemon=True)
        thread.start()

        def stop():
            srv.shutdown(socket.SHUT_RDWR)
            srv.close()
            thread.join(5)
        self.addCleanup(stop)

    def collect(self, srv):
        """Takes every message as the collector does, up to its end, and answers ok."""
        with contextlib.suppress(OSError):
            while True:
                conn, _ = srv.accept()
                with conn:
                    chunks = []
                    while chunk := conn.recv(64 * 1024):
                        chunks.append(chunk)
                    self.got.append(b"".join(chunks).decode("utf-8"))
                    conn.sendall(b'{"ok": true}')

    def test_every_shared_case(self):
        self.assertTrue(CASES, "the shared cases are empty: the test checks nothing")
        for i, case in enumerate(CASES):
            with self.subTest(case["what"]):
                page = os.path.join(self.dir, str(i), case.get("name", "page.html"))
                os.makedirs(os.path.dirname(page))
                body = None
                if "fill" in case:
                    body = (case["fill"][0] * case["fill"][1]).encode("utf-8")
                elif "fileBase64" in case:
                    body = base64.b64decode(case["fileBase64"])
                elif "file" in case:
                    body = case["file"].encode("utf-8")
                if body is not None:
                    with open(page, "wb") as f:
                        f.write(body)
                got = self.run_hook(case["input"].replace("{page}", page))
                if case.get("copied"):
                    sent = json.loads(got)
                    self.assertEqual((sent["html"].encode("utf-8"), sent["path"]), (body, page))
                elif case["sends"] is None:
                    self.assertIsNone(got)
                else:
                    self.assertEqual(got, case["sends"].replace("{page}", page))

    def run_hook(self, text):
        """Runs the hook against the listener and returns what reached it, or None: the hook
        waits for the collector's answer, which comes once the message is taken."""
        before = len(self.got)
        out = io.StringIO()
        try:
            sys.stdin = io.StringIO(text)
            with contextlib.redirect_stdout(out):
                self.assertEqual(copy.main([self.socket]), 0)
        finally:
            sys.stdin = sys.__stdin__
        self.assertEqual(out.getvalue(), "")
        sent = self.got[before:]
        self.assertLessEqual(len(sent), 1)
        return sent[0] if sent else None


class TheHookStaysQuiet(unittest.TestCase):
    def test_a_publish_with_no_panel_ends_well(self):
        # The hook runs on machines where the panel is not installed at all.
        page = tempfile.NamedTemporaryFile("w", suffix=".html", delete=False, encoding="utf-8")
        page.write("<p>hi")
        page.close()
        self.addCleanup(os.unlink, page.name)
        stdin = sys.stdin
        sys.stdin = io.StringIO(json.dumps({"tool_name": "Artifact", "session_id": "s",
                                            "tool_input": {"file_path": page.name}}))
        try:
            self.assertEqual(copy.main(["/run/nothing-here.sock"]), 0)
        finally:
            sys.stdin = stdin


if __name__ == "__main__":
    unittest.main()
