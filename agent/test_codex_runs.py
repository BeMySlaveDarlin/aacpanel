import json
import os
import shutil
import sqlite3
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402
import codex_archive  # noqa: E402
import contours  # noqa: E402
import ctx  # noqa: E402
import notes  # noqa: E402
import sesstate  # noqa: E402
from sesstate import runs as calls  # noqa: E402
from test_codex import ROLLOUT, put_env  # noqa: E402
from test_codex_archive import THREADS  # noqa: E402
from test_codex_procs import CLAUDE_PID, RUN, RUN_PID, SHELL_PID, Procs  # noqa: E402
from test_sesstate import line, result  # noqa: E402

SESSION = "77777777-7777-4777-8777-777777777777"
AGENT_ID = "a5555555555555555"

# The threads of the runs, as codex numbers them: by the time they are made.
X = "01a12700-0000-7000-8000-0000000000a1"
Y = "01a12701-0000-7000-8000-0000000000a2"
Z = "01a12702-0000-7000-8000-0000000000a3"

T0 = "2026-10-10T12:00:00.000Z"
MS = calls.ms(T0)
SEC = 1000

# What a run was asked and what codex made of it: none of it may reach a list.
ASKED = "the prompt of the reviewer, with the private things of a customer"
TOLD = "Title codex made of the prompt"
COMMAND = "codex exec -s read-only 'look at the private thing of a customer'"


def stamp(ms):
    return codex_archive.stamp(ms)


def at(sec):
    return stamp(MS + sec * SEC)


def bash(use_id, sec, command=COMMAND, cwd="/srv/app", sidechain=False, **data):
    """A call of the shell the session made, sec seconds after T0."""
    record = {"type": "assistant", "timestamp": at(sec), "cwd": cwd,
              "message": {"content": [{"type": "tool_use", "id": use_id, "name": "Bash",
                                       "input": {"command": command, **data}}]}}
    if sidechain:
        record.update(isSidechain=True, agentId=AGENT_ID)
    return line(record)


def back(use_id, sec, **outcome):
    return result(use_id, "done", at=at(sec), **outcome)


def sent_off(use_id, task, sec):
    """The answer that a call went to the background, sec seconds after T0."""
    return result(use_id, f"Command running in background with ID: {task}", at=at(sec), backgroundTaskId=task)


def ended(use_id, task, sec):
    """The notification that the task of a call sent to the background ended."""
    return line({"type": "queue-operation", "operation": "enqueue", "timestamp": at(sec),
                 "content": f"<task-notification>\n<task-id>{task}</task-id>\n"
                            f"<tool-use-id>{use_id}</tool-use-id>\n<status>completed</status>\n"
                            f"<summary>Background command done</summary>\n</task-notification>"})


