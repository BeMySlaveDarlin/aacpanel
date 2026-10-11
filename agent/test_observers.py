#!/usr/bin/env python3
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import asked  # noqa: E402
import chat  # noqa: E402
import observers  # noqa: E402
import sesstate  # noqa: E402
from collect import live  # noqa: E402

SESSION = "abcd1234-0000-4000-8000-000000000000"
OTHER = "ef015678-1111-4111-8111-111111111111"
THREAD = "019a1f00-0000-7000-8000-00000000abcd"
LAB = "/srv/proj/lab"

WOKE = "0123456789abcdef"
WAITS = "00112233445566aa"
ENDED = "ffeeddccbbaa0099"

# The record of an observer as the executor writes it: a claude session owns it
# by its process and its conversation, and the spec keeps the session's PATH,
# which the screens never see.
def record(oid, name, wake="line", conversation=SESSION, **more):
    spec = {"name": name, "command": f"run {name}", "dir": LAB, "wake": wake, "gather": 10,
            "text": "look at it", "path": "/usr/bin:/home/u/bin"}
    if wake == "match":
        spec["pattern"] = "^FAIL"
    return {"id": oid, "spec": spec, "started": "2026-10-11T20:02:00+03:00",
            "owner": {"session": "lab", "conversation": conversation, "pid": 4242, "procStart": "77"},
            "offset": 0, "woken": 0, **more}


def alarm(oid, name, conversation=SESSION, **more):
    return {"id": oid, "name": name, "owner": {"session": "lab", "conversation": conversation},
            "set": "2026-10-11T20:00:00+03:00", "next": "2026-10-11T21:00:00+03:00", "rang": 0, **more}


class OnDisk(unittest.TestCase):
    """The executor writes the files; the collector reads them."""

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        old = os.environ.get("XDG_STATE_HOME")
        os.environ["XDG_STATE_HOME"] = self.dir.name
        self.addCleanup(lambda: os.environ.pop("XDG_STATE_HOME", None) if old is None
                        else os.environ.__setitem__("XDG_STATE_HOME", old))

    def put(self, rec, files=None):
        path = os.path.join(self.dir.name, "aacpanel", "observers", rec["id"])
        os.makedirs(path, exist_ok=True)
        with open(os.path.join(path, "observer.json"), "w", encoding="utf-8") as f:
            f.write(json.dumps(rec))
        for name, body in (files or {}).items():
            with open(os.path.join(path, name), "wb") as f:
                f.write(body if isinstance(body, bytes) else body.encode())

    def ring(self, rec):
        path = os.path.join(self.dir.name, "aacpanel", "alarms")
        os.makedirs(path, exist_ok=True)
        with open(os.path.join(path, rec["id"] + ".json"), "w", encoding="utf-8") as f:
            f.write(json.dumps(rec))


