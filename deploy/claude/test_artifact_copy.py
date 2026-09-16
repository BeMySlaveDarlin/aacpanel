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


def publish(path, **over):
    payload = {
        "hook_event_name": "PostToolUse",
        "tool_name": "Artifact",
        "session_id": "11111111-1111-4111-8111-111111111111",
        "cwd": "/srv/proj",
        "tool_input": {"file_path": path, "title": "The roadmap", "description": "what follows what",
                       "favicon": "🗺"},
        "tool_response": {"url": "https://claude.ai/public/artifacts/ab12cd34"},
    }
    payload.update(over)
    return payload


class WhatIsCopied(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.page = os.path.join(self.dir.name, "roadmap.html")
        with open(self.page, "w", encoding="utf-8") as f:
            f.write("<!doctype html><title>Roadmap</title><p>hello")

    def tearDown(self):
        self.dir.cleanup()

    def test_a_publish_is_read_off_the_disk(self):
        got = copy.page_of(publish(self.page))
        self.assertEqual(got["html"], "<!doctype html><title>Roadmap</title><p>hello")
        self.assertEqual(got["title"], "The roadmap")
        self.assertEqual(got["url"], "https://claude.ai/public/artifacts/ab12cd34")
        self.assertEqual(got["cwd"], "/srv/proj")

    def test_another_tool_is_not_a_publish(self):
        self.assertIsNone(copy.page_of(publish(self.page, tool_name="Write")))

    def test_reading_and_listing_copy_nothing(self):
        # Only a publish makes a page. The other actions of the tool carry a
        # file path too, and copying on them would keep documents the session
        # never published.
        for action in ("read", "list", "delete", "quickstart"):
            payload = publish(self.page)
            payload["tool_input"]["action"] = action
            self.assertIsNone(copy.page_of(payload), action)

    def test_an_asset_upload_is_not_a_page(self):
        payload = publish(self.page)
        payload["tool_input"]["asset"] = True
        self.assertIsNone(copy.page_of(payload))

    def test_a_file_that_is_not_a_page_is_left_alone(self):
        other = os.path.join(self.dir.name, "data.json")
        with open(other, "w", encoding="utf-8") as f:
            f.write("{}")
        self.assertIsNone(copy.page_of(publish(other)))

    def test_a_missing_file_is_silence_and_not_a_crash(self):
        # The hook runs after the tool, and a file the tool wrote may already
        # be gone. A publish is not worth failing over a copy.
        self.assertIsNone(copy.page_of(publish(os.path.join(self.dir.name, "nothing.html"))))

    def test_a_page_past_the_ceiling_is_left_to_its_link(self):
        with open(self.page, "w", encoding="utf-8") as f:
            f.write("x" * (copy.MAX_HTML + 1))
        self.assertIsNone(copy.page_of(publish(self.page)))

    def test_the_address_is_found_in_a_sentence_too(self):
        # The tool answers in prose as often as in fields, and the card offers
        # the link as a second way in.
        payload = publish(self.page, tool_response="Published roadmap.html at https://claude.ai/x/ab12cd34.")
        self.assertEqual(copy.page_of(payload)["url"], "https://claude.ai/x/ab12cd34")

    def test_without_an_address_the_copy_still_goes(self):
        payload = publish(self.page, tool_response={})
        self.assertEqual(copy.page_of(payload)["url"], "")


class OverTheSocket(unittest.TestCase):
    def test_the_copy_reaches_a_listener_and_reads_its_answer(self):
        here, there = socket.socketpair()
        seen = {}

        def collector():
            with there:
                chunks = []
                while True:
                    chunk = there.recv(64 * 1024)
                    if not chunk:
                        break
                    chunks.append(chunk)
                seen["got"] = json.loads(b"".join(chunks).decode("utf-8"))
                there.sendall(json.dumps({"ok": True, "id": "ab12cd34"}).encode("utf-8"))

        thread = threading.Thread(target=collector)
        thread.start()
        # The socket is already connected here, so the send is exercised
        # against a live peer without a path on disk.
        payload = {"session": "s", "html": "<p>hi"}
        here.sendall(json.dumps(payload).encode("utf-8"))
        here.shutdown(socket.SHUT_WR)
        answer = here.recv(64 * 1024)
        here.close()
        thread.join(5)
        self.assertEqual(seen["got"], payload)
        self.assertTrue(json.loads(answer.decode("utf-8"))["ok"])


class TheHookStaysQuiet(unittest.TestCase):
    def test_a_publish_with_no_panel_ends_well(self):
        # The hook runs on machines where the panel is not installed at all.
        page = tempfile.NamedTemporaryFile("w", suffix=".html", delete=False, encoding="utf-8")
        page.write("<p>hi")
        page.close()
        self.addCleanup(os.unlink, page.name)
        stdin = sys.stdin
        sys.stdin = io.StringIO(json.dumps(publish(page.name)))
        try:
            self.assertEqual(copy.main(["/run/nothing-here.sock"]), 0)
        finally:
            sys.stdin = stdin

    def test_nonsense_on_the_input_ends_well(self):
        stdin = sys.stdin
        sys.stdin = io.StringIO("not json")
        try:
            self.assertEqual(copy.main([]), 0)
        finally:
            sys.stdin = stdin


if __name__ == "__main__":
    unittest.main()
