import json
import os
import shutil
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402
import chat  # noqa: E402
import contours  # noqa: E402
import ctx  # noqa: E402
import held  # noqa: E402
import notes  # noqa: E402
from chat import codex, harness  # noqa: E402
from collect import live  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
ROLLOUT = os.path.join(HERE, "testdata", "codex-rollout.jsonl")

THREAD = "01a12345-0000-7000-8000-00000000abcd"
CLAUDE = "44444444-4444-4444-4444-444444444444"


def records_at():
    """Returns the records of the fixture with the place each starts at."""
    out, pos = [], 0
    with open(ROLLOUT, "rb") as f:
        for raw in f:
            out.append((pos, json.loads(raw)))
            pos += len(raw)
    return out


def pos_of(item_id):
    for pos, record in records_at():
        item = (record.get("payload") or {}).get("item") or {}
        if item.get("id") == item_id:
            return pos
    raise AssertionError(f"no item {item_id} in the fixture")


def put_env(test, name, value):
    old = os.environ.get(name)
    test.addCleanup(lambda: os.environ.pop(name, None) if old is None
                    else os.environ.__setitem__(name, old))
    if value is None:
        os.environ.pop(name, None)
    else:
        os.environ[name] = value


class Homes(unittest.TestCase):
    def test_without_the_variable_the_home_is_the_personal_one(self):
        put_env(self, contours.CODEX_ENV, None)
        self.assertEqual(contours.codex_homes(), [("personal", os.path.expanduser("~/.codex"))])

    def test_a_home_is_named_by_the_rule_of_claude(self):
        put_env(self, contours.CODEX_ENV, os.pathsep.join(
            ["~/.codex", "/home/u/.codex-profiles/work", " /home/u/.codex-work ", "~/.codex"]))
        self.assertEqual(contours.codex_homes(), [
            ("personal", os.path.expanduser("~/.codex")),
            ("work", "/home/u/.codex-profiles/work"),
            ("codex-work", "/home/u/.codex-work"),
        ], "~ is expanded, spaces and repeats dropped, a home named as a claude directory is")


class Runtime(unittest.TestCase):
    """A runtime directory and codex homes of the test's own, with the fixture as the rollout of a thread."""

    def setUp(self):
        self.root = test_barrier.tmp_path(prefix="codex-")
        self.addCleanup(shutil.rmtree, self.root, True)
        put_env(self, "XDG_RUNTIME_DIR", os.path.join(self.root, "run"))
        os.makedirs(held.stream_dir())
        self.home = os.path.join(self.root, ".codex-profiles", "work")
        put_env(self, contours.CODEX_ENV, os.pathsep.join([os.path.join(self.root, ".codex"), self.home]))
        self.rollout = os.path.join(self.home, "sessions", "2026", "10", "09",
                                    f"rollout-2026-10-09T12-00-00-{THREAD}.jsonl")
        os.makedirs(os.path.dirname(self.rollout))
        shutil.copy(ROLLOUT, self.rollout)
        # A claude conversation is looked for first: an empty directory of
        # projects keeps the search away from the machine's own.
        projects = os.path.join(self.root, "projects")
        os.makedirs(projects)
        self.addCleanup(setattr, chat, "PROJECTS_DIR", chat.PROJECTS_DIR)
        chat.PROJECTS_DIR = projects

    def follow(self, holder=None, **extra):
        """Writes the file the executor keeps for a thread it follows."""
        data = {"protocol": 1, "agent": "codex", "name": f"codex-{THREAD[-8:]}", "sessionId": THREAD,
                "pid": 0, "holder": os.getpid() if holder is None else holder,
                "started": "2026-10-09T12:00:00Z", "busy": False, "model": "gpt-6-astra",
                "effort": "xhigh", "waiting": [], "queue": 0, "tasks": 0,
                "updated": "2026-10-09T12:00:06Z", "cwd": "/home/u/Projects/demo",
                "contour": "work", "codexHome": self.home, "transcript": self.rollout}
        data.update(extra)
        with open(os.path.join(held.stream_dir(), THREAD + ".json"), "w", encoding="utf-8") as f:
            json.dump(data, f)

    def dead_pid(self):
        done = subprocess.Popen(["true"])
        done.wait()
        return done.pid


