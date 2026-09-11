import json
import os
import shutil
import subprocess
import time
import unittest

import test_barrier  # noqa: F401

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "rate-snapshot.sh")

PAYLOAD = json.dumps({
    "model": {"display_name": "Opus"},
    "rate_limits": {
        "five_hour": {"used_percentage": 11, "resets_at": 1788190000},
        "seven_day": {"used_percentage": 62, "resets_at": 1788450000},
    },
})


class RateSnapshot(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_path(prefix="rate-")
        self.addCleanup(shutil.rmtree, self.dir, True)
        self.snap = os.path.join(self.dir, "rate-limits.json")

    def run_script(self, payload=PAYLOAD, env=None, snap=None):
        full = {**os.environ,
                "AACP_RATE_SNAPSHOT": snap if snap is not None else self.snap,
                "AACP_STATUSLINE_NEXT": ""}
        full.update(env or {})
        return subprocess.run(["bash", SCRIPT], input=payload, env=full,
                              capture_output=True, text=True, timeout=20)

    def read(self):
        with open(self.snap, encoding="utf-8") as f:
            return json.load(f)

    def test_the_snapshot_is_written_from_the_payload(self):
        self.run_script()
        got = self.read()
        self.assertEqual(got["fiveHour"], {"pct": 11, "resetsAt": 1788190000})
        self.assertEqual(got["sevenDay"], {"pct": 62, "resetsAt": 1788450000})
        self.assertAlmostEqual(got["at"], int(time.time()), delta=30)

    def test_the_snapshot_lands_in_the_directory_of_its_own_contour(self):
        contour = os.path.join(self.dir, "work")
        os.makedirs(contour)
        self.run_script(env={"CLAUDE_CONFIG_DIR": contour}, snap="")
        self.assertTrue(os.path.isfile(os.path.join(contour, "rate-limits.json")))
        self.assertFalse(os.path.exists(self.snap))

    def test_a_fresh_snapshot_is_not_rewritten(self):
        self.run_script()
        first = os.stat(self.snap).st_mtime_ns
        self.run_script()
        self.assertEqual(os.stat(self.snap).st_mtime_ns, first)

    def test_a_payload_without_limits_does_not_wipe_the_previous_one(self):
        self.run_script()
        before = self.read()
        self.run_script(payload=json.dumps({"model": {"display_name": "Opus"}}))
        self.assertEqual(self.read(), before)

    def test_a_payload_without_limits_creates_no_file(self):
        self.run_script(payload="{}")
        self.assertFalse(os.path.exists(self.snap))

    def test_a_broken_payload_does_not_break_the_script(self):
        out = self.run_script(payload="not json at all")
        self.assertEqual(out.returncode, 0)
        self.assertFalse(os.path.exists(self.snap))

    def test_the_payload_goes_further_down_the_chain(self):
        nxt = os.path.join(self.dir, "next.sh")
        seen = os.path.join(self.dir, "seen.json")
        with open(nxt, "w", encoding="utf-8") as f:
            f.write("#!/bin/sh\ncat > %s\necho 'STATUS LINE'\n" % seen)
        os.chmod(nxt, 0o755)

        out = self.run_script(env={"AACP_STATUSLINE_NEXT": nxt})
        self.assertEqual(out.stdout.strip(), "STATUS LINE")
        with open(seen, encoding="utf-8") as f:
            self.assertEqual(json.load(f)["rate_limits"]["five_hour"]["used_percentage"], 11)

    def test_a_failing_external_statusline_does_not_cancel_the_snapshot(self):
        nxt = os.path.join(self.dir, "broken.sh")
        with open(nxt, "w", encoding="utf-8") as f:
            f.write("#!/bin/sh\nexit 3\n")
        os.chmod(nxt, 0o755)

        self.run_script(env={"AACP_STATUSLINE_NEXT": nxt})
        self.assertEqual(self.read()["fiveHour"]["pct"], 11)

    def test_a_missing_external_script_is_not_an_error(self):
        out = self.run_script(env={"AACP_STATUSLINE_NEXT": os.path.join(self.dir, "no-such-file")})
        self.assertEqual(out.returncode, 0)
        self.assertEqual(self.read()["fiveHour"]["pct"], 11)

    def test_without_the_variable_the_chain_stays_silent(self):
        full = {k: v for k, v in os.environ.items() if k != "AACP_STATUSLINE_NEXT"}
        full["AACP_RATE_SNAPSHOT"] = self.snap
        out = subprocess.run(["bash", SCRIPT], input=PAYLOAD, env=full,
                             capture_output=True, text=True, timeout=20)
        self.assertEqual(out.returncode, 0)
        self.assertEqual(out.stderr, "")
        self.assertEqual(out.stdout, "")
        self.assertEqual(self.read()["fiveHour"]["pct"], 11)

    def test_a_reader_never_sees_half_of_the_json(self):
        self.run_script()
        leftovers = [n for n in os.listdir(self.dir) if ".tmp." in n]
        self.assertEqual(leftovers, [])


if __name__ == "__main__":
    unittest.main()
