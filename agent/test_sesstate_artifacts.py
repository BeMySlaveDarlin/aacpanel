import os
import unittest

import test_barrier  # noqa: F401
from test_sesstate import Transcript, background, line, plan


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


if __name__ == "__main__":
    unittest.main()