class Rows(Runtime):
    def test_a_thread_a_live_executor_follows_is_a_row(self):
        self.follow()
        self.assertEqual(ctx.codex_sessions(), [{
            "session": "codex-0000abcd", "sessionId": THREAD, "cwd": "/home/u/Projects/demo",
            "profile": "work", "agent": "codex", "transport": "stream",
            "model": "gpt-6-astra", "effort": "xhigh", "startedAt": "2026-10-09T12:00:00Z",
            "tokens": 42000, "limit": 258400, "pct": 16.3, "limitKnown": True,
            "lastRequestAt": "2026-10-09T12:00:00.000Z", "status": "idle",
        }])

    def test_a_thread_carries_the_account_of_the_contour_its_home_is_named_after(self):
        claude = os.path.join(self.root, ".claude-profiles", "work")
        os.makedirs(claude)
        put_env(self, contours.HOME_ENV, os.pathsep.join([os.path.join(self.root, ".claude"), claude]))
        self.follow()
        self.assertEqual(ctx.codex_sessions()[0].get("configDir"), claude)
        self.follow(contour="elsewhere")
        self.assertNotIn("configDir", ctx.codex_sessions()[0], "a home named like no account has none")

    def test_a_thread_whose_executor_is_gone_is_no_row(self):
        self.follow(holder=self.dead_pid())
        self.assertEqual(ctx.codex_sessions(), [])

    def test_a_holder_of_claude_is_no_codex_row(self):
        self.follow()
        os.rename(os.path.join(held.stream_dir(), THREAD + ".json"),
                  os.path.join(held.stream_dir(), CLAUDE + ".json"))
        with open(os.path.join(held.stream_dir(), CLAUDE + ".json"), "w", encoding="utf-8") as f:
            json.dump({"protocol": 1, "sessionId": CLAUDE, "pid": 4321, "holder": os.getpid(),
                       "waiting": []}, f)
        self.assertEqual(ctx.codex_sessions(), [])

    def test_a_turn_at_work_is_busy(self):
        self.follow(busy=True)
        self.assertEqual(ctx.codex_sessions()[0]["status"], "busy")

    def test_an_approval_waited_for_is_a_dialog(self):
        self.follow(busy=True, waiting=["Bash"])
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["status"], row["waitingFor"]), ("waiting", "dialog open"))

    def test_the_rollout_is_found_by_the_id_when_the_file_names_none(self):
        self.follow(transcript="")
        self.assertEqual(ctx.codex_sessions()[0]["tokens"], 42000)

    def test_a_thread_that_made_no_request_has_the_window_of_its_turn(self):
        with open(self.rollout, "w", encoding="utf-8") as f:
            for _, record in records_at()[:4]:
                f.write(json.dumps(record) + "\n")
        self.follow()
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["tokens"], row["limit"], row["pct"]), (0, 258400, 0.0))
        self.assertTrue(row["noRequests"])
        self.assertIsNone(row["lastRequestAt"])

    def test_the_last_report_counts(self):
        with open(self.rollout, "a", encoding="utf-8") as f:
            f.write(json.dumps({"timestamp": "2026-10-09T12:02:00Z", "type": "event_msg", "payload": {
                "type": "token_count", "info": {"last_token_usage": {"input_tokens": 129200},
                                                "model_context_window": 258400}}}) + "\n")
            f.write(json.dumps({"timestamp": "2026-10-09T12:02:01Z", "type": "event_msg", "payload": {
                "type": "token_count", "info": None, "rate_limits": {}}}) + "\n")
        self.follow()
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["tokens"], row["pct"], row["lastRequestAt"]),
                         (129200, 50.0, "2026-10-09T12:02:00Z"),
                         "a report of the limits alone carries no count and must not take its place")

    def test_the_mode_the_plan_and_the_queue_come_from_the_executor(self):
        self.follow(mode="auto", plan=True, queue=2)
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["mode"], row["plan"], row["queued"]), ("auto", True, 2))
        self.follow(mode="custom", plan=False, queue=0)
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["mode"], row["plan"]), ("custom", False))
        self.assertNotIn("queued", row, "an empty queue is no queue on the row, as on a claude row")

    def test_the_fill_the_daemon_said_beats_an_older_rollout(self):
        self.follow(context={"tokens": 200000, "window": 258400, "at": "2026-10-09T12:05:00.123456789+00:00"})
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["tokens"], row["limit"], row["pct"], row["lastRequestAt"]),
                         (200000, 258400, 77.4, "2026-10-09T12:05:00.123456789+00:00"))

    def test_a_rollout_newer_than_what_the_daemon_said_wins(self):
        self.follow(context={"tokens": 200000, "window": 258400, "at": "2026-10-09T11:00:00Z"})
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["tokens"], row["lastRequestAt"]), (42000, "2026-10-09T12:00:00.000Z"),
                         "a turn another client ran reaches the panel through the rollout alone")

    def test_a_fill_that_does_not_read_is_left_for_the_rollout(self):
        for broken in ({"tokens": "many", "window": 1, "at": "2026-10-09T13:00:00Z"},
                       {"tokens": 1, "window": 1, "at": "soon"}, "full"):
            self.follow(context=broken)
            self.assertEqual(ctx.codex_sessions()[0]["tokens"], 42000, broken)

    def test_the_snapshot_adds_the_thread_after_the_claude_rows_and_leaves_them_alone(self):
        self.follow()
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = notes.Board(os.path.join(self.root, "notes.json"))
        claude = {"session": "site", "sessionId": CLAUDE, "cwd": "/srv/proj/site"}
        for name, value in (("sessions", lambda: {"sessions": [dict(claude)], "notes": []}),
                            ("session_profiles", dict), ("session_births", dict),
                            ("live_session_waits", dict), ("live_session_status", lambda: {"site": "busy"}),
                            ("live_session_status_at", dict)):
            owner = live.ctx if name == "sessions" else live
            self.addCleanup(setattr, owner, name, getattr(owner, name))
            setattr(owner, name, value)
        rows = live.sessions()["sessions"]
        self.assertEqual([r["session"] for r in rows], ["site", "codex-0000abcd"])
        self.assertEqual(rows[0], {**claude, "home": False, "status": "busy"},
                         "a claude row took something of the codex thread")
        self.assertEqual(rows[1]["agent"], "codex")


