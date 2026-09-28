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
RESTARTED = "ef015678-1111-4111-8111-111111111111"

CONFIG = "/srv/claude"
LAB = "/srv/proj/lab"

PLAN = {
    "configDir": CONFIG, "dir": LAB,
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

    def put(self, data, config=CONFIG, cwd=LAB):
        root = os.path.join(self.dir.name, "aacpanel", "plans")
        os.makedirs(root, exist_ok=True)
        with open(os.path.join(root, plans.name_of(config, cwd)), "w", encoding="utf-8") as f:
            f.write(data if isinstance(data, str) else json.dumps(data))


class Name(unittest.TestCase):
    def test_the_name_is_the_hash_the_executor_computes(self):
        # Pinned beside the executor's own test of the same place: the two
        # sides name one file.
        self.assertEqual(plans.name_of(CONFIG, LAB), "d2ba143628e62863dae2533062199cf6.json")
        self.assertEqual(plans.name_of("/srv/claude/", "//srv/proj/./lab/"), plans.name_of(CONFIG, LAB),
                         "a place written another way is another file")

    def test_a_place_that_is_not_one_has_no_file(self):
        for config, cwd in (("srv/claude", LAB), (CONFIG, ""), (None, LAB), (CONFIG, 5)):
            self.assertIsNone(plans.name_of(config, cwd), (config, cwd))


class Of(PlanOnDisk):
    def test_the_plan_is_read_in_the_shape_the_screens_take(self):
        self.put(PLAN)
        self.assertEqual(plans.of(CONFIG, LAB), SHAPE,
                         "the place, the process and the conversation are the host's; the screens take the steps")

    def test_a_step_the_screens_cannot_draw_is_left_out(self):
        self.put({**PLAN, "items": [{"text": "  ", "status": "active"}, {"text": "x", "status": "in_progress"},
                                    "a line", {"text": "kept", "status": "pending", "since": 5}]})
        self.assertEqual(plans.of(CONFIG, LAB)["items"], [{"text": "kept", "status": "pending"}])

    def test_a_plan_with_no_step_left_is_no_plan(self):
        self.put({**PLAN, "items": [{"text": "x", "status": "unknown"}]})
        self.assertIsNone(plans.of(CONFIG, LAB), "an empty plan row on a card says there is a plan")

    def test_without_a_note_there_is_no_note(self):
        self.put({**PLAN, "note": ""})
        self.assertNotIn("note", plans.of(CONFIG, LAB))

    def test_only_the_file_of_this_place_is_its_plan(self):
        self.put({**PLAN, "dir": "/srv/proj/other"})
        self.assertIsNone(plans.of(CONFIG, LAB), "a file naming another place was read")
        self.put({**PLAN, "configDir": "/srv/claude-work"})
        self.assertIsNone(plans.of(CONFIG, LAB), "a file of another account was read")
        self.put("not json")
        self.assertIsNone(plans.of(CONFIG, LAB))
        self.put(PLAN)
        self.assertIsNone(plans.of(CONFIG, "/srv/proj/lab-2"))
        self.assertIsNone(plans.of("/srv/claude-work", LAB))
        self.assertIsNone(plans.of(CONFIG, "srv/proj/lab"), "a relative path was taken for a place")

    def test_a_conversation_named_sees_the_plan_only_while_it_sent_it_last(self):
        self.put(PLAN)
        self.assertEqual(plans.of(CONFIG, LAB, sid=SESSION), SHAPE)
        self.assertIsNone(plans.of(CONFIG, LAB, sid=RESTARTED))


class PlanOnTheCard(PlanOnDisk):
    """The plan reaches the snapshot, which is all the panel reads of a session."""

    def setUp(self):
        super().setUp()
        board = notes.Board(os.path.join(self.dir.name, "notes.json"))
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = board
        self.rows = {"sessions": [{"session": "lab", "sessionId": SESSION, "cwd": LAB}]}
        self.places = {SESSION: (CONFIG, LAB)}
        for name, value in (("sessions", lambda: self.rows), ("session_profiles", dict),
                            ("session_births", dict), ("live_session_waits", dict),
                            ("live_session_status", dict), ("live_session_status_at", dict),
                            ("live_session_places", lambda: self.places)):
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

    def test_a_session_started_again_in_the_place_shows_the_plan_at_once(self):
        # The restart is another conversation in the same place; it has not
        # sent the plan yet, and the card shows what the one before it left.
        self.put(PLAN)
        self.rows = {"sessions": [{"session": "lab", "sessionId": RESTARTED, "cwd": LAB}]}
        self.places = {RESTARTED: (CONFIG, LAB)}
        self.assertEqual(self.only().get("plan"), SHAPE)

    def test_a_session_of_another_place_does_not_show_it(self):
        self.put(PLAN)
        self.places = {SESSION: (CONFIG, "/srv/proj/other")}
        self.assertNotIn("plan", self.only())
        self.places = {}
        self.assertNotIn("plan", self.only(), "a session not placed showed a plan")


class LivePlaces(unittest.TestCase):
    """The place of a live session is read from the file claude keeps of it: where it lies and what it names."""

    def setUp(self):
        self.files = [
            ("personal", CONFIG, {"pid": 11, "sessionId": SESSION, "cwd": LAB, "procStart": "100"}),
            ("personal", CONFIG, {"pid": 12, "sessionId": "dead", "cwd": LAB, "procStart": "100"}),
            ("personal", CONFIG, {"pid": 13, "sessionId": "nowhere", "procStart": "100"}),
        ]
        for holder, name, value in ((live, "session_files", lambda: self.files),
                                    (live.ctx, "proc_start", lambda pid: "100" if pid in (11, 13) else None)):
            self.addCleanup(setattr, holder, name, getattr(holder, name))
            setattr(holder, name, value)

    def test_a_live_session_is_placed_by_its_file(self):
        self.assertEqual(live.live_session_places(), {SESSION: (CONFIG, LAB)})


class PlanInTheConversation(PlanOnDisk):
    """The state of an open conversation carries the plan the feed draws."""

    def setUp(self):
        super().setUp()
        self.projects = os.path.join(self.dir.name, "projects")
        os.makedirs(os.path.join(self.projects, "-srv-proj-lab"))
        with open(os.path.join(self.projects, "-srv-proj-lab", f"{SESSION}.jsonl"), "w", encoding="utf-8") as f:
            f.write(json.dumps({"type": "user", "message": {"content": "hello"}, "cwd": LAB,
                                "timestamp": "2026-09-28T10:00:00Z"}) + "\n")
        self.old_projects, chat.PROJECTS_DIR = chat.PROJECTS_DIR, self.projects
        self.addCleanup(lambda: setattr(chat, "PROJECTS_DIR", self.old_projects))
        self.old_book, asked.BOOK = asked.BOOK, asked.Book(os.path.join(self.dir.name, "asked.json"))
        self.addCleanup(lambda: setattr(asked, "BOOK", self.old_book))
        self.old_cache, sesstate.SHARED = sesstate.SHARED, sesstate.Cache()
        self.addCleanup(lambda: setattr(sesstate, "SHARED", self.old_cache))
        self.places = {SESSION: (CONFIG, LAB)}
        self.addCleanup(setattr, live, "live_session_places", live.live_session_places)
        live.live_session_places = lambda: self.places

    def state(self):
        return chat.answer({"session": SESSION, "limit": 20, "state": True})["state"]

    def test_the_state_carries_the_plan(self):
        self.put(PLAN)
        self.assertEqual(self.state().get("plan"), SHAPE)

    def test_a_live_conversation_shows_the_plan_its_place_keeps_whoever_sent_it(self):
        self.put({**PLAN, "sessionId": RESTARTED})
        self.assertEqual(self.state().get("plan"), SHAPE)

    def test_a_conversation_without_a_plan_carries_none(self):
        self.assertNotIn("plan", self.state())

    def test_a_conversation_that_is_over_shows_the_plan_only_while_it_sent_it_last(self):
        # Placed by its transcript: the account the transcript lies under and
        # the directory it names.
        self.places = {}
        self.put({**PLAN, "configDir": self.dir.name}, config=self.dir.name)
        self.assertEqual(self.state().get("plan"), SHAPE)
        self.put({**PLAN, "configDir": self.dir.name, "sessionId": RESTARTED}, config=self.dir.name)
        self.assertNotIn("plan", self.state(), "an old conversation showed the plan a later session keeps")

    def test_a_window_of_the_feed_alone_reads_no_plan(self):
        self.put(PLAN)
        self.assertNotIn("state", chat.answer({"session": SESSION, "limit": 20}))


if __name__ == "__main__":
    unittest.main()
