import json
import os
import shutil
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import archive  # noqa: E402
import chat  # noqa: E402
import ctx  # noqa: E402
import models  # noqa: E402

def setUpModule():
    models.CACHE_PATH = None


UUID_A = "11111111-1111-4111-8111-111111111111"
UUID_B = "22222222-2222-4222-8222-222222222222"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def usage(tokens):
    return {"input_tokens": tokens, "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0}


class Row(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_path(prefix="ctx-row-")
        self.addCleanup(shutil.rmtree, self.dir, True)

    def write(self, uuid, *records):
        path = os.path.join(self.dir, f"{uuid}.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            for record in records:
                f.write(line(record))
        return path

    def live(self, transcript, name="aacpanel", sid=UUID_A, cwd="/opt/x"):
        return {"name": name, "sessionId": sid, "cwd": cwd,
                "transcript": transcript, "procStartedAt": None}

    def test_a_known_model_takes_its_limit_from_the_table(self):
        path = self.write(UUID_A,
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(300_000)}})
        row = ctx._row(self.live(path))
        self.assertEqual(row["limit"], 1_000_000)
        self.assertTrue(row["limitKnown"])
        self.assertEqual(row["tokens"], 300_000)
        self.assertEqual(row["pct"], 30.0)

    def test_an_unknown_model_gets_a_guessed_million(self):
        path = self.write(UUID_A,
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "unknown-model-2099", "usage": usage(198_000)}})
        row = ctx._row(self.live(path))
        self.assertEqual(row["limit"], 1_000_000)
        self.assertFalse(row["limitKnown"])
        self.assertLess(row["pct"], 99.2,
                        "with a guessed million the percent must not look like an imminent compaction")

    def test_there_have_been_no_requests_yet(self):
        path = self.write(UUID_A, {"type": "user", "timestamp": "2026-08-24T10:00:00Z",
                                    "message": {"content": "hello"}})
        row = ctx._row(self.live(path))
        self.assertTrue(row["noRequests"])
        self.assertEqual(row["tokens"], 0)

    def test_the_transcript_path_travels_in_the_row(self):
        path = self.write(UUID_A, {"type": "assistant", "timestamp": "2026-09-01T10:00:00Z",
                                   "message": {"model": "claude-opus-5", "usage": usage(1000)}})
        self.assertEqual(ctx._row(self.live(path))["transcript"], path)

    def test_without_a_transcript_the_session_is_not_lost(self):
        row = ctx._row(self.live(None))
        self.assertTrue(row["noRequests"])
        self.assertEqual(row["sessionId"], UUID_A)

    def test_a_compaction_with_no_request_after_it_is_marked_stale(self):
        path = self.write(UUID_A,
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(900_000)}},
            {"type": "user", "timestamp": "2026-08-24T10:01:00Z", "isCompactSummary": True,
             "message": {"content": "summary"}})
        row = ctx._row(self.live(path))
        self.assertTrue(row["stale"])

    def test_a_context_over_the_limit_drops_the_known_limit_mark(self):
        path = self.write(UUID_A,
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(1_200_000)}})
        row = ctx._row(self.live(path))
        self.assertFalse(row["limitKnown"])
        self.assertEqual(row["pct"], 120.0)


    def test_the_mode_travels_with_its_own_time(self):
        path = self.write(
            UUID_A,
            {"type": "permission-mode", "permissionMode": "acceptEdits"},
            {"type": "assistant", "timestamp": "2026-09-01T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(1000)}},
        )
        row = ctx._row(self.live(path))
        self.assertEqual(row["mode"], "acceptEdits")
        self.assertEqual(row["modeAt"], "2026-09-01T10:00:00Z")

    def test_without_a_mode_there_is_no_field_at_all(self):
        path = self.write(
            UUID_A,
            {"type": "assistant", "timestamp": "2026-09-01T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(1000)}},
        )
        row = ctx._row(self.live(path))
        self.assertNotIn("mode", row)
        self.assertNotIn("modeAt", row)



class LiveSessions(unittest.TestCase):
    def setUp(self):
        self.sessions_dir = test_barrier.tmp_path(prefix="ctx-sessions-")
        self.addCleanup(shutil.rmtree, self.sessions_dir, True)
        self.old_live = archive.LIVE
        archive.LIVE = self.sessions_dir
        self.addCleanup(setattr, archive, "LIVE", self.old_live)

        self.projects_dir = test_barrier.tmp_path(prefix="ctx-projects-")
        self.addCleanup(shutil.rmtree, self.projects_dir, True)
        self.old_projects = chat.PROJECTS_DIR
        chat.PROJECTS_DIR = self.projects_dir
        self.addCleanup(setattr, chat, "PROJECTS_DIR", self.old_projects)

        self.proc = subprocess.Popen(["sleep", "60"])
        self.addCleanup(self.proc.wait)
        self.addCleanup(self.proc.terminate)
        self.pid = self.proc.pid
        self.start = ctx.proc_start(self.pid)

    def put(self, name, sid, pid=None, start=None, cwd="/opt/x", **extra):
        pid = self.pid if pid is None else pid
        data = {"pid": pid, "sessionId": sid, "cwd": cwd, "name": name,
                "procStart": self.start if start is None else start}
        data.update(extra)
        with open(os.path.join(self.sessions_dir, f"{pid}.json"), "w", encoding="utf-8") as f:
            json.dump(data, f)

    def test_a_live_session_gets_into_the_list(self):
        self.put("aacpanel", UUID_A)
        got = ctx.live_sessions()
        self.assertEqual([s["sessionId"] for s in got], [UUID_A])

    def test_a_dead_file_does_not_count_as_live(self):
        self.put("aacpanel", UUID_B, start="1")
        self.assertEqual(ctx.live_sessions(), [])

    def test_a_file_without_a_pid_or_a_uuid_is_skipped(self):
        with open(os.path.join(self.sessions_dir, "no-pid.json"), "w", encoding="utf-8") as f:
            json.dump({"cwd": "/opt/x", "name": "aacpanel"}, f)
        self.assertEqual(ctx.live_sessions(), [])

    def test_a_one_shot_run_does_not_count_as_a_session(self):
        self.put("tmp-8b", UUID_B)
        old = ctx._oneshot
        ctx._oneshot = lambda pid: True
        self.addCleanup(setattr, ctx, "_oneshot", old)
        self.assertEqual(ctx.live_sessions(), [])

    def test_a_background_process_of_the_daemon_does_not_count_as_a_session(self):
        self.put("demo", UUID_B, kind="bg")
        self.assertEqual(ctx.live_sessions(), [])

    def test_an_interactive_session_with_a_kind_stays(self):
        self.put("aacpanel", UUID_A, kind="interactive")
        self.assertEqual([s["sessionId"] for s in ctx.live_sessions()], [UUID_A])

    def test_the_name_comes_from_the_directory_when_it_is_not_named(self):
        self.put("", UUID_A, cwd="/srv/proj/aacpanel")
        got = ctx.live_sessions()
        self.assertEqual(got[0]["name"], "aacpanel")


if __name__ == "__main__":
    unittest.main()
