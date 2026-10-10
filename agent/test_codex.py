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
        put_env(self, "XDG_STATE_HOME", os.path.join(self.root, "state"))
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

    def keep_checklist(self, thread=THREAD, **extra):
        """Writes the checklist the executor's server keeps for a thread, under the name of the thread."""
        data = {"configDir": self.home, "dir": "/home/u/Projects/demo", "name": f"codex-{thread[-8:]}",
                "sessionId": thread, "pid": 800, "at": "2026-10-09T12:05:00Z",
                "items": [{"text": "read the router", "status": "done", "since": "2026-10-09T12:04:00Z"},
                          {"text": "fix its test", "status": "active", "since": "2026-10-09T12:05:00Z"}]}
        data.update(extra)
        root = os.path.join(os.environ["XDG_STATE_HOME"], "aacpanel", "checklists")
        os.makedirs(root, exist_ok=True)
        with open(os.path.join(root, f"codex-{thread}.json"), "w", encoding="utf-8") as f:
            json.dump(data, f)


# What the screens are given of the checklist keep_checklist writes.
CHECKLIST = {"items": [{"text": "read the router", "status": "done", "since": "2026-10-09T12:04:00Z"},
                       {"text": "fix its test", "status": "active", "since": "2026-10-09T12:05:00Z"}],
             "at": "2026-10-09T12:05:00Z"}


class Checklist(Runtime):
    """The checklist a thread keeps through the panel's tool, by its thread."""

    def test_the_row_of_a_thread_carries_its_checklist(self):
        self.follow()
        self.assertNotIn("checklist", ctx.codex_sessions()[0], "a thread that keeps none shows none")
        self.keep_checklist()
        self.assertEqual(ctx.codex_sessions()[0].get("checklist"), CHECKLIST)

    def test_the_checklist_of_a_thread_is_its_own_whatever_its_place_is_called(self):
        self.follow()
        self.keep_checklist(configDir="/srv/real/codex", dir="/srv/real/demo")
        self.assertEqual(ctx.codex_sessions()[0].get("checklist"), CHECKLIST,
                         "the server of the thread may name its place by other paths")
        self.keep_checklist(thread=CLAUDE)
        self.keep_checklist(sessionId=CLAUDE)
        self.assertNotIn("checklist", ctx.codex_sessions()[0], "a file that names another thread is not its own")

    def test_the_state_of_the_conversation_carries_the_checklist(self):
        self.follow()
        self.keep_checklist()
        state = chat.answer({"session": THREAD, "limit": 50, "state": True}).get("state")
        self.assertEqual(state, {"tasks": [], "agents": [], "checklist": CHECKLIST})


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

    def test_a_thread_started_for_a_session_names_it_as_its_parent(self):
        self.follow(parent="lead")
        self.assertEqual(ctx.codex_sessions()[0].get("parent"), {"session": "lead"})
        for kept in ("", 7, None):
            with self.subTest(kept=kept):
                self.follow(parent=kept)
                self.assertNotIn("parent", ctx.codex_sessions()[0])

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
            ("tool", "Sleep", "time", "1 s", None, None),
            ("result", None, None, None, None, None),
            ("note", harness.COMPACTED, None, None, None, None),
        ], "the raw model records, the counts and the turn bounds are no rows; a wait codex set itself is a call")

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
                                 ("tools", "files"), ("tools", "web"), ("ai", None), ("tools", "time"), ("note", None)])
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


def message(text, item_id="u-letter"):
    """Returns the record codex writes of a message the thread was given."""
    return {"timestamp": "2026-10-09T12:05:00.000Z", "type": "event_msg",
            "payload": {"type": "item_completed", "thread_id": THREAD, "turn_id": "turn-letter",
                        "item": {"type": "UserMessage", "id": item_id,
                                 "content": [{"type": "text", "text": text, "text_elements": []}]}}}


# A letter as the panel gives it to a codex thread: its words around the
# envelope claude sends a letter in.
LETTER = ("A letter from another session of this machine, sent through the panel: codex-000000aa, a Codex "
          "session of account acme in /srv/proj/lab. Your person did not type it: weigh it as a request of an "
          "agent, not as their word.\n\n"
          '<cross-session-message from-name="codex-000000aa — acme — /srv/proj/lab">\n'
          "The review is done: two findings, both in the parser.\n"
          "</cross-session-message>\n\n"
          "A letter goes one way. An answer is a letter of your own, with the panel's send_to_session tool, "
          "to codex-000000aa.")