class Feed(Runtime):
    def items(self):
        out = []
        for pos, record in records_at():
            out += chat.parse(record, pos)
        return out

    def test_the_finished_items_are_the_rows_of_the_feed(self):
        shown = [(i["role"], i.get("name") or i.get("text"), i.get("kind"), i.get("arg"),
                  i.get("edited"), i.get("failed")) for i in self.items()]
        self.assertEqual(shown, [
            ("me", "Find the router and list its files", None, None, None, None),
            ("mind", "**Locating the router**\n\nI will search the tree.", None, None, None, None),
            ("ai", "I will look for it first.", None, None, None, None),
            ("tool", "Bash", "bash", "ls -la src/router", None, None),
            ("result", None, None, None, None, None),
            ("tool", "Bash", "bash", "go test ./src/router", None, None),
            ("result", None, None, None, None, True),
            ("tool", "Edit", "files", "/home/u/Projects/demo/src/router/routes.go",
             "/home/u/Projects/demo/src/router/routes.go", None),
            ("result", None, None, None, None, None),
            ("tool", "Edit", "files", "/home/u/Projects/demo/NOTES.md", "/home/u/Projects/demo/NOTES.md", None),
            ("result", None, None, None, None, None),
            ("tool", "WebSearch", "web", "", None, None),
            ("result", None, None, None, None, None),
            ("ai", "The router lives in `src/router`: two files, and its test fails.", None, None, None, None),
            ("note", harness.COMPACTED, None, None, None, None),
        ], "the raw model records, the counts, the turn bounds and a sleep are no rows")

    def test_a_turn_a_person_stopped_says_so(self):
        def aborted(reason):
            record = {"timestamp": "2026-10-09T12:02:00.000Z", "type": "event_msg",
                      "payload": {"type": "turn_aborted", "turn_id": "t", "reason": reason}}
            return [(i["role"], i.get("text")) for i in chat.parse(record, 7)]
        self.assertEqual(aborted("interrupted"), [("note", harness.STOPPED)])
        self.assertEqual(aborted("replaced"), [], "only a stop of a person is drawn")

    def test_the_window_folds_the_calls_the_way_it_folds_claude_ones(self):
        reply = chat.answer({"session": THREAD, "limit": 50})
        self.assertTrue(reply["ok"], reply)
        shown = [(i["role"], i.get("kind")) for i in reply["items"]]
        self.assertEqual(shown, [("me", None), ("mind", None), ("ai", None), ("tools", "bash"),
                                 ("tools", "files"), ("tools", "web"), ("ai", None), ("note", None)])
        bash = reply["items"][3]["calls"]
        self.assertEqual([(c["arg"], c.get("failed", False), "open" in c) for c in bash],
                         [("ls -la src/router", False, False), ("go test ./src/router", True, False)])
        files = reply["items"][4]["calls"]
        self.assertEqual([(c["use"], c["index"]) for c in files], [("patch-1#0", 0), ("patch-1#1", 1)])
        self.assertEqual(chat.answer({"session": THREAD, "limit": 50, "state": True}).get("state"),
                         {"tasks": [], "agents": []}, "the work of a session is claude's")

    def test_the_answer_after_a_change_carries_the_files_it_changed(self):
        work = os.path.join(self.root, "demo")
        os.makedirs(os.path.join(work, "src", "router"))
        with open(os.path.join(work, "NOTES.md"), "w", encoding="utf-8") as f:
            f.write("notes\n")
        with open(self.rollout, "w", encoding="utf-8") as f:
            for _, record in records_at():
                f.write(json.dumps(record).replace("/home/u/Projects/demo", work) + "\n")
        reply = chat.answer({"session": THREAD, "limit": 50})
        answer = [i for i in reply["items"] if i["role"] == "ai"][-1]
        self.assertEqual([f["name"] for f in answer.get("files", [])], ["NOTES.md"],
                         "the file the change added lies in the directory the rollout names")

    def test_the_search_reads_the_words_of_the_conversation(self):
        reply = chat.answer({"session": THREAD, "search": {"q": "router"}})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual([(m["pos"], m["role"]) for m in reply["matches"]],
                         [(pos_of("u1"), "me"), (pos_of("a2"), "assistant"), (pos_of("a2"), "assistant")],
                         "the command lines and the thinking that name the router are not searched")

    def test_what_only_claude_has_is_refused_rather_than_broken(self):
        for want in ({"agent": "a1"}, {"task": {"id": "b1"}}, {"image": {"pos": 0, "index": 0}}):
            reply = chat.answer({"session": THREAD, **want})
            self.assertIsInstance(reply, dict, want)
            self.assertNotIn("not read", reply.get("error", ""), want)

    def test_the_directory_is_the_one_the_head_names(self):
        self.assertEqual(chat.transcript_cwd(self.rollout), "/home/u/Projects/demo")