class Runs(Procs):
    """A claude session of the contour work, the codex homes of the test and the database of each."""

    def setUp(self):
        super().setUp()
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = notes.Board(os.path.join(self.root, "notes.json"))
        # The transcripts are found among the contours, and the contours are
        # the test's own: the machine's registry and home stay out of it.
        self.addCleanup(setattr, chat, "PROJECTS_DIR", chat.PROJECTS_DIR)
        chat.PROJECTS_DIR = None
        self.addCleanup(setattr, contours, "REGISTRY", contours.REGISTRY)
        contours.REGISTRY = ""
        self.addCleanup(setattr, contours, "HOME", contours.HOME)
        contours.HOME = os.path.join(self.root, ".claude")
        self.config = os.path.join(self.root, ".claude-profiles", "work")
        for d in (contours.HOME, self.config):
            os.makedirs(os.path.join(d, "projects"))
        put_env(self, contours.HOME_ENV, os.pathsep.join([contours.HOME, self.config]))
        self.transcript = os.path.join(self.config, "projects", "-srv-app", f"{SESSION}.jsonl")
        self.addCleanup(sesstate.SHARED.forget, set())
        self.rows = {self.home: [], self.personal: []}

    def converse(self, *chunks):
        os.makedirs(os.path.dirname(self.transcript), exist_ok=True)
        with open(self.transcript, "w", encoding="utf-8") as f:
            f.write(line({"type": "user", "timestamp": T0, "cwd": "/srv/app", "message": {"content": "go"}}))
            f.write("".join(chunks))

    def made(self, thread, made, done=None, home=None, cwd="/srv/app/.agents/tasks/t1/run", **cols):
        """Puts a thread of codex exec into the database of a home, made `made` seconds after T0."""
        home = home or self.home
        rollout = self.thread(home, thread, open_turn=False)[1]
        row = {"id": thread, "rollout_path": rollout,
               "created_at": (MS + made * SEC) // 1000, "updated_at": (MS + (done or made + 60) * SEC) // 1000,
               "created_at_ms": MS + made * SEC, "updated_at_ms": MS + (done or made + 60) * SEC,
               "source": "exec", "model_provider": "openai", "cwd": cwd, "title": TOLD,
               "sandbox_policy": "{}", "approval_mode": "never", "tokens_used": 1_500_000,
               "first_user_message": ASKED, "preview": ASKED, "name": TOLD, "model": "gpt-6-sol",
               "reasoning_effort": "high", "thread_source": "user", "agent_role": "reviewer"}
        row.update(cols)
        self.rows[home].append(row)
        db = sqlite3.connect(os.path.join(home, codex_archive.STATE_DB))
        try:
            db.executescript(THREADS if len(self.rows[home]) == 1 else "")
            db.execute(f"INSERT INTO threads ({', '.join(row)}) VALUES ({', '.join('?' for _ in row)})",
                       list(row.values()))
            db.commit()
        finally:
            db.close()
        return rollout

    def answer(self):
        reply = chat.answer({"session": SESSION, "limit": 10, "state": True})
        self.assertTrue(reply["ok"], reply)
        return reply

    def agents(self):
        return [a for a in self.answer()["state"]["agents"] if a.get("agent") == "codex"]

    def named(self):
        return [(a["id"], a["name"]) for a in self.agents()]


