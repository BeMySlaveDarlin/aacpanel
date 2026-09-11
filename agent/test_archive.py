import json
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import archive  # noqa: E402
import contours  # noqa: E402
import models  # noqa: E402

def setUpModule():
    models.CACHE_PATH = None


UUID_A = "11111111-1111-4111-8111-111111111111"
UUID_B = "22222222-2222-4222-8222-222222222222"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def usage(tokens):
    return {"input_tokens": tokens, "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0}


class Scan(unittest.TestCase):
    def write(self, *records):
        path = os.path.join(self.dir.name, f"{UUID_A}.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            for record in records:
                f.write(line(record))
        return path

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)

    def test_takes_the_last_context_and_the_peak(self):
        path = self.write(
            {"type": "user", "timestamp": "2026-08-24T10:00:00Z", "cwd": "/opt/x",
             "message": {"content": "hello"}},
            {"type": "assistant", "timestamp": "2026-08-24T10:00:01Z",
             "message": {"model": "claude-opus-5", "usage": usage(500_000)}},
            {"type": "assistant", "timestamp": "2026-08-24T10:00:02Z",
             "message": {"model": "claude-opus-5", "usage": usage(300_000)}},
        )
        got = archive.scan(path)
        self.assertEqual(got["tokens"], 300_000, "the context is the last one, not the largest")
        self.assertEqual(got["tokensMax"], 500_000, "the peak is the largest over the conversation")
        self.assertEqual(got["cwd"], "/opt/x")
        self.assertEqual(got["model"], "claude-opus-5")

    def test_a_service_record_does_not_zero_the_context(self):
        path = self.write(
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(380_000)}},
            {"type": "assistant", "timestamp": "2026-08-24T10:00:05Z",
             "message": {"model": "<synthetic>", "usage": usage(0),
                         "content": [{"type": "text", "text": "API Error: 529 Overloaded."}]}},
        )
        got = archive.scan(path)
        self.assertEqual(got["tokens"], 380_000)
        self.assertEqual(got["model"], "claude-opus-5")

    def test_a_compaction_flags_the_context_as_overstated(self):
        path = self.write(
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(900_000)}},
            {"type": "user", "timestamp": "2026-08-24T10:01:00Z", "isCompactSummary": True,
             "message": {"content": "summary"}},
        )
        got = archive.scan(path)
        self.assertTrue(got["stale"], "after a compaction with no request the context must be flagged")
        self.assertEqual(got["compacts"], 1)

    def test_a_request_after_a_compaction_clears_the_flag(self):
        path = self.write(
            {"type": "assistant", "timestamp": "2026-08-24T10:00:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(900_000)}},
            {"type": "user", "timestamp": "2026-08-24T10:01:00Z", "isCompactSummary": True,
             "message": {"content": "summary"}},
            {"type": "assistant", "timestamp": "2026-08-24T10:02:00Z",
             "message": {"model": "claude-opus-5", "usage": usage(62_000)}},
        )
        got = archive.scan(path)
        self.assertFalse(got["stale"])
        self.assertEqual(got["tokens"], 62_000)


class Page(unittest.TestCase):
    def setUp(self):
        self.root = test_barrier.tmp_dir()
        self.addCleanup(self.root.cleanup)
        self.projects = os.path.join(self.root.name, "projects")
        os.makedirs(os.path.join(self.projects, "-opt-x"))
        self.old = archive.PROJECTS
        archive.PROJECTS = self.projects
        self.addCleanup(lambda: setattr(archive, "PROJECTS", self.old))
        self.index = archive.Index(os.path.join(self.root.name, "index.json"))

    def put(self, uuid, records, mtime=None):
        path = os.path.join(self.projects, "-opt-x", f"{uuid}.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            for record in records:
                f.write(line(record))
        if mtime:
            os.utime(path, (mtime, mtime))
        return path

    def talk(self, tokens, cwd="/opt/x"):
        return [
            {"type": "user", "timestamp": "2026-08-24T10:00:00Z", "cwd": cwd,
             "message": {"content": "hello"}},
            {"type": "assistant", "timestamp": "2026-08-24T10:00:01Z",
             "message": {"model": "claude-opus-5", "usage": usage(tokens)}},
        ]

    def test_the_freshest_come_first(self):
        self.put(UUID_A, self.talk(100_000), mtime=1000)
        self.put(UUID_B, self.talk(200_000), mtime=2000)
        rows = self.index.page(limit=10)["rows"]
        self.assertEqual([r["sessionId"] for r in rows], [UUID_B, UUID_A])

    def test_every_transcript_gets_its_own_row(self):
        self.put(UUID_A, self.talk(767_000), mtime=1000)
        self.put(UUID_B, self.talk(89_000), mtime=2000)
        rows = self.index.page(limit=10)["rows"]
        self.assertEqual(len(rows), 2)
        self.assertEqual(sorted(r["pctMax"] for r in rows), [8.9, 76.7])

    def test_the_second_time_it_is_read_from_the_cache(self):
        path = self.put(UUID_A, self.talk(100_000))
        self.index.page(limit=10)
        st = os.stat(path)
        with open(path, "w", encoding="utf-8") as f:
            f.write("x" * st.st_size)
        os.utime(path, (st.st_mtime, st.st_mtime))
        rows = self.index.page(limit=10)["rows"]
        self.assertEqual(rows[0]["tokens"], 100_000, "the parse was redone although the size and the time are the same")

    def test_a_rewritten_file_is_parsed_again(self):
        path = self.put(UUID_A, self.talk(100_000))
        self.index.page(limit=10)
        st = os.stat(path)
        with open(path, "w", encoding="utf-8") as f:
            for record in self.talk(400_000):
                f.write(line(record))
        with open(path, "a", encoding="utf-8") as f:
            f.write(" " * max(0, st.st_size - os.path.getsize(path)))
        os.utime(path, (st.st_mtime + 10, st.st_mtime + 10))
        rows = self.index.page(limit=10)["rows"]
        self.assertEqual(rows[0]["tokens"], 400_000, "the change of the time went unnoticed — the archive shows yesterday's numbers")

    def test_a_file_that_grew_is_parsed_again(self):
        path = self.put(UUID_A, self.talk(100_000))
        self.index.page(limit=10)
        with open(path, "a", encoding="utf-8") as f:
            f.write(line({"type": "assistant", "timestamp": "2026-08-24T11:00:00Z",
                          "message": {"model": "claude-opus-5", "usage": usage(400_000)}}))
        rows = self.index.page(limit=10)["rows"]
        self.assertEqual(rows[0]["tokens"], 400_000, "a file that grew must be read again")

    def test_the_page_is_not_shorter_than_asked_for(self):
        for i in range(4):
            self.put(f"3333333{i}-3333-4333-8333-333333333333",
                     [{"type": "file-history-snapshot", "timestamp": "2026-08-24T10:00:00Z"}],
                     mtime=3000 + i)
        self.put(UUID_A, self.talk(100_000), mtime=1000)
        page = self.index.page(limit=5)
        self.assertEqual(len(page["rows"]), 5)
        self.assertEqual(page["total"], 5)

    def test_the_top_up_takes_only_the_started_ones(self):
        for i in range(3):
            self.put(f"4444444{i}-4444-4444-8444-444444444444",
                     [{"type": "file-history-snapshot", "timestamp": "2026-08-24T10:00:00Z"}],
                     mtime=3000 + i)
        self.put(UUID_A, self.talk(100_000), mtime=1000)
        rows = self.index.page(limit=5, started=True)["rows"]
        self.assertEqual([r["sessionId"] for r in rows], [UUID_A])

    def test_live_conversations_are_skipped(self):
        self.put(UUID_A, self.talk(100_000), mtime=1000)
        self.put(UUID_B, self.talk(200_000), mtime=2000)
        rows = self.index.page(limit=10, skip=[UUID_B])["rows"]
        self.assertEqual([r["sessionId"] for r in rows], [UUID_A])

    def test_an_observed_name_beats_the_directory(self):
        self.put(UUID_A, self.talk(100_000, cwd="/home/u"))
        self.index.names[UUID_A] = "home"
        row = self.index.page(limit=10)["rows"][0]
        self.assertEqual(row["name"], "home")
        self.assertFalse(row["nameGuessed"])

    def test_without_an_observation_the_name_comes_from_the_directory_and_is_flagged(self):
        self.put(UUID_A, self.talk(100_000, cwd="/srv/proj/aacpanel"))
        row = self.index.page(limit=10)["rows"][0]
        self.assertEqual(row["name"], "aacpanel")
        self.assertTrue(row["nameGuessed"], "the derived name must be flagged")

    def test_a_home_conversation_is_known_by_its_directory(self):
        home = os.path.expanduser("~")
        self.put(UUID_A, self.talk(100_000, cwd=home))
        self.put(UUID_B, self.talk(100_000, cwd="/opt/x"), mtime=1000)
        rows = {r["sessionId"]: r["home"] for r in self.index.page(limit=10)["rows"]}
        self.assertTrue(rows[UUID_A])
        self.assertFalse(rows[UUID_B])


class Profile(unittest.TestCase):
    def talk(self, tokens, cwd="/opt/x"):
        return [
            {"type": "user", "timestamp": "2026-08-24T10:00:00Z", "cwd": cwd,
             "message": {"content": "hello"}},
            {"type": "assistant", "timestamp": "2026-08-24T10:00:01Z",
             "message": {"model": "claude-opus-5", "usage": usage(tokens)}},
        ]

    def setUp(self):
        self.root = test_barrier.tmp_path(prefix="archive-profile-")
        self.addCleanup(shutil.rmtree, self.root, True)
        self.home = os.path.join(self.root, ".claude")
        os.makedirs(os.path.join(self.home, "projects"))
        self.reg = os.path.join(self.root, "registry.conf")

        for mod, name in ((contours, "HOME"), (contours, "REGISTRY"), (archive, "PROJECTS")):
            self.addCleanup(setattr, mod, name, getattr(mod, name))
        contours.HOME = self.home
        contours.REGISTRY = self.reg
        archive.PROJECTS = None

        self.index = archive.Index(os.path.join(self.root, "index.json"))

    def work(self):
        d = os.path.join(self.root, "work")
        os.makedirs(os.path.join(d, "projects"))
        with open(self.reg, "w", encoding="utf-8") as f:
            f.write("# profile | prefix | config | token\n"
                    "work | /srv/proj/Labs/ | %s | -\n"
                    "personal | * | %s | -\n" % (d, self.home))
        return os.path.join(d, "projects")

    def put(self, root, uuid, tokens):
        d = os.path.join(root, "-opt-x")
        os.makedirs(d, exist_ok=True)
        path = os.path.join(d, f"{uuid}.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            for record in self.talk(tokens):
                f.write(line(record))
        return path

    def test_without_a_profile_the_personal_one_is_returned(self):
        work = self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.put(work, UUID_B, 200_000)
        rows = self.index.page(limit=10)["rows"]
        self.assertEqual([r["sessionId"] for r in rows], [UUID_A])

    def test_an_explicit_profile_returns_only_its_own(self):
        work = self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.put(work, UUID_B, 200_000)
        rows = self.index.page(limit=10, profile="work")["rows"]
        self.assertEqual([r["sessionId"] for r in rows], [UUID_B])

    def test_another_profile_is_not_visible_in_the_personal_one(self):
        work = self.work()
        self.put(work, UUID_B, 200_000)
        rows = self.index.page(limit=10)["rows"]
        self.assertEqual(rows, [], "a work conversation must not show up in the personal archive")

    def test_an_unknown_profile_returns_an_empty_page(self):
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        page = self.index.page(limit=10, profile="ghost")
        self.assertEqual(page["rows"], [])
        self.assertEqual(page["total"], 0)

    def test_the_index_remembers_the_profile_of_the_record(self):
        work = self.work()
        self.put(work, UUID_B, 200_000)
        self.index.page(limit=10, profile="work")
        self.assertEqual(self.index.scanned[UUID_B]["profile"], "work")

    def test_the_row_names_its_own_contour(self):
        work = self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.put(work, UUID_B, 200_000)
        rows = self.index.page(limit=10, profile="work")["rows"]
        self.assertEqual([r["profile"] for r in rows], ["work"])
        self.assertEqual([r["profile"] for r in self.index.page(limit=10)["rows"]], ["personal"])

    def test_several_contours_at_once(self):
        work = self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.put(work, UUID_B, 200_000)
        page = self.index.page(limit=10, profiles=["personal", "work"])
        self.assertEqual({r["sessionId"] for r in page["rows"]}, {UUID_A, UUID_B})
        self.assertEqual({r["profile"] for r in page["rows"]}, {"personal", "work"})

    def test_the_counter_counts_what_was_selected(self):
        work = self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.put(work, UUID_B, 200_000)
        self.assertEqual(self.index.page(limit=10, profiles=["work"])["total"], 1)
        self.assertEqual(self.index.page(limit=10, profiles=["personal", "work"])["total"], 2)

    def test_an_unknown_name_in_the_list_does_not_pull_in_the_personal_one(self):
        work = self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.put(work, UUID_B, 200_000)
        page = self.index.page(limit=10, profiles=["work", "ghost"])
        self.assertEqual([r["sessionId"] for r in page["rows"]], [UUID_B])
        self.assertEqual(page["total"], 1)

    def test_a_contour_can_be_named_by_its_config_directory(self):
        work = self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.put(work, UUID_B, 200_000)

        page = self.index.page(limit=10, profiles=[os.path.join(self.root, "work")])
        self.assertEqual([r["sessionId"] for r in page["rows"]], [UUID_B])
        self.assertEqual(page["total"], 1)
        self.assertEqual([r["profile"] for r in page["rows"]], ["work"])

        page = self.index.page(limit=10, profiles=[self.home + "/"])
        self.assertEqual([r["sessionId"] for r in page["rows"]], [UUID_A])

    def test_a_foreign_directory_does_not_pull_in_the_personal_one(self):
        self.work()
        self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        page = self.index.page(limit=10, profiles=[os.path.join(self.root, "no-such")])
        self.assertEqual(page["rows"], [])
        self.assertEqual(page["total"], 0)

    def test_the_profile_of_the_record_is_updated_on_a_cache_hit(self):
        path = self.put(os.path.join(self.home, "projects"), UUID_A, 100_000)
        self.index.page(limit=10)
        self.assertEqual(self.index.scanned[UUID_A]["profile"], "personal")
        st = os.stat(path)
        work = self.work()
        moved = self.put(work, UUID_A, 100_000)
        os.utime(moved, (st.st_mtime, st.st_mtime))
        os.remove(path)
        self.assertEqual(os.path.getsize(moved), st.st_size,
                         "the test records must match byte for byte — otherwise there is no cache hit")
        rows = self.index.page(limit=10, profile="work")["rows"]
        self.assertEqual([r["sessionId"] for r in rows], [UUID_A])
        self.assertEqual(self.index.scanned[UUID_A]["profile"], "work")


class Names(unittest.TestCase):
    def setUp(self):
        self.root = test_barrier.tmp_dir()
        self.addCleanup(self.root.cleanup)
        self.live = os.path.join(self.root.name, "sessions")
        os.makedirs(self.live)
        self.old = archive.LIVE
        archive.LIVE = self.live
        self.addCleanup(lambda: setattr(archive, "LIVE", self.old))
        self.index = archive.Index(os.path.join(self.root.name, "index.json"))

    def put(self, pid, data):
        with open(os.path.join(self.live, f"{pid}.json"), "w", encoding="utf-8") as f:
            json.dump(data, f)

    def test_the_name_of_a_live_session_is_remembered(self):
        self.put(123, {"sessionId": UUID_A, "name": "home", "cwd": "/home/u"})
        self.index.observe()
        self.assertEqual(self.index.names[UUID_A], "home")

    def test_a_one_off_run_is_not_remembered(self):
        self.put(124, {"sessionId": UUID_B, "name": "tmp-8b", "cwd": "/opt/x"})
        self.index.observe()
        self.assertNotIn(UUID_B, self.index.names)

    def test_a_background_process_of_the_daemon_is_not_remembered(self):
        self.put(125, {"sessionId": UUID_B, "name": "demo",
                       "cwd": "/opt/x", "kind": "bg"})
        self.index.observe()
        self.assertNotIn(UUID_B, self.index.names)

    def test_an_unknown_kind_of_process_stays_a_session(self):
        self.put(126, {"sessionId": UUID_B, "name": "home", "cwd": "/home/u",
                       "kind": "a-new-kind-from-a-future-version"})
        self.index.observe()
        self.assertEqual(self.index.names.get(UUID_B), "home",
                         "an unknown kind of process hid a live session")

    def test_a_subkind_of_background_stays_background(self):
        self.put(127, {"sessionId": UUID_B, "name": "demo",
                       "cwd": "/opt/x", "kind": "bg-spare"})
        self.index.observe()
        self.assertNotIn(UUID_B, self.index.names)

    def test_the_names_survive_a_restart(self):
        self.put(123, {"sessionId": UUID_A, "name": "home", "cwd": "/home/u"})
        self.index.observe()
        again = archive.Index(self.index.path)
        self.assertEqual(again.names.get(UUID_A), "home",
                         "the name did not survive a restart of the agent — and it restarts on every edit")


if __name__ == "__main__":
    unittest.main()


class Mode(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)

    def write(self, *records):
        path = os.path.join(self.dir.name, "t.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            for record in records:
                f.write(line(record))
        return path

    def test_takes_the_last_mode(self):
        path = self.write(
            {"type": "permission-mode", "permissionMode": "default"},
            {"type": "user", "timestamp": "2026-09-01T10:00:00Z", "permissionMode": "default",
             "message": {"content": "hello"}},
            {"type": "permission-mode", "permissionMode": "bypassPermissions"},
        )
        self.assertEqual(archive.scan(path)["mode"], "bypassPermissions")

    def test_reads_the_mode_from_a_human_prompt(self):
        path = self.write(
            {"type": "user", "timestamp": "2026-09-01T10:00:00Z", "permissionMode": "plan",
             "message": {"content": "hello"}},
        )
        self.assertEqual(archive.scan(path)["mode"], "plan")

    def test_with_no_mode_it_is_empty_not_default(self):
        path = self.write(
            {"type": "user", "timestamp": "2026-09-01T10:00:00Z", "message": {"content": "hello"}},
        )
        self.assertEqual(archive.scan(path)["mode"], "")