class Of(OnDisk):
    def test_the_observers_of_a_conversation_are_read_in_the_shape_the_screens_take(self):
        self.put(record(WOKE, "ci", wake="match", woken=2, lastWake="2026-10-11T20:30:00+03:00"))
        self.put(record(WAITS, "wake-2030msk", wake="exit"))
        got = {v["name"]: v for v in observers.of(SESSION)}
        self.assertEqual(got["ci"], {"id": WOKE, "name": "ci", "command": "run ci", "dir": LAB, "wake": "match",
                                     "pattern": "^FAIL", "at": "2026-10-11T20:02:00+03:00", "woken": 2,
                                     "lastWake": "2026-10-11T20:30:00+03:00"})
        self.assertEqual(got["wake-2030msk"]["woken"], 0)
        self.assertNotIn("lastWake", got["wake-2030msk"], "an observer that never woke its session says when it did")
        for view in got.values():
            self.assertNotIn("text", view)
            self.assertNotIn("path", view, "the PATH of the session went to the screens")

    def test_the_oldest_comes_first(self):
        self.put({**record(WOKE, "late"), "started": "2026-10-11T21:00:00+03:00"})
        self.put({**record(WAITS, "early"), "started": "2026-10-11T19:00:00+03:00"})
        self.assertEqual([v["name"] for v in observers.of(SESSION)], ["early", "late"])

    def test_only_the_observers_of_the_conversation_are_its_own(self):
        self.put(record(WOKE, "ci"))
        self.put(record(WAITS, "theirs", conversation=OTHER))
        self.put(record(ENDED, "codex", conversation=THREAD))
        self.assertEqual([v["name"] for v in observers.of(SESSION)], ["ci"])
        self.assertEqual([v["name"] for v in observers.of(THREAD)], ["codex"], "a thread is told by its thread")
        self.assertEqual(observers.of(""), [])
        self.assertEqual(observers.of(None), [])

    def test_a_command_that_ended_says_how_as_soon_as_its_unit_wrote_it(self):
        self.put(record(WOKE, "ci"), {"exit.json": json.dumps({"code": 0, "at": "2026-10-11T20:40:00+03:00"})})
        self.put(record(WAITS, "oom"), {"exit.json": json.dumps({"code": -1, "signal": "killed", "memory": True,
                                                                  "at": "2026-10-11T20:41:00+03:00"})})
        self.put(record(ENDED, "lost", ended={"code": 0, "lost": True, "at": "2026-10-11T20:42:00+03:00"}))
        got = {v["name"]: v for v in observers.of(SESSION)}
        self.assertEqual((got["ci"]["done"], got["ci"]["doneAt"], got["ci"]["ending"]),
                         (True, "2026-10-11T20:40:00+03:00", {"code": 0}), "an exit code 0 is a code")
        self.assertEqual(got["oom"]["ending"], {"code": -1, "signal": "killed", "memory": True})
        self.assertEqual(got["lost"]["ending"], {"code": 0, "lost": True})

    def test_a_record_that_names_no_observer_is_left_out(self):
        self.put({**record(WOKE, "ci"), "id": WAITS})
        self.put({**record(WAITS, "bad"), "spec": {"name": "bad", "command": "x", "wake": "sometimes"}})
        self.put(record(ENDED, "ok"))
        os.makedirs(os.path.join(self.dir.name, "aacpanel", "observers", "not-an-id"))
        with open(os.path.join(self.dir.name, "aacpanel", "observers", "not-an-id", "observer.json"), "w") as f:
            f.write(json.dumps(record("not-an-id", "stray")))
        self.assertEqual([v["name"] for v in observers.of(SESSION)], ["ok"])

    def test_no_directory_is_no_observers(self):
        self.assertEqual(observers.of(SESSION), [])
        self.assertEqual(observers.alarms_of(SESSION), [])
        self.assertEqual(observers.held_by_conversation(), {})


class Alarms(OnDisk):
    def test_the_alarms_of_a_conversation_are_read_the_soonest_first(self):
        self.ring(alarm(WOKE, "standup", note=" post the summary ", every=3600 * 10**9, rang=2,
                        lastRing="2026-10-11T20:00:00+03:00", next="2026-10-11T22:00:00+03:00"))
        self.ring(alarm(WAITS, "once", next="2026-10-11T20:30:00+03:00"))
        self.ring(alarm(ENDED, "theirs", conversation=OTHER))
        self.assertEqual(observers.alarms_of(SESSION), [
            {"id": WAITS, "name": "once", "at": "2026-10-11T20:00:00+03:00", "next": "2026-10-11T20:30:00+03:00",
             "rang": 0},
            {"id": WOKE, "name": "standup", "at": "2026-10-11T20:00:00+03:00", "next": "2026-10-11T22:00:00+03:00",
             "rang": 2, "note": "post the summary", "every": 3600, "lastRing": "2026-10-11T20:00:00+03:00"},
        ])

    def test_a_file_that_is_no_alarm_is_left_out(self):
        self.ring({**alarm(WOKE, "a"), "id": WAITS})
        self.ring({**alarm(WAITS, "b"), "next": ""})
        self.ring(alarm(ENDED, "kept"))
        path = os.path.join(self.dir.name, "aacpanel", "alarms")
        with open(os.path.join(path, ENDED + ".json.tmp"), "w") as f:
            f.write(json.dumps(alarm(ENDED, "half-written")))
        self.assertEqual([a["name"] for a in observers.alarms_of(SESSION)], ["kept"])


class Held(OnDisk):
    def test_the_row_counts_the_observers_still_running_and_the_alarms_ahead(self):
        self.put(record(WOKE, "ci"))
        self.put(record(WAITS, "done"), {"exit.json": json.dumps({"code": 0, "at": "2026-10-11T20:40:00+03:00"})})
        self.put(record(ENDED, "codex", conversation=THREAD))
        self.ring(alarm(WOKE, "standup"))
        self.ring(alarm(WAITS, "thread", conversation=THREAD))
        self.assertEqual(observers.held_by_conversation(), {
            SESSION: {"observers": 1, "alarms": 1},
            THREAD: {"observers": 1, "alarms": 1},
        })

    def test_held_work_is_counted_apart_from_the_tasks_a_restart_waits_for(self):
        busy = {"tasks": [{"id": "b1", "kind": "bash"}], "agents": []}
        work = live.work_of(busy, {"observers": 2, "alarms": 1})
        self.assertEqual(work, {"tasks": 1, "agents": 0, "observers": 2, "alarms": 1})
        self.assertEqual(live.work_of(busy), {"tasks": 1, "agents": 0})


