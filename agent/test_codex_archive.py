import json
import os
import shutil
import sqlite3
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402
import archive  # noqa: E402
import chat  # noqa: E402
import codex_archive  # noqa: E402
import contours  # noqa: E402
import models  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
ROLLOUT = os.path.join(HERE, "testdata", "codex-rollout.jsonl")

# The columns of the threads table of codex's state database, as codex 0.162
# makes it.
THREADS = """
CREATE TABLE threads (
    id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL, source TEXT NOT NULL, model_provider TEXT NOT NULL, cwd TEXT NOT NULL,
    title TEXT NOT NULL, sandbox_policy TEXT NOT NULL, approval_mode TEXT NOT NULL,
    tokens_used INTEGER NOT NULL DEFAULT 0, has_user_event INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0, archived_at INTEGER, git_sha TEXT, git_branch TEXT,
    git_origin_url TEXT, cli_version TEXT NOT NULL DEFAULT '', first_user_message TEXT NOT NULL DEFAULT '',
    agent_nickname TEXT, agent_role TEXT, memory_mode TEXT NOT NULL DEFAULT 'enabled', model TEXT,
    reasoning_effort TEXT, agent_path TEXT, created_at_ms INTEGER, updated_at_ms INTEGER, thread_source TEXT,
    preview TEXT NOT NULL DEFAULT '', recency_at INTEGER NOT NULL DEFAULT 0,
    recency_at_ms INTEGER NOT NULL DEFAULT 0, history_mode TEXT NOT NULL DEFAULT 'legacy', name TEXT,
    is_pinned INTEGER NOT NULL DEFAULT 0, thread_section_id TEXT, section_position INTEGER,
    section_entered_at_ms INTEGER, project_id TEXT, originator TEXT, daybreak_enabled BOOLEAN,
    creator_user_id TEXT, creator_account_id TEXT);
CREATE TABLE thread_spawn_edges (
    parent_thread_id TEXT NOT NULL, child_thread_id TEXT NOT NULL PRIMARY KEY, status TEXT NOT NULL);
"""

# What a person wrote and what codex made of it: none of it may reach a row.
FIRST = "the first request of the person about a customer's private things"
GENERATED = "Title codex made of the request"

# 2026-10-10T10:00:00Z in milliseconds, and an hour.
T0 = 1_791_626_400_000
HOUR = 3_600_000


def tid(n):
    return f"01a12345-0000-7000-8000-{n:012d}"


def put_env(test, name, value):
    old = os.environ.get(name)
    test.addCleanup(lambda: os.environ.pop(name, None) if old is None
                    else os.environ.__setitem__(name, old))
    os.environ[name] = value


