#!/usr/bin/env python3
"""A script a skill tells the agent to run by path has to be runnable by path."""

import os
import re
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
SKILLS = HERE / "skills"

# A line of a skill that starts a command with the path of a file of the
# repository: "<repo>/deploy/claude/notify.py "…"". A path behind python3 or
# bash is not one of these — there the interpreter is named and the bit of the
# file is not read at all.
CALL = re.compile(r"^\s*<repo>/(\S+)", re.MULTILINE)


def calls():
    for skill in sorted(SKILLS.glob("*/SKILL.md")):
        for path in dict.fromkeys(CALL.findall(skill.read_text())):
            yield skill, path


class SkillCalls(unittest.TestCase):
    def test_a_skill_names_files_that_are_there(self):
        for skill, path in calls():
            with self.subTest(skill=skill.parent.name, path=path):
                self.assertTrue((HERE.parent.parent / path).exists(),
                                f"{skill.parent.name} calls {path}, and there is no such file")

    def test_a_file_called_by_path_is_executable(self):
        for skill, path in calls():
            target = HERE.parent.parent / path
            if not target.exists():
                continue
            with self.subTest(skill=skill.parent.name, path=path):
                self.assertTrue(os.access(target, os.X_OK),
                                f"{skill.parent.name} calls {path} by path, and the file is not executable — "
                                "the agent gets Permission denied")

    def test_a_file_called_by_path_says_what_runs_it(self):
        for skill, path in calls():
            target = HERE.parent.parent / path
            if not target.exists():
                continue
            with self.subTest(skill=skill.parent.name, path=path):
                self.assertTrue(target.read_bytes().startswith(b"#!"),
                                f"{path} is run by path with no shebang — the shell of the day decides what runs it")

    def test_the_skills_do_call_something(self):
        # A regular expression that stops matching turns the three tests above
        # into three passes over nothing.
        self.assertTrue(list(calls()), "no skill of the delivery calls a file of the repository by path")


if __name__ == "__main__":
    unittest.main()
