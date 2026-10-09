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
        self.assertNotIn("state", chat.answer({"session": THREAD, "limit": 50, "state": True}),
                         "the state of a session is claude's")

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


if __name__ == "__main__":
    unittest.main()