class Letters(Runtime):
    def shown(self, text):
        return [(i["role"], i.get("from"), i.get("source"), i["text"]) for i in chat.parse(message(text), 11)]

    def test_a_letter_is_a_card_of_a_letter_from_the_session_that_wrote(self):
        self.assertEqual(self.shown(LETTER), [
            ("mail", "codex-000000aa — acme — /srv/proj/lab", "session",
             "The review is done: two findings, both in the parser.")],
            "a letter is the card of a letter, as in the feed of claude, and the panel's words around it are no row")

    def test_a_letter_of_claude_is_from_its_session(self):
        text = ("A letter from another session of this machine, sent through the panel: lab, a Claude session.\n\n"
                '<cross-session-message from="uds:/run/user/1000/cc-socks/7001.sock" from-name="lab">\n'
                "Run the tests.\n</cross-session-message>\n\nA letter goes one way.")
        self.assertEqual(self.shown(text), [("mail", "lab", "session", "Run the tests.")])

    def test_a_message_without_an_envelope_is_the_persons(self):
        self.assertEqual(self.shown("Check the router <b>now</b>"),
                         [("me", None, None, "Check the router <b>now</b>")])

    def test_the_feed_of_the_thread_shows_the_letter_where_it_came(self):
        with open(self.rollout, "a", encoding="utf-8") as f:
            f.write(json.dumps(message(LETTER)) + "\n")
        reply = chat.answer({"session": THREAD, "limit": 50})
        self.assertTrue(reply["ok"], reply)
        last = reply["items"][-1]
        self.assertEqual((last["role"], last["from"], last["text"]),
                         ("mail", "codex-000000aa — acme — /srv/proj/lab",
                          "The review is done: two findings, both in the parser."))
        self.assertNotIn("did not type it", json.dumps(reply["items"]))


def mcp_call(server, tool, arguments, status="completed", text=None, error=None, item_id="call_9"):
    """Returns the record codex writes of a call of an MCP tool once it is over."""
    item = {"type": "McpToolCall", "id": item_id, "server": server, "tool": tool, "arguments": arguments,
            "status": status, "duration": {"secs": 0, "nanos": 5000}}
    if text is not None:
        item["result"] = {"content": [{"type": "text", "text": text}], "isError": status == "failed"}
    if error is not None:
        item["error"] = {"message": error}
    return {"timestamp": "2026-10-09T12:06:00.000Z", "type": "event_msg",
            "payload": {"type": "item_completed", "thread_id": THREAD, "turn_id": "turn-mcp", "item": item,
                        "started_at_ms": 1791554760000, "completed_at_ms": 1791554761000}}


