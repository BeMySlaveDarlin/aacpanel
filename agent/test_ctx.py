import datetime as dt
import json
import os
import shutil
import subprocess
import sys
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import archive  # noqa: E402
import chat  # noqa: E402
import ctx  # noqa: E402
import models  # noqa: E402
import sesstate  # noqa: E402
from test_sesstate import spawn  # noqa: E402

def setUpModule():
    models.CACHE_PATH = None


UUID_A = "11111111-1111-4111-8111-111111111111"
UUID_B = "22222222-2222-4222-8222-222222222222"

REQUEST_AT = "2026-08-24T10:00:00Z"
REQUEST_EPOCH = int(dt.datetime(2026, 8, 24, 10, tzinfo=dt.timezone.utc).timestamp())


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def usage(tokens):
    return {"input_tokens": tokens, "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0}


class Row(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_path(prefix="ctx-row-")
        self.addCleanup(shutil.rmtree, self.dir, True)
        self.models = os.path.join(self.dir, "session-models")
        self.addCleanup(setattr, ctx, "SESSION_MODELS", ctx.SESSION_MODELS)
        ctx.SESSION_MODELS = self.models

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

    def test_the_totals_of_the_conversation_travel_in_the_row(self):
        path = self.write(UUID_A,
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "claude-opus-5",
                         "usage": {"input_tokens": 1_000, "cache_creation_input_tokens": 4_000,
                                   "cache_read_input_tokens": 95_000, "output_tokens": 800}}},
            {"type": "assistant", "timestamp": "2026-08-24T10:00:01Z",
             "message": {"model": "claude-opus-5",
                         "usage": {"input_tokens": 500, "cache_creation_input_tokens": 0,
                                   "cache_read_input_tokens": 100_000, "output_tokens": 1_200}}})
        row = ctx._row(self.live(path))
        self.assertEqual(row["tokensIn"], 200_500)
        self.assertEqual(row["tokensOut"], 2_000)

    def test_before_the_first_request_the_totals_are_zero(self):
        path = self.write(UUID_A, {"type": "user", "timestamp": "2026-08-24T10:00:00Z",
                                    "message": {"content": "hello"}})
        row = ctx._row(self.live(path))
        self.assertEqual(row["tokensIn"], 0)
        self.assertEqual(row["tokensOut"], 0)

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



class StatusLineSnapshot(Row):
    """What the status line saw for the session against what the transcript says."""

    def snapshot(self, at, model="claude-haiku-4-5", effort="xhigh", sid=UUID_A, raw=None):
        os.makedirs(self.models, exist_ok=True)
        with open(os.path.join(self.models, sid + ".json"), "w", encoding="utf-8") as f:
            if raw is not None:
                f.write(raw)
            else:
                json.dump({"at": at, "sessionId": sid,
                           "model": {"id": model, "displayName": "Haiku"},
                           "effort": effort}, f)

    def transcript(self, effort="high"):
        return self.write(UUID_A,
            {"type": "assistant", "timestamp": REQUEST_AT, "effort": effort,
             "message": {"model": "claude-opus-5", "usage": usage(100_000)}})

    def test_a_snapshot_fresher_than_the_last_request_overrides_the_transcript(self):
        self.snapshot(at=REQUEST_EPOCH + 300)
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["model"], "claude-haiku-4-5")
        self.assertEqual(row["effort"], "xhigh")

    def test_the_limit_follows_the_model_of_the_snapshot(self):
        self.snapshot(at=REQUEST_EPOCH + 300)
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["limit"], 200_000)
        self.assertTrue(row["limitKnown"])
        self.assertEqual(row["pct"], 50.0)

    def test_a_snapshot_older_than_the_last_request_does_not_override(self):
        self.snapshot(at=REQUEST_EPOCH - 300)
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["model"], "claude-opus-5")
        self.assertEqual(row["effort"], "high")
        self.assertEqual(row["limit"], 1_000_000)

    def test_a_snapshot_of_the_same_second_as_the_request_does_not_override(self):
        self.snapshot(at=REQUEST_EPOCH)
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["model"], "claude-opus-5")

    def test_without_a_snapshot_the_transcript_stays(self):
        self.snapshot(at=REQUEST_EPOCH + 300, sid=UUID_B)
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["model"], "claude-opus-5")
        self.assertEqual(row["effort"], "high")

    def test_before_the_first_request_the_snapshot_names_the_model(self):
        path = self.write(UUID_A, {"type": "user", "timestamp": REQUEST_AT,
                                    "message": {"content": "hello"}})
        self.snapshot(at=REQUEST_EPOCH - 3600)
        row = ctx._row(self.live(path))
        self.assertTrue(row["noRequests"])
        self.assertEqual(row["model"], "claude-haiku-4-5")
        self.assertEqual(row["effort"], "xhigh")
        self.assertEqual(row["limit"], 200_000)

    def test_a_fresh_snapshot_without_effort_clears_the_effort(self):
        self.snapshot(at=REQUEST_EPOCH + 300, effort=None)
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["effort"], "")

    def test_a_snapshot_without_a_model_keeps_the_model_of_the_transcript(self):
        self.snapshot(at=REQUEST_EPOCH + 300, model="")
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["model"], "claude-opus-5")

    def test_a_broken_snapshot_is_ignored(self):
        self.snapshot(at=0, raw="{half of")
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["model"], "claude-opus-5")

    def test_a_snapshot_without_a_time_is_ignored(self):
        self.snapshot(at=0, raw=json.dumps({"model": {"id": "claude-haiku-4-5"}}))
        row = ctx._row(self.live(self.transcript()))
        self.assertEqual(row["model"], "claude-opus-5")

