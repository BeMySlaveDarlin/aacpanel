import json
import os
import pathlib
import unittest
from unittest.mock import patch

import test_barrier
import agent
import archive
import claude_bg
import ctx
from collect import live


class BackgroundSessions(unittest.TestCase):
    def setUp(self):
        self.tmp = test_barrier.tmp_dir(prefix="claude-bg-")
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        self.config = self.root / "account"
        self.sessions = self.config / "sessions"
        self.sessions.mkdir(parents=True)
        self.pid = 4242
        self.proc = self.root / "proc"
        proc = self.proc / str(self.pid)
        proc.mkdir(parents=True)
        (proc / "stat").write_text(f"{self.pid} (claude) S 1 " + "0 " * 17 + "123")
        (proc / "cmdline").write_bytes(b"claude\0bg-spare\0")
        self.proc_patch = patch.object(ctx, "PROC", str(self.proc))
        self.proc_patch.start()
        self.addCleanup(self.proc_patch.stop)
        self.sid = "11111111-1111-4111-8111-111111111111"
        self.file = self.sessions / f"{self.pid}.json"
        self.file.write_text(json.dumps({"kind": "bg", "pid": self.pid,
            "sessionId": self.sid, "cwd": str(self.root), "name": "lead",
            "procStart": ctx.proc_start(self.pid), "status": "idle"}))
        self.output = self.root / "agents.json"
        self.log = self.root / "calls"
        self.cli = self.root / "claude"
        self.cli.write_text('#!/bin/sh\nprintf "%s:%s\\n" "$CLAUDE_CONFIG_DIR" "$*" >> "' + str(self.log) + '"\ncat "' + str(self.output) + '"\n')
        self.cli.chmod(0o755)
        for obj, name, value in [(archive, "LIVE", str(self.sessions)),
                                 (agent, "CLAUDE_SESSIONS", str(self.sessions))]:
            old = getattr(obj, name)
            self.addCleanup(setattr, obj, name, old)
            setattr(obj, name, value)
        self.addCleanup(claude_bg._cache.clear)
        claude_bg._cache.clear()
        self.binary = patch.object(claude_bg, "program", lambda: str(self.cli))
        self.binary.start()
        self.addCleanup(self.binary.stop)

    def catalogue(self, state="blocked", status="idle", **extra):
        self.output.write_text(json.dumps([{"kind": "background", "id": "11111111",
            "pid": self.pid, "sessionId": self.sid, "status": status, "state": state, **extra}]))

    def test_background_session_reaches_the_snapshot_with_its_state_and_feed(self):
        self.catalogue()
        transcript = self.root / (self.sid + ".jsonl")
        transcript.write_text(json.dumps({"type": "assistant", "timestamp": "2026-01-01T00:00:00Z",
            "message": {"model": "claude-opus-5", "content": [{"type": "text", "text": "Work is done."}],
                        "usage": {"input_tokens": 12, "output_tokens": 3}}}) + "\n")
        with patch.object(ctx.chat, "claude_path", return_value=str(transcript)), \
             patch.object(ctx, "codex_live", return_value=[]), \
             patch.object(ctx, "codex_sessions", return_value=[]):
            found = ctx.live_sessions()
            self.assertEqual(len(found), 1)
            self.assertEqual(found[0]["transcript"], str(transcript))
            rows = live.sessions()["sessions"]
        self.assertIn(self.sid, [r["sessionId"] for r in rows])
        row = next(r for r in rows if r["sessionId"] == self.sid)
        self.assertEqual((row["transport"], row["status"]), ("bg", "waiting"))
        self.assertIn("input", row["waitingFor"])
        self.assertTrue(row["outside"])
        self.assertEqual(row["model"], "claude-opus-5")
        self.assertGreater(row["messages"], 0)
        self.assertEqual(self.log.read_text().strip(), str(self.config) + ":agents --json")

    def test_working_busy_and_idle_map_to_panel_states(self):
        for state, status, expected in [("working", "idle", "busy"), ("", "busy", "busy"), ("", "idle", "idle")]:
            with self.subTest(state=state, status=status):
                self.catalogue(state, status)
                claude_bg._cache.clear()
                self.assertEqual(claude_bg.row(ctx.live_sessions()[0])["status"], expected)

    def test_failed_cli_keeps_the_live_session_with_unknown_state(self):
        self.output.write_text("invalid JSON")
        row = claude_bg.row(ctx.live_sessions()[0])
        self.assertEqual((row["transport"], row["status"]), ("bg", "unknown"))
        self.assertNotIn("waitingFor", row)

    def test_cli_timeout_keeps_the_session_visible(self):
        self.cli.write_text("#!/bin/sh\nexec sleep 1\n")
        with patch.object(claude_bg, "TIMEOUT", 0.01):
            self.assertEqual(claude_bg.row(ctx.live_sessions()[0])["status"], "unknown")

    def test_catalogue_is_polled_once_per_contour_and_interval(self):
        self.catalogue()
        found = ctx.live_sessions()[0]
        claude_bg.row(found)
        claude_bg.row(found)
        self.assertEqual(len(self.log.read_text().splitlines()), 1)

    def test_reused_pid_cannot_lend_its_state_to_another_conversation(self):
        self.catalogue(sessionId="22222222-2222-4222-8222-222222222222")
        self.assertEqual(claude_bg.row(ctx.live_sessions()[0])["status"], "unknown")

    def test_a_print_run_with_strict_empty_mcp_is_not_a_background_session(self):
        data = json.loads(self.file.read_text())
        data["kind"] = "interactive"
        self.file.write_text(json.dumps(data))
        args = self.root / "proc" / str(self.pid)
        args.mkdir(parents=True, exist_ok=True)
        (args / "cmdline").write_bytes(b"claude\0-p\0voice\0--strict-mcp-config\0--mcp-config\0{}\0")
        with patch.object(ctx, "PROC", str(self.root / "proc")), \
             patch.object(ctx, "proc_start", return_value=data["procStart"]):
            self.assertEqual(ctx.live_sessions(), [])

