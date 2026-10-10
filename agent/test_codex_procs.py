import json
import os
import sqlite3
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import agent  # noqa: E402
import archive  # noqa: E402
import chat  # noqa: E402
import codex_archive  # noqa: E402
import ctx  # noqa: E402
import held  # noqa: E402
import notes  # noqa: E402
from chat import codex  # noqa: E402
from collect import live  # noqa: E402
from test_codex import THREAD, Runtime, put_env  # noqa: E402
from test_codex_archive import THREADS  # noqa: E402

# Pids no machine hands out, past the ceiling of the kernel.
CLAUDE_PID, SHELL_PID, RUN_PID, OTHER_PID = 5000100, 5000200, 5000300, 5000400

SESSION = "55555555-5555-4555-8555-555555555555"
GONE = "66666666-6666-4666-8666-666666666666"

RUN = "01a12600-0000-7000-8000-0000000000e1"
CHILD = "01a12601-0000-7000-8000-0000000000e2"
TUI = "01a12602-0000-7000-8000-0000000000e3"
CALL = "01a12603-0000-7000-8000-0000000000e4"

BOOT = 1791640000
TICKS = 500


class Procs(Runtime):
    """A tree laid out as /proc, the codex homes of the test and the files of its claude sessions."""

    def setUp(self):
        super().setUp()
        self.proc = os.path.join(self.root, "proc")
        os.makedirs(self.proc)
        with open(os.path.join(self.proc, "stat"), "w") as f:
            f.write(f"cpu 0 0 0 0\nbtime {BOOT}\n")
        self.addCleanup(setattr, ctx, "PROC", ctx.PROC)
        ctx.PROC = self.proc
        # ~/.codex is the personal home of the test, as the variable lists it.
        put_env(self, "HOME", self.root)
        put_env(self, "XDG_STATE_HOME", os.path.join(self.root, "state"))
        self.personal = os.path.join(self.root, ".codex")
        self.lives = os.path.join(self.root, "claude-sessions")
        os.makedirs(self.lives)
        self.addCleanup(setattr, archive, "LIVE", archive.LIVE)
        archive.LIVE = self.lives
        self.bin = os.path.join(self.personal, "packages", "standalone", "current", "bin", "codex")

    def process(self, pid, comm, args, env=None, ppid=1, files=(), cwd=None):
        d = os.path.join(self.proc, str(pid))
        os.makedirs(os.path.join(d, "fd"))
        with open(os.path.join(d, "comm"), "w") as f:
            f.write(comm + "\n")
        with open(os.path.join(d, "cmdline"), "wb") as f:
            f.write(b"\0".join(a.encode() for a in args) + b"\0")
        with open(os.path.join(d, "environ"), "wb") as f:
            f.write(b"".join(f"{k}={v}".encode() + b"\0" for k, v in (env or {}).items()))
        with open(os.path.join(d, "stat"), "w") as f:
            f.write(f"{pid} ({comm}) S {ppid} " + " ".join(["0"] * 17) + f" {TICKS} 0 0\n")
        for n, path in enumerate(files):
            os.symlink(path, os.path.join(d, "fd", str(n + 3)))
        if cwd:
            os.symlink(cwd, os.path.join(d, "cwd"))

    def claude(self, pid=CLAUDE_PID, sid=SESSION, name="lab", transcript=False):
        self.process(pid, "claude", ["claude"])
        with open(os.path.join(self.lives, f"{pid}.json"), "w", encoding="utf-8") as f:
            json.dump({"pid": pid, "sessionId": sid, "cwd": "/srv/lab", "name": name,
                       "procStart": str(TICKS)}, f)
        if transcript:
            folder = os.path.join(chat.PROJECTS_DIR, "-srv-lab")
            os.makedirs(folder, exist_ok=True)
            with open(os.path.join(folder, f"{sid}.jsonl"), "w", encoding="utf-8") as f:
                f.write(json.dumps({"type": "user", "timestamp": "2026-10-10T12:00:00Z", "cwd": "/srv/lab",
                                    "message": {"content": "go"}}) + "\n")

    def thread(self, home, thread, source="exec", open_turn=True, started="2026-10-10T12:00:00.000Z"):
        """Writes the rollout of a thread and returns the files a codex that writes it holds."""
        rollout = os.path.join(home, "sessions", "2026", "10", "10", f"rollout-2026-10-10T12-00-00-{thread}.jsonl")
        os.makedirs(os.path.dirname(rollout), exist_ok=True)
        records = [
            {"timestamp": started, "type": "session_meta",
             "payload": {"id": thread, "cwd": "/srv/lab/task", "source": source,
                         "base_instructions": {"text": "the instructions of the model"}}},
            {"timestamp": "2026-10-10T12:00:00.100Z", "type": "event_msg",
             "payload": {"type": "task_started", "turn_id": "t1", "model_context_window": 258400}},
            {"timestamp": "2026-10-10T12:00:00.200Z", "type": "turn_context",
             "payload": {"turn_id": "t1", "cwd": "/srv/lab/task", "model": "gpt-6-sol", "effort": "high"}},
            {"timestamp": "2026-10-10T12:00:05.000Z", "type": "event_msg",
             "payload": {"type": "token_count",
                         "info": {"last_token_usage": {"input_tokens": 25840}, "model_context_window": 258400}}},
        ]
        if not open_turn:
            records.append({"timestamp": "2026-10-10T12:00:09.000Z", "type": "event_msg",
                            "payload": {"type": "task_complete", "turn_id": "t1"}})
        with open(rollout, "w", encoding="utf-8") as f:
            for record in records:
                f.write(json.dumps(record) + "\n")
        return [os.path.join(home, "thread-writer-locks", f"{thread}.lock"), rollout]

    def exec_run(self, pid=RUN_PID, ppid=1, thread=RUN, home=None, **env):
        """Starts a run of codex exec in the tree, in the home of work unless told otherwise."""
        home = self.home if home is None else home
        values = {"CODEX_AGENT_ROLE": "reviewer", **env}
        if home != self.personal:
            values["CODEX_HOME"] = home
        values = {k: v for k, v in values.items() if v is not None}
        self.process(pid, "codex", [self.bin, "exec", "-s", "read-only", "review the change"],
                     env=values, ppid=ppid, files=self.thread(home, thread))