class Calls(Runtime):
    def call(self, item_id, index=0):
        return chat.answer({"session": THREAD, "call": {"pos": pos_of(item_id), "index": index}})

    def test_a_command_opens_with_what_it_ran_and_printed(self):
        got = self.call("exec-2")
        self.assertTrue(got["ok"], got)
        self.assertEqual(got["tool"], "Bash")
        self.assertEqual(json.loads(got["args"]), {"command": "go test ./src/router",
                                                   "cwd": "/home/u/Projects/demo"})
        self.assertEqual((got["result"], got["failed"]), ("Exit code 1\nFAIL\n", True))
        self.assertEqual((got["at"], got["resultAt"]),
                         ("2026-10-09T12:46:41.000Z", "2026-10-09T12:46:42.000Z"),
                         "the call runs from when codex started it to when it finished")

    def test_a_command_that_went_well_is_its_output(self):
        got = self.call("exec-1")
        self.assertEqual((got["result"], got["failed"]), ("total 8\nmain.go\nroutes.go\n", False))

    def test_a_change_opens_the_diff_of_its_file(self):
        update = self.call("patch-1", 0)
        self.assertEqual(update["tool"], "Edit")
        self.assertEqual(update["args"], "--- /home/u/Projects/demo/src/router/routes.go\n"
                                         "+++ /home/u/Projects/demo/src/router/routes.go\n"
                                         "@@ -1,2 +1,2 @@\n-old\n+new\n")
        self.assertEqual(update["result"], "Success")
        added = self.call("patch-1", 1)
        self.assertEqual(added["args"], "--- /dev/null\n+++ /home/u/Projects/demo/NOTES.md\n+notes\n")
        self.assertFalse(self.call("patch-1", 2)["ok"], "there is no third file in the change")

    def test_a_search_opens_with_nothing_to_show(self):
        got = self.call("x1")
        self.assertEqual((got["tool"], got["args"], got["result"]), ("WebSearch", "", ""))

    def test_a_message_is_no_call(self):
        self.assertFalse(self.call("a1")["ok"])