class Linking(Runs):
    def test_a_run_a_past_conversation_started_is_an_agent_of_it_that_is_over(self):
        self.converse(bash("toolu_1", 10, description="Review the change", run_in_background=True),
                      back("toolu_1", 11, backgroundTaskId="b1"))
        self.made(X, 15, done=300)
        self.assertEqual(self.agents(), [{
            "agent": "codex", "id": X, "name": "Review the change", "status": "done",
            "at": at(15), "doneAt": at(300), "model": "gpt-6-sol",
            "tokens": 25840, "limit": 258400, "limitKnown": True, "last": "2026-10-10T12:00:05.000Z",
        }], "the run reads as the run at work does: its context, not the tokens it spent over its life")

    def test_its_feed_is_its_thread(self):
        self.converse(bash("toolu_1", 10, run_in_background=True))
        shutil.copy(ROLLOUT, self.made(X, 15))
        self.assertEqual([a["id"] for a in self.agents()], [X])
        reply = chat.answer({"session": X, "limit": 200, "state": True})
        self.assertTrue(reply["ok"], reply)
        self.assertIn("ls -la src/router", [c["arg"] for i in reply["items"] for c in i.get("calls") or ()],
                      "the rollout of the run is its feed")
        self.assertEqual(reply["state"], {"tasks": [], "agents": []})

    def test_a_run_is_made_within_the_minute_after_the_call(self):
        # A later call elsewhere keeps the span of the session open past the
        # minute of the first one.
        self.converse(bash("toolu_1", 100), back("toolu_1", 105),
                      bash("toolu_2", 1000, cwd="/srv/elsewhere", run_in_background=True))
        self.made(X, 99)
        self.made(Y, 160)
        self.made(Z, 161)
        self.assertEqual([a["id"] for a in self.agents()], [Y],
                         "a thread made before the call or past its minute is no run of it")

    def test_a_run_works_where_the_call_ran_or_where_its_command_says(self):
        self.converse(bash("toolu_1", 10, command="cd /srv/other/wt && codex exec 'go'", run_in_background=True))
        self.made(X, 12, cwd="/srv/app/sub")
        self.made(Y, 13, cwd="/srv/other/wt/.agents/tasks/t2/run")
        self.made(Z, 14, cwd="/srv/third")
        self.assertEqual(sorted(a["id"] for a in self.agents()), [X, Y],
                         "below the call's directory or in one the command names, and nowhere else")

    def test_a_run_of_another_contour_is_not_the_sessions(self):
        self.converse(bash("toolu_1", 10, run_in_background=True), bash("toolu_2", 20, run_in_background=True))
        self.made(X, 12, home=self.personal)
        self.made(Y, 22)
        self.assertEqual([a["id"] for a in self.agents()], [Y],
                         "the session of work runs codex in the home of work: the personal one is another account's")

    def test_a_run_is_named_by_the_call_then_by_its_role_and_never_by_what_it_was_asked(self):
        self.converse(bash("toolu_1", 10, description="Review the change", run_in_background=True),
                      bash("toolu_2", 100, run_in_background=True),
                      bash("toolu_3", 200, run_in_background=True))
        self.made(X, 12)
        self.made(Y, 102, agent_role="worker")
        self.made(Z, 202, agent_role=None)
        self.assertEqual(sorted(self.named()), [(X, "Review the change"), (Y, "worker"), (Z, "codex exec")])
        said = json.dumps(self.answer()["state"], ensure_ascii=False)
        for words in (ASKED, TOLD, "private thing"):
            self.assertNotIn(words, said, "neither what the run was asked nor the command stays in the state")

    def test_a_resume_makes_no_second_agent_and_takes_no_run(self):
        self.converse(bash("toolu_1", 0, description="Start the review", run_in_background=True),
                      bash("toolu_2", 300, command=f"codex exec resume {X} 'go on'", description="Go on"),
                      bash("toolu_3", 302, description="Second opinion", run_in_background=True),
                      back("toolu_2", 900))
        self.made(X, 3, done=900)
        self.made(Y, 305, done=400)
        self.assertEqual(self.named(), [(X, "Start the review"), (Y, "Second opinion")],
                         "the resumed thread is one agent, named by the call that started it, and the "
                         "call that resumed it starts nothing")

    def test_calls_sent_off_one_after_another_make_their_runs_in_that_order(self):
        self.converse(bash("toolu_1", 0, command="tail .agents/codex.log", description="Read the log"),
                      back("toolu_1", 1),
                      bash("toolu_2", 2, description="First", run_in_background=True),
                      sent_off("toolu_2", "b2", 3),
                      bash("toolu_3", 4, description="Second", run_in_background=True),
                      sent_off("toolu_3", "b3", 5),
                      ended("toolu_2", "b2", 300), ended("toolu_3", "b3", 310))
        self.made(X, 6)
        self.made(Y, 8)
        self.assertEqual(sorted(self.named()), [(X, "First"), (Y, "Second")],
                         "of several calls, one that came back before the thread was made started none of it")

    def test_one_call_can_start_several_runs(self):
        self.converse(bash("toolu_1", 0, description="Two reviewers"), back("toolu_1", 600))
        self.made(X, 3)
        self.made(Y, 20)
        self.assertEqual(sorted(self.named()), [(X, "Two reviewers"), (Y, "Two reviewers")])

    def test_a_call_still_out_owns_the_runs_it_makes_past_the_minute(self):
        self.converse(bash("toolu_1", 0, description="Three reviewers in turn"), back("toolu_1", 300))
        self.made(X, 10)
        self.made(Y, 120)
        self.made(Z, 250)
        self.made("01a12703-0000-7000-8000-0000000000a4", 420)
        self.assertEqual(sorted(self.named()), [(X, "Three reviewers in turn"), (Y, "Three reviewers in turn"),
                                                (Z, "Three reviewers in turn")],
                         "a thread made two minutes after the call came back is none of its")

    def test_a_call_sent_to_the_background_is_out_until_its_task_ends(self):
        self.converse(bash("toolu_1", 0, description="Reviewers in the background", run_in_background=True),
                      sent_off("toolu_1", "b1", 1), ended("toolu_1", "b1", 300))
        self.made(X, 120)
        self.made(Y, 420)
        self.assertEqual(self.named(), [(X, "Reviewers in the background")],
                         "the answer that the call went to the background is not its coming back")

    def test_a_contour_without_a_codex_home_of_its_own_runs_codex_in_the_default_one(self):
        lab = os.path.join(self.root, ".claude-profiles", "lab")
        os.makedirs(os.path.join(lab, "projects"))
        put_env(self, contours.HOME_ENV, os.pathsep.join([contours.HOME, self.config, lab]))
        self.transcript = os.path.join(lab, "projects", "-srv-app", f"{SESSION}.jsonl")
        self.converse(bash("toolu_1", 10, run_in_background=True))
        self.made(X, 12, home=self.personal)
        self.made(Y, 13, home=self.personal, cwd="/srv/other")
        self.assertEqual([a["id"] for a in self.agents()], [X],
                         "codex started without a home of the contour writes to ~/.codex, and the "
                         "directory still keeps the threads of others away")

    def test_a_run_a_subagent_of_the_session_started_is_the_sessions(self):
        self.converse()
        folder = os.path.join(self.transcript[:-len(".jsonl")], "subagents")
        os.makedirs(folder)
        with open(os.path.join(folder, f"agent-{AGENT_ID}.meta.json"), "w", encoding="utf-8") as f:
            json.dump({"agentType": "general-purpose", "description": "Check the work"}, f)
        with open(os.path.join(folder, f"agent-{AGENT_ID}.jsonl"), "w", encoding="utf-8") as f:
            f.write(bash("toolu_9", 10, description="Ask codex", sidechain=True, run_in_background=True))
        self.made(X, 12)
        self.assertEqual(self.named(), [(X, "Ask codex")])

    def test_a_run_that_is_over_stands_after_the_agents_at_work(self):
        self.converse(bash("toolu_1", 10, run_in_background=True),
                      line({"type": "assistant", "timestamp": at(20), "cwd": "/srv/app",
                            "message": {"content": [{"type": "tool_use", "id": "toolu_2", "name": "Agent",
                                                     "input": {"name": "alpha", "description": "audit"}}]}}),
                      result("toolu_2", f"Spawned successfully.\nagent_id: alpha@session-abc\nname: alpha",
                             at=at(21), status="teammate_spawned"))
        self.made(X, 12, done=3000)
        self.assertEqual([(a["name"], a["status"]) for a in self.answer()["state"]["agents"]],
                         [("alpha", "active"), ("reviewer", "done")])


