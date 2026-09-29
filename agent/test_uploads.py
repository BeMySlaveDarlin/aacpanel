import base64
import json
import os
import shutil
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402
import chat  # noqa: E402

UUID = "55555555-5555-4555-8555-555555555555"

PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

AT = "2026-09-20T10:00:00Z"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def queued(text):
    """A message typed while the model answered: the queue takes it, the session reads it later."""
    return [line({"type": "queue-operation", "operation": "enqueue", "content": text, "timestamp": AT}),
            line({"type": "attachment", "timestamp": AT,
                  "attachment": {"type": "queued_command", "prompt": text, "commandMode": "prompt"}})]


def prompt(text):
    return line({"type": "user", "timestamp": AT, "message": {"content": text}})


def read_call(use, path):
    return line({"type": "assistant", "timestamp": AT, "message": {"content": [
        {"type": "tool_use", "id": use, "name": "Read", "input": {"file_path": path}}]}})


def result(use, *parts):
    return line({"type": "user", "timestamp": AT, "message": {"content": [
        {"type": "tool_result", "tool_use_id": use, "content": list(parts)}]}})


def picture(data, media="image/png"):
    return {"type": "image", "source": {"type": "base64", "media_type": media,
                                        "data": base64.b64encode(data).decode("ascii")}}