class Location(Runtime):
    def test_a_thread_nobody_follows_is_found_under_its_home(self):
        self.assertEqual(chat.transcript_path(THREAD), self.rollout)
        self.assertEqual(chat.transcript_path(THREAD, "work"), self.rollout)
        self.assertEqual(chat.transcript_path(THREAD, "personal"), "",
                         "a thread of work must not be found under another contour")

    def test_the_executor_names_the_rollout_first(self):
        elsewhere = os.path.join(self.root, "moved", f"rollout-2026-10-09T12-00-00-{THREAD}.jsonl")
        os.makedirs(os.path.dirname(elsewhere))
        shutil.copy(ROLLOUT, elsewhere)
        self.follow(transcript=elsewhere)
        self.assertEqual(chat.transcript_path(THREAD), elsewhere)

    def test_a_file_the_executor_names_for_another_thread_is_not_taken(self):
        other = os.path.join(self.root, f"rollout-2026-10-09T12-00-00-{CLAUDE}.jsonl")
        shutil.copy(ROLLOUT, other)
        self.follow(transcript=other)
        self.assertEqual(chat.transcript_path(THREAD), self.rollout)

    def test_a_claude_conversation_of_the_same_id_comes_first(self):
        claude = os.path.join(chat.PROJECTS_DIR, "-srv-proj-x", f"{THREAD}.jsonl")
        os.makedirs(os.path.dirname(claude))
        with open(claude, "w", encoding="utf-8") as f:
            f.write(json.dumps({"type": "user", "cwd": "/srv/proj/x", "message": {"content": "hi"}}) + "\n")
        self.assertEqual(chat.transcript_path(THREAD), claude)
        self.assertEqual(chat.transcript_cwd(claude), "/srv/proj/x")

    def test_a_rollout_is_told_from_a_transcript_by_its_name(self):
        self.assertTrue(codex.is_rollout(self.rollout))
        self.assertFalse(codex.is_rollout(f"/x/projects/-srv/{CLAUDE}.jsonl"))


DECISIONS = os.path.join(HERE, "testdata", "codex-decisions.jsonl")


