import os
import unittest

import test_barrier  # noqa: F401
from test_sesstate import Transcript, background, line


class Artifacts(Transcript):
    def setUp(self):
        super().setUp()
        self.cwd = os.path.join(self.dir.name, "proj")
        os.makedirs(self.cwd)

    def wrote(self, tool, path, at="2026-08-30T10:00:00Z", cwd=None):
        return line({"type": "assistant", "timestamp": at, "cwd": cwd or self.cwd,
                     "message": {"content": [
                         {"type": "tool_use", "id": "toolu_w", "name": tool,
                          "input": {"file_path": path}}]}})

    def touch(self, rel, text="# plan\n"):
        full = os.path.join(self.cwd, rel)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w", encoding="utf-8") as f:
            f.write(text)
        return full

    def published(self, tool_id="toolu_a", url="https://claude.ai/code/artifact/aaaa1111",
                  at="2026-08-30T10:00:00Z", **data):
        out = line({"type": "assistant", "timestamp": at, "cwd": self.cwd,
                    "message": {"content": [
                        {"type": "tool_use", "id": tool_id, "name": "Artifact", "input": data}]}})
        if url:
            out += line({"type": "user", "timestamp": at, "cwd": self.cwd,
                         "message": {"content": [
                             {"type": "tool_result", "tool_use_id": tool_id,
                              "content": f"Published {data.get('file_path', '/x.html')} at {url}"
                                         "\n\nLive subscription: arming in the background."}]}})
        return out

    def test_a_publish_gets_into_the_list_with_its_link(self):
        got = self.state(self.published(file_path="/tmp/x/plan.html", title="Wave plan",
                                        description="Epics and the path", favicon="🎯"))
        self.assertEqual([(a["title"], a["desc"], a["icon"], a.get("url"), a["count"])
                          for a in got["artifacts"]],
                         [("Wave plan", "Epics and the path", "🎯",
                           "https://claude.ai/code/artifact/aaaa1111", 1)])

    def test_a_republish_is_not_a_second_artifact(self):
        got = self.state(
            self.published(tool_id="toolu_a", file_path="/tmp/x/plan.html", title="Wave plan"),
            self.published(tool_id="toolu_b", url="", file_path="/tmp/x/plan.html",
                           label="second version", at="2026-08-30T11:00:00Z"),
        )
        self.assertEqual([(a["title"], a["count"], a.get("label"), a.get("url"))
                          for a in got["artifacts"]],
                         [("Wave plan", 2, "second version",
                           "https://claude.ai/code/artifact/aaaa1111")])

    def test_reading_an_artifact_does_not_count_as_one(self):
        got = self.state(self.published(action="read", url="",
                                        file_path="/tmp/x/plan.html", title="Wave plan"))
        self.assertEqual(got["artifacts"], [],
                         "a read got into the list — so the action of the call is not looked at at all")

    def test_one_document_covers_all_its_edits(self):
        self.touch("docs/plan.md")
        got = self.state(
            self.wrote("Write", "docs/plan.md"),
            self.wrote("Edit", os.path.join(self.cwd, "docs/plan.md"), at="2026-08-30T11:00:00Z"),
            self.wrote("Edit", "docs/plan.md", at="2026-08-30T12:00:00Z"),
        )
        self.assertEqual([(d["file"], d["dir"], d["count"], d["at"]) for d in got["docs"]],
                         [("plan.md", "docs", 3, "2026-08-30T12:00:00Z")])

    def test_a_document_outside_the_session_directory_does_not_get_into_the_list(self):
        outside = os.path.join(self.dir.name, "elsewhere")
        os.makedirs(outside)
        with open(os.path.join(outside, "note.md"), "w", encoding="utf-8") as f:
            f.write("here")
        got = self.state(self.wrote("Write", os.path.join(outside, "note.md")))
        self.assertEqual(got["docs"], [])

    def test_code_does_not_count_as_a_document(self):
        self.touch("main.go", "package main\n")
        got = self.state(self.wrote("Write", "main.go"))
        self.assertEqual(got["docs"], [])

    def test_a_deleted_document_leaves_the_list(self):
        got = self.state(self.wrote("Write", "gone.md"))
        self.assertEqual(got["docs"], [], "the document is not on disk, yet a row is in the list")