class Uploads(unittest.TestCase):
    """A picture the panel sent reaches the session as a path; the feed draws the picture."""

    def setUp(self):
        self.root = os.path.realpath(test_barrier.tmp_path(prefix="chat-uploads-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.files = os.path.join(self.root, "files")
        os.makedirs(self.files)
        env = mock.patch.dict(os.environ, {"AACP_FILES": self.files})
        env.start()
        self.addCleanup(env.stop)

        self.cwd = os.path.join(self.root, "proj")
        os.makedirs(self.cwd)
        projects = os.path.join(self.root, "projects", "-srv-proj-shop")
        os.makedirs(projects)
        self.path = os.path.join(projects, f"{UUID}.jsonl")
        self.old, chat.PROJECTS_DIR = chat.PROJECTS_DIR, os.path.dirname(projects)
        self.addCleanup(lambda: setattr(chat, "PROJECTS_DIR", self.old))

    def sent(self, name, data=PNG):
        with open(os.path.join(self.files, name), "wb") as f:
            f.write(data)
        return os.path.join(self.files, name)

    def write(self, *raws):
        with open(self.path, "w", encoding="utf-8") as f:
            f.write(line({"type": "user", "cwd": self.cwd, "timestamp": AT, "message": {"content": "hello"}}))
            for raw in raws:
                f.write("".join(raw) if isinstance(raw, list) else raw)
        chat.tail.PIECES.forget()

    def mine(self):
        return [i for i in chat.feed(self.path, limit=40)["items"] if i["role"] == "me"]

    def test_a_message_sent_with_a_picture_carries_the_picture(self):
        path = self.sent("20260920-100000-ab12cd-shot.png")
        self.write(prompt(f"the task is on the screen\n{path}"))
        me = self.mine()[-1]
        self.assertEqual(me["shots"], [{"upload": "20260920-100000-ab12cd-shot.png", "path": path,
                                        "media": "image/png", "bytes": len(PNG)}])
        self.assertEqual(me["text"], f"the task is on the screen\n{path}",
                         "the words stay whole: the screen takes the paths out, the queue matches by them")

    def test_a_message_that_waited_in_the_queue_carries_it_as_well(self):
        path = self.sent("20260920-100000-ab12cd-shot.png")
        self.write(queued(f"and its task is on the screen\n{path}"))
        mine = [m for m in self.mine() if m["text"].startswith("and its task")]
        self.assertEqual(len(mine), 1, mine)
        self.assertEqual([s["upload"] for s in mine[0].get("shots", [])], ["20260920-100000-ab12cd-shot.png"])

    def test_every_picture_of_a_message_is_drawn_once(self):
        one = self.sent("20260920-100000-ab12cd-one.png")
        two = self.sent("20260920-100000-ef34ab-two.jpg")
        self.write(prompt(f"{one}\n{two}\n{one}"))
        self.assertEqual([(s["upload"], s["media"]) for s in self.mine()[-1]["shots"]],
                         [("20260920-100000-ab12cd-one.png", "image/png"),
                          ("20260920-100000-ef34ab-two.jpg", "image/jpeg")])

    def test_a_path_typed_to_a_picture_elsewhere_stays_words(self):
        # The same name is in the directory as well: what counts is where the
        # path points, not what the file is called.
        self.sent("20260920-100000-ab12cd-shot.png")
        elsewhere = os.path.join(self.cwd, "20260920-100000-ab12cd-shot.png")
        with open(elsewhere, "wb") as f:
            f.write(PNG)
        self.write(prompt(f"look at\n{elsewhere}"))
        self.assertNotIn("shots", self.mine()[-1])

    def test_only_a_picture_that_is_still_there_is_drawn(self):
        gone = os.path.join(self.files, "20260920-100000-ab12cd-gone.png")
        page = self.sent("20260920-100000-ab12cd-logo.svg", b"<svg xmlns='http://www.w3.org/2000/svg'/>")
        notes = self.sent("20260920-100000-ab12cd-notes.txt", b"plain words")
        big = self.sent("20260920-100000-ab12cd-big.png", b"\x89PNG" + b"\0" * chat.MAX_MEDIA)
        for path in (gone, page, notes, big):
            with self.subTest(path=os.path.basename(path)):
                self.write(prompt(f"here\n{path}"))
                self.assertNotIn("shots", self.mine()[-1])

    def test_a_picture_the_panel_sent_is_served_by_its_name(self):
        self.sent("20260920-100000-ab12cd-shot.png")
        self.write()
        reply = chat.answer({"session": UUID, "image": {"upload": "20260920-100000-ab12cd-shot.png"}})
        self.assertTrue(reply["ok"], reply.get("error"))
        self.assertEqual(reply["media"], "image/png")
        self.assertEqual(base64.b64decode(reply["data"]), PNG)

    def test_nothing_but_a_picture_directly_in_the_directory_is_served(self):
        outside = os.path.join(self.root, "secret.png")
        with open(outside, "wb") as f:
            f.write(PNG)
        os.symlink(outside, os.path.join(self.files, "20260920-100000-ab12cd-link.png"))
        self.sent("20260920-100000-ab12cd-logo.svg", b"<svg xmlns='http://www.w3.org/2000/svg'/>")
        self.sent(".hidden.png")
        self.write()
        for name in ("../secret.png", outside, "20260920-100000-ab12cd-link.png",
                     "20260920-100000-ab12cd-logo.svg", ".hidden.png", "", "..", "missing.png"):
            with self.subTest(name=name):
                reply = chat.answer({"session": UUID, "image": {"upload": name}})
                self.assertFalse(reply["ok"], name)
                self.assertIn("among the files the panel sent", reply["error"])
                self.assertNotIn("data", reply)


class CallPictures(unittest.TestCase):
    """A picture a call returned is reached from the call, by where its result lies."""

    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.path = os.path.join(self.dir.name, f"{UUID}.jsonl")
        self.raws = [read_call("toolu_1", "/srv/proj/shop/shot.png"),
                     result("toolu_1", {"type": "text", "text": "the page as it stands"}, picture(PNG))]
        with open(self.path, "w", encoding="utf-8") as f:
            f.write("".join(self.raws))
        chat.tail.PIECES.forget()
        self.result_pos = len(self.raws[0].encode("utf-8"))

    def call(self):
        tools = [i for i in chat.feed(self.path, limit=40)["items"] if i["role"] == "tools"]
        return tools[0]["calls"][0]

    def test_the_call_carries_the_picture_it_returned(self):
        shots = self.call()["shots"]
        self.assertEqual([(s["pos"], s["index"], s["part"], s["media"]) for s in shots],
                         [(self.result_pos, 0, 1, "image/png")])
        self.assertNotIn("data", shots[0], "the bytes stay in the transcript until the picture is asked for")

    def test_a_call_that_returned_words_carries_no_picture(self):
        with open(self.path, "w", encoding="utf-8") as f:
            f.write(read_call("toolu_1", "/srv/proj/shop/notes.txt"))
            f.write(result("toolu_1", {"type": "text", "text": "plain words"}))
        chat.tail.PIECES.forget()
        self.assertNotIn("shots", self.call())

    def test_the_picture_is_served_by_the_place_of_the_result(self):
        got = chat.image(self.path, self.result_pos, 0, 1)
        self.assertEqual(got["media"], "image/png")
        self.assertEqual(base64.b64decode(got["data"]), PNG)

    def test_the_picture_of_a_result_goes_over_the_socket(self):
        projects = os.path.join(self.dir.name, "projects", "-srv-proj-shop")
        os.makedirs(projects)
        shutil.copy(self.path, os.path.join(projects, f"{UUID}.jsonl"))
        old, chat.PROJECTS_DIR = chat.PROJECTS_DIR, os.path.dirname(projects)
        self.addCleanup(lambda: setattr(chat, "PROJECTS_DIR", old))
        reply = chat.answer({"session": UUID, "image": {"pos": self.result_pos, "index": 0, "part": 1}})
        self.assertTrue(reply["ok"], reply.get("error"))
        self.assertEqual(base64.b64decode(reply["data"]), PNG)

    def test_a_place_that_holds_no_picture_serves_nothing(self):
        self.assertIsNone(chat.image(self.path, self.result_pos, 0), "the result itself is not a picture")
        for part in (-1, 0, 2, 9):
            self.assertIsNone(chat.image(self.path, self.result_pos, 0, part), part)
        self.assertIsNone(chat.image(self.path, 0, 0, 0), "a call is not a result")


if __name__ == "__main__":
    unittest.main()
