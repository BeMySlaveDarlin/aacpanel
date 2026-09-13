"""A file handed over by ranges says when it last changed."""
import datetime
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402

UUID = "44444444-4444-4444-4444-444444444444"

CHANGED = 1_757_760_646  # a second within a minute, not on its edge


class RawFileVersion(unittest.TestCase):
    """The panel names the copy a phone saves by the time of the last change,
    so the answer carries it — in the clock of this host, with its offset."""

    def setUp(self):
        self.root = os.path.realpath(test_barrier.tmp_path(prefix="chat-mtime-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.cwd = os.path.join(self.root, "proj")
        os.makedirs(self.cwd)
        self.file = os.path.join(self.cwd, "cv.pdf")
        with open(self.file, "wb") as f:
            f.write(b"%PDF-1.4\n")
        os.utime(self.file, (CHANGED, CHANGED))

    def test_the_range_says_when_the_file_changed(self):
        got = chat.read_raw("cv.pdf", self.cwd)
        self.assertIsNotNone(got)
        at = datetime.datetime.fromisoformat(got["mtime"])
        self.assertIsNotNone(at.utcoffset(),
                             "the stamp has no offset — the panel keeps a clock of its own, "
                             "and the name would say a time nobody sees in a listing here")
        self.assertEqual(int(at.timestamp()), CHANGED)
        self.assertEqual(at.second, 46, "the stamp lost the seconds — a change a minute later "
                                        "would be told from this one by nothing")

    def test_the_stamp_is_the_clock_of_this_host(self):
        got = chat.read_raw("cv.pdf", self.cwd)
        here = datetime.datetime.fromtimestamp(CHANGED).astimezone()
        self.assertEqual(got["mtime"], here.isoformat(timespec="seconds"))

    def test_a_change_moves_the_stamp(self):
        before = chat.read_raw("cv.pdf", self.cwd)["mtime"]
        with open(self.file, "ab") as f:
            f.write(b"%%EOF\n")
        os.utime(self.file, (CHANGED + 300, CHANGED + 300))
        after = chat.read_raw("cv.pdf", self.cwd)["mtime"]
        self.assertNotEqual(before, after)

    def test_the_stamp_crosses_the_socket(self):
        projects = os.path.join(self.root, "projects")
        os.makedirs(os.path.join(projects, "-srv-proj-x"))
        with open(os.path.join(projects, "-srv-proj-x", f"{UUID}.jsonl"), "w",
                  encoding="utf-8") as f:
            f.write(chat_line({"type": "user", "cwd": self.cwd,
                               "message": {"content": "hello"},
                               "timestamp": "2026-08-23T10:00:00Z"}))
        old, chat.PROJECTS_DIR = chat.PROJECTS_DIR, projects
        self.addCleanup(lambda: setattr(chat, "PROJECTS_DIR", old))

        reply = chat.answer({"session": UUID, "raw": "cv.pdf"})
        self.assertTrue(reply["ok"], reply.get("error"))
        self.assertEqual(int(datetime.datetime.fromisoformat(reply["mtime"]).timestamp()), CHANGED)


def chat_line(record):
    import json
    return json.dumps(record, ensure_ascii=False) + "\n"


if __name__ == "__main__":
    unittest.main()
