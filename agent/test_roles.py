#!/usr/bin/env python3
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import notes  # noqa: E402
import roles  # noqa: E402
import sessionfile  # noqa: E402
from collect import live  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
CASES = os.path.join(HERE, "testdata", "role-repos.json")

SESSION = "abcd1234-0000-4000-8000-000000000000"
BESIDE = "0123cdef-2222-4222-8222-222222222222"
CONFIG = "/srv/claude"


class Layout(unittest.TestCase):
    """The shared layout of checkouts, worktrees and plain directories, made afresh for every test."""

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.root = os.path.realpath(self.dir.name)
        with open(CASES, encoding="utf-8") as f:
            self.cases = json.load(f)
        for path in self.cases["dirs"]:
            os.makedirs(os.path.join(self.root, path), exist_ok=True)
        for path, body in self.cases["files"].items():
            with open(os.path.join(self.root, path), "w", encoding="utf-8") as f:
                f.write(body.replace("{root}", self.root))

    def at(self, path):
        return os.path.join(self.root, path)


class Repo(Layout):
    def test_the_repo_of_a_directory_is_the_one_the_executor_finds(self):
        self.assertTrue(self.cases["repos"], "the shared cases are empty: the test checks nothing")
        for path, want in self.cases["repos"]:
            self.assertEqual(roles.repo(self.at(path)), self.at(want), path)

    def test_a_directory_written_another_way_works_in_the_same_repo(self):
        self.assertEqual(roles.repo(self.at("lab") + "//./src/../"), self.at("lab/.git"))

    def test_a_path_that_is_not_one_has_no_repo(self):
        for path in (None, "", "srv/proj", 7):
            self.assertIsNone(roles.repo(path), repr(path))

    def test_the_role_of_a_checkout_and_of_its_worktree_lie_in_one_file(self):
        lab = roles.file_name(CONFIG, self.at("lab"), "lead")
        self.assertEqual(roles.file_name(CONFIG, self.at("worktrees/lab-fix"), "lead"), lab)
        self.assertEqual(lab, sessionfile.file_name(CONFIG, self.at("lab/.git"), "lead"))
        self.assertNotEqual(roles.file_name(CONFIG, self.at("shop"), "lead"), lab,
                            "two repositories keep the role of lead in one file")


class RoleOnTheCard(Layout):
    """The executor writes the role; the collector reads it into the row of the session."""

    def setUp(self):
        super().setUp()
        old = os.environ.get("XDG_STATE_HOME")
        os.environ["XDG_STATE_HOME"] = self.at("state")
        self.addCleanup(lambda: os.environ.pop("XDG_STATE_HOME", None) if old is None
                        else os.environ.__setitem__("XDG_STATE_HOME", old))
        board = notes.Board(self.at("notes.json"))
        self.addCleanup(setattr, notes, "BOARD", notes.BOARD)
        notes.BOARD = board
        self.rows = {"sessions": [{"session": "lead", "sessionId": SESSION, "cwd": self.at("lab")}]}
        self.places = {SESSION: (CONFIG, self.at("lab"), "lead")}
        for name, value in (("sessions", lambda: self.rows), ("session_profiles", dict),
                            ("session_births", dict), ("live_session_waits", dict),
                            ("live_session_status", dict), ("live_session_status_at", dict),
                            ("live_session_places", lambda: self.places)):
            holder = live.ctx if name == "sessions" else live
            self.addCleanup(setattr, holder, name, getattr(holder, name))
            setattr(holder, name, value)

    def put(self, text, cwd="lab", name="lead", **over):
        data = {"configDir": CONFIG, "repo": roles.repo(self.at(cwd)), "dir": self.at(cwd), "name": name,
                "role": text, "by": "person", "at": "2026-10-19T03:00:00Z", **over}
        os.makedirs(roles.roles_dir(), exist_ok=True)
        with open(os.path.join(roles.roles_dir(), roles.file_name(CONFIG, self.at(cwd), name)), "w",
                  encoding="utf-8") as f:
            json.dump(data, f)

    def rows_by_name(self):
        return {s["session"]: s.get("role") for s in live.sessions()["sessions"]}

    def test_a_session_with_a_role_carries_it(self):
        self.put("  the lead of the db split \n")
        self.assertEqual(live.sessions()["sessions"][0].get("role"), "the lead of the db split")

    def test_a_session_without_a_role_carries_none(self):
        self.assertNotIn("role", live.sessions()["sessions"][0], "an empty role on every card draws a line of nothing")
        self.put("   ")
        self.assertNotIn("role", live.sessions()["sessions"][0], "a blank role was carried")

    def only(self):
        """The row of the one session, read afresh: the snapshot adds to the rows it is handed."""
        self.rows = {"sessions": [{"session": "lead", "sessionId": SESSION, "cwd": self.at("lab")}]}
        return live.sessions()["sessions"][0]

    def test_a_session_in_a_worktree_carries_the_role_set_in_the_checkout(self):
        self.put("the lead of the db split")
        self.places = {SESSION: (CONFIG, self.at("worktrees/lab-fix/sub"), "lead")}
        self.assertEqual(live.sessions()["sessions"][0].get("role"), "the lead of the db split")

    def test_sessions_of_one_directory_carry_roles_of_their_own(self):
        self.put("the lead of the db split")
        self.put("watches the work queue", name="lead-watch")

        def both():
            self.rows = {"sessions": [{"session": "lead", "sessionId": SESSION, "cwd": self.at("lab")},
                                      {"session": "lead-watch", "sessionId": BESIDE, "cwd": self.at("lab")}]}
        both()
        self.places = {SESSION: (CONFIG, self.at("lab"), "lead"), BESIDE: (CONFIG, self.at("lab"), "lead-watch")}
        self.assertEqual(self.rows_by_name(), {"lead": "the lead of the db split", "lead-watch": "watches the work queue"})
        both()
        self.places = {SESSION: (CONFIG, self.at("shop"), "lead"), BESIDE: (CONFIG, self.at("lab"), "lead-review")}
        self.assertEqual(self.rows_by_name(), {"lead": None, "lead-watch": None},
                         "a session of another repository or under another name carried a role")

    def test_a_file_naming_another_session_is_not_its_role(self):
        self.put("the lead of the db split")
        self.assertEqual(self.only().get("role"), "the lead of the db split")
        for over in ({"configDir": "/srv/other"}, {"repo": "/srv/elsewhere"}, {"name": "lead-review"}):
            self.put("the lead of the db split", **over)
            self.assertNotIn("role", self.only(), over)


if __name__ == "__main__":
    unittest.main()