class McpCalls(Runtime):
    def add(self, record):
        """Appends a record to the rollout and returns where it starts."""
        pos = os.path.getsize(self.rollout)
        with open(self.rollout, "a", encoding="utf-8") as f:
            f.write(json.dumps(record) + "\n")
        return pos

    def test_a_call_of_an_mcp_tool_is_a_call_as_claude_draws_one(self):
        rows = chat.parse(mcp_call("docs", "search", {"query": "router tests", "limit": 3}, text="2 pages"), 4)
        self.assertEqual([(r["role"], r.get("name"), r.get("kind"), r.get("arg"), r.get("failed")) for r in rows],
                         [("tool", "docs: search", "mcp", "router tests", None), ("result", None, None, None, None)])
        failed = chat.parse(mcp_call("docs", "search", {"query": "x"}, status="failed", error="user rejected MCP tool call"), 4)
        self.assertEqual(failed[1].get("failed"), True, "a call codex did not run, or that failed, reads as failed")
        browser = chat.parse(mcp_call("chrome-devtools", "navigate_page", {"url": "http://127.0.0.1:8080/"}, text="ok"), 4)
        self.assertEqual((browser[0]["kind"], browser[0]["arg"]), ("browser", "http://127.0.0.1:8080/"))

    def test_a_call_opens_with_its_arguments_and_what_it_answered(self):
        pos = self.add(mcp_call("docs", "search", {"query": "router tests"}, text="2 pages:\nrouter.md\ntests.md"))
        got = chat.answer({"session": THREAD, "call": {"pos": pos, "index": 0}})
        self.assertTrue(got["ok"], got)
        self.assertEqual((got["tool"], json.loads(got["args"]), got["result"], got["failed"]),
                         ("mcp__docs__search", {"query": "router tests"}, "2 pages:\nrouter.md\ntests.md", False))
        refused = self.add(mcp_call("docs", "search", {"query": "x"}, status="failed",
                                    error="MCP tool call requires approval, but approval policy is never", item_id="call_10"))
        got = chat.answer({"session": THREAD, "call": {"pos": refused, "index": 0}})
        self.assertEqual((got["result"], got["failed"]),
                         ("MCP tool call requires approval, but approval policy is never", True))

    def test_a_letter_of_the_thread_is_an_outgoing_letter(self):
        rows = chat.parse(mcp_call("aacpanel", "send_to_session", {"to": "lab", "text": "The review is done."},
                                   text="Sent: a letter from codex-0000abcd to lab."), 4)
        self.assertEqual([(r["role"], r.get("dir"), r.get("from"), r.get("source"), r["text"], r.get("undelivered"))
                          for r in rows],
                         [("mail", "out", "lab", "session", "The review is done.", None)])
        lost = chat.parse(mcp_call("aacpanel", "send_to_session", {"to": "shop", "text": "hello"}, status="failed",
                                   text="Nothing was sent: there is no live session shop"), 4)
        self.assertEqual((lost[0]["role"], lost[0].get("undelivered")),
                         ("mail", "Nothing was sent: there is no live session shop"),
                         "a letter that reached nobody says why")
        listed = chat.parse(mcp_call("aacpanel", "send_to_session", {}, text="The live sessions of this machine"), 4)
        self.assertEqual([(r["role"], r.get("name")) for r in listed],
                         [("tool", "aacpanel: send_to_session"), ("result", None)],
                         "the list of sessions sends nothing and stays a call")

    def test_a_secret_the_thread_asked_for_is_the_card_of_a_secret(self):
        """The card takes its name from the answer and its title and notepad from the call, as claude's does."""
        args = {"name": "github-token", "title": "GitHub token", "template": "GH_TOKEN=\n"}
        rows = chat.parse(mcp_call("aacpanel", "secret_ask", args, text="Asked as github-token.\nThe person fills "
                                   "the notepad in the panel."), 4)
        self.assertEqual(rows, [{"role": "secret", "use": "call_9", "name": "github-token", "title": "GitHub token",
                                 "template": "GH_TOKEN=\n", "at": "2026-10-09T12:06:00.000Z", "pos": 4}])
        refused = chat.parse(mcp_call("aacpanel", "secret_ask", {"name": "x"}, status="failed",
                                      text="Nothing was asked: the call names no codex thread."), 4)
        self.assertEqual([r["role"] for r in refused], ["tool", "result"], "a refusal asked nothing and stays a call")

    def test_a_brief_the_thread_published_is_the_card_of_a_brief(self):
        shelf = {"seven": {"title": "Seven questions", "eyebrow": "after the review",
                           "questions": [{"kind": "pick"}, {"kind": "none"}]}}
        record = mcp_call("aacpanel", "brief_publish", {"doc": {"id": "seven", "title": "Seven questions"}},
                          text="Published as seven. The answers arrive in this session as a message.")
        rows = chat.parse(record, 4, shelf=shelf.get)
        self.assertEqual([(r["role"], r["id"], r["title"], r.get("eyebrow"), r["questions"], r["use"]) for r in rows],
                         [("brief", "seven", "Seven questions", "after the review", 1, "call_9")])
        self.assertEqual([(r["role"], r["title"]) for r in chat.parse(record, 4)], [("brief", "seven")],
                         "without the shelf the card names the brief")
        rules = chat.parse(mcp_call("aacpanel", "brief_publish", {}, text="# How a brief is written"), 4)
        self.assertEqual([r["role"] for r in rules], ["tool", "result"], "reading the rules publishes nothing")

    def test_the_feed_of_the_thread_shows_the_calls_where_they_came(self):
        self.add(mcp_call("docs", "search", {"query": "router tests"}, text="2 pages"))
        self.add(mcp_call("aacpanel", "send_to_session", {"to": "lab", "text": "done"}, text="Sent.", item_id="call_11"))
        reply = chat.answer({"session": THREAD, "limit": 50})
        self.assertTrue(reply["ok"], reply)
        letter = reply["items"][-1]
        self.assertEqual((letter["role"], letter.get("dir"), letter["text"]), ("mail", "out", "done"))
        calls = [c for i in reply["items"] if i["role"] == "tools" for c in i["calls"]]
        self.assertIn(("docs: search", "router tests"), [(c["name"], c.get("arg")) for c in calls])


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

    def test_the_state_of_the_thread_lists_its_agents_as_the_last_call_said(self):
        reply = chat.answer({"session": THREAD, "limit": 50, "state": True})
        self.assertEqual([(a["name"], a.get("text"), a["status"], a.get("doneAt"), a["model"])
                          for a in reply["state"]["agents"]],
                         [("Hopper", "checker", "closed", "2026-10-09T12:00:08.000Z", "gpt-6-astra"),
                          ("Euclid", "worker", "completed", "2026-10-09T12:00:04.000Z", "gpt-6-astra")],
                         "named by its nickname with its role under it, and over since the call that said so")

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


