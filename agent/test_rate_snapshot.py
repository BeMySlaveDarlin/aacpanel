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
                "AACP_SESSION_MODELS": os.path.join(self.dir, "session-models"),
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
        full["AACP_SESSION_MODELS"] = os.path.join(self.dir, "session-models")
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


SESSION = "3f2b1c40-9e1d-4a7b-8c6e-5d4f3a2b1c0e"
OTHER_SESSION = "7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d"

SESSION_PAYLOAD = {
    "session_id": SESSION,
    "model": {"id": "claude-fable-5-1", "display_name": "Fable 5.1"},
    "effort": {"level": "xhigh"},
    "context_window": {"context_window_size": 1000000},
    "rate_limits": {
        "five_hour": {"used_percentage": 11, "resets_at": 1788190000},
        "seven_day": {"used_percentage": 62, "resets_at": 1788450000},
    },
}


def payload(**changes):
    data = json.loads(json.dumps(SESSION_PAYLOAD))
    for key, value in changes.items():
        if value is None:
            data.pop(key, None)
        else:
            data[key] = value
    return json.dumps(data)


class SessionModel(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_path(prefix="rate-")
        self.addCleanup(shutil.rmtree, self.dir, True)
        self.snap = os.path.join(self.dir, "rate-limits.json")
        self.models = os.path.join(self.dir, "session-models")

    def run_script(self, data=None, env=None):
        full = {**os.environ, "AACP_RATE_SNAPSHOT": self.snap,
                "AACP_SESSION_MODELS": self.models, "AACP_STATUSLINE_NEXT": ""}
        full.update(env or {})
        return subprocess.run(["bash", SCRIPT], input=payload() if data is None else data,
                              env=full, capture_output=True, text=True, timeout=20)

    def path(self, sid=SESSION):
        return os.path.join(self.models, sid + ".json")

    def read(self, sid=SESSION):
        with open(self.path(sid), encoding="utf-8") as f:
            return json.load(f)

    def test_the_model_and_the_effort_of_the_session_are_written(self):
        self.run_script()
        got = self.read()
        self.assertEqual(got["sessionId"], SESSION)
        self.assertEqual(got["model"], {"id": "claude-fable-5-1", "displayName": "Fable 5.1"})
        self.assertEqual(got["effort"], "xhigh")
        self.assertEqual(got["contextWindow"], 1000000)
        self.assertAlmostEqual(got["at"], int(time.time()), delta=30)

    def test_the_file_lands_in_the_directory_of_its_own_contour(self):
        contour = os.path.join(self.dir, "work")
        os.makedirs(contour)
        self.run_script(env={"CLAUDE_CONFIG_DIR": contour, "AACP_SESSION_MODELS": ""})
        self.assertTrue(os.path.isfile(os.path.join(contour, "session-models", SESSION + ".json")))
        self.assertFalse(os.path.exists(self.models))

    def test_a_payload_without_limits_still_carries_the_model(self):
        self.run_script(payload(rate_limits=None))
        self.assertEqual(self.read()["model"]["id"], "claude-fable-5-1")
        self.assertFalse(os.path.exists(self.snap))

    def test_an_unchanged_model_and_effort_do_not_rewrite_the_file(self):
        self.run_script()
        first = os.stat(self.path()).st_mtime_ns
        self.run_script()
        self.assertEqual(os.stat(self.path()).st_mtime_ns, first)

    def test_a_changed_effort_is_written_at_once(self):
        self.run_script()
        self.run_script(payload(effort={"level": "low"}))
        self.assertEqual(self.read()["effort"], "low")

    def test_a_changed_model_is_written_at_once(self):
        self.run_script()
        self.run_script(payload(model={"id": "claude-sonnet-5", "display_name": "Sonnet"}))
        self.assertEqual(self.read()["model"]["id"], "claude-sonnet-5")

    def test_a_model_without_effort_is_written_with_none(self):
        self.run_script(payload(effort=None))
        self.assertIsNone(self.read()["effort"])

    def test_an_effort_given_as_a_bare_string_is_taken_as_well(self):
        self.run_script(payload(effort="medium"))
        self.assertEqual(self.read()["effort"], "medium")

    def test_a_payload_without_a_session_writes_no_file(self):
        self.run_script(payload(session_id=None))
        self.assertFalse(os.path.exists(self.models))

    def test_a_session_id_that_is_not_a_uuid_writes_nothing(self):
        self.run_script(payload(session_id="../escape"))
        self.assertFalse(os.path.exists(os.path.join(self.dir, "escape.json")))
        self.assertFalse(os.path.exists(self.models))

    def test_without_jq_nothing_is_written_and_nothing_breaks(self):
        bin_dir = os.path.join(self.dir, "bin")
        os.makedirs(bin_dir)
        for tool in ("bash", "sh", "cat", "date", "stat", "mv", "rm", "mkdir", "find"):
            found = shutil.which(tool)
            if found:
                os.symlink(found, os.path.join(bin_dir, tool))
        out = self.run_script(env={"PATH": bin_dir})
        self.assertEqual(out.returncode, 0)
        self.assertEqual(out.stderr, "")
        self.assertFalse(os.path.exists(self.snap))
        self.assertFalse(os.path.exists(self.models))

    def test_a_broken_payload_writes_nothing(self):
        out = self.run_script("not json at all")
        self.assertEqual(out.returncode, 0)
        self.assertFalse(os.path.exists(self.models))

    def test_a_reader_never_sees_half_of_the_json(self):
        self.run_script()
        self.assertEqual([n for n in os.listdir(self.models) if ".tmp." in n], [])

    def test_files_of_sessions_silent_for_a_week_are_swept_on_a_write(self):
        os.makedirs(self.models)
        old = self.path(OTHER_SESSION)
        with open(old, "w", encoding="utf-8") as f:
            f.write("{}")
        stamp = time.time() - 8 * 86400
        os.utime(old, (stamp, stamp))
        self.run_script()
        self.assertFalse(os.path.exists(old))
        self.assertTrue(os.path.exists(self.path()))

    def test_a_fresh_file_of_another_session_survives_a_write(self):
        os.makedirs(self.models)
        other = self.path(OTHER_SESSION)
        with open(other, "w", encoding="utf-8") as f:
            f.write("{}")
        self.run_script()
        self.assertTrue(os.path.exists(other))

if __name__ == "__main__":
    unittest.main()