class Scan(Procs):
    def test_a_run_of_codex_exec_is_found_with_its_thread_and_the_contour_of_its_home(self):
        self.exec_run(CLAUDE_CODE_SESSION_ID=SESSION)
        self.assertEqual(ctx.codex_processes(), [{
            "pid": RUN_PID, "home": self.home, "contour": "work", "thread": RUN,
            "rollout": self.thread(self.home, RUN)[1], "born": BOOT + TICKS / os.sysconf("SC_CLK_TCK"),
            "cwd": "/srv/lab/task", "exec": True, "claude": SESSION, "role": "reviewer",
        }], "the program of the personal home serves every home: the contour is the one CODEX_HOME names")

    def test_without_a_home_in_its_environment_a_codex_writes_to_the_personal_one(self):
        self.exec_run(home=self.personal)
        found = ctx.codex_processes()
        self.assertEqual([(p["home"], p["contour"], p["thread"]) for p in found],
                         [(self.personal, "personal", RUN)])

    def test_the_feed_of_a_live_thread_of_a_home_no_contour_lists_opens(self):
        elsewhere = os.path.join(self.root, ".codex-elsewhere")
        self.exec_run(home=elsewhere)
        self.assertEqual([p["contour"] for p in ctx.codex_processes()], ["codex-elsewhere"])
        reply = chat.answer({"session": RUN, "limit": 10})
        self.assertTrue(reply["ok"], reply)

    def test_the_processes_of_a_daemon_are_left_to_the_executor(self):
        env = {"CODEX_HOME": self.home, "CLAUDE_CODE_SESSION_ID": SESSION}
        self.claude()
        self.process(RUN_PID, "codex", [self.bin, "app-server", "--listen", "unix://"], env=env,
                     files=self.thread(self.home, RUN, source="vscode"))
        self.process(OTHER_PID, "codex", [self.bin, "--remote", "unix:///run/codex.sock", "resume", TUI],
                     env=env, files=self.thread(self.home, TUI, source="cli"))
        self.process(SHELL_PID, "codex-code-mode", [self.bin + "-code-mode-host"], env=env)
        self.assertEqual(ctx.codex_processes(), [],
                         "the daemon carries the environment of the claude it was started from, "
                         "and its threads are the executor's")

    def test_a_thread_an_executor_follows_is_left_to_it(self):
        self.process(OTHER_PID, "codex", [self.bin, "resume", THREAD, "-C", "/srv/lab"],
                     env={"CODEX_HOME": self.home}, files=self.thread(self.home, THREAD, source="cli"))
        self.follow()
        self.assertEqual(ctx.codex_processes(), [], "codex in tmux the panel started is the executor's thread")

    def test_a_codex_that_writes_no_thread_yet_has_nothing_to_read(self):
        self.process(OTHER_PID, "codex", [self.bin], env={"CODEX_HOME": self.home}, cwd="/srv/lab")
        self.assertEqual(ctx.codex_processes(), [])

    def test_the_own_thread_of_a_codex_with_subagents_is_the_eldest(self):
        files = self.thread(self.home, CHILD, source={"subagent": {"thread_spawn": {"parent_thread_id": RUN}}})
        self.process(RUN_PID, "codex", [self.bin, "exec", "go"], env={"CODEX_HOME": self.home},
                     files=files + self.thread(self.home, RUN))
        self.assertEqual([p["thread"] for p in ctx.codex_processes()], [RUN])

    def test_a_process_of_another_name_is_no_codex(self):
        self.process(RUN_PID, "node", ["node", "codex.js", "exec"], env={"CODEX_HOME": self.home},
                     files=self.thread(self.home, RUN))
        self.assertEqual(ctx.codex_processes(), [])