class Live(Runs):
    """A run whose process still lives is the run at work, though its thread is in the database already."""

    def test_the_live_run_and_the_one_of_the_database_are_one_agent_the_live_one(self):
        self.claude(sid=SESSION)
        self.process(SHELL_PID, "bash", ["bash", "-c", "codex exec"], ppid=CLAUDE_PID)
        self.exec_run(ppid=SHELL_PID, CLAUDE_CODE_SESSION_ID=SESSION)
        self.converse(bash("toolu_1", 10, description="Review the change"))
        self.made(RUN, 12)
        self.assertEqual([(a["id"], a["status"], a["name"]) for a in self.agents()],
                         [(RUN, "active", "reviewer")])
        os.remove(os.path.join(self.proc, str(RUN_PID), "comm"))
        self.assertEqual([(a["id"], a["status"], a["name"]) for a in self.agents()],
                         [(RUN, "done", "Review the change")], "a run whose process is gone is over")


class Calls(unittest.TestCase):
    """What the state of a session keeps of a call that may have started a run."""

    def test_a_call_is_kept_without_its_command(self):
        state = sesstate.State()
        sesstate._feed_record(state, json.loads(bash(
            "toolu_1", 0, command="cd /srv/wt && ~/bin/run-codex.sh 'the private thing'",
            description="Review", run_in_background=True)), "")
        self.assertEqual(list(state.codex.values()), [{
            "id": "toolu_1", "at": MS, "back": 0, "cwd": "/srv/app", "text": "Review", "bg": True,
            "paths": ["/srv/wt", os.path.expanduser("~/bin/run-codex.sh")], "resume": False,
        }])

    def test_a_call_that_names_no_codex_is_not_kept(self):
        state = sesstate.State()
        sesstate._feed_record(state, json.loads(bash("toolu_1", 0, command="make test")), "")
        self.assertEqual(state.codex, {})


if __name__ == "__main__":
    unittest.main()
