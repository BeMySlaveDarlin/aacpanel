#!/usr/bin/env python3

import importlib.util
import json
import os
import pathlib
import tempfile
import time
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


stamp = load("prompt-stamp")

SNAPSHOT = {
    "at": 0,
    "host": {
        "cpuPct": 7.0,
        "mem": {"pct": 21.0},
        "disks": [
            {"mount": "/", "total": 900 * 1024 ** 3, "used": 200 * 1024 ** 3, "pct": 22.0},
            {"mount": "/boot/efi", "total": 1024 ** 3, "used": 0, "pct": 1.0},
        ],
    },
    "sessions": [
        {"sessionId": "mine", "tokens": 500_000, "limit": 1_000_000, "pct": 50.0, "limitKnown": True},
        {"sessionId": "other", "tokens": 900_000, "limit": 1_000_000, "pct": 90.0, "limitKnown": True},
    ],
    "limits": {
        "contours": [
            {"profile": "personal", "fiveHour": {"pct": 16}, "sevenDay": {"pct": 35}, "ageSec": 5},
            {"profile": "work", "fiveHour": {"pct": 80}, "sevenDay": {"pct": 90}, "ageSec": 5},
        ],
    },
}


def snapshot(**overrides):
    data = json.loads(json.dumps(SNAPSHOT))
    data["at"] = int(time.time())
    data.update(overrides)
    return data


class TestLines(unittest.TestCase):
    def test_context_belongs_to_this_session(self):
        line = stamp.line_context(snapshot(), "mine")
        self.assertIn("50%", line)
        self.assertIn("500k/1000k", line)
        self.assertIn("finalize from 800k", line)

        other = stamp.line_context(snapshot(), "other")
        self.assertIn("90%", other)

        self.assertIsNone(stamp.line_context(snapshot(), "unknown"),
                          "the session was not found, yet the line was drawn anyway")

    def test_unknown_model_window_is_said_aloud(self):
        data = snapshot()
        data["sessions"][0]["limitKnown"] = False
        self.assertIn("not exact", stamp.line_context(data, "mine"))

    def test_limits_follow_the_account(self):
        personal = stamp.line_limits(snapshot(), "/home/u/.claude")
        self.assertIn("5h 16%", personal)
        self.assertIn("week 35%", personal)

        work = stamp.line_limits(snapshot(), "/home/u/.claude-work")
        self.assertIn("5h 80%", work, "the percentages of somebody else's account are shown")

    def test_stale_limits_say_their_age(self):
        data = snapshot()
        data["limits"]["contours"][0]["ageSec"] = 3600
        self.assertIn("min old", stamp.line_limits(data, "/home/u/.claude"))

    def test_small_disks_are_not_listed(self):
        line = stamp.line_disks(snapshot())
        self.assertIn("/", line)
        self.assertNotIn("/boot/efi", line)

    def test_alarms_only_when_there_is_something_to_say(self):
        self.assertIsNone(stamp.line_alarms(snapshot()))

        data = snapshot()
        data["host"]["disks"][0]["pct"] = 95.0
        self.assertIn("DISK", stamp.line_alarms(data))

        data = snapshot()
        data["host"]["mem"]["pct"] = 93.0
        self.assertIn("MEMORY", stamp.line_alarms(data))


class TestStamp(unittest.TestCase):
    def test_missing_blocks_shrink_the_stamp_but_do_not_break_it(self):
        lines = stamp.stamp({"at": int(time.time())}, "mine", "/home/u/.claude", with_date=True)
        self.assertTrue(lines, "the stamp went silent entirely")
        self.assertTrue(lines[0].startswith("["), "no date — the one thing that does not depend on the snapshot")

    def test_no_snapshot_says_so(self):
        lines = stamp.stamp(None, "mine", "/home/u/.claude", with_date=True)
        self.assertTrue(any("unavailable" in line for line in lines))

    def test_agent_turn_has_no_date(self):
        lines = stamp.stamp(snapshot(), "mine", "/home/u/.claude", with_date=False)
        self.assertFalse(any(line.startswith("[") for line in lines))

    def test_stale_snapshot_is_admitted(self):
        data = snapshot()
        data["at"] = int(time.time()) - 3600
        lines = stamp.stamp(data, "mine", "/home/u/.claude", with_date=True)
        self.assertTrue(any("is the collector stopped" in line for line in lines))


class TestThrottle(unittest.TestCase):

    def test_first_call_speaks_then_falls_silent(self):
        with tempfile.TemporaryDirectory() as state:
            self.assertTrue(stamp.throttle(state, "sid", 50, None), "the first call said nothing")
            self.assertFalse(stamp.throttle(state, "sid", 50, None), "the second call spoke")

    def test_new_context_step_speaks_again(self):
        with tempfile.TemporaryDirectory() as state:
            stamp.throttle(state, "sid", 50, None)
            self.assertTrue(stamp.throttle(state, "sid", 61, None),
                            "crossing a step of the context went unnoticed")

    def test_new_alarm_speaks_again(self):
        with tempfile.TemporaryDirectory() as state:
            stamp.throttle(state, "sid", 50, None)
            self.assertTrue(stamp.throttle(state, "sid", 50, "DISK / 95%"),
                            "a fresh alarm went unnoticed")

    def test_sessions_do_not_share_the_throttle(self):
        with tempfile.TemporaryDirectory() as state:
            stamp.throttle(state, "one", 50, None)
            self.assertTrue(stamp.throttle(state, "two", 50, None))

    def test_unwritable_state_still_speaks(self):
        self.assertTrue(stamp.throttle("/nonexistent/dir", "sid", 50, None))


class TestReadState(unittest.TestCase):
    def test_broken_snapshot_is_not_a_crash(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as fh:
            fh.write("{ not json")
            path = fh.name
        try:
            self.assertIsNone(stamp.read_state(path))
        finally:
            os.unlink(path)

    def test_missing_snapshot_is_not_a_crash(self):
        self.assertIsNone(stamp.read_state("/nonexistent/state.json"))


if __name__ == "__main__":
    unittest.main()