SECOND = os.path.join(HERE, "testdata", "codex-agents-second.jsonl")
SECOND_FEED = os.path.join(HERE, "testdata", "codex-agent-second-lexer.jsonl")
LEXER = "01a12348-0000-7000-8000-0000000000c1"
TESTS = "01a12348-0000-7000-8000-0000000000c2"

# The tools codex has for its agents, as its protocol lists them, and the
# name the feed gives each: the name claude gives the call that does the same,
# where claude has one.
NINE = (("spawnAgent", "Agent"), ("sendInput", "SendInput"), ("resumeAgent", "ResumeAgent"), ("wait", "Wait"),
        ("closeAgent", "CloseAgent"), ("sendMessage", "SendMessage"), ("followupTask", "FollowupTask"),
        ("interruptAgent", "InterruptAgent"), ("listAgents", "ListAgents"))


def snake(word):
    return "".join(f"_{c.lower()}" if c.isupper() else c for c in word)


class SecondAgents(Runtime):
    """A thread that works with the second set of codex's tools for agents: calls, starts and how agents stand."""

    def setUp(self):
        super().setUp()
        shutil.copy(SECOND, self.rollout)
        self.lexer = os.path.join(os.path.dirname(self.rollout), f"rollout-2026-10-09T13-00-04-{LEXER}.jsonl")
        shutil.copy(SECOND_FEED, self.lexer)

    def items(self):
        reply = chat.answer({"session": THREAD, "limit": 200})
        self.assertTrue(reply["ok"], reply)
        return reply["items"]

    def at(self, use):
        with open(self.rollout, "rb") as f:
            pos = 0
            for raw in f:
                payload = json.loads(raw).get("payload") or {}
                if payload.get("type") == "function_call" and payload.get("call_id") == use:
                    return pos
                pos += len(raw)
        raise AssertionError(f"no call {use} in the fixture")

    def opened(self, use):
        got = chat.answer({"session": THREAD, "call": {"pos": self.at(use), "index": 0}})
        self.assertTrue(got["ok"], got)
        return got

    def test_every_call_but_a_start_is_a_call_among_the_calls(self):
        calls = [(c["name"], c["arg"], c.get("failed", False), c.get("open", False))
                 for i in self.items() if i["role"] == "tools" for c in i["calls"]]
        self.assertEqual(calls, [("SendMessage", "lexer", False, False), ("Wait", "", False, False),
                                 ("InterruptAgent", "tests", False, False), ("ListAgents", "", False, False),
                                 ("FollowupTask", "tests", False, False), ("Wait", "", True, False)],
                         "a call is drawn once, from the call: the item of a wait beside it is no second wait, "
                         "and a wait the person stopped the turn in failed")

    def test_a_start_is_a_card_of_the_agent_its_role_and_its_model(self):
        cards = [i for i in self.items() if i["role"] == "spawn"]
        self.assertEqual([c["spawned"] for c in cards], [
            [{"id": LEXER, "name": "lexer", "role": "explorer", "model": "gpt-6-astra", "effort": "low",
              "state": "running"}],
            [{"id": TESTS, "name": "tests", "role": "", "model": "gpt-6-astra", "effort": "medium",
              "state": "running"}]])
        self.assertEqual([(c["use"], c["text"]) for c in cards], [("call_spawn_lexer", ""), ("call_spawn_tests", "")],
                         "the task of the second set travels encrypted: the card has none")
        self.assertEqual(sorted(cards[0]), ["at", "cut", "pos", "role", "spawned", "text", "use"])

    def test_how_an_agent_stands_follows_what_happened_to_it(self):
        items = self.items()
        stood = [[(a["id"], a["state"]) for a in i["spawned"]] for i in items if i["role"] == "agentstates"]
        self.assertEqual(stood, [[(TESTS, "interrupted")], [(TESTS, "running")], [(LEXER, "completed")]],
                         "an interrupt stops an agent, a task more sets it to work, the end of its turn finishes "
                         "it; a word sent changes nothing")
        lines = [(i["from"], i["text"], i["level"]) for i in items if i["role"] == "notice"]
        self.assertEqual(lines, [("agent tests", "was interrupted", "warn"), ("agent lexer", "finished", "ok")])

    def test_no_word_sent_to_an_agent_is_shown(self):
        shown = json.dumps(self.items())
        for use in ("call_send_1", "call_followup_1", "call_list_1", "call_wait_2"):
            shown += json.dumps(self.opened(use))
        self.assertNotIn("gAAAA", shown, "what the thread sends its agents is encrypted, and stays out")

    def test_a_call_opens_with_whom_it_named_and_what_codex_answered(self):
        sent = self.opened("call_send_1")
        self.assertEqual((sent["tool"], json.loads(sent["args"]), sent["result"], sent["failed"]),
                         ("SendMessage", {"target": "lexer"}, "", False))
        listed = self.opened("call_list_1")
        self.assertEqual((listed["tool"], listed["args"]), ("ListAgents", ""))
        self.assertEqual([a["agent_status"] for a in json.loads(listed["result"])["agents"]],
                         ["running", "running", "interrupted"])
        stopped = self.opened("call_interrupt_1")
        self.assertEqual(json.loads(stopped["result"]), {"previous_status": "running"})
        cut = self.opened("call_wait_2")
        self.assertEqual((cut["tool"], json.loads(cut["args"]), cut["result"], cut["failed"]),
                         ("Wait", {"timeout_ms": 60000}, "aborted by user after 2.1s", True))
        self.assertNotIn("pending", cut)

    def test_a_call_whose_answer_has_not_come_is_open(self):
        with open(SECOND, encoding="utf-8") as f:
            lines = f.readlines()
        at = next(n for n, line in enumerate(lines) if '"call_wait_2"' in line)
        with open(self.rollout, "w", encoding="utf-8") as f:
            f.writelines(lines[:at + 1])
        last = [c for i in self.items() if i["role"] == "tools" for c in i["calls"]][-1]
        self.assertEqual((last["name"], last.get("open")), ("Wait", True))
        self.assertTrue(self.opened("call_wait_2").get("pending"))

    def test_every_tool_codex_has_for_agents_has_its_name(self):
        for tool, word in NINE:
            name = snake(tool)
            item = {"type": "event_msg", "timestamp": "2026-10-09T13:00:00.000Z",
                    "payload": {"type": "item_completed", "thread_id": THREAD,
                                "item": {"type": "CollabAgentToolCall", "id": f"first-{name}", "tool": name,
                                         "status": "completed", "receiver_thread_ids": [WORKER],
                                         "agents_states": {}}}}
            rows = chat.parse(item, 1)
            drawn = rows[0].get("name") if rows[0]["role"] == "tool" else rows[0]["role"]
            self.assertEqual(drawn, "spawn" if word == "Agent" else word, f"{name} of the first set")
            call ={"type": "response_item", "timestamp": "2026-10-09T13:00:00.000Z",
                    "payload": {"type": "function_call", "name": "wait_agent" if name == "wait" else name,
                                "namespace": "collaboration", "arguments": "{}", "call_id": f"second-{name}"}}
            rows = chat.parse(call, 1, asks={})
            self.assertEqual([r.get("name") for r in rows], [] if word == "Agent" else [word], f"{name} of the second set")

    def state(self):
        reply = chat.answer({"session": THREAD, "limit": 50, "state": True})
        self.assertTrue(reply["ok"], reply)
        return reply["state"]

    def finish_tests(self):
        """Writes into the rollout that the agent tests finished its turn."""
        with open(SECOND, encoding="utf-8") as f:
            done = json.loads(next(line for line in f if '"kind": "completed"' in line))
        done["timestamp"] = "2026-10-09T13:00:40.000Z"
        done["payload"]["item"].update(id=f"subagent-completed-{TESTS}", agent_thread_id=TESTS,
                                       agent_path="/root/tests")
        with open(self.rollout, "a", encoding="utf-8") as f:
            f.write(json.dumps(done) + "\n")

    def test_the_state_of_the_thread_lists_its_agents_as_they_stand(self):
        self.assertEqual(self.state()["agents"], [
            {"agent": "codex", "kind": "subagent", "id": TESTS, "name": "tests", "status": "active",
             "at": "2026-10-09T13:00:08.000Z", "model": "gpt-6-astra"},
            {"agent": "codex", "kind": "subagent", "id": LEXER, "name": "lexer", "text": "explorer",
             "status": "completed", "at": "2026-10-09T13:00:05.000Z", "doneAt": "2026-10-09T13:00:24.000Z",
             "model": "gpt-6-astra"}],
            "the one at work first; an agent interrupted and given a task more is at work again")
        self.finish_tests()
        self.assertEqual([(a["name"], a["status"], a.get("doneAt")) for a in self.state()["agents"]],
                         [("tests", "completed", "2026-10-09T13:00:40.000Z"),
                          ("lexer", "completed", "2026-10-09T13:00:24.000Z")],
                         "the rollout is read on, and the freshest end stands first")

    def test_an_agent_at_work_is_at_work_while_its_own_thread_runs_a_turn(self):
        own = os.path.join(os.path.dirname(self.rollout), f"rollout-2026-10-09T13-00-07-{TESTS}.jsonl")
        turn = "01a12348-3333-7000-8000-000000000002"
        records = [
            {"timestamp": "2026-10-09T13:00:08.000Z", "type": "session_meta",
             "payload": {"id": TESTS, "thread_source": "subagent", "cwd": "/home/u/Projects/demo"}},
            {"timestamp": "2026-10-09T13:00:09.000Z", "type": "event_msg",
             "payload": {"type": "task_started", "turn_id": turn, "model_context_window": 258400}},
            {"timestamp": "2026-10-09T13:00:10.000Z", "type": "event_msg",
             "payload": {"type": "token_count", "info": {"last_token_usage": {"input_tokens": 52000},
                                                         "model_context_window": 258400}}},
        ]
        with open(own, "w", encoding="utf-8") as f:
            f.writelines(json.dumps(r) + "\n" for r in records)
        tests = self.state()["agents"][0]
        self.assertEqual((tests["name"], tests["status"], tests["tokens"], tests["limit"]),
                         ("tests", "active", 52000, 258400), "its thread runs a turn, and says how full it is")
        with open(own, "a", encoding="utf-8") as f:
            f.write(json.dumps({"timestamp": "2026-10-09T13:00:30.000Z", "type": "event_msg",
                                "payload": {"type": "turn_aborted", "turn_id": turn, "reason": "interrupted"}}) + "\n")
        self.assertEqual(sorted((a["name"], a["status"]) for a in self.state()["agents"]),
                         [("lexer", "completed"), ("tests", "idle")],
                         "a turn of the agent stopped from outside is no word of the thread's, and the agent waits")
        self.follow(busy=False)
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["status"], "work" in row), ("idle", False), "an agent that waits keeps nobody busy")

    def test_a_thread_whose_agents_work_is_busy_with_its_turn_over(self):
        self.follow(busy=False)
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["status"], row.get("turnOver"), row.get("work")),
                         ("busy", True, {"tasks": 0, "agents": 1}))
        self.follow(busy=True)
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["status"], row.get("turnOver"), row.get("work")),
                         ("busy", None, {"tasks": 0, "agents": 1}), "a turn of its own is no turn over")
        self.follow(busy=False, waiting=["AskUserQuestion"])
        self.assertEqual(ctx.codex_sessions()[0]["status"], "waiting", "a person waited for comes first")
        self.finish_tests()
        self.follow(busy=False)
        row = ctx.codex_sessions()[0]
        self.assertEqual((row["status"], "turnOver" in row, "work" in row), ("idle", False, False),
                         "a thread whose agents are over is free")

    def test_the_feed_of_an_agent_is_its_own_thread_and_none_of_its_parents(self):
        self.assertEqual(chat.transcript_path(LEXER), self.lexer)
        reply = chat.answer({"session": LEXER, "limit": 50})
        self.assertTrue(reply["ok"], reply)
        rows = [(i["role"], [(c["name"], c["arg"]) for c in i["calls"]] if i["role"] == "tools" else i.get("text"))
                for i in reply["items"]]
        self.assertEqual(rows, [("tools", [("Letter", "from /root"), ("SendMessage", "/root")]),
                                ("tools", [("Bash", "ls src/lexer")]), ("ai", "The lexer drops escaped quotes.")],
                         "the messages it took of its parent are not its feed, its task came as a letter whose "
                         "words are not shown, and the parent is named by its path")


