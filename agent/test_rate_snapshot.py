import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import unittest

import test_barrier

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "rate-snapshot.sh"
CASES = json.loads((HERE.parent / "deploy/claude/testdata/statusline.json").read_text())


class RateSnapshot(unittest.TestCase):
    def test_the_shell_and_executor_keep_the_same_bytes(self):
        for case in CASES:
            with self.subTest(case["name"]), test_barrier.tmp_dir(prefix="rate-") as tmp:
                root = Path(tmp)
                now = int(time.time())
                expand = lambda text: text.replace("@NOW@", str(now))
                # Fix only the clock's second: mtime and find still see real
                # files, and the snapshot bytes can be compared without a race.
                bin_dir = root / "bin"
                bin_dir.mkdir()
                date = bin_dir / "date"
                date.write_text(f"#!/bin/sh\nprintf '%s\\n' {now}\n")
                date.chmod(0o755)
                env = {**os.environ, "CLAUDE_CONFIG_DIR": tmp,
                       "AACP_RATE_SNAPSHOT": "", "AACP_SESSION_MODELS": "",
                       "AACP_RATE_THROTTLE": "", "AACP_STATUSLINE_NEXT": "",
                       "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"],
                       **case.get("env", {})}
                for name, f in case.get("initial", {}).items():
                    path = root / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text(expand(f["text"]))
                    os.utime(path, (now - f["age"], now - f["age"]))
                out = subprocess.run(["bash", str(SCRIPT)], input=case["payload"],
                                     env=env, capture_output=True, text=True, timeout=20)
                self.assertEqual((out.returncode, out.stdout, out.stderr), (0, case["stdout"], ""))
                got = {str(p.relative_to(root)): p.read_text() for p in root.rglob("*")
                       if p.is_file() and not p.is_relative_to(bin_dir)}
                self.assertEqual(got, {name: expand(text) for name, text in case["want"].items()})
                for name in case.get("unchanged", []):
                    self.assertEqual((root / name).stat().st_mtime_ns,
                                     (now - case["initial"][name]["age"]) * 10**9)

    def test_without_jq_the_shell_still_calls_the_next_line(self):
        with test_barrier.tmp_dir(prefix="rate-") as tmp:
            root = Path(tmp)
            for tool in ("bash", "sh", "cat"):
                (root / tool).symlink_to(shutil.which(tool))
            env = {**os.environ, "PATH": tmp, "CLAUDE_CONFIG_DIR": tmp,
                   "AACP_RATE_SNAPSHOT": "", "AACP_SESSION_MODELS": "",
                   "AACP_STATUSLINE_NEXT": "cat"}
            out = subprocess.run(["bash", str(SCRIPT)], input=CASES[0]["payload"],
                                 env=env, capture_output=True, text=True, timeout=20)
            self.assertEqual((out.returncode, out.stdout, out.stderr),
                             (0, CASES[0]["payload"].rstrip("\n"), ""))
            self.assertFalse((root / "rate-limits.json").exists())
            self.assertFalse((root / "session-models").exists())


if __name__ == "__main__":
    unittest.main()
