import importlib
import io
import json
import os
import shutil
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import agent  # noqa: E402
import contours  # noqa: E402
from collect import limits as collected  # noqa: E402

NOW = 2_000_000_000


class Forecast(unittest.TestCase):
    def setUp(self):
        self.root = test_barrier.tmp_path(prefix="limit-forecast-")
        self.addCleanup(shutil.rmtree, self.root, True)
        self.state = os.path.join(self.root, "state")
        self.history = os.path.join(self.state, "limits-history.json")
        self.home = os.path.join(self.root, "personal")
        self.work = os.path.join(self.root, "work")
        os.makedirs(self.home)
        os.makedirs(self.work)
        self.registry = os.path.join(self.root, "registry.conf")
        with open(self.registry, "w", encoding="utf-8") as f:
            f.write(f"work | /srv/work/ | {self.work} | -\n")
        self.codex_dir = os.path.join(self.root, "run", "aacpanel-stream", "codex-limits")
        os.makedirs(self.codex_dir)
        for target, name, value in ((contours, "HOME", self.home), (contours, "REGISTRY", self.registry),
                                    (agent, "LIMITS_PATH", None)):
            self.enterContext(patch.object(target, name, value))
        self.enterContext(patch.dict(os.environ, {"AACP_STATE_DIR": self.state,
                                                 "XDG_RUNTIME_DIR": os.path.join(self.root, "run")}))

    def claude(self, at, pct, reset=NOW + 3600, seven=None, home=None):
        data = {"at": at, "fiveHour": {"pct": pct, "resetsAt": reset}}
        if seven is not None:
            data["sevenDay"] = {"pct": seven, "resetsAt": reset}
        with open(os.path.join(home or self.home, "rate-limits.json"), "w", encoding="utf-8") as f:
            json.dump(data, f)

    def codex(self, at, five, seven, contour="personal", reset=NOW + 3600):
        with open(os.path.join(self.codex_dir, contour + ".json"), "w", encoding="utf-8") as f:
            json.dump({"at": at, "contour": contour,
                       "primary": {"usedPercent": five, "windowDurationMins": 300, "resetsAt": reset},
                       "secondary": {"usedPercent": seven, "windowDurationMins": 10080, "resetsAt": reset}}, f)

    def collect(self, now=NOW):
        with patch.object(collected.time, "time", return_value=now):
            return collected.limits()

    def readings(self, samples, reset=NOW + 3600):
        for at, pct in samples:
            self.claude(at, pct, reset)
            got = self.collect(at)
        return got["fiveHour"]

    def test_pace_projects_out_tight_and_fine(self):
        for start, end, tone, projected in ((20, 40, "out", 160),
                                            (60, 65, "tight", 95), (20, 25, "fine", 55)):
            with self.subTest(tone=tone):
                window = self.readings([(NOW - 600, start), (NOW - 300, (start + end) / 2), (NOW, end)])
                forecast = window["forecast"]
                self.assertEqual(forecast["pace"], (end - start) * 6)
                self.assertEqual(forecast["pct"], projected)
                self.assertEqual(forecast["tone"], tone)
                if tone == "out":
                    self.assertEqual(forecast["runsOutAt"], NOW + 1800)
                else:
                    self.assertNotIn("runsOutAt", forecast)

    def test_running_out_exactly_at_reset_is_tight(self):
        window = self.readings([(NOW - 1200, 20), (NOW - 600, 30), (NOW, 40)])
        self.assertEqual(window["forecast"], {"pace": 60, "pct": 100, "tone": "tight"})

    def test_an_exhausted_window_is_out_even_without_pace(self):
        for pct in (100, 102):
            with self.subTest(pct=pct):
                window = self.readings([(NOW - 1200, pct), (NOW - 600, pct), (NOW, pct)])
                self.assertEqual(window["forecast"], {"pace": 0, "pct": pct, "tone": "out"})

    def test_fresh_readings_with_the_same_percentage_have_zero_pace(self):
        window = self.readings([(NOW - 1200, 20), (NOW - 600, 20), (NOW, 20)])
        self.assertEqual(window["forecast"], {"pace": 0, "pct": 20, "tone": "fine"})

    def test_three_distinct_snapshots_and_ten_minutes_are_required(self):
        cases = ([(NOW - 600, 10), (NOW, 20)],
                 [(NOW - 599, 10), (NOW - 300, 15), (NOW, 20)])
        for samples in cases:
            with self.subTest(samples=samples):
                shutil.rmtree(self.state, ignore_errors=True)
                self.assertNotIn("forecast", self.readings(samples))

    def test_rereading_one_snapshot_does_not_build_history(self):
        self.claude(NOW - 1200, 20)
        for now in (NOW - 1200, NOW - 600, NOW):
            self.assertNotIn("forecast", self.collect(now)["fiveHour"])

    def test_only_the_last_thirty_minutes_set_the_pace(self):
        window = self.readings([(NOW - 2400, 0), (NOW - 1800, 10), (NOW - 600, 20), (NOW, 40)])
        self.assertEqual(window["forecast"], {"pace": 60, "pct": 100, "tone": "tight"})

    def test_old_snapshots_cannot_keep_a_forecast_alive(self):
        self.readings([(NOW - 1200, 10), (NOW - 600, 15), (NOW, 20)], reset=NOW + 7200)
        self.assertNotIn("forecast", self.collect(NOW + 1801)["fiveHour"])

    def test_a_changed_reset_starts_a_new_history_even_when_usage_increases(self):
        self.readings([(NOW - 1200, 10), (NOW - 600, 20), (NOW, 30)])
        self.claude(NOW + 60, 40, reset=NOW + 7200)
        self.assertNotIn("forecast", self.collect(NOW + 60)["fiveHour"])
        with open(self.history, encoding="utf-8") as f:
            self.assertEqual(json.load(f)[0]["samples"], [[NOW + 60, 40]])

    def test_a_lower_percentage_starts_a_new_history_even_with_the_same_reset(self):
        self.readings([(NOW - 1200, 10), (NOW - 600, 20), (NOW, 30)])
        self.claude(NOW + 60, 15)
        self.assertNotIn("forecast", self.collect(NOW + 60)["fiveHour"])
        with open(self.history, encoding="utf-8") as f:
            self.assertEqual(json.load(f)[0]["samples"], [[NOW + 60, 15]])

    def test_history_survives_a_collector_restart(self):
        self.readings([(NOW - 1200, 10), (NOW - 600, 20)])
        importlib.reload(collected)
        self.claude(NOW, 30)
        self.assertEqual(self.collect()["fiveHour"]["forecast"],
                         {"pace": 60, "pct": 90, "tone": "fine"})

    def test_corrupt_history_does_not_break_collection(self):
        broken = (b"{", b"\xff", b"{}", b"[null]",
                  b'[{"key": ["personal", "claude", "fiveHour"], "resetsAt": 2000003600, "samples": [null]}]',
                  json.dumps([{"key": ["personal", "claude", "fiveHour"],
                               "resetsAt": 10 ** 400, "samples": [[NOW, 20]]}]).encode("utf-8"))
        os.makedirs(self.state)
        for content in broken:
            with self.subTest(content=content):
                with open(self.history, "wb") as f:
                    f.write(content)
                self.claude(NOW, 20)
                self.assertEqual(self.collect()["fiveHour"], {"pct": 20, "resetsAt": NOW + 3600})

    def test_the_saved_history_keeps_only_the_last_hour(self):
        self.readings([(NOW - 4200, 0), (NOW - 3600, 10), (NOW - 1800, 20), (NOW, 30)])
        with open(self.history, encoding="utf-8") as f:
            self.assertEqual(json.load(f)[0]["samples"],
                             [[NOW - 3600, 10], [NOW - 1800, 20], [NOW, 30]])

    def test_inactive_windows_are_pruned_from_the_file(self):
        self.readings([(NOW - 1200, 10), (NOW - 600, 20), (NOW, 30)], reset=NOW + 7200)
        os.unlink(os.path.join(self.home, "rate-limits.json"))
        self.assertIsNone(self.collect(NOW + 3601))
        with open(self.history, encoding="utf-8") as f:
            self.assertEqual(json.load(f), [])

    def test_failed_atomic_replacement_preserves_the_last_saved_history(self):
        self.readings([(NOW - 1200, 10), (NOW - 600, 20)])
        with open(self.history, encoding="utf-8") as f:
            before = f.read()
        self.claude(NOW, 30)
        with patch("collect.forecast.os.replace", side_effect=OSError("write refused")), \
                patch("sys.stderr", new_callable=io.StringIO) as stderr:
            self.assertEqual(self.collect()["fiveHour"]["forecast"]["pace"], 60)
            self.assertIn("could not write limit history", stderr.getvalue())
        with open(self.history, encoding="utf-8") as f:
            self.assertEqual(f.read(), before)

    def test_forecasts_are_independent_for_contours_providers_and_windows(self):
        for i, at in enumerate((NOW - 1200, NOW - 600, NOW)):
            self.claude(at, 10 + i * 10, seven=20 + i)
            self.claude(at, 10 + i, seven=30 + i * 2, home=self.work)
            self.codex(at, 40 + i * 5, 50 + i)
            self.codex(at, 20 + i * 2, 70 + i * 3, contour="work")
            got = self.collect(at)
        by = {row["profile"]: row for row in got["contours"]}
        for contour, claude_paces, codex_paces in (("personal", (60, 6), (30, 6)),
                                                   ("work", (6, 12), (12, 18))):
            row = by[contour]
            for key, pace in zip(("fiveHour", "sevenDay"), claude_paces):
                self.assertEqual(row[key]["forecast"]["pace"], pace)
            for key, window, pace in zip(("fiveHour", "sevenDay"), row["codex"]["windows"], codex_paces):
                self.assertEqual(window["forecast"]["pace"], pace)
                self.assertEqual(row["codex"][key]["forecast"], window["forecast"])
        self.assertEqual(got["fiveHour"]["forecast"], by["personal"]["fiveHour"]["forecast"])

    def test_codex_only_contours_also_get_forecasts(self):
        for i, at in enumerate((NOW - 1200, NOW - 600, NOW)):
            self.codex(at, 20 + i * 2, 30 + i, contour="work")
            got = self.collect(at)
        self.assertEqual(got["profile"], "work")
        self.assertEqual(got["codex"]["fiveHour"]["forecast"]["pace"], 12)

    def test_unknown_and_expired_windows_have_no_forecast(self):
        for reset in (None, NOW):
            with self.subTest(reset=reset):
                window = self.readings([(NOW - 1200, 10), (NOW - 600, 20), (NOW, 30)], reset=reset)
                self.assertNotIn("forecast", window)


if __name__ == "__main__":
    unittest.main()
