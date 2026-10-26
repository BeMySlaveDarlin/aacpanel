import datetime
import json
import os
import unittest

import test_barrier
import agent
import asked
import sesstate
import sessionfile
import turns
from collect import live


class Turns(unittest.TestCase):
    def setUp(self):
        self.tmp = test_barrier.tmp_dir()
        self.addCleanup(self.tmp.cleanup)
        self.old = os.environ.get("XDG_STATE_HOME")
        os.environ["XDG_STATE_HOME"] = self.tmp.name
        self.addCleanup(lambda: os.environ.pop("XDG_STATE_HOME", None) if self.old is None
                        else os.environ.__setitem__("XDG_STATE_HOME", self.old))
        self.prompt = "2026-09-28T10:00:00Z"
        self.at = int(datetime.datetime.fromisoformat(self.prompt).timestamp() * 1000)
        self.place = ("/srv/claude", "/srv/proj/lab", "lab")
        self.mark = {"configDir": self.place[0], "dir": self.place[1], "name": self.place[2],
                     "sessionId": "conversation", "pid": 4242, "at": self.at + 1000,
                     "next": "owner", "what": "Choose the release window"}
        self.transcript = os.path.join(self.tmp.name, "conversation.jsonl")
        self.append({"type": "user", "timestamp": self.prompt, "message": {"content": "ship it"}})
        self.replace(agent, "SESSION_STATE", sesstate.Cache())
        self.replace(asked, "BOOK", asked.Book(os.path.join(self.tmp.name, "asked.json")))
        self.replace(live.ctx, "sessions", lambda: {"sessions": [{
            "session": "lab", "sessionId": "conversation", "cwd": self.place[1],
            "transcript": self.transcript, "status": "idle"}]})
        for name in ("session_profiles", "session_births", "live_session_waits", "live_session_status",
                     "live_session_status_at", "live_session_remote", "live_session_version"):
            self.replace(live, name, dict)
        self.replace(live, "live_session_places", lambda: {"conversation": self.place})

    def replace(self, obj, name, value):
        self.addCleanup(setattr, obj, name, getattr(obj, name))
        setattr(obj, name, value)

    def append(self, record):
        with open(self.transcript, "a", encoding="utf-8") as f:
            f.write(json.dumps(record) + "\n")

    def put(self, **changes):
        root = sessionfile.state_dir("turns")
        os.makedirs(root, exist_ok=True)
        with open(os.path.join(root, sessionfile.file_name(*self.place)), "w", encoding="utf-8") as f:
            json.dump({**self.mark, **changes}, f)

    def row(self):
        return live.sessions()["sessions"][0]

    def test_a_fresh_declaration_reaches_the_row_without_changing_idle(self):
        for next_move in ("owner", "self", "done"):
            self.put(next=next_move)
            row = self.row()
            self.assertEqual(row["status"], "idle")
            expected = {"next": next_move, "what": self.mark["what"], "at": self.mark["at"]}
            if next_move == "self":
                expected["stalled"] = True
            self.assertEqual(row["turn"], expected)

    def test_a_human_prompt_expires_the_declaration(self):
        self.put()
        self.assertIn("turn", self.row())
        self.append({"type": "user", "timestamp": "2026-09-28T10:00:02Z", "message": {"content": "tomorrow"}})
        self.assertNotIn("turn", self.row())
        self.put(at=self.at + 2000)
        self.assertNotIn("turn", self.row(), "equal times cannot prove the declaration followed the prompt")

    def test_notifications_and_compaction_do_not_answer_a_request(self):
        self.put()
        for flag in ("isMeta", "isCompactSummary", "isSidechain"):
            self.append({"type": "user", flag: True, "timestamp": "2026-09-28T10:00:02Z",
                         "message": {"content": "background work reported"}})
        self.append({"type": "user", "timestamp": "2026-09-28T10:00:03Z",
                     "message": {"content": [{"type": "tool_result", "tool_use_id": "tool", "content": "done"}]}})
        self.assertIn("turn", self.row())
        self.assertEqual(agent.SESSION_STATE.state(self.transcript).prompted, self.prompt)

    def test_a_declaration_belongs_to_its_conversation_and_place(self):
        for field, value in (("sessionId", "another"), ("configDir", "/other"), ("dir", "/other"), ("name", "other")):
            self.put(**{field: value})
            self.assertNotIn("turn", self.row(), field)

    def test_a_missing_or_invalid_declaration_is_absent(self):
        self.assertNotIn("turn", self.row())
        for changes in ({"at": True}, {"at": "now"}, {"at": 0}, {"next": "later"}, {"what": " "}, {"what": 42}):
            self.put(**changes)
            self.assertNotIn("turn", self.row(), changes)

    def test_a_notification_does_not_clear_an_unanswered_question(self):
        asked.BOOK.put({"sessionId": "conversation", "questions": [{"text": "Which release?", "header": "Release"}],
                        "toolUseId": "tool-question", "at": "2026-09-28T10:00:01Z"})
        self.append({"type": "user", "isMeta": True, "timestamp": "2026-09-28T10:00:02Z",
                     "message": {"content": "background work reported"}})
        self.assertIn("ask", self.row())
        self.append({"type": "user", "timestamp": "2026-09-28T10:00:03Z", "message": {"content": "tomorrow"}})
        self.assertNotIn("ask", self.row())