GAP = 2.1


def stamp(ts):
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(ts))


class Birth(unittest.TestCase):
    """When a process was born.

    The pair is started apart and looked at in /proc only here, in the tests:
    procfs stamps the directory of a process when it builds the inode, at the
    first look, so an elder nobody has looked at passes for a newborn. A birth
    counted that late takes away from the session everything it began earlier.
    """

    @classmethod
    def setUpClass(cls):
        cls.procs = []
        cls.elder_at = time.time()
        cls.elder = cls.spawn()
        time.sleep(GAP)
        cls.younger = cls.spawn()

    @classmethod
    def spawn(cls):
        proc = subprocess.Popen(["sleep", "60"])
        cls.procs.append(proc)
        return proc.pid

    @classmethod
    def tearDownClass(cls):
        for proc in cls.procs:
            proc.terminate()
            proc.wait()

    def setUp(self):
        self.dir = test_barrier.tmp_path(prefix="ctx-birth-")
        self.addCleanup(shutil.rmtree, self.dir, True)

    def test_the_birth_is_the_moment_the_process_started(self):
        born = ctx.started_at(self.elder)
        self.assertIsNotNone(born)
        self.assertAlmostEqual(born, self.elder_at, delta=1.5)

    def test_the_elder_of_two_is_older_although_both_are_looked_at_at_once(self):
        gap = ctx.started_at(self.younger) - ctx.started_at(self.elder)
        self.assertAlmostEqual(gap, GAP, delta=1.0,
                               msg="the two are of one age: the birth came from the look, not the start")

    def test_an_agent_spawned_after_the_birth_of_a_process_looked_at_late_stays_active(self):
        path = os.path.join(self.dir, "agents.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(spawn("toolu_1", "alpha", at=stamp(self.elder_at + 1)))
        got = sesstate.read(path, born=ctx.started_at(self.elder)).snapshot()
        self.assertEqual([(a["name"], a["status"]) for a in got["agents"]], [("alpha", "active")])

    def test_a_process_that_is_gone_has_no_birth(self):
        proc = subprocess.Popen(["true"])
        proc.wait()
        self.assertIsNone(ctx.started_at(proc.pid))

    def test_without_a_boot_time_the_birth_is_unknown(self):
        self.addCleanup(setattr, ctx, "_boot_time", ctx._boot_time)
        ctx._boot_time = lambda: None
        self.assertIsNone(ctx.started_at(os.getpid()))


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
        ctx._oneshot = lambda pid, sid=None: True
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


class Lineage(unittest.TestCase):
    """What a session's process was started under, read up the chain of parents."""

    def table(self, rows):
        # rows: pid -> (parent, comm) or (parent, comm, arguments)
        def args(pid):
            row = rows.get(pid, (None, ""))
            return list(row[2]) if len(row) > 2 else [row[1]]
        for name, fake in (("parent_pid", lambda pid: rows.get(pid, (None, ""))[0]),
                           ("_comm", lambda pid: rows.get(pid, (None, ""))[1]),
                           ("proc_args", args)):
            self.addCleanup(setattr, ctx, name, getattr(ctx, name))
            setattr(ctx, name, fake)

    def under(self, *server):
        """A claude in a shell of a pane of the server of tmux started with these arguments."""
        self.table({500: (60, "claude"), 60: (40, "bash"),
                    40: (1, "tmux: server", ("tmux",) + server + ("new-session", "-d", "-s", "work"))})
        return ctx.lineage(500, {})

    def test_a_console_on_the_users_own_server_is_not_outside(self):
        self.assertEqual(self.under(), {})
        self.assertEqual(self.under("-u", "-f", "/dev/null"), {})
        self.assertEqual(self.under("-L", "default"), {})

    def test_a_claude_typed_into_a_terminal_of_the_panel_is_not_outside(self):
        self.assertEqual(self.under("-L", ctx.PANEL_TMUX), {})
        self.assertEqual(self.under("-L" + ctx.PANEL_TMUX), {})

    def test_a_claude_under_a_server_of_tmux_of_its_own_is_outside_and_named_with_it(self):
        for server, named in ((("-L", "work"), "-L work"),
                              (("-2uLwork",), "-L work"),
                              (("-f", "-L", "-L", "x"), "-L x"),
                              (("-S", "/srv/sock"), "-S /srv/sock"),
                              (("-L", ctx.PANEL_TMUX, "-S", "/tmp/tmux-1000/" + ctx.PANEL_TMUX),
                               "-S /tmp/tmux-1000/" + ctx.PANEL_TMUX)):
            with self.subTest(server=server):
                self.assertEqual(self.under(*server), {"outside": True, "tmuxServer": named})

    def test_a_real_server_of_tmux_keeps_the_socket_its_start_named(self):
        tmux = shutil.which("tmux")
        if not tmux:
            self.skipTest("tmux is not installed: there is no server to read")
        # Short, since a unix socket path ends at 108 bytes, and the test's own:
        # the server is started and killed on a socket nothing else uses.
        home = test_barrier.tmp_path("tx")
        self.addCleanup(shutil.rmtree, home, True)
        label = "aacp-ctx-%d" % os.getpid()
        env = {k: v for k, v in os.environ.items() if k != "TMUX"}
        env["TMUX_TMPDIR"] = home

        def run(*args):
            return subprocess.run([tmux, "-L", label, "-f", "/dev/null", *args], env=env,
                                  capture_output=True, text=True, timeout=10, check=False)
        started = run("new-session", "-d", "sleep 60")
        self.addCleanup(run, "kill-server")
        self.assertEqual(started.returncode, 0, started.stderr)
        pane = int(run("list-panes", "-F", "#{pane_pid}").stdout.split()[0])
        self.assertEqual(ctx.lineage(pane, {}), {"outside": True, "tmuxServer": "-L " + label})

    def test_a_claude_started_by_a_run_inside_another_session_names_it(self):
        self.table({500: (400, "claude"), 400: (300, "python3"), 300: (200, "bash"),
                    200: (100, "claude"), 100: (1, "aacpanel-exec")})
        got = ctx.lineage(500, {200: {"session": "acme", "sessionId": UUID_A}})
        self.assertEqual(got, {"outside": True, "parent": {"session": "acme", "sessionId": UUID_A}})

    def test_a_console_of_the_panel_is_not_outside(self):
        self.table({500: (40, "claude"), 40: (1, "tmux: server")})
        self.assertEqual(ctx.lineage(500, {}), {})

    def environ(self, by_pid):
        """Fakes the environment of the processes: pid -> the variables the collector reads of it."""
        self.addCleanup(setattr, ctx, "_environ", ctx._environ)
        ctx._environ = lambda pid, names: {k: v for k, v in by_pid.get(pid, {}).items() if k in names}

    def test_a_session_of_the_panel_names_the_session_that_had_it_opened(self):
        self.environ({500: {ctx.PARENT_VAR: "lead"}})
        for where in ({500: (40, "claude"), 40: (1, "tmux: server")},
                      {500: (40, "claude"), 40: (1, "aacpanel-exec")}):
            with self.subTest(where=where[40][1]):
                self.table(where)
                self.assertEqual(ctx.lineage(500, {}), {"parent": {"session": "lead"}})

    def test_a_session_a_person_opened_has_no_parent(self):
        self.environ({500: {"HOME": "/home/u"}})
        self.table({500: (40, "claude"), 40: (1, "aacpanel-exec")})
        self.assertEqual(ctx.lineage(500, {}), {})

    def test_a_run_is_named_by_the_session_it_runs_in_whatever_its_environment_says(self):
        # A run inherits the environment of the session it runs inside the
        # work of: the variable there names the parent of that session, not of
        # the run.
        self.environ({500: {ctx.PARENT_VAR: "lead"}})
        self.table({500: (200, "claude"), 200: (100, "claude"), 100: (1, "aacpanel-exec")})
        got = ctx.lineage(500, {200: {"session": "child", "sessionId": UUID_A}})
        self.assertEqual(got, {"outside": True, "parent": {"session": "child", "sessionId": UUID_A}})

    def test_a_claude_out_of_reach_has_no_parent_from_its_environment(self):
        self.environ({500: {ctx.PARENT_VAR: "lead"}})
        self.assertEqual(self.under("-L", "work"), {"outside": True, "tmuxServer": "-L work"})

    def test_a_session_on_the_stream_is_not_outside(self):
        self.table({500: (40, "claude"), 40: (1, "aacpanel-exec")})
        self.assertEqual(ctx.lineage(500, {}), {})

    def test_a_claude_typed_into_a_terminal_is_outside_with_no_parent(self):
        self.table({500: (60, "claude"), 60: (50, "bash"), 50: (1, "konsole")})
        self.assertEqual(ctx.lineage(500, {}), {"outside": True})

    def test_the_nearest_session_up_the_chain_is_the_parent(self):
        self.table({500: (200, "claude"), 200: (100, "claude"), 100: (1, "tmux: server")})
        owners = {200: {"session": "inner", "sessionId": UUID_A}, 100: {"session": "outer", "sessionId": UUID_B}}
        self.assertEqual(ctx.lineage(500, owners)["parent"]["session"], "inner")

    def test_a_loop_in_the_chain_ends(self):
        self.table({500: (60, "claude"), 60: (70, "x"), 70: (60, "y")})
        self.assertEqual(ctx.lineage(500, {}), {"outside": True})

    def test_the_chain_of_real_processes_is_read(self):
        shell = subprocess.Popen(["bash", "-c", "sleep 60 & wait"])
        self.addCleanup(shell.wait)
        self.addCleanup(shell.terminate)
        child = None
        for _ in range(50):
            kids = [int(p) for p in os.listdir("/proc") if p.isdigit() and ctx.parent_pid(int(p)) == shell.pid]
            if kids:
                child = kids[0]
                break
            time.sleep(0.05)
        self.assertIsNotNone(child, "the shell did not start its child")
        self.addCleanup(lambda: subprocess.run(["kill", str(child)], check=False))
        got = ctx.lineage(child, {shell.pid: {"session": "outer", "sessionId": UUID_A}})
        self.assertEqual(got.get("parent", {}).get("session"), "outer")

    def test_the_snapshot_rows_carry_where_a_session_came_from(self):
        self.table({500: (200, "claude"), 200: (100, "claude"), 100: (1, "aacpanel-exec")})
        lives = [{"name": "acme", "sessionId": UUID_A, "cwd": "/x", "pid": 200, "transcript": "", "procStartedAt": None},
                 {"name": "acme-e8", "sessionId": UUID_B, "cwd": "/x", "pid": 500, "transcript": "", "procStartedAt": None}]
        for name, fake in (("live_sessions", lambda: [dict(x) for x in lives]),
                           ("_row", lambda live: {"session": live["name"], "sessionId": live["sessionId"], "pct": 0.0})):
            self.addCleanup(setattr, ctx, name, getattr(ctx, name))
            setattr(ctx, name, fake)
        rows = {r["session"]: r for r in ctx.sessions()["sessions"]}
        self.assertNotIn("outside", rows["acme"])
        self.assertEqual(rows["acme-e8"].get("parent"), {"session": "acme", "sessionId": UUID_A})
        self.assertTrue(rows["acme-e8"].get("outside"))


if __name__ == "__main__":
    unittest.main()