class Decisions(Runtime):
    """What a person decides on in a codex thread, drawn in the feed: questions, plans, goals and reviews."""

    def setUp(self):
        super().setUp()
        shutil.copy(DECISIONS, self.rollout)

    def items(self):
        reply = chat.answer({"session": THREAD, "limit": 200})
        self.assertTrue(reply["ok"], reply)
        return reply["items"]

    def test_a_question_of_plan_mode_is_a_card_with_its_answer_and_note(self):
        cards = [i for i in self.items() if i["role"] == "asked"]
        self.assertEqual([(c["use"], c["asked"], c.get("status")) for c in cards], [
            ("call_98cbdb05caae472f9653ea18994208a0",
             [{"text": "Which indentation style do you prefer?", "answer": ["Spaces (Recommended)"],
               "note": "keep it short"}], None),
            ("call_dismissed", [{"text": "Which file first?"}], "rejected"),
        ], "a question put away from the panel is a card of a question refused")

    def test_a_plan_is_a_row_of_its_own(self):
        plans = [i for i in self.items() if i["role"] == "plan"]
        self.assertEqual(len(plans), 1)
        self.assertTrue(plans[0]["text"].startswith("1. Add a root `.editorconfig`"), plans[0])

    def test_a_goal_is_a_row_at_every_change_and_a_turn_of_its_own_says_so(self):
        items = self.items()
        goals = [(i["text"], i["status"], i["tokensUsed"], i["tokenBudget"]) for i in items if i["role"] == "goal"]
        self.assertEqual(goals[:2], [("Reply with the word OK", "active", 0, 2000),
                                     ("Reply with the word OK", "budgetLimited", 2330, 2000)])
        self.assertEqual(len(goals), 5)
        notes = [i["text"] for i in items if i["role"] == "note"]
        self.assertEqual(notes.count(codex.GOAL_TURN), 2, "a turn codex starts for its goal says why it started")
        self.assertNotIn("codex_internal_context", json.dumps(items), "the words codex gives itself are no row")

    def test_a_goal_cleared_says_so(self):
        said = {"timestamp": "2026-10-10T08:41:00.000Z", "type": "response_item", "payload": {
            "type": "message", "role": "developer", "content": [{"type": "input_text", "text":
                '<codex_internal_context source="goal">\nUser cleared the goal.\n</codex_internal_context>'}]}}
        told = {"timestamp": "2026-10-10T08:41:00.000Z", "type": "event_msg",
                "payload": {"type": "thread_goal_cleared", "threadId": THREAD}}
        for record in (said, told):
            self.assertEqual([(i["role"], i["text"]) for i in chat.parse(record, 9)], [("note", codex.GOAL_CLEARED)])
        mine = dict(said, payload=dict(said["payload"], role="assistant"))
        self.assertEqual(chat.parse(mine, 9), [], "the model saying the words clears nothing")
        other = dict(said, payload=dict(said["payload"], content=[{"type": "input_text", "text": "set it again"}]))
        self.assertEqual(chat.parse(other, 9), [])

    def test_a_review_begins_and_ends_with_its_findings(self):
        reviews = [i for i in self.items() if i["role"] == "review"]
        self.assertEqual([(r["state"], r.get("verdict"), len(r.get("findings", []))) for r in reviews],
                         [("start", None, 0), ("end", "patch is incorrect", 1),
                          ("start", None, 0), ("end", "patch is correct", 0)])
        self.assertEqual(reviews[0]["text"], "current changes")
        found = reviews[1]["findings"][0]
        self.assertEqual((found["title"], found["priority"], found["path"], found["lines"]),
                         ("[P1] Preserve addition semantics in `add`", 1, "/srv/proj/calc.py", [2, 2]))

    def test_the_thread_of_a_review_shows_its_work_and_not_its_words(self):
        items = self.items()
        self.assertFalse([i for i in items if i["role"] == "me"],
                         "the prompt codex gives the thread of a review is no message of the person's")
        calls = [c for i in items if i["role"] == "tools" for c in i.get("calls", [])]
        self.assertTrue(any(c["arg"].startswith("pwd; find ..") for c in calls), "the review's commands are its work")

    def test_a_single_record_reads_the_same_without_the_reader(self):
        with open(DECISIONS, "rb") as f:
            for pos, raw in enumerate(f):
                self.assertIsInstance(chat.parse(json.loads(raw), pos), list)


AGENTS = os.path.join(HERE, "testdata", "codex-agents.jsonl")
AGENT_FEED = os.path.join(HERE, "testdata", "codex-agent-worker.jsonl")
WORKER = "01a12346-0000-7000-8000-0000000000a1"
CHECKER = "01a12346-0000-7000-8000-0000000000a2"