class Reconcile(unittest.TestCase):
    def row(self, next_move="self", status="idle", **fields):
        return {"session": "lab", "status": status, "turn": {"next": next_move, "at": 1}, **fields}

    def test_each_source_of_a_wake_keeps_a_self_declaration_waiting(self):
        for key in ("tasks", "agents", "wakes", "workflows", "observers", "alarms"):
            with self.subTest(key=key):
                row = self.row(work={key: 1})
                turns.reconcile([row])
                self.assertNotIn("stalled", row["turn"])

    def test_no_wake_marks_only_an_idle_self_declaration(self):
        for next_move in ("owner", "self", "done"):
            for status in ("idle", "busy", "waiting"):
                row = self.row(next_move, status)
                turns.reconcile([row])
                self.assertEqual(row["turn"].get("stalled", False), next_move == "self" and status == "idle")
                self.assertEqual(row["status"], status)

    def test_a_child_counts_before_or_after_its_parent_without_recursion(self):
        for status, next_move, wakes in (("busy", "done", True), ("waiting", "done", True),
                                         ("idle", "self", True), ("idle", "owner", True),
                                         ("idle", None, True), ("idle", "done", False)):
            for child_first in (False, True):
                parent = self.row()
                child = self.row(next_move, status, session="helper", parent={"session": "lab"})
                rows = [child, parent] if child_first else [parent, child]
                turns.reconcile(rows)
                self.assertEqual(parent["turn"].get("stalled", False), not wakes, (status, next_move, child_first))
                if next_move == "self":
                    self.assertTrue(child["turn"]["stalled"], "the child's own stall was hidden by its parent")

    def test_a_child_of_someone_else_does_not_wake_this_session(self):
        parent = self.row()
        child = self.row("self", session="helper", parent={"session": "another"})
        turns.reconcile([parent, child])
        self.assertTrue(parent["turn"]["stalled"])

    def test_a_new_wake_or_declaration_clears_a_previous_stall(self):
        row = self.row()
        turns.reconcile([row])
        self.assertTrue(row["turn"]["stalled"])
        row["work"] = {"alarms": 1}
        turns.reconcile([row])
        self.assertNotIn("stalled", row["turn"])
        for next_move in ("owner", "done"):
            row["turn"] = {"next": next_move, "stalled": True}
            turns.reconcile([row])
            self.assertNotIn("stalled", row["turn"])

    def test_codex_rows_are_not_declared_stalled(self):
        row = self.row(agent="codex")
        turns.reconcile([row])
        self.assertNotIn("stalled", row["turn"])
