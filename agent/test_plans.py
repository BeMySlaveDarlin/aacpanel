#!/usr/bin/env python3
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import asked  # noqa: E402
import chat  # noqa: E402
import notes  # noqa: E402
import plans  # noqa: E402
import sesstate  # noqa: E402
from collect import live  # noqa: E402

SESSION = "abcd1234-0000-4000-8000-000000000000"

PLAN = {
    "sessionId": SESSION, "pid": 4242, "at": "2026-09-28T10:25:00Z", "note": "waits on the test base",
    "items": [
        {"text": "read the code", "status": "done", "since": "2026-09-28T10:10:00Z"},
        {"text": "write the tests", "status": "active", "since": "2026-09-28T10:10:00Z"},
        {"text": "mutate", "status": "pending"},
        {"text": "a second collector", "status": "dropped"},
    ],
}

SHAPE = {
    "items": [
        {"text": "read the code", "status": "done", "since": "2026-09-28T10:10:00Z"},
        {"text": "write the tests", "status": "active", "since": "2026-09-28T10:10:00Z"},
        {"text": "mutate", "status": "pending"},
        {"text": "a second collector", "status": "dropped"},
    ],
    "at": "2026-09-28T10:25:00Z",
    "note": "waits on the test base",
}


class PlanOnDisk(unittest.TestCase):
    """The executor's plan server writes the file; the collector reads it."""

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        old = os.environ.get("XDG_STATE_HOME")
        os.environ["XDG_STATE_HOME"] = self.dir.name
        self.addCleanup(lambda: os.environ.pop("XDG_STATE_HOME", None) if old is None
                        else os.environ.__setitem__("XDG_STATE_HOME", old))

    def put(self, data, sid=SESSION):
        root = os.path.join(self.dir.name, "aacpanel", "plans")
        os.makedirs(root, exist_ok=True)
        with open(os.path.join(root, sid + ".json"), "w", encoding="utf-8") as f:
            f.write(data if isinstance(data, str) else json.dumps(data))


class Of(PlanOnDisk):
    def test_the_plan_is_read_in_the_shape_the_screens_take(self):
        self.put(PLAN)
        self.assertEqual(plans.of(SESSION), SHAPE,
                         "the process and the conversation are the host's; the screens take the steps")

    def test_a_step_the_screens_cannot_draw_is_left_out(self):
        self.put({**PLAN, "items": [{"text": "  ", "status": "active"}, {"text": "x", "status": "in_progress"},
                                    "a line", {"text": "kept", "status": "pending", "since": 5}]})
        self.assertEqual(plans.of(SESSION)["items"], [{"text": "kept", "status": "pending"}])

    def test_a_plan_with_no_step_left_is_no_plan(self):
        self.put({**PLAN, "items": [{"text": "x", "status": "unknown"}]})
        self.assertIsNone(plans.of(SESSION), "an empty plan row on a card says there is a plan")

    def test_without_a_note_there_is_no_note(self):
        self.put({**PLAN, "note": ""})
        self.assertNotIn("note", plans.of(SESSION))

    def test_only_the_file_of_this_conversation_is_its_plan(self):
        self.put({**PLAN, "sessionId": "another"})
        self.assertIsNone(plans.of(SESSION))
        self.put("not json")
        self.assertIsNone(plans.of(SESSION))
        self.assertIsNone(plans.of("../" + SESSION), "a path passed for a conversation was read")
        self.assertIsNone(plans.of(""))
        self.assertIsNone(plans.of("77777777-7777-4777-8777-777777777777"))


class PlanOnTheCard(PlanOnDisk):
    """The plan reaches the snapshot, which is all the panel reads of a session."""

    def setUp(self):
        super().setUp()
        board = notes.Board(os.path.join(self.dir.name, "notes.json"))
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = board
        rows = {"sessions": [{"session": "lab", "sessionId": SESSION, "cwd": "/srv/proj/lab"}]}
        for name, value in (("sessions", lambda: rows), ("session_profiles", dict),
                            ("session_births", dict), ("live_session_waits", dict),
                            ("live_session_status", dict), ("live_session_status_at", dict)):
            holder = live.ctx if name == "sessions" else live
            self.addCleanup(setattr, holder, name, getattr(holder, name))
            setattr(holder, name, value)

    def only(self):
        return live.sessions()["sessions"][0]

    def test_a_session_that_keeps_a_plan_carries_it(self):
        self.put(PLAN)
        self.assertEqual(self.only().get("plan"), SHAPE)

    def test_a_session_without_a_plan_carries_nothing(self):
        self.assertNotIn("plan", self.only(), "an empty plan on every card draws a row of nothing")


class PlanInTheConversation(PlanOnDisk):
    """The state of an open conversation carries the plan the feed draws."""

    def setUp(self):
        super().setUp()
        self.projects = os.path.join(self.dir.name, "projects")
        os.makedirs(os.path.join(self.projects, "-srv-proj-lab"))
        with open(os.path.join(self.projects, "-srv-proj-lab", f"{SESSION}.jsonl"), "w", encoding="utf-8") as f:
            f.write(json.dumps({"type": "user", "message": {"content": "hello"},
                                "timestamp": "2026-09-28T10:00:00Z"}) + "\n")
        self.old_projects, chat.PROJECTS_DIR = chat.PROJECTS_DIR, self.projects
        self.addCleanup(lambda: setattr(chat, "PROJECTS_DIR", self.old_projects))
        self.old_book, asked.BOOK = asked.BOOK, asked.Book(os.path.join(self.dir.name, "asked.json"))
        self.addCleanup(lambda: setattr(asked, "BOOK", self.old_book))
        self.old_cache, sesstate.SHARED = sesstate.SHARED, sesstate.Cache()
        self.addCleanup(lambda: setattr(sesstate, "SHARED", self.old_cache))

    def test_the_state_carries_the_plan(self):
        self.put(PLAN)
        reply = chat.answer({"session": SESSION, "limit": 20, "state": True})
        self.assertEqual(reply["state"].get("plan"), SHAPE)

    def test_a_conversation_without_a_plan_carries_none(self):
        reply = chat.answer({"session": SESSION, "limit": 20, "state": True})
        self.assertNotIn("plan", reply["state"])

    def test_a_window_of_the_feed_alone_reads_no_plan(self):
        self.put(PLAN)
        self.assertNotIn("state", chat.answer({"session": SESSION, "limit": 20}))


if __name__ == "__main__":
    unittest.main()
