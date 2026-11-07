import json
import os
import pathlib
import shutil
import subprocess
import unittest

import test_barrier
from chat import server, taskstore


class TaskStore(unittest.TestCase):
    def setUp(self):
        self.root = pathlib.Path(test_barrier.tmp_path(prefix="taskstore"))
        self.addCleanup(shutil.rmtree, self.root)
        self.store = self.root / ".claude" / "session"
        (self.store / "tasks").mkdir(parents=True)
        (self.store / "handoff").mkdir()
        (self.store / "meta.json").write_text('{"version":2,"next_id":3}')

    def write(self, name, raw):
        (self.store / name).write_text(raw, encoding="utf-8")

    def test_fixed_store_read_keeps_bad_files_and_ignores_attachments(self):
        self.write("tasks/1.json", '{"id":1,"title":"Work"}')
        self.write("tasks/2.json", "{broken")
        self.write("handoff/final.json", '{"key":"final"}')
        (self.store / "handoff" / "final").mkdir()
        (self.store / "handoff" / "final" / "private.txt").write_text("not a store file")
        reply = server.answer({"taskStore": {"cwd": str(self.root)}})
        self.assertTrue(reply["ok"])
        out = reply["taskStore"]
        self.assertTrue(out["tasksComplete"])
        self.assertTrue(out["handoffsComplete"])
        self.assertEqual(out["meta"]["next_id"], 3)
        self.assertEqual([f["name"] for f in out["files"]],
                         ["handoff/final.json", "tasks/1.json", "tasks/2.json"])
        self.assertEqual(out["files"][-1]["raw"], "{broken")

    def test_pages_advance_without_losing_files(self):
        for number in range(1, 4):
            self.write(f"tasks/{number}.json", json.dumps({"id": number, "body": "x" * 80}))
        saved = taskstore.PAGE_BYTES
        taskstore.PAGE_BYTES = 150
        self.addCleanup(setattr, taskstore, "PAGE_BYTES", saved)
        after = ""
        names = []
        while True:
            reply = taskstore.answer({"cwd": str(self.root), "after": after})
            self.assertTrue(reply["ok"])
            out = reply["taskStore"]
            names.extend(f["name"] for f in out["files"])
            if not out.get("next"):
                break
            self.assertGreater(out["next"], after)
            after = out["next"]
        self.assertEqual(names, ["tasks/1.json", "tasks/2.json", "tasks/3.json"])

    def test_worktrees_use_the_common_repository_store(self):
        def git(*args):
            subprocess.run(("git", "-C", str(self.root), *args), check=True,
                           capture_output=True)
        git("init", "-q", "-b", "main")
        git("-c", "user.name=Test", "-c", "user.email=t@example.org", "commit", "--allow-empty", "-qm", "first")
        linked = self.root / "linked"
        git("worktree", "add", "-qb", "work", str(linked))
        self.write("tasks/1.json", '{"id":1}')
        out = taskstore.answer({"cwd": str(linked)})["taskStore"]
        self.assertEqual(out["root"], os.path.realpath(self.root))
        self.assertEqual(out["files"][0]["name"], "tasks/1.json")

    def test_missing_directories_and_unreadable_files_do_not_prove_deletion(self):
        shutil.rmtree(self.store / "tasks")
        self.write("meta.json", "broken")
        (self.store / "handoff" / "bad.json").symlink_to(self.root / "elsewhere")
        out = taskstore.answer({"cwd": str(self.root)})["taskStore"]
        self.assertFalse(out["tasksComplete"])
        self.assertTrue(out["errors"])
        self.assertTrue(out["files"][0]["error"])
        self.assertFalse(taskstore.answer({"cwd": "relative"})["ok"])
        empty = self.root / "empty"
        empty.mkdir()
        self.assertFalse(taskstore.answer({"cwd": str(empty)})["taskStore"]["present"])

    def test_an_oversized_encoded_file_keeps_its_name_and_other_files(self):
        self.write("tasks/1.json", "\x00" * 100)
        self.write("tasks/2.json", '{"id":2}')
        saved = taskstore.PAGE_BYTES
        taskstore.PAGE_BYTES = 300
        self.addCleanup(setattr, taskstore, "PAGE_BYTES", saved)
        out = taskstore.answer({"cwd": str(self.root)})["taskStore"]
        self.assertEqual(len(out["files"]), 2)
        self.assertEqual(out["files"][0]["name"], "tasks/1.json")
        self.assertTrue(out["files"][0]["error"])
        self.assertEqual(out["files"][1]["raw"], '{"id":2}')