EXTRAS = os.path.join(HERE, "testdata", "codex-extras.jsonl")


class CodexOwnTools(Runtime):
    """What else codex does in a turn: a picture looked at, web searches, a wait, a hook, messages of agents."""

    def setUp(self):
        super().setUp()
        shutil.copy(EXTRAS, self.rollout)

    def items(self):
        reply = chat.answer({"session": THREAD, "limit": 50})
        self.assertTrue(reply["ok"], reply)
        return reply["items"]

    def opened(self, use):
        for pos, record in records_at_of(self.rollout):
            payload = record.get("payload") or {}
            if (payload.get("item") or {}).get("id") == use or payload.get("id") == use or payload.get("call_id") == use:
                got = chat.answer({"session": THREAD, "call": {"pos": pos, "index": 0}})
                self.assertTrue(got["ok"], got)
                return got
        raise AssertionError(f"no {use} in the fixture")

    def test_each_is_a_call_among_the_calls_and_the_last_answer_of_an_agent_a_letter(self):
        items = self.items()
        calls = [(i["kind"], c["name"], c["arg"], c.get("open", False))
                 for i in items if i["role"] == "tools" for c in i["calls"]]
        self.assertEqual(calls, [
            ("files", "ViewImage", "/home/u/Projects/demo/shots/login.png", False),
            ("web", "WebSearch", "oauth pkce flow", False),
            ("web", "WebSearch", "https://example.org/pkce", False),
            ("web", "WebSearch", "verifier in https://example.org/pkce", False),
            ("time", "Sleep", "30 s", False),
            ("time", "Sleep", "3 min", False),
            ("hook", "subagent-stop hook", "Check the agents before you answer.", False),
            ("agents", "Letter", "from checker", False),
            ("agents", "Letter", "to checker", False),
        ], "a search says what it looked for, a wait how long, a hook what it said, a letter from whom or to whom")
        mail = [(i["from"], i["source"], i["text"]) for i in items if i["role"] == "mail"]
        self.assertEqual(mail, [("checker", "agent", "The login screen keeps the token in the page.")])
        self.assertNotIn("gAAAA", json.dumps(items))

    def test_a_wait_is_open_while_codex_waits(self):
        with open(EXTRAS, encoding="utf-8") as f:
            lines = f.readlines()
        at = next(n for n, line in enumerate(lines) if '"call_sleep_1"' in line)
        with open(self.rollout, "w", encoding="utf-8") as f:
            f.writelines(lines[:at + 1])
        last = [c for i in self.items() if i["role"] == "tools" for c in i["calls"]][-1]
        self.assertEqual((last["name"], last["arg"], last.get("open")), ("Sleep", "30 s", True))

    def test_each_opens_with_what_it_was_and_what_came_of_it(self):
        search = self.opened("ws-1")
        self.assertEqual((search["tool"], json.loads(search["args"])["query"]), ("WebSearch", "oauth pkce flow"))
        self.assertEqual(search["result"], "PKCE explained\nhttps://example.org/pkce\n\nOAuth 2.1\nhttps://example.org/oauth21")
        image = self.opened("view-1")
        self.assertEqual((image["tool"], json.loads(image["args"])), ("ViewImage", {"path": "/home/u/Projects/demo/shots/login.png"}))
        hook = self.opened("hook-1")
        self.assertEqual((hook["tool"], hook["result"]), ("subagent-stop hook", "Check the agents before you answer."))
        sleep = self.opened("fc_sleep_1")
        self.assertEqual((sleep["tool"], json.loads(sleep["args"])), ("Sleep", {"duration_ms": 30000}))
        letter = self.opened("am_message_13")
        self.assertEqual((letter["tool"], json.loads(letter["args"]), letter["result"]),
                         ("Letter", {"type": "MESSAGE", "from": "/root/checker", "to": "/root", "words": "encrypted"}, ""))
        self.assertNotIn("gAAAA", json.dumps(letter))


