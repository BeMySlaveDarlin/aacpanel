#!/usr/bin/env python3

import importlib.util
import json
import os
import pathlib
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load():
    spec = importlib.util.spec_from_file_location("cross_profile_msg", HERE / "cross-profile-msg.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def session_file(config_dir, pid, name, cwd):
    d = config_dir / "sessions"
    d.mkdir(parents=True, exist_ok=True)
    (d / f"{pid}.json").write_text(json.dumps({"pid": pid, "name": name, "cwd": cwd, "status": "idle"}))


class Profiles(unittest.TestCase):
    def setUp(self):
        self.mod = load()
        self.tmp = pathlib.Path(tempfile.mkdtemp())
        self.env = dict(os.environ)
        for key in ("AACP_CLAUDE_HOME", "AACP_CLAUDE", "AACP_HOSTCFG", "AACP_STATE_DIR", "CLAUDE_CONFIG_DIR"):
            os.environ.pop(key, None)

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.env)

    def test_env_wins_over_host_env(self):
        host_env = self.tmp / "host.env"
        host_env.write_text("AACP_CLAUDE_HOME=/from/file\n")
        os.environ["AACP_CLAUDE_HOME"] = "/from/env"
        self.assertEqual([p for _, p in self.mod.profiles(host_env)], [pathlib.Path("/from/env")])

    def test_host_env_read_when_env_is_empty(self):
        host_env = self.tmp / "host.env"
        host_env.write_text("# comment\nAACP_CLAUDE_HOME=/a:/b\n")
        self.assertEqual([p for _, p in self.mod.profiles(host_env)], [pathlib.Path("/a"), pathlib.Path("/b")])

    def test_single_account_without_any_setting(self):
        host_env = self.tmp / "missing.env"
        got = self.mod.profiles(host_env)
        self.assertEqual(got, [("personal", pathlib.Path.home() / ".claude")])

    def test_names_come_from_the_directory(self):
        os.environ["AACP_CLAUDE_HOME"] = str(pathlib.Path.home() / ".claude") + ":/home/u/.claude-work"
        names = [n for n, _ in self.mod.profiles(self.tmp / "none")]
        self.assertEqual(names, ["personal", "claude-work"])

    def test_claude_binary_from_setting_else_path(self):
        host_env = self.tmp / "host.env"
        host_env.write_text("AACP_CLAUDE=/opt/bin/claude-wrapper\n")
        self.assertEqual(self.mod.claude_bin(host_env), "/opt/bin/claude-wrapper")
        self.assertEqual(self.mod.claude_bin(self.tmp / "none"), "claude")


class Sessions(unittest.TestCase):
    def setUp(self):
        self.mod = load()
        self.tmp = pathlib.Path(tempfile.mkdtemp())
        self.env = dict(os.environ)
        os.environ.pop("CLAUDE_CONFIG_DIR", None)

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.env)

    def test_dead_pid_is_not_a_session(self):
        cfg = self.tmp / "acct"
        session_file(cfg, os.getpid(), "alive", "/srv/proj")
        session_file(cfg, 4194300, "dead", "/srv/proj")
        self.assertEqual([s["name"] for s in self.mod.sessions(cfg)], ["alive"])

    def test_find_by_name_across_accounts(self):
        a, b = self.tmp / "a", self.tmp / "b"
        session_file(a, os.getpid(), "site", "/srv/site")
        (b / "sessions").mkdir(parents=True)
        os.environ["AACP_CLAUDE_HOME"] = f"{a}:{b}"
        prof, s = self.mod.find("site")
        self.assertEqual((prof, s["cwd"]), ("a", "/srv/site"))

    def test_missing_and_ambiguous_names_refuse(self):
        a, b = self.tmp / "a", self.tmp / "b"
        session_file(a, os.getpid(), "twin", "/srv/a")
        session_file(b, os.getpid(), "twin", "/srv/b")
        os.environ["AACP_CLAUDE_HOME"] = f"{a}:{b}"
        with self.assertRaises(SystemExit) as stop:
            self.mod.find("nobody")
        self.assertEqual(stop.exception.code, 2)
        with self.assertRaises(SystemExit) as stop:
            self.mod.find("twin")
        self.assertEqual(stop.exception.code, 2)

    def test_prompt_keeps_the_text_between_markers(self):
        text = "line one\nline two"
        prompt = self.mod.prompt_for("site", text)
        self.assertIn('to="site"', prompt)
        self.assertIn("<<<MESSAGE START>>>\n" + text + "\n<<<MESSAGE END>>>", prompt)


if __name__ == "__main__":
    unittest.main()