class Agents(Runtime):
    """The agents a codex thread starts, in its feed: a card of each start, the other calls among the calls."""

    def setUp(self):
        super().setUp()
        shutil.copy(AGENTS, self.rollout)
        self.worker = os.path.join(os.path.dirname(self.rollout), f"rollout-2026-10-09T12-00-02-{WORKER}.jsonl")
        shutil.copy(AGENT_FEED, self.worker)

    def items(self):
        reply = chat.answer({"session": THREAD, "limit": 200})
        self.assertTrue(reply["ok"], reply)
        return reply["items"]

    def at(self, item_id):
        with open(AGENTS, "rb") as f:
            pos = 0
            for raw in f:
                if (json.loads(raw).get("payload") or {}).get("item", {}).get("id") == item_id:
                    return pos
                pos += len(raw)
        raise AssertionError(f"no item {item_id} in the fixture")

    def test_a_start_is_a_card_of_the_agent_its_model_and_its_task(self):
        cards = [i for i in self.items() if i["role"] == "spawn"]
        self.assertEqual([c["use"] for c in cards], ["spawn-1", "spawn-2", "spawn-3"])
        worker, checker, failed = cards
        self.assertEqual(worker["spawned"], [{"id": WORKER, "name": "Euclid", "role": "worker",
                                              "model": "gpt-6-astra", "effort": "medium", "state": "pending_init"}])
        self.assertEqual(worker["text"], "Read src/parser and list what it misses.\n\nAnswer in one line.")
        self.assertEqual(checker["spawned"][0]["id"], CHECKER)
        self.assertEqual(sorted(worker), ["at", "cut", "pos", "role", "spawned", "text", "use"],
                         "the card names who was started, on what, and its task: nothing else")
        self.assertEqual((failed["spawned"], failed.get("status"), failed["text"]),
                         ([], "failed", "Write the docs of the parser."),
                         "a start that failed is a card of its task with nobody started")

    def test_every_other_call_to_agents_is_a_call_among_the_calls(self):
        items = self.items()
        calls = [(c["name"], c["arg"], c.get("failed", False))
                 for i in items if i["role"] == "tools" and i["kind"] == "agents" for c in i["calls"]]
        self.assertEqual(calls, [("Wait", "Euclid, Hopper", False), ("SendInput", "Hopper", False),
                                 ("Wait", "Hopper", False), ("CloseAgent", "Hopper", False)])
        drawn = [i for i in items if i["role"] != "agentstates"]
        wait = next(n for n, i in enumerate(drawn) if i["role"] == "tools" and i["kind"] == "agents")
        self.assertEqual((drawn[wait + 1]["role"], drawn[wait + 1].get("kind"), drawn[wait + 1]["run"]),
                         ("tools", "bash", drawn[wait]["run"]),
                         "the command after a wait is of the same run: how the agents stand breaks none")

    def test_how_the_agents_stand_after_a_call_is_a_row_of_its_own(self):
        stood = [[(a["id"], a["state"]) for a in i["spawned"]] for i in self.items() if i["role"] == "agentstates"]
        self.assertEqual(stood, [[(WORKER, "completed"), (CHECKER, "running")], [(CHECKER, "running")],
                                 [(CHECKER, "errored")], [(CHECKER, "shutdown")]])

    def test_no_word_an_agent_said_or_was_sent_is_in_a_row(self):
        shown = json.dumps(self.items())
        for words in ("The parser misses escapes", "segfault", "Stop after the first failure"):
            self.assertNotIn(words, shown, "what an agent answered, its error and a word sent to it open "
                                           "with the call, they are no row")

    def test_a_state_codex_does_not_have_carries_no_words(self):
        with open(AGENTS, encoding="utf-8") as f:
            record = json.loads(f.readlines()[5])
        record["payload"]["item"]["agents_states"] = {WORKER: {"summary": "the secret plan"},
                                                     CHECKER: "the answer is 42"}
        rows = chat.parse(record, 9)
        self.assertEqual([r["role"] for r in rows], ["tool", "result"],
                         "a state that is no word of codex is no state")
        self.assertNotIn("secret", json.dumps(rows))
        self.assertNotIn("42", json.dumps(rows))

    def test_a_call_to_agents_opens_with_whom_it_named_and_how_they_stood(self):
        got = chat.answer({"session": THREAD, "call": {"pos": self.at("wait-1"), "index": 0}})
        self.assertTrue(got["ok"], got)
        self.assertEqual(got["tool"], "Wait")
        self.assertEqual(json.loads(got["args"]), {"agents": [
            {"nickname": "Euclid", "role": "worker", "thread": WORKER},
            {"nickname": "Hopper", "role": "checker", "thread": CHECKER}]})
        self.assertEqual(got["result"], "Euclid: completed\nThe parser misses escapes.\n\nHopper: running")
        sent = chat.answer({"session": THREAD, "call": {"pos": self.at("send-1"), "index": 0}})
        self.assertEqual(json.loads(sent["args"])["prompt"], "Stop after the first failure.")

    def test_the_feed_of_an_agent_is_its_own_thread_by_its_id(self):
        self.assertEqual(chat.transcript_path(WORKER), self.worker)
        reply = chat.answer({"session": WORKER, "limit": 50})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual([(i["role"], i["text"]) for i in reply["items"]],
                         [("task", "Read src/parser and list what it misses."), ("ai", "The parser misses escapes.")],
                         "what the thread that started the agent gave it is a task, not words of the person")

    def test_the_prompt_of_a_run_of_codex_exec_is_a_task(self):
        first = self.items()[0]
        self.assertEqual((first["role"], first["text"]), ("task", "Check the parser with two agents"),
                         "the claude session that ran codex exec wrote it, not the person")

    def test_a_thread_a_person_types_into_keeps_their_messages(self):
        with open(self.rollout, encoding="utf-8") as f:
            lines = f.readlines()
        head = json.loads(lines[0])
        head["payload"]["source"] = "cli"
        with open(self.rollout, "w", encoding="utf-8") as f:
            f.writelines([json.dumps(head) + "\n"] + lines[1:])
        first = self.items()[0]
        self.assertEqual((first["role"], first["text"]), ("me", "Check the parser with two agents"))

    def test_the_task_is_found_as_a_letter(self):
        reply = chat.answer({"session": WORKER, "search": {"q": "list what it misses"}})
        self.assertTrue(reply["ok"], reply)
        self.assertEqual([m["role"] for m in reply["matches"]], ["letter"])