def records_at_of(path):
    """Returns the records of a rollout with the place each starts at."""
    out, pos = [], 0
    with open(path, "rb") as f:
        for raw in f:
            out.append((pos, json.loads(raw)))
            pos += len(raw)
    return out


ASYNC_QUESTIONS = os.path.join(HERE, "testdata", "codex-async-questions.jsonl")


class AsyncQuestions(Runtime):
    """The questions codex asks without waiting: a card in the feed, and a question on the row while it hangs."""

    def setUp(self):
        super().setUp()
        shutil.copy(ASYNC_QUESTIONS, self.rollout)

    def items(self):
        reply = chat.answer({"session": THREAD, "limit": 50})
        self.assertTrue(reply["ok"], reply)
        return reply["items"]

    def test_a_question_asked_without_waiting_is_a_card_of_its_questions_and_options(self):
        items = self.items()
        cards = [(i["use"], i["asks"]) for i in items if i["role"] == "question"]
        self.assertEqual(cards, [
            ("call_ask_theme", [{"title": "Which theme should the page take?", "options": ["Light", "Dark"]}]),
            ("call_ask_logo", [{"title": "Should the logo stay?", "options": []}])])
        said = [i["text"] for i in items if i["role"] == "ai"]
        self.assertEqual(said, ["I ask, then go on.", "The page is dark now.", "The header is done."],
                         "the words codex delivered with the question are the question, not an answer beside it")
        rows = [i["role"] for i in items if i["role"] in ("question", "me")]
        self.assertEqual(rows, ["me", "question", "me", "me", "question"],
                         "the answer stands after its question, where the person sent it")

    def test_a_question_nobody_answered_stands_on_the_row_and_leaves_it_with_the_next_message(self):
        self.follow(busy=True)
        row = ctx.codex_sessions()[0]
        self.assertEqual(row.get("ask"), {"header": "", "text": "Should the logo stay?", "count": 1,
                                          "at": "2026-10-09T14:00:15.000Z", "async": True},
                         "the question answered by the next message is gone, the last one hangs")
        self.assertEqual(row["status"], "busy", "codex goes on: a question it did not wait for holds nothing")
        with open(ASYNC_QUESTIONS, encoding="utf-8") as f:
            answer = json.loads(next(line for line in f if '"Dark"' in line and "UserMessage" in line))
        answer["timestamp"] = "2026-10-09T14:00:40.000Z"
        with open(self.rollout, "a", encoding="utf-8") as f:
            f.write(json.dumps(answer) + "\n")
        self.assertNotIn("ask", ctx.codex_sessions()[0])

    def test_questions_in_a_row_are_counted_together(self):
        with open(ASYNC_QUESTIONS, encoding="utf-8") as f:
            lines = f.readlines()
        asked = next(line for line in lines if '"call_ask_logo"' in line and '"async"' in line)
        with open(self.rollout, "a", encoding="utf-8") as f:
            f.write(asked.replace("call_ask_logo", "call_ask_more"))
        self.follow()
        self.assertEqual(ctx.codex_sessions()[0]["ask"]["count"], 2)


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