class Homes(unittest.TestCase):
    """Two codex homes of the test's own and a directory of claude transcripts beside them."""

    def setUp(self):
        self.root = os.path.realpath(test_barrier.tmp_path(prefix="codexarch-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.addCleanup(self.unlock)
        models.CACHE_PATH = None
        self.personal = os.path.join(self.root, ".codex")
        self.work = os.path.join(self.root, ".codex-profiles", "work")
        put_env(self, contours.CODEX_ENV, os.pathsep.join([self.personal, self.work]))
        put_env(self, "XDG_STATE_HOME", os.path.join(self.root, "state"))
        self.projects = os.path.join(self.root, "projects")
        os.makedirs(os.path.join(self.projects, "-srv-app"))
        self.addCleanup(setattr, archive, "PROJECTS", archive.PROJECTS)
        archive.PROJECTS = self.projects
        self.addCleanup(setattr, chat, "PROJECTS_DIR", chat.PROJECTS_DIR)
        chat.PROJECTS_DIR = self.projects
        self.index = archive.Index(os.path.join(self.root, "index.json"))
        self.locked = []

    def unlock(self):
        for path in self.locked:
            os.chmod(path, 0o755)

    def rollout(self, home, thread):
        path = os.path.join(home, "sessions", "2026", "10", "10", f"rollout-2026-10-10T10-00-00-{thread}.jsonl")
        os.makedirs(os.path.dirname(path), exist_ok=True)
        shutil.copy(ROLLOUT, path)
        return path

    def thread(self, n, home=None, rollout=True, **cols):
        """Returns the row of a thread of a person in codex's terminal, with the columns named changed."""
        home = home or self.work
        row = {"id": tid(n), "rollout_path": self.rollout(home, tid(n)) if rollout else
               os.path.join(home, "sessions", "gone", f"rollout-{tid(n)}.jsonl"),
               "created_at": (T0 + n * HOUR) // 1000, "updated_at": (T0 + n * HOUR) // 1000,
               "created_at_ms": T0 + n * HOUR - HOUR, "updated_at_ms": T0 + n * HOUR,
               "source": "cli", "model_provider": "openai", "cwd": "/srv/app", "title": FIRST,
               "sandbox_policy": "{}", "approval_mode": "on-request", "tokens_used": 120_000,
               "first_user_message": FIRST, "preview": FIRST, "name": GENERATED, "model": "gpt-6-astra",
               "reasoning_effort": "xhigh", "thread_source": "user", "originator": None}
        row.update(cols)
        return row

    def database(self, home, rows, edges=()):
        os.makedirs(home, exist_ok=True)
        db = sqlite3.connect(os.path.join(home, codex_archive.STATE_DB))
        db.execute("PRAGMA journal_mode=wal")
        db.executescript(THREADS)
        for row in rows:
            db.execute(f"INSERT INTO threads ({', '.join(row)}) VALUES ({', '.join('?' for _ in row)})",
                       list(row.values()))
        for parent, child in edges:
            db.execute("INSERT INTO thread_spawn_edges VALUES (?, ?, 'open')", (parent, child))
        db.commit()
        return db

    def give(self, thread, name):
        """Keeps the name the panel gave a thread, the way the executor does."""
        path = os.path.join(self.root, "state", "aacpanel-stream", "named", thread + ".json")
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"name": name}, f)

    def claude(self, uuid, stamp):
        path = os.path.join(self.projects, "-srv-app", f"{uuid}.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            for record in ({"type": "user", "timestamp": stamp, "cwd": "/srv/app", "message": {"content": "hello"}},
                           {"type": "assistant", "timestamp": stamp,
                            "message": {"model": "claude-opus-5", "usage": {"input_tokens": 1000}}}):
                f.write(json.dumps(record) + "\n")

    def page(self, **want):
        return self.index.page(limit=50, **want)["rows"]


