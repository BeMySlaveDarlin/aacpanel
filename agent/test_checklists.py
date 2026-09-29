#!/usr/bin/env python3
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import asked  # noqa: E402
import chat  # noqa: E402
import checklists  # noqa: E402
import notes  # noqa: E402
import sesstate  # noqa: E402
from collect import live  # noqa: E402

SESSION = "abcd1234-0000-4000-8000-000000000000"
RESTARTED = "ef015678-1111-4111-8111-111111111111"
BESIDE = "0123cdef-2222-4222-8222-222222222222"

CONFIG = "/srv/claude"
LAB = "/srv/proj/lab"

# The checklist the session lab keeps; PLACED is one a server that keeps one
# checklist a place wrote, with no name in it.
CHECKLIST = {
    "configDir": CONFIG, "dir": LAB, "name": "lab",
    "sessionId": SESSION, "pid": 4242, "at": "2026-09-28T10:25:00Z", "note": "waits on the test base",
    "items": [
        {"text": "read the code", "status": "done", "since": "2026-09-28T10:10:00Z"},
        {"text": "write the tests", "status": "active", "since": "2026-09-28T10:10:00Z"},
        {"text": "mutate", "status": "pending"},
        {"text": "a second collector", "status": "dropped"},
    ],
}
PLACED = {k: v for k, v in CHECKLIST.items() if k != "name"}

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

OTHER = {**CHECKLIST, "name": "lab-review", "sessionId": BESIDE, "note": "",
         "items": [{"text": "review the change", "status": "active"}]}
OTHER_SHAPE = {"items": [{"text": "review the change", "status": "active"}], "at": "2026-09-28T10:25:00Z"}


class ChecklistOnDisk(unittest.TestCase):
    """The executor's server writes the file; the collector reads it."""

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        old = os.environ.get("XDG_STATE_HOME")
        os.environ["XDG_STATE_HOME"] = self.dir.name
        self.addCleanup(lambda: os.environ.pop("XDG_STATE_HOME", None) if old is None
                        else os.environ.__setitem__("XDG_STATE_HOME", old))

    def put(self, data, config=CONFIG, cwd=LAB, name="lab"):
        root = os.path.join(self.dir.name, "aacpanel", "checklists")
        os.makedirs(root, exist_ok=True)
        with open(os.path.join(root, checklists.name_of(config, cwd, name)), "w", encoding="utf-8") as f:
            f.write(data if isinstance(data, str) else json.dumps(data))


class Name(unittest.TestCase):
    def test_the_name_is_the_hash_the_executor_computes(self):
        # Pinned beside the executor's own test of the same session: the two
        # sides name one file.
        self.assertEqual(checklists.name_of(CONFIG, LAB, "lab"), "f7138acc90830a36ac4a7481c26dfb18.json")
        self.assertEqual(checklists.name_of(CONFIG, LAB), "d2ba143628e62863dae2533062199cf6.json",
                         "a session without a name is told by its place")
        self.assertEqual(checklists.name_of(CONFIG, LAB, ""), checklists.name_of(CONFIG, LAB))
        self.assertNotEqual(checklists.name_of(CONFIG, LAB, "lab-review"), checklists.name_of(CONFIG, LAB, "lab"),
                            "two sessions of one place share a file")
        self.assertEqual(checklists.name_of("/srv/claude/", "//srv/proj/./lab/", "lab"),
                         checklists.name_of(CONFIG, LAB, "lab"), "a place written another way is another file")

    def test_a_place_that_is_not_one_has_no_file(self):
        for config, cwd in (("srv/claude", LAB), (CONFIG, ""), (None, LAB), (CONFIG, 5)):
            self.assertIsNone(checklists.name_of(config, cwd, "lab"), (config, cwd))


