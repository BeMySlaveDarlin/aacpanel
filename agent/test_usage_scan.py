import hashlib
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
from usage import scan  # noqa: E402

SESSION_A = "11111111-1111-4111-8111-111111111111"
SESSION_B = "22222222-2222-4222-8222-222222222222"


class Tree(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.root = self.dir.name

    def root_file(self, session, stamp="2026-09-08T10:00:00.000Z", slug="-opt-x"):
        path = os.path.join(self.root, slug, session + ".jsonl")
        os.makedirs(os.path.dirname(path), exist_ok=True)
        self.write_record(path, stamp)
        return path

    def subagent_file(self, session, agent, stamp="2026-09-08T10:00:00.000Z",
                     slug="-opt-x"):
        path = os.path.join(self.root, slug, session, "subagents",
                            "agent-%s.jsonl" % agent)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        self.write_record(path, stamp)
        return path

    def write_record(self, path, stamp):
        with open(path, "w", encoding="utf-8") as f:
            f.write(json.dumps({"parentUuid": None, "isSidechain": False,
                                "type": "user", "session_id": SESSION_A,
                                "cwd": "/opt/x", "version": "2.1.224",
                                "timestamp": stamp,
                                "message": {"role": "user", "content": "hello"}},
                               ensure_ascii=False) + "\n")

    def pairs(self):
        return [("personal", self.root)]


class Listing(Tree):
    def test_both_shapes_of_files_get_into_the_listing(self):
        root_path = self.root_file(SESSION_A)
        sub = self.subagent_file(SESSION_A, "a1111111111111111")
        found = {i["path"] for i in scan.scan_list(self.pairs())}
        self.assertEqual(found, {root_path, sub})

    def test_the_contour_travels_with_the_file(self):
        self.root_file(SESSION_A)
        row = scan.scan_list(self.pairs())[0]
        self.assertEqual(row["contour"], "personal")

    def test_the_inode_and_the_size_are_taken_from_disk(self):
        path = self.root_file(SESSION_A)
        st = os.stat(path)
        row = scan.scan_list(self.pairs())[0]
        self.assertEqual((row["inode"], row["size"]), (st.st_ino, st.st_size))

    def test_the_head_and_the_first_stamp_are_read_on_demand(self):
        path = self.root_file(SESSION_A, "2026-09-08T10:00:00.000Z")
        cheap = scan.scan_list(self.pairs())[0]
        self.assertNotIn("head", cheap)
        full = scan.scan_list(self.pairs(), heads=True)[0]
        self.assertEqual(full["head"], scan.head_sum(path))
        self.assertEqual(full["first"], scan.first_stamp(path))

    def test_an_empty_directory_is_not_an_error(self):
        self.assertEqual(scan.scan_list(self.pairs()), [])

    def test_foreign_files_do_not_count_as_transcripts(self):
        os.makedirs(os.path.join(self.root, "-opt-x"), exist_ok=True)
        with open(os.path.join(self.root, "-opt-x", "notes.md"), "w") as f:
            f.write("not a transcript")
        self.assertEqual(scan.scan_list(self.pairs()), [])


class Order(Tree):
    def test_files_are_ordered_by_the_time_of_their_first_record(self):
        late = self.root_file(SESSION_A, "2026-09-08T20:00:00.000Z", slug="-a")
        early = self.root_file(SESSION_B, "2026-05-02T10:35:56.912Z", slug="-b")
        order = scan.order_by_first_record(scan.scan_list(self.pairs()))
        self.assertEqual([i["path"] for i in order], [early, late])

    def test_a_file_without_a_time_goes_to_the_end(self):
        normal = self.root_file(SESSION_A, "2026-09-08T10:00:00.000Z", slug="-a")
        empty = os.path.join(self.root, "-b", SESSION_B + ".jsonl")
        os.makedirs(os.path.dirname(empty))
        open(empty, "w").close()
        order = scan.order_by_first_record(scan.scan_list(self.pairs()))
        self.assertEqual([i["path"] for i in order], [normal, empty])

    def test_ordering_does_not_read_the_head_twice(self):
        late = self.root_file(SESSION_A, "2026-09-08T20:00:00.000Z", slug="-a")
        early = self.root_file(SESSION_B, "2026-05-02T10:35:56.912Z", slug="-b")
        order = scan.order_by_first_record(scan.scan_list(self.pairs(), heads=True))
        self.assertEqual([i["path"] for i in order], [early, late])

    def test_the_time_is_found_in_a_record_longer_than_the_head(self):
        path = os.path.join(self.root, "-a", SESSION_A + ".jsonl")
        os.makedirs(os.path.dirname(path))
        with open(path, "w", encoding="utf-8") as f:
            f.write(json.dumps({"type": "user", "timestamp": "2026-09-08T10:00:00.000Z",
                                "message": {"role": "user", "content": "x" * 9000}},
                               ensure_ascii=False) + "\n")
        self.assertEqual(scan.first_stamp(path), "2026-09-08T10:00:00.000Z")

    def test_there_is_no_time_at_all(self):
        path = os.path.join(self.root, "-a", SESSION_A + ".jsonl")
        os.makedirs(os.path.dirname(path))
        with open(path, "w", encoding="utf-8") as f:
            f.write('{"type":"queue-operation","uuid":"q1"}\n')
        self.assertEqual(scan.first_stamp(path), "")


class Head(Tree):
    def test_the_same_head_gives_the_same_hash(self):
        a = self.root_file(SESSION_A, slug="-a")
        b = self.root_file(SESSION_A, slug="-b")
        self.assertEqual(scan.head_sum(a), scan.head_sum(b))

    def test_a_different_head_gives_a_different_hash(self):
        a = self.root_file(SESSION_A, "2026-09-08T10:00:00.000Z", slug="-a")
        b = self.root_file(SESSION_A, "2026-09-08T11:00:00.000Z", slug="-b")
        self.assertNotEqual(scan.head_sum(a), scan.head_sum(b))

    def test_the_head_is_the_first_kilobytes_not_the_whole_file(self):
        path = os.path.join(self.root, "-a", SESSION_A + ".jsonl")
        os.makedirs(os.path.dirname(path))
        with open(path, "wb") as f:
            f.write(b"a" * scan.HEAD_BYTES + b"b" * 100)
        self.assertEqual(scan.head_sum(path),
                         hashlib.sha256(b"a" * scan.HEAD_BYTES).digest())

    def test_a_missing_file_answers_with_emptiness(self):
        self.assertEqual(scan.head_sum(os.path.join(self.root, "missing.jsonl")), b"")


if __name__ == "__main__":
    unittest.main()