class Parent(Procs):
    def test_a_run_sent_off_on_its_own_is_named_by_the_session_in_its_environment(self):
        self.claude()
        self.exec_run(ppid=1, CLAUDE_CODE_SESSION_ID=SESSION)
        self.assertEqual([p.get("parent") for p in ctx.codex_live()], [SESSION],
                         "a run whose parent let it go is still the session's that started it")

    def test_a_run_without_the_word_is_found_up_the_chain(self):
        self.claude()
        self.process(SHELL_PID, "bash", ["bash", "-c", "codex exec"], ppid=CLAUDE_PID)
        self.exec_run(ppid=SHELL_PID)
        self.assertEqual([p.get("parent") for p in ctx.codex_live()], [SESSION])

    def test_a_run_a_codex_started_is_no_run_of_claude(self):
        self.claude()
        env = {"CODEX_HOME": self.home, "CLAUDE_CODE_SESSION_ID": SESSION}
        self.process(OTHER_PID, "codex", [self.bin], env=env, files=self.thread(self.home, TUI, source="cli"))
        self.process(SHELL_PID, "bash", ["bash", "-lc", "codex exec"], env=env, ppid=OTHER_PID)
        self.exec_run(ppid=SHELL_PID, CLAUDE_CODE_SESSION_ID=SESSION)
        got = {p["thread"]: p.get("parent") for p in ctx.codex_live()}
        self.assertEqual(got, {TUI: None, RUN: ""},
                         "a codex hands down the environment of the claude that started it to every command")

    def test_a_run_whose_session_is_gone_stands_on_its_own(self):
        self.exec_run(CLAUDE_CODE_SESSION_ID=GONE)
        procs = ctx.codex_live()
        self.assertEqual([p.get("parent") for p in procs], [""])
        self.assertEqual(ctx.codex_runs(procs), {})
        rows = ctx.codex_own_rows(procs)
        self.assertEqual(rows, [{
            "session": f"codex-{RUN[-8:]}", "sessionId": RUN, "cwd": "/srv/lab/task",
            "profile": "work", "agent": "codex", "outside": True, "model": "gpt-6-sol", "effort": "high",
            "startedAt": ctx._iso(BOOT + TICKS / os.sysconf("SC_CLK_TCK")), "status": "busy",
            "tokens": 25840, "limit": 258400, "pct": 10.0, "limitKnown": True,
            "lastRequestAt": "2026-10-10T12:00:05.000Z", "title": "reviewer",
        }], "a run nobody live started is only read, by its role and never by what it was asked")