class Of(ChecklistOnDisk):
    def test_the_checklist_is_read_in_the_shape_the_screens_take(self):
        self.put(CHECKLIST)
        self.assertEqual(checklists.of(CONFIG, LAB, "lab"), SHAPE,
                         "the place, the process and the conversation are the host's; the screens take the steps")

    def test_a_step_the_screens_cannot_draw_is_left_out(self):
        self.put({**CHECKLIST, "items": [{"text": "  ", "status": "active"}, {"text": "x", "status": "in_progress"},
                                    "a line", {"text": "kept", "status": "pending", "since": 5}]})
        self.assertEqual(checklists.of(CONFIG, LAB, "lab")["items"], [{"text": "kept", "status": "pending"}])

    def test_a_checklist_with_no_step_left_is_no_checklist(self):
        self.put({**CHECKLIST, "items": [{"text": "x", "status": "unknown"}]})
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab"), "an empty checklist row on a card says there is a checklist")

    def test_without_a_note_there_is_no_note(self):
        self.put({**CHECKLIST, "note": ""})
        self.assertNotIn("note", checklists.of(CONFIG, LAB, "lab"))

    def test_only_the_file_of_this_session_is_its_checklist(self):
        self.put({**CHECKLIST, "dir": "/srv/proj/other"})
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab"), "a file naming another place was read")
        self.put({**CHECKLIST, "configDir": "/srv/claude-work"})
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab"), "a file of another account was read")
        self.put({**CHECKLIST, "name": "lab-review"})
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab"), "a file naming another session was read")
        self.put(PLACED)
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab"), "a file naming no session was read as a named one")
        self.put("not json")
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab"))
        self.put(CHECKLIST)
        self.assertIsNone(checklists.of(CONFIG, "/srv/proj/lab-2", "lab"))
        self.assertIsNone(checklists.of("/srv/claude-work", LAB, "lab"))
        self.assertIsNone(checklists.of(CONFIG, "srv/proj/lab", "lab"), "a relative path was taken for a place")
        self.assertIsNone(checklists.of(CONFIG, LAB), "a session without a name read the checklist of a named one")

    def test_sessions_of_one_place_read_checklists_of_their_own(self):
        self.put(CHECKLIST)
        self.put(OTHER, name="lab-review")
        self.assertEqual(checklists.of(CONFIG, LAB, "lab", sid=SESSION), SHAPE)
        self.assertEqual(checklists.of(CONFIG, LAB, "lab-review", sid=BESIDE), OTHER_SHAPE)
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab-tests", sid=RESTARTED))

    def test_a_session_started_again_under_its_name_reads_its_checklist(self):
        self.put(CHECKLIST)
        self.assertEqual(checklists.of(CONFIG, LAB, "lab", sid=RESTARTED), SHAPE)

    def test_a_session_without_a_name_reads_the_checklist_of_its_place_whoever_sent_it(self):
        self.put(PLACED, name=None)
        self.assertEqual(checklists.of(CONFIG, LAB), SHAPE)
        self.assertEqual(checklists.of(CONFIG, LAB, None, sid=RESTARTED), SHAPE)

    def test_a_named_session_reads_the_checklist_of_its_place_only_while_it_sent_it(self):
        # A server that does not tell the sessions of a place apart
        # writes the checklist of the place: which session sent it is told
        # only by the conversation.
        self.put(PLACED, name=None)
        self.assertEqual(checklists.of(CONFIG, LAB, "lab", sid=SESSION), SHAPE)
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab-review", sid=BESIDE),
                          "another session of the place showed the checklist it did not send")
        self.assertIsNone(checklists.of(CONFIG, LAB, "lab"), "a session of no known conversation was given one")
        self.put(OTHER, name="lab-review")
        self.assertEqual(checklists.of(CONFIG, LAB, "lab-review", sid=BESIDE), OTHER_SHAPE,
                         "the checklist of its own was passed over for the place's")


class OfConversation(ChecklistOnDisk):
    """A conversation that is over shows the checklist it left: its session's or its place's, while it sent it last."""

    def test_a_conversation_shows_the_checklist_it_sent_last_under_any_name(self):
        self.put(CHECKLIST)
        self.put(OTHER, name="lab-review")
        self.assertEqual(checklists.of_conversation(CONFIG, LAB, SESSION), SHAPE)
        self.assertEqual(checklists.of_conversation(CONFIG, LAB, BESIDE), OTHER_SHAPE)
        self.assertIsNone(checklists.of_conversation(CONFIG, LAB, RESTARTED))

    def test_a_checklist_of_the_place_is_shown_to_the_conversation_that_sent_it(self):
        self.put(PLACED, name=None)
        self.assertEqual(checklists.of_conversation(CONFIG, LAB, SESSION), SHAPE)

    def test_a_later_conversation_takes_the_checklist_from_it(self):
        self.put({**CHECKLIST, "sessionId": RESTARTED})
        self.assertIsNone(checklists.of_conversation(CONFIG, LAB, SESSION))

    def test_a_file_of_another_place_or_lying_under_another_name_is_not_read(self):
        self.put({**CHECKLIST, "dir": "/srv/proj/other"})
        self.assertIsNone(checklists.of_conversation(CONFIG, LAB, SESSION))
        self.put(CHECKLIST, name="lab-review")
        self.assertIsNone(checklists.of_conversation(CONFIG, LAB, SESSION),
                          "a file that lies where another session's does was read")
        self.assertIsNone(checklists.of_conversation(CONFIG, "srv/proj/lab", SESSION))