class Tail(OnDisk):
    def test_the_tail_is_the_end_of_the_log(self):
        self.put(record(WOKE, "ci"), {"log": "one\ntwo\n"})
        self.assertEqual(observers.tail(SESSION, WOKE), {"text": "one\ntwo\n", "cut": False, "size": 8})

    def test_a_log_moved_aside_joins_the_one_there_now(self):
        self.put(record(WOKE, "ci"), {"log.1": "old\n", "log": "new\n"})
        self.assertEqual(observers.tail(SESSION, WOKE), {"text": "old\nnew\n", "cut": False, "size": 8})

    def test_a_long_log_is_read_from_its_end(self):
        body = "".join(f"line {i}\n" for i in range(10000))
        self.put(record(WOKE, "ci"), {"log.1": "old\n", "log": body})
        got = observers.tail(SESSION, WOKE)
        self.assertTrue(got["cut"])
        self.assertEqual(len(got["text"].encode()), observers.TAIL)
        self.assertTrue(got["text"].endswith("line 9999\n"))
        self.assertNotIn("old", got["text"])

    def test_the_log_is_read_for_its_own_conversation_alone(self):
        self.put(record(WOKE, "ci", conversation=OTHER), {"log": "secret\n"})
        self.assertIsNone(observers.tail(SESSION, WOKE))
        self.assertIsNone(observers.tail(SESSION, "../" + WOKE))
        self.assertIsNone(observers.tail(SESSION, WAITS))

    def test_an_observer_that_printed_nothing_has_an_empty_log(self):
        self.put(record(WOKE, "ci"))
        self.assertEqual(observers.tail(SESSION, WOKE), {"text": "", "cut": False, "size": 0})


class InTheConversation(OnDisk):
    """The state of an open conversation carries its observers and alarms, and the tail is asked of it."""

    def setUp(self):
        super().setUp()
        self.projects = os.path.join(self.dir.name, "projects")
        os.makedirs(os.path.join(self.projects, "-srv-proj-lab"))
        with open(os.path.join(self.projects, "-srv-proj-lab", f"{SESSION}.jsonl"), "w", encoding="utf-8") as f:
            f.write(json.dumps({"type": "user", "message": {"content": "hello"}, "cwd": LAB,
                                "timestamp": "2026-10-11T20:00:00Z"}) + "\n")
        self.old_projects, chat.PROJECTS_DIR = chat.PROJECTS_DIR, self.projects
        self.addCleanup(lambda: setattr(chat, "PROJECTS_DIR", self.old_projects))
        self.old_book, asked.BOOK = asked.BOOK, asked.Book(os.path.join(self.dir.name, "asked.json"))
        self.addCleanup(lambda: setattr(asked, "BOOK", self.old_book))
        self.old_cache, sesstate.SHARED = sesstate.SHARED, sesstate.Cache()
        self.addCleanup(lambda: setattr(sesstate, "SHARED", self.old_cache))
        self.addCleanup(setattr, live, "live_session_places", live.live_session_places)
        live.live_session_places = lambda: {}

    def test_the_state_carries_the_observers_and_the_alarms(self):
        self.put(record(WOKE, "ci"))
        self.ring(alarm(WAITS, "standup"))
        state = chat.answer({"session": SESSION, "limit": 20, "state": True})["state"]
        self.assertEqual([o["name"] for o in state["observers"]], ["ci"])
        self.assertEqual([a["name"] for a in state["alarms"]], ["standup"])

    def test_a_conversation_without_them_carries_no_lists_of_them(self):
        state = chat.answer({"session": SESSION, "limit": 20, "state": True})["state"]
        self.assertNotIn("observers", state)
        self.assertNotIn("alarms", state)

    def test_the_tail_of_an_observer_is_asked_by_its_id(self):
        self.put(record(WOKE, "ci"), {"log": "run 42 failed\n"})
        got = chat.answer({"session": SESSION, "observer": {"id": WOKE}})
        self.assertEqual((got["ok"], got["text"], got["cut"]), (True, "run 42 failed\n", False))
        self.put(record(WAITS, "theirs", conversation=OTHER), {"log": "secret\n"})
        refused = chat.answer({"session": SESSION, "observer": {"id": WAITS}})
        self.assertFalse(refused["ok"])
        self.assertNotIn("secret", json.dumps(refused))


if __name__ == "__main__":
    unittest.main()