class Terminal(Procs):
    def test_a_codex_in_a_terminal_is_a_row_of_its_own_only_read(self):
        self.claude()
        files = self.thread(self.home, TUI, source="cli", open_turn=False)
        self.process(OTHER_PID, "codex", [self.bin, "resume", TUI],
                     env={"CODEX_HOME": self.home, "CLAUDE_CODE_SESSION_ID": SESSION}, ppid=CLAUDE_PID,
                     files=files)
        rows = ctx.codex_own_rows(ctx.codex_live())
        self.assertEqual([(r["session"], r["outside"], r["status"], r.get("title")) for r in rows],
                         [(f"codex-{TUI[-8:]}", True, "idle", None)],
                         "a codex in a terminal is a session of its own, whoever it was started under; "
                         "the name codex made of the first request is not shown")

    def give(self, name, called):
        """Keeps the name the panel gave the thread, and puts the name codex calls it by into the database of its home."""
        os.makedirs(os.path.dirname(held.named_path(TUI)))
        with open(held.named_path(TUI), "w", encoding="utf-8") as f:
            json.dump({"name": name}, f)
        files = self.thread(self.home, TUI, source="cli")
        row = {"id": TUI, "rollout_path": files[1], "created_at": 0, "updated_at": 0, "source": "cli",
               "model_provider": "openai", "cwd": "/srv/lab", "title": "Title codex made of the request",
               "sandbox_policy": "{}", "approval_mode": "on-request", "name": called}
        db = sqlite3.connect(os.path.join(self.home, codex_archive.STATE_DB))
        try:
            db.executescript(THREADS)
            db.execute(f"INSERT INTO threads ({', '.join(row)}) VALUES ({', '.join('?' for _ in row)})",
                       list(row.values()))
            db.commit()
        finally:
            db.close()
        self.process(OTHER_PID, "codex", [self.bin, "resume", TUI], env={"CODEX_HOME": self.home}, files=files)

    def test_the_name_the_panel_gave_the_thread_stays_with_it(self):
        self.give("router fix", " router fix")
        rows = ctx.codex_own_rows(ctx.codex_live())
        self.assertEqual([(r["title"], r["status"]) for r in rows], [("router fix", "busy")],
                         "codex calls the thread by the name the panel gave it")

    def test_a_name_given_in_codex_s_terminal_since_takes_the_panel_s_away(self):
        self.give("router fix", "parser rewrite")
        rows = ctx.codex_own_rows(ctx.codex_live())
        self.assertEqual([(r["session"], r.get("title")) for r in rows], [(f"codex-{TUI[-8:]}", None)],
                         "the row reads by the tail of its id once codex calls the thread otherwise")

    def test_a_name_codex_does_not_call_the_thread_by_is_not_shown(self):
        self.give("router fix", None)
        rows = ctx.codex_own_rows(ctx.codex_live())
        self.assertEqual([r.get("title") for r in rows], [None])