class ChecklistOnTheCard(ChecklistOnDisk):
    """The checklist reaches the snapshot, which is all the panel reads of a session."""

    def setUp(self):
        super().setUp()
        board = notes.Board(os.path.join(self.dir.name, "notes.json"))
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = board
        self.rows = {"sessions": [{"session": "lab", "sessionId": SESSION, "cwd": LAB}]}
        self.places = {SESSION: (CONFIG, LAB, "lab")}
        for name, value in (("sessions", lambda: self.rows), ("session_profiles", dict),
                            ("session_births", dict), ("live_session_waits", dict),
                            ("live_session_status", dict), ("live_session_status_at", dict),
                            ("live_session_places", lambda: self.places)):
            holder = live.ctx if name == "sessions" else live
            self.addCleanup(setattr, holder, name, getattr(holder, name))
            setattr(holder, name, value)

    def only(self):
        return live.sessions()["sessions"][0]

    def test_a_session_that_keeps_a_checklist_carries_it(self):
        self.put(CHECKLIST)
        self.assertEqual(self.only().get("checklist"), SHAPE)

    def test_a_session_without_a_checklist_carries_nothing(self):
        self.assertNotIn("checklist", self.only(), "an empty checklist on every card draws a row of nothing")

    def test_a_session_started_again_under_its_name_shows_the_checklist_at_once(self):
        # The restart is another conversation under the same name; it has not
        # sent the checklist yet, and the card shows what the one before it
        # left.
        self.put(CHECKLIST)
        self.rows = {"sessions": [{"session": "lab", "sessionId": RESTARTED, "cwd": LAB}]}
        self.places = {RESTARTED: (CONFIG, LAB, "lab")}
        self.assertEqual(self.only().get("checklist"), SHAPE)

    def test_two_sessions_of_one_directory_show_checklists_of_their_own(self):
        self.put(CHECKLIST)
        self.rows = {"sessions": [{"session": "lab", "sessionId": SESSION, "cwd": LAB},
                                  {"session": "lab-review", "sessionId": BESIDE, "cwd": LAB},
                                  {"session": "lab-tests", "sessionId": RESTARTED, "cwd": LAB}]}
        self.places = {SESSION: (CONFIG, LAB, "lab"), BESIDE: (CONFIG, LAB, "lab-review"),
                       RESTARTED: (CONFIG, LAB, "lab-tests")}
        self.put(OTHER, name="lab-review")
        got = {s["session"]: s.get("checklist") for s in live.sessions()["sessions"]}
        self.assertEqual(got, {"lab": SHAPE, "lab-review": OTHER_SHAPE, "lab-tests": None})

    def test_a_checklist_of_the_place_is_shown_only_by_the_session_that_sent_it(self):
        self.put(PLACED, name=None)
        self.rows = {"sessions": [{"session": "lab", "sessionId": SESSION, "cwd": LAB},
                                  {"session": "lab-review", "sessionId": BESIDE, "cwd": LAB}]}
        self.places = {SESSION: (CONFIG, LAB, "lab"), BESIDE: (CONFIG, LAB, "lab-review")}
        got = {s["session"]: s.get("checklist") for s in live.sessions()["sessions"]}
        self.assertEqual(got, {"lab": SHAPE, "lab-review": None})

    def test_a_session_of_another_place_does_not_show_it(self):
        self.put(CHECKLIST)
        self.places = {SESSION: (CONFIG, "/srv/proj/other", "lab")}
        self.assertNotIn("checklist", self.only())
        self.places = {}
        self.assertNotIn("checklist", self.only(), "a session not placed showed a checklist")


