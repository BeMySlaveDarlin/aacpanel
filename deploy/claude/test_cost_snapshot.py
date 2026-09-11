#!/usr/bin/env python3

import importlib.util
import json
import os
import pathlib
import sys
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


cost = load("cost-snapshot")


def transcript(rows):
    fh = tempfile.NamedTemporaryFile("w", suffix=".jsonl", delete=False, encoding="utf-8")
    with fh:
        for row in rows:
            fh.write(json.dumps(row) + "\n")
    return fh.name


def answer(mid, out=0, inp=0, cache_read=0, cache_write=0, side=False):
    return {
        "isSidechain": side,
        "message": {"id": mid, "usage": {
            "output_tokens": out, "input_tokens": inp,
            "cache_read_input_tokens": cache_read,
            "cache_creation_input_tokens": cache_write,
        }},
    }


class TestSumUsage(unittest.TestCase):
    def tearDown(self):
        for path in getattr(self, "files", []):
            os.unlink(path)

    def make(self, rows):
        path = transcript(rows)
        self.files = getattr(self, "files", []) + [path]
        return path

    def test_same_message_counted_once(self):
        sums = cost.sum_usage(self.make([
            answer("m1", out=100, inp=10),
            answer("m1", out=100, inp=10),
        ]))
        self.assertEqual(sums["main"]["output_tokens"], 100, "the duplicate was counted twice")
        self.assertEqual(sums["main"]["input_tokens"], 10)
        self.assertEqual(sums["main"]["messages"], 1)

    def test_different_messages_add_up(self):
        sums = cost.sum_usage(self.make([
            answer("m1", out=100),
            answer("m2", out=50),
            answer("m3", out=7),
        ]))
        self.assertEqual(sums["main"]["output_tokens"], 157)
        self.assertEqual(sums["main"]["messages"], 3)

    def test_subagents_counted_apart(self):
        sums = cost.sum_usage(self.make([
            answer("m1", out=100),
            answer("s1", out=400, side=True),
            answer("s2", out=50, side=True),
        ]))
        self.assertEqual(sums["main"]["output_tokens"], 100)
        self.assertEqual(sums["side"]["output_tokens"], 450)
        self.assertEqual(sums["side"]["messages"], 2)

    def test_cache_does_not_mix_with_input(self):
        sums = cost.sum_usage(self.make([
            answer("m1", inp=10, cache_read=5000, cache_write=50),
        ]))
        self.assertEqual(sums["main"]["input_tokens"], 10)
        self.assertEqual(sums["main"]["cache_read_input_tokens"], 5000)
        self.assertEqual(sums["main"]["cache_creation_input_tokens"], 50)

    def test_rows_without_usage_are_skipped(self):
        sums = cost.sum_usage(self.make([
            {"type": "user", "message": {"content": "hello"}},
            {"type": "system", "subtype": "compact"},
            answer("m1", out=10),
        ]))
        self.assertEqual(sums["main"]["messages"], 1)

    def test_broken_lines_do_not_stop_the_count(self):
        path = self.make([answer("m1", out=10)])
        with open(path, "a", encoding="utf-8") as f:
            f.write('{"message": {"id": "m2", "usage"')
        sums = cost.sum_usage(path)
        self.assertEqual(sums["main"]["output_tokens"], 10)

    def test_missing_file_is_not_a_crash(self):
        self.assertIsNone(cost.sum_usage("/nonexistent/transcript.jsonl"))


class TestHook(unittest.TestCase):

    def test_snapshot_lands_in_the_account_log(self):
        with tempfile.TemporaryDirectory() as config:
            path = transcript([answer("m1", out=120), answer("s1", out=400, side=True)])
            try:
                os.environ["CLAUDE_CONFIG_DIR"] = config
                payload = json.dumps({
                    "transcript_path": path,
                    "session_id": "abc",
                    "hook_event_name": "Stop",
                })
                sys.stdin = io_stub(payload)
                cost.hook()
            finally:
                sys.stdin = sys.__stdin__
                os.environ.pop("CLAUDE_CONFIG_DIR", None)
                os.unlink(path)

            log = pathlib.Path(config) / "logs" / "cost.jsonl"
            self.assertTrue(log.exists(), "the snapshot was not written")
            rec = json.loads(log.read_text(encoding="utf-8").strip())
            self.assertEqual(rec["session"], "abc")
            self.assertEqual(rec["event"], "Stop")
            self.assertEqual(rec["main"]["output_tokens"], 120)
            self.assertEqual(rec["side"]["output_tokens"], 400)

    def test_missing_transcript_writes_nothing(self):
        with tempfile.TemporaryDirectory() as config:
            try:
                os.environ["CLAUDE_CONFIG_DIR"] = config
                sys.stdin = io_stub(json.dumps({"transcript_path": "/nope.jsonl"}))
                cost.hook()
            finally:
                sys.stdin = sys.__stdin__
                os.environ.pop("CLAUDE_CONFIG_DIR", None)
            self.assertFalse((pathlib.Path(config) / "logs").exists())


def io_stub(text):
    import io
    return io.StringIO(text)


if __name__ == "__main__":
    unittest.main()