class Sent(Transcript):
    """Files the session delivered to the human through SendUserFile."""

    def setUp(self):
        super().setUp()
        self.cwd = os.path.join(self.dir.name, "proj")
        os.makedirs(self.cwd)
        self.elsewhere = os.path.join(self.dir.name, "scratch")
        os.makedirs(self.elsewhere)
        self.pdf = self.touch("out/report.pdf", "%PDF-1.4\n")
        self.txt = self.touch("out/notes.txt", "notes\n")

    def touch(self, rel, text, root=None):
        full = os.path.join(root or self.cwd, rel)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w", encoding="utf-8") as f:
            f.write(text)
        return full

    def delivered(self, tool_id, files, caption="here you go", at="2026-08-30T10:00:00Z"):
        """A call that went through: the answer carries the receipt of every file."""
        out = line({"type": "assistant", "timestamp": at, "cwd": self.cwd,
                    "message": {"content": [
                        {"type": "tool_use", "id": tool_id, "name": "SendUserFile",
                         "input": {"files": [f for f, _, _ in files], "caption": caption}}]}})
        receipt = [{"path": f, "size": size, "isImage": media.startswith("image/"),
                    "media_type": media, "pathValidated": True, "file_uuid": f"u-{n}"}
                   for n, (f, size, media) in enumerate(files)]
        out += line({"type": "user", "timestamp": at, "cwd": self.cwd,
                     "message": {"content": [
                         {"type": "tool_result", "tool_use_id": tool_id,
                          "content": f"{len(files)} files delivered to user."}]},
                     "toolUseResult": {"caption": caption, "attachments": receipt}})
        return out

    def failed(self, tool_id, at="2026-08-30T09:00:00Z"):
        """A call the harness refused: the input did not parse, nothing was sent."""
        out = line({"type": "assistant", "timestamp": at, "cwd": self.cwd,
                    "message": {"content": [
                        {"type": "tool_use", "id": tool_id, "name": "SendUserFile",
                         "input": {"__unparsedToolInput": {"raw": "{\"files\": /x.pdf}", "len": 20}}}]}})
        out += line({"type": "user", "timestamp": at, "cwd": self.cwd,
                     "message": {"content": [
                         {"type": "tool_result", "tool_use_id": tool_id, "is_error": True,
                          "content": "<tool_use_error>InputValidationError: SendUserFile was called "
                                     "with input that could not be parsed as JSON.</tool_use_error>"}]},
                     "toolUseResult": "InputValidationError: JSON parse failed (20 bytes)"})
        return out

    def test_a_delivered_file_gets_into_the_list_with_its_size_and_type(self):
        got = self.state(self.delivered("toolu_s", [(self.pdf, 89537, "application/pdf"),
                                                    (self.txt, 3097, "text/plain")]))
        self.assertEqual([(s["file"], s["path"], s["size"], s["media"], s["count"], s["at"])
                          for s in got["sent"]],
                         [("report.pdf", self.pdf, 89537, "application/pdf", 1, "2026-08-30T10:00:00Z"),
                          ("notes.txt", self.txt, 3097, "text/plain", 1, "2026-08-30T10:00:00Z")])

    def test_a_failed_delivery_leaves_nothing(self):
        got = self.state(self.failed("toolu_bad"))
        self.assertEqual(got["sent"], [], "a call the harness refused got into the list as a delivery")

    def test_a_failed_call_does_not_hide_the_one_that_went_through(self):
        got = self.state(self.failed("toolu_bad"),
                         self.delivered("toolu_ok", [(self.pdf, 10, "application/pdf")]))
        self.assertEqual([s["file"] for s in got["sent"]], ["report.pdf"])

    def test_a_second_delivery_of_the_same_file_is_one_row(self):
        got = self.state(
            self.delivered("toolu_1", [(self.pdf, 100, "application/pdf")]),
            self.delivered("toolu_2", [(self.pdf, 120, "application/pdf")], at="2026-08-30T11:00:00Z"),
        )
        self.assertEqual([(s["file"], s["size"], s["count"], s["at"]) for s in got["sent"]],
                         [("report.pdf", 120, 2, "2026-08-30T11:00:00Z")])

    def test_the_latest_delivery_stands_first(self):
        got = self.state(
            self.delivered("toolu_1", [(self.pdf, 100, "application/pdf")]),
            self.delivered("toolu_2", [(self.txt, 5, "text/plain")], at="2026-08-30T11:00:00Z"),
        )
        self.assertEqual([s["file"] for s in got["sent"]], ["notes.txt", "report.pdf"])

    def test_a_delivered_file_gone_from_disk_leaves_the_list(self):
        got = self.state(self.delivered("toolu_1", [(self.pdf, 100, "application/pdf")]))
        self.assertEqual(len(got["sent"]), 1)
        os.remove(self.pdf)
        got = self.state(self.delivered("toolu_1", [(self.pdf, 100, "application/pdf")]))
        self.assertEqual(got["sent"], [], "the file is not on disk, yet a row is in the list")

    def test_a_delivery_does_not_count_as_a_document(self):
        got = self.state(self.delivered("toolu_1", [(self.txt, 5, "text/plain")]))
        self.assertEqual(got["docs"], [], "sending a text file is not writing one")

    def test_a_file_sent_from_elsewhere_is_marked_outside(self):
        """The reader opens a file only inside the directory of the conversation:
        a file sent from anywhere else keeps its row, marked, so the list does
        not offer a tap that ends in a refusal."""
        away = self.touch("resume.pdf", "%PDF-1.4\n", root=self.elsewhere)
        got = self.state(self.delivered("toolu_1", [(self.pdf, 10, "application/pdf"),
                                                    (away, 10, "application/pdf")]))
        self.assertEqual([(s["file"], s.get("outside")) for s in got["sent"]],
                         [("report.pdf", None), ("resume.pdf", True)])

    def test_a_file_inside_the_directory_carries_no_mark(self):
        got = self.state(self.delivered("toolu_1", [(self.pdf, 10, "application/pdf")]))
        self.assertNotIn("outside", got["sent"][0], "a file the reader opens is marked as one it cannot")

    def test_a_link_that_leads_outside_is_marked(self):
        target = self.touch("resume.pdf", "%PDF-1.4\n", root=self.elsewhere)
        link = os.path.join(self.cwd, "resume.pdf")
        os.symlink(target, link)
        got = self.state(self.delivered("toolu_1", [(link, 10, "application/pdf")]))
        self.assertTrue(got["sent"][0].get("outside"), "a link into another directory passes for a file inside")

    def test_a_neighbour_with_a_shared_prefix_is_outside(self):
        near = self.cwd + "-2"
        os.makedirs(near)
        away = self.touch("resume.pdf", "%PDF-1.4\n", root=near)
        got = self.state(self.delivered("toolu_1", [(away, 10, "application/pdf")]))
        self.assertTrue(got["sent"][0].get("outside"), "a neighbour sharing the prefix passes for the directory")

    def test_a_second_delivery_keeps_the_mark(self):
        away = self.touch("resume.pdf", "%PDF-1.4\n", root=self.elsewhere)
        got = self.state(
            self.delivered("toolu_1", [(away, 10, "application/pdf")]),
            self.delivered("toolu_2", [(away, 12, "application/pdf")], at="2026-08-30T11:00:00Z"),
        )
        self.assertEqual([(s["count"], s.get("outside")) for s in got["sent"]], [(2, True)])


if __name__ == "__main__":
    unittest.main()