class Threads(Homes):
    def test_only_the_conversations_of_a_person_stand_in_the_archive(self):
        self.database(self.work, [
            self.thread(1),
            self.thread(2, source="vscode", originator="codex-tui", thread_source=None),
            self.thread(3, source="exec", originator="codex_exec"),
            self.thread(4, source="exec"),
            self.thread(5, source=json.dumps({"subagent": {"thread_spawn": {"parent_thread_id": tid(1)}}}),
                        thread_source="subagent", agent_role="worker", agent_nickname="Euclid"),
            self.thread(6, source="vscode"),
            self.thread(7, archived=1),
            self.thread(8, rollout=False),
        ], edges=[(tid(1), tid(6))]).close()

        got = [r["sessionId"] for r in self.page()]
        self.assertEqual(got, [tid(2), tid(1)],
                         "a run of codex exec, a subagent's thread, one spawned by another, one archived in "
                         "codex and one without its rollout are not conversations of the archive")

    def test_a_row_names_no_word_of_the_conversation(self):
        self.database(self.work, [self.thread(1)]).close()
        row = self.page()[0]
        said = json.dumps(row, ensure_ascii=False)
        self.assertNotIn(FIRST, said, "the first message of the thread went into its row")
        self.assertNotIn(GENERATED, said, "the name codex made of the first request went into the row")
        self.assertEqual(row["name"], "app", "a thread the panel did not name reads by its directory")
        self.assertTrue(row["nameGuessed"])
        self.assertEqual(row["prompts"], [])

    def test_a_thread_reads_by_the_name_the_panel_gave_it_while_codex_calls_it_so(self):
        self.database(self.work, [self.thread(1, name="login-bug"), self.thread(2, name="renamed in codex")]).close()
        self.give(tid(1), "login-bug")
        self.give(tid(2), "the panel's old name")
        names = {r["sessionId"]: (r["name"], r["nameGuessed"]) for r in self.page()}
        self.assertEqual(names, {tid(1): ("login-bug", False), tid(2): ("app", True)},
                         "a thread renamed in codex since reads as the session of its directory, as it does live")

    def test_a_row_says_the_contour_of_its_home_and_what_codex_ran(self):
        self.database(self.work, [self.thread(1, tokens_used=29_361_219)]).close()
        self.database(self.personal, [self.thread(2, home=self.personal, cwd="/srv/other", model=None,
                                                  reasoning_effort=None, tokens_used=0)]).close()
        rows = {r["sessionId"]: r for r in self.page()}
        work, personal = rows[tid(1)], rows[tid(2)]
        self.assertEqual((work["agent"], work["profile"], personal["profile"]), ("codex", "work", "personal"))
        self.assertEqual((work["model"], work["effort"], work["tokensUsed"]), ("gpt-6-astra", "xhigh", 29_361_219))
        self.assertEqual((work["startedAt"], work["lastAt"]), ("2026-10-10T10:00:00.000Z", "2026-10-10T11:00:00.000Z"),
                         "the thread starts when codex made it and last spoke when it last changed")
        self.assertEqual((personal["model"], personal["noRequests"]), ("", True), "a thread nobody wrote to ran nothing")
        self.assertFalse(work["noRequests"])

    def test_threads_stand_among_claude_conversations_by_their_last_answer(self):
        self.database(self.work, [self.thread(1), self.thread(3)]).close()
        self.claude("22222222-2222-4222-8222-222222222222", "2026-10-10T12:00:00Z")
        self.claude("44444444-4444-4444-4444-444444444444", "2026-10-10T14:00:00Z")
        rows = self.page()
        self.assertEqual([r["sessionId"] for r in rows],
                         ["44444444-4444-4444-4444-444444444444", tid(3),
                          "22222222-2222-4222-8222-222222222222", tid(1)])
        self.assertEqual([r.get("agent", "claude") for r in rows], ["claude", "codex", "claude", "codex"])
        page = self.index.page(limit=2, offset=1)
        self.assertEqual(([r["sessionId"] for r in page["rows"]], page["total"]),
                         ([tid(3), "22222222-2222-4222-8222-222222222222"], 4))

    def test_a_contour_pages_its_own_threads(self):
        self.database(self.work, [self.thread(1)]).close()
        self.database(self.personal, [self.thread(2, home=self.personal)]).close()
        self.assertEqual([r["sessionId"] for r in self.page(profile="work")], [tid(1)])
        self.assertEqual([r["sessionId"] for r in self.page(profiles=["personal"])], [tid(2)])
        self.assertEqual(len(self.page()), 2, "asked for no contour, the archive has every one")

    def test_a_project_has_the_threads_that_ran_in_it_or_below_it(self):
        self.database(self.work, [self.thread(1), self.thread(2, cwd="/srv/app/sub"), self.thread(3, cwd="/srv/apple"),
                                  self.thread(4, cwd="/srv")]).close()
        self.assertEqual(sorted(r["sessionId"] for r in self.page(under="/srv/app")), [tid(1), tid(2)])

    def test_a_live_thread_is_left_out(self):
        self.database(self.work, [self.thread(1), self.thread(2)]).close()
        self.assertEqual([r["sessionId"] for r in self.page(skip=[tid(2)])], [tid(1)])

    def test_the_feed_of_a_past_thread_opens_by_its_id(self):
        # The thread of the fixture: a rollout shows the words of its own thread only.
        own = "01a12345-0000-7000-8000-00000000abcd"
        self.database(self.work, [self.thread(1, id=own, rollout_path=self.rollout(self.work, own))]).close()
        row = self.page()[0]
        self.assertEqual(row["sessionId"], own)
        reply = chat.answer({"session": row["sessionId"]})
        self.assertTrue(reply["ok"], reply.get("error"))
        self.assertTrue(any(item["role"] == "ai" for item in reply["items"]), "the feed of the rollout is empty")