class Agents(Procs):
    """A run of codex exec is an agent of the session that started it."""

    AGENT = {"agent": "codex", "id": RUN, "name": "reviewer", "status": "active", "model": "gpt-6-sol",
             "tokens": 25840, "limit": 258400, "limitKnown": True, "last": "2026-10-10T12:00:05.000Z"}

    def setUp(self):
        super().setUp()
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = notes.Board(os.path.join(self.root, "notes.json"))
        self.claude(transcript=True)
        self.process(SHELL_PID, "bash", ["bash", "-c", "codex exec"], ppid=CLAUDE_PID)
        self.exec_run(ppid=SHELL_PID, CLAUDE_CODE_SESSION_ID=SESSION)

    def agent(self):
        return {**self.AGENT, "at": ctx._utc(BOOT + TICKS / os.sysconf("SC_CLK_TCK"))}

    def test_the_card_of_the_session_counts_the_run_among_its_agents(self):
        for name in ("session_profiles", "session_births", "live_session_waits",
                     "live_session_status", "live_session_status_at"):
            self.addCleanup(setattr, live, name, getattr(live, name))
            setattr(live, name, dict)
        self.addCleanup(agent.SESSION_STATE.forget, set())
        rows = live.sessions()["sessions"]
        self.assertEqual([r["session"] for r in rows], ["lab"], "a run of a live session is no session of its own")
        self.assertEqual(rows[0].get("work"), {"tasks": 0, "agents": 1})
        self.assertNotIn("codexRuns", rows[0], "what the reader of the state takes off stays out of the snapshot")

    def test_the_state_of_the_feed_has_the_run_among_its_agents(self):
        reply = chat.answer({"session": SESSION, "limit": 10, "state": True})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual(reply["state"]["agents"], [self.agent()])

    def test_the_feed_of_the_run_is_its_thread(self):
        reply = chat.answer({"session": RUN, "limit": 10, "state": True})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual(reply["state"], {"tasks": [], "agents": []})

    def test_a_run_that_is_over_leaves_the_agents(self):
        os.remove(os.path.join(self.proc, str(RUN_PID), "comm"))
        reply = chat.answer({"session": SESSION, "limit": 10, "state": True})
        self.assertEqual(reply["state"]["agents"], [])


def turn(model, effort, at="2026-10-10T12:10:00.000Z"):
    """The records a turn starts with: its start and what it runs with."""
    return [{"timestamp": at, "type": "event_msg",
             "payload": {"type": "task_started", "turn_id": "t2", "model_context_window": 258400}},
            {"timestamp": at, "type": "turn_context",
             "payload": {"turn_id": "t2", "cwd": "/srv/lab/task", "model": model, "effort": effort}}]


def work(kb):
    """What a turn writes after its start, about a kilobyte at a time, and the report of the context after it."""
    out = [{"timestamp": "2026-10-10T12:20:00.000Z", "type": "response_item",
            "payload": {"type": "function_call_output", "call_id": f"call-{n}", "output": "line of a build log " * 50}}
           for n in range(kb)]
    out.append({"timestamp": "2026-10-10T12:30:00.000Z", "type": "event_msg",
                "payload": {"type": "token_count",
                            "info": {"last_token_usage": {"input_tokens": 129200}, "model_context_window": 258400}}})
    return out


class LongTurn(Procs):
    """A turn names its model once, at its start, and a long one writes its tail full of the reports of the context."""

    def setUp(self):
        super().setUp()
        self.exec_run(CLAUDE_CODE_SESSION_ID=GONE)
        self.rollout = self.thread(self.home, RUN)[1]

    def write(self, records):
        with open(self.rollout, "a", encoding="utf-8") as f:
            for record in records:
                f.write(json.dumps(record) + "\n")

    def test_a_long_run_has_the_model_its_turn_named_at_its_start(self):
        self.write(work(1500))
        self.assertGreater(os.path.getsize(self.rollout), max(codex.TAIL_STEPS),
                           "the turn named its model before every tail the reports are looked for in")
        rows = ctx.codex_own_rows(ctx.codex_live())
        self.assertEqual([(r["model"], r["effort"], r["tokens"], r["status"]) for r in rows],
                         [("gpt-6-sol", "high", 129200, "busy")])

    def test_the_last_turn_before_the_tail_names_the_model(self):
        self.write(work(100) + turn("gpt-6-luna", "low") + work(100))
        found = codex.context(self.rollout)
        self.assertEqual((found["model"], found["effort"]), ("gpt-6-luna", "low"))

    def test_a_turn_started_since_the_last_look_names_the_model(self):
        self.write(work(100))
        self.assertEqual(codex.context(self.rollout)["model"], "gpt-6-sol")
        self.write(turn("gpt-6-luna", "low") + work(100))
        found = codex.context(self.rollout)
        self.assertEqual((found["model"], found["effort"]), ("gpt-6-luna", "low"),
                         "the look before the tail reads on from where it stopped")

    def test_the_model_of_a_turn_in_the_tail_is_the_one_it_names(self):
        self.write(work(100) + turn("gpt-6-luna", "low") + work(2))
        found = codex.context(self.rollout)
        self.assertEqual((found["model"], found["effort"]), ("gpt-6-luna", "low"))


