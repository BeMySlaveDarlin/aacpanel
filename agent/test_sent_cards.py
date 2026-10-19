"""The card of sent files: a file the reader does not open says so."""
import json
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402

UUID = "44444444-4444-4444-4444-444444444444"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


class SentFilesTheReaderDoesNotOpen(unittest.TestCase):
    """The reader opens a file wherever it lies, a secret aside. A session may
    send a secret, and the card is still drawn for it — but its row is marked,
    so the panel does not offer a tap that ends in a refusal."""

    def setUp(self):
        from unittest import mock
        self.root = os.path.realpath(test_barrier.tmp_path(prefix="chat-sent-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.cwd = os.path.join(self.root, "proj")
        self.elsewhere = os.path.join(self.root, "scratch")
        state = os.path.join(self.root, "state")
        self.secrets = os.path.join(state, "aacpanel", "secrets")
        os.makedirs(os.path.join(self.cwd, "cv"))
        os.makedirs(self.elsewhere)
        os.makedirs(self.secrets)
        env = mock.patch.dict(os.environ, {"XDG_STATE_HOME": state})
        env.start()
        self.addCleanup(env.stop)
        self.path = os.path.join(self.root, f"{UUID}.jsonl")

    def write(self, files, cwd=None):
        cwd = self.cwd if cwd is None else cwd
        call = {"type": "assistant", "timestamp": "2026-08-23T10:00:00Z", "cwd": cwd,
                "message": {"content": [{"type": "tool_use", "id": "t1", "name": "SendUserFile",
                                         "input": {"files": files, "caption": "here"}}]}}
        attachments = [{"path": path, "size": 100, "isImage": False,
                        "media_type": "application/pdf", "pathValidated": True,
                        "file_uuid": f"u-{n}"} for n, path in enumerate(files)]
        receipt = {"type": "user", "timestamp": "2026-08-23T10:00:02Z", "cwd": cwd,
                   "message": {"content": [{"type": "tool_result", "tool_use_id": "t1",
                                            "content": f"{len(files)} files delivered to user."}]},
                   "toolUseResult": {"caption": "here", "attachments": attachments}}
        with open(self.path, "w", encoding="utf-8") as f:
            f.write(line(call))
            f.write(line(receipt))

    def card(self):
        items = chat.feed(self.path)["items"]
        cards = [i for i in items if i["role"] == "sent"]
        self.assertEqual(len(cards), 1, items)
        return cards[0]

    def test_a_secret_is_marked_and_a_file_from_elsewhere_is_not(self):
        inside = os.path.join(self.cwd, "cv", "resume.pdf")
        away = os.path.join(self.elsewhere, "resume.pdf")
        secret = os.path.join(self.secrets, "token")
        self.write([inside, away, secret])
        files = self.card()["files"]
        self.assertEqual([(f["name"], f.get("outside")) for f in files],
                         [("resume.pdf", None), ("resume.pdf", None), ("token", True)])

    def test_a_link_into_the_secrets_is_marked(self):
        target = os.path.join(self.secrets, "token")
        with open(target, "w", encoding="utf-8") as f:
            f.write("not for the panel")
        link = os.path.join(self.cwd, "cv", "token.txt")
        os.symlink(target, link)
        self.write([link])
        self.assertTrue(self.card()["files"][0].get("outside"))

    def test_without_a_directory_an_absolute_path_still_opens(self):
        self.write([os.path.join(self.elsewhere, "resume.pdf"), "cv/resume.pdf"], cwd="")
        self.assertEqual([f.get("outside") for f in self.card()["files"]], [None, True],
                         "a relative path has nothing to start from without the directory")

    def test_the_mark_stays_in_a_window_after_a_position(self):
        self.write([os.path.join(self.cwd, "cv", "resume.pdf"), os.path.join(self.secrets, "token")])
        items = chat.feed(self.path, after=0)["items"]
        cards = [i for i in items if i["role"] == "sent"]
        self.assertEqual([f.get("outside") for f in cards[0]["files"]], [None, True])

if __name__ == "__main__":
    unittest.main()
