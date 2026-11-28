"""Recent subscription readings, kept across collector restarts."""
import json
import math
import os

LOOKBACK = 30 * 60
RETENTION = 60 * 60
MIN_SPAN = 10 * 60
MIN_SAMPLES = 3


def _number(value):
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        return False
    try:
        return math.isfinite(value)
    except OverflowError:
        return False


class History:
    def __init__(self, path, now):
        self.path = path
        self.now = now
        self.windows = {}
        try:
            with open(path, encoding="utf-8") as f:
                rows = json.load(f)
        except (OSError, ValueError):
            return
        if not isinstance(rows, list):
            return
        for row in rows:
            if not isinstance(row, dict):
                continue
            key, reset, samples = row.get("key"), row.get("resetsAt"), row.get("samples")
            if (not isinstance(key, list) or len(key) != 3 or not all(isinstance(k, str) for k in key)
                    or not _number(reset) or reset <= now or not isinstance(samples, list)):
                continue
            kept = []
            for sample in samples:
                if (not isinstance(sample, list) or len(sample) != 2
                        or not all(_number(v) for v in sample) or sample[1] < 0):
                    kept = []
                    break
                at, pct = sample
                if kept and (at <= kept[-1][0] or pct < kept[-1][1]):
                    kept = []
                    break
                if now - RETENTION <= at <= now:
                    kept.append(sample)
            if kept:
                self.windows[tuple(key)] = {"resetsAt": reset, "samples": kept}

    def window(self, key, window, at):
        if not isinstance(window, dict):
            return
        window.pop("forecast", None)
        pct, reset = window.get("pct"), window.get("resetsAt")
        if not _number(pct) or pct < 0 or not _number(reset) or reset <= self.now:
            self.windows.pop(key, None)
            return
        if not _number(at) or not self.now - RETENTION <= at <= self.now:
            return
        entry = self.windows.get(key)
        if entry is None or entry["resetsAt"] != reset or pct < entry["samples"][-1][1]:
            entry = {"resetsAt": reset, "samples": []}
            self.windows[key] = entry
        samples = entry["samples"]
        # Re-reading a snapshot is not a new observation of the account.
        if samples and at < samples[-1][0]:
            return
        if samples and at == samples[-1][0]:
            samples[-1] = [at, pct]
        else:
            samples.append([at, pct])
        recent = [sample for sample in samples if sample[0] >= self.now - LOOKBACK]
        if len(recent) < MIN_SAMPLES or recent[-1][0] - recent[0][0] < MIN_SPAN:
            return
        pace = (recent[-1][1] - recent[0][1]) * 3600 / (recent[-1][0] - recent[0][0])
        projected = pct + pace * (reset - self.now) / 3600
        forecast = {"pace": pace, "pct": projected}
        if pace > 0 and pct < 100:
            runs_out = self.now + (100 - pct) * 3600 / pace
            if runs_out < reset:
                forecast["runsOutAt"] = int(runs_out)
        forecast["tone"] = ("out" if pct >= 100 or "runsOutAt" in forecast
                            else "tight" if projected >= 95 else "fine")
        window["forecast"] = forecast

    def save(self):
        os.makedirs(os.path.dirname(self.path), exist_ok=True)
        tmp = self.path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump([{"key": key, **entry} for key, entry in self.windows.items()], f)
        os.replace(tmp, self.path)