class McpServer(Procs):
    """Codex as an MCP server of a claude session: a thread for every call of its tool, all of them the session's."""

    def setUp(self):
        super().setUp()
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = notes.Board(os.path.join(self.root, "notes.json"))
        self.claude(transcript=True)
        files = (self.thread(self.home, RUN, source="mcp", open_turn=False)
                 + self.thread(self.home, CHILD, source={"subagent": {"thread_spawn": {"parent_thread_id": RUN}}})
                 + self.thread(self.home, CALL, source="mcp", started="2026-10-10T12:05:00.000Z"))
        self.process(RUN_PID, "codex", [self.bin, "mcp-server"], ppid=CLAUDE_PID, files=files,
                     env={"CODEX_HOME": self.home, "CLAUDE_CODE_SESSION_ID": SESSION})

    def test_every_thread_of_the_server_is_a_run_of_the_session_that_started_it(self):
        procs = ctx.codex_live()
        self.assertEqual([(p["thread"], p.get("parent")) for p in procs], [(RUN, SESSION), (CALL, SESSION)],
                         "the thread of a subagent is part of the turn of the thread that started it")
        self.assertEqual(ctx.codex_own_rows(procs), [], "codex as an MCP server is no session of its own")

    def test_a_thread_is_at_work_while_its_turn_runs_and_idle_after(self):
        runs = ctx.codex_runs(ctx.codex_live())
        self.assertEqual([(a["id"], a["name"], a["status"], a["at"]) for a in runs[SESSION]], [
            (CALL, "codex mcp-server", "active", "2026-10-10T12:05:00.000Z"),
            (RUN, "codex mcp-server", "idle", "2026-10-10T12:00:00.000Z"),
        ], "the server lives as long as the session, and says nothing of whether a thread is at work")

    def test_the_card_of_the_session_counts_the_thread_at_work_alone(self):
        for name in ("session_profiles", "session_births", "live_session_waits",
                     "live_session_status", "live_session_status_at"):
            self.addCleanup(setattr, live, name, getattr(live, name))
            setattr(live, name, dict)
        self.addCleanup(agent.SESSION_STATE.forget, set())
        snapshot = live.sessions()
        self.assertEqual([r["session"] for r in snapshot["sessions"]], ["lab"])
        self.assertEqual(snapshot["sessions"][0].get("work"), {"tasks": 0, "agents": 1})

    def test_the_state_of_the_feed_has_the_threads_among_its_agents(self):
        reply = chat.answer({"session": SESSION, "limit": 10, "state": True})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual([(a["id"], a["status"]) for a in reply["state"]["agents"]],
                         [(CALL, "active"), (RUN, "idle")])

    def test_an_idle_thread_stands_among_the_agents_that_are_not_at_work(self):
        own = {"name": "alpha", "status": "active", "at": "2026-10-10T11:00:00.000Z"}
        state = ctx.with_codex_runs({"agents": [own]}, ctx.codex_runs(ctx.codex_live())[SESSION])
        self.assertEqual([a.get("id") or a["name"] for a in state["agents"]], [CALL, "alpha", RUN])


if __name__ == "__main__":
    unittest.main()