class Asks(Runtime):
    """The question or the form a codex thread waits on, offered as a question of claude's."""

    ASK = {"sessionId": THREAD, "toolUseId": "0", "at": "2026-10-10T08:40:00Z",
           "questions": [{"id": "indent", "text": "Tabs or spaces?", "header": "Indent", "multi": False,
                          "options": [{"label": "Tabs", "description": ""}], "other": True}]}

    def test_the_feed_carries_the_question_the_thread_waits_on(self):
        self.follow(waiting=["AskUserQuestion"], ask=self.ASK)
        reply = chat.answer({"session": THREAD, "limit": 50, "state": True})
        self.assertEqual(reply.get("state"), {"tasks": [], "agents": [], "ask": self.ASK})
        self.assertEqual(ctx.codex_sessions()[0]["waitingFor"], "input needed")
        self.assertNotIn("ask", ctx.codex_sessions()[0], "the words of a question stay out of the snapshot")

    def test_a_thread_that_asks_nothing_has_a_state_without_a_question(self):
        self.follow(ask={"questions": []})
        reply = chat.answer({"session": THREAD, "limit": 50, "state": True})
        self.assertEqual(reply.get("state"), {"tasks": [], "agents": []},
                         "a question answered leaves the screen only when a state without it comes")
        self.assertNotIn("state", chat.answer({"session": THREAD, "limit": 50}), "a state not asked for")


class Extras(Runtime):
    def test_the_name_the_goal_and_the_terminals_come_from_the_executor(self):
        goal = {"objective": "ship it", "status": "active", "tokensUsed": 12, "tokenBudget": None,
                "timeUsedSeconds": 3, "updatedAt": 1791621270}
        self.follow(title="login-bug", goal=goal, processes=2)
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["title"], row["goal"], row["processes"], row["session"]),
                         ("login-bug", goal, 2, "codex-0000abcd"))
        self.follow(processes=0, goal={"objective": "", "status": "active"})
        row = ctx.codex_sessions()[0]
        for key in ("title", "goal", "processes"):
            self.assertNotIn(key, row)

    def test_a_thread_codex_runs_in_a_terminal_of_the_panel_lives_in_tmux(self):
        self.follow(terminal="shop")
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["transport"], row["tmux"]), ("tmux", "shop"))
        self.follow()
        row = ctx.codex_sessions()[0]
        self.assertEqual(row["transport"], "stream", "a thread the daemon alone holds is reached through the panel")
        self.assertNotIn("tmux", row)


if __name__ == "__main__":
    unittest.main()