class ReadOnly(Homes):
    """The collector reads the homes under ProtectHome=read-only: no file of them can be made or written."""

    WRITER = (
        "import sqlite3, sys\n"
        "db = sqlite3.connect(sys.argv[1], isolation_level=None)\n"
        "db.execute('PRAGMA wal_autocheckpoint=0')\n"
        "db.execute(sys.argv[2], sys.argv[3:])\n"
        "print('ready', flush=True)\n"
        "sys.stdin.read()\n"
    )

    def setUp(self):
        super().setUp()
        if os.geteuid() == 0:
            self.skipTest("root writes whatever the modes of the files say")

    def lock(self, home):
        for name in os.listdir(home):
            path = os.path.join(home, name)
            if os.path.isfile(path):
                os.chmod(path, 0o444)
        os.chmod(home, 0o555)
        self.locked.append(home)

    def writer(self, home, row):
        """Starts codex's part: another process that wrote a thread into the WAL and holds the database open."""
        sql = f"INSERT INTO threads ({', '.join(row)}) VALUES ({', '.join('?' for _ in row)})"
        proc = subprocess.Popen([sys.executable, "-c", self.WRITER, os.path.join(home, codex_archive.STATE_DB), sql,
                                 *[str(v) if v is not None else "" for v in row.values()]],
                                stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
        self.addCleanup(proc.wait, 10)
        self.addCleanup(proc.stdout.close)
        self.addCleanup(proc.stdin.close)
        self.assertEqual(proc.stdout.readline().strip(), "ready")
        return proc

    def test_a_home_codex_writes_to_is_read_with_what_its_wal_holds(self):
        db = self.database(self.work, [self.thread(1)])
        self.writer(self.work, self.thread(2))
        db.close()
        self.assertGreater(os.path.getsize(os.path.join(self.work, codex_archive.STATE_DB + "-wal")), 0)
        self.lock(self.work)
        self.assertEqual([r["sessionId"] for r in self.page()], [tid(2), tid(1)],
                         "the thread codex has only in its WAL so far is missing from the archive")

    def test_a_home_nobody_writes_to_is_read_without_its_shared_memory_file(self):
        self.database(self.work, [self.thread(1)]).close()
        self.assertFalse(os.path.exists(os.path.join(self.work, codex_archive.STATE_DB + "-shm")))
        self.lock(self.work)
        self.assertEqual([r["sessionId"] for r in self.page()], [tid(1)],
                         "a read-only open needs the shared-memory file, and the directory takes none")

    def test_a_wal_with_rows_and_no_shared_memory_file_is_left_alone(self):
        db = self.database(self.work, [self.thread(1)])
        # The first thread is in the database file, the second in the WAL alone.
        db.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        self.writer(self.work, self.thread(2))
        db.close()
        copy = os.path.join(self.root, "copy")
        os.makedirs(copy)
        for name in (codex_archive.STATE_DB, codex_archive.STATE_DB + "-wal"):
            shutil.copy(os.path.join(self.work, name), os.path.join(copy, name))
        put_env(self, contours.CODEX_ENV, copy)
        self.lock(copy)
        self.assertEqual(self.page(), [],
                         "the database was read without the rows of its WAL, and the archive lost them without a word")


if __name__ == "__main__":
    unittest.main()