class LivePlaces(unittest.TestCase):
    """The place and the name of a live session are read from the file claude keeps of it, and its process."""

    def setUp(self):
        self.files = [
            ("personal", CONFIG, {"pid": 11, "sessionId": SESSION, "cwd": LAB, "procStart": "100", "name": "lab"}),
            ("personal", CONFIG, {"pid": 12, "sessionId": "dead", "cwd": LAB, "procStart": "100"}),
            ("personal", CONFIG, {"pid": 13, "sessionId": "nowhere", "procStart": "100"}),
            ("personal", CONFIG, {"pid": 14, "sessionId": RESTARTED, "cwd": LAB, "procStart": "100"}),
            ("personal", CONFIG, {"pid": 15, "sessionId": BESIDE, "cwd": LAB, "procStart": "100", "name": ""}),
        ]
        self.args = {11: ["claude", "-n", "lab-old"], 14: ["claude", "--resume", RESTARTED, "--name=lab-review"],
                     15: ["claude"]}
        for holder, name, value in ((live, "session_files", lambda: self.files),
                                    (live.ctx, "proc_start", lambda pid: "100" if pid in (11, 13, 14, 15) else None),
                                    (live.ctx, "proc_args", lambda pid: self.args.get(pid, []))):
            self.addCleanup(setattr, holder, name, getattr(holder, name))
            setattr(holder, name, value)

    def test_a_live_session_is_placed_and_named_by_its_file_and_else_by_its_process(self):
        self.assertEqual(live.live_session_places(), {
            SESSION: (CONFIG, LAB, "lab"),
            RESTARTED: (CONFIG, LAB, "lab-review"),
            BESIDE: (CONFIG, LAB, None),
        })

    def test_the_name_is_taken_in_either_form_claude_takes(self):
        for args, name in ((["claude", "-n", "lab"], "lab"), (["claude", "--name", "lab"], "lab"),
                           (["claude", "-n=lab"], "lab"), (["claude", "--name="], None), (["claude", "-n"], None),
                           ([], None)):
            self.args[14] = args
            self.assertEqual(live.session_name({"pid": 14}), name, args)


class ChecklistInTheConversation(ChecklistOnDisk):
    """The state of an open conversation carries the checklist the feed draws."""

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
        self.places = {SESSION: (CONFIG, LAB, "lab")}
        self.addCleanup(setattr, live, "live_session_places", live.live_session_places)
        live.live_session_places = lambda: self.places

    def state(self):
        return chat.answer({"session": SESSION, "limit": 20, "state": True})["state"]

    def test_the_state_carries_the_checklist(self):
        self.put(CHECKLIST)
        self.assertEqual(self.state().get("checklist"), SHAPE)

    def test_a_live_conversation_shows_the_checklist_its_session_keeps_whoever_sent_it(self):
        self.put({**CHECKLIST, "sessionId": RESTARTED})
        self.assertEqual(self.state().get("checklist"), SHAPE)

    def test_a_live_conversation_does_not_show_the_checklist_of_another_session_of_its_place(self):
        self.put(OTHER, name="lab-review")
        self.assertNotIn("checklist", self.state())
        self.put({**PLACED, "sessionId": BESIDE}, name=None)
        self.assertNotIn("checklist", self.state(), "the checklist another conversation left in the place was shown")
        self.put(PLACED, name=None)
        self.assertEqual(self.state().get("checklist"), SHAPE)

    def test_a_conversation_without_a_checklist_carries_none(self):
        self.assertNotIn("checklist", self.state())

    def test_a_conversation_that_is_over_shows_the_checklist_only_while_it_sent_it_last(self):
        # Placed by its transcript: the account the transcript lies under and
        # the directory it names; the name is the one its checklist bears.
        self.places = {}
        self.put({**CHECKLIST, "configDir": self.dir.name}, config=self.dir.name)
        self.assertEqual(self.state().get("checklist"), SHAPE)
        self.put({**CHECKLIST, "configDir": self.dir.name, "sessionId": RESTARTED}, config=self.dir.name)
        self.assertNotIn("checklist", self.state(), "an old conversation showed the checklist a later session keeps")
        self.put({**PLACED, "configDir": self.dir.name}, config=self.dir.name, name=None)
        self.assertEqual(self.state().get("checklist"), SHAPE, "the checklist it left in its place was not shown")

    def test_a_window_of_the_feed_alone_reads_no_checklist(self):
        self.put(CHECKLIST)
        self.assertNotIn("state", chat.answer({"session": SESSION, "limit": 20}))


if __name__ == "__main__":
    unittest.main()
