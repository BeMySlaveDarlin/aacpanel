#!/usr/bin/env python3

import contextlib
import importlib.util
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


reminder = load("checklist-reminder")

# The cases this script shares with the executor's hook of the same name,
# which answers the same stops the same way.
CASES = json.loads((HERE / "testdata" / "checklist-reminder.json").read_text(encoding="utf-8"))
CLAUDE = CASES["claude"]["pid"]
SHELL = CASES["shell"]


class Place(unittest.TestCase):
    """A state and a config directory, a /proc with the claude running the hook and a shell below it."""

    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = tmp.name
        self.xdg = os.path.join(self.tmp, "state")
        self.config = os.path.join(self.tmp, "claude")
        self.proc = os.path.join(self.tmp, "proc")
        self.transcript = os.path.join(self.tmp, "transcript.jsonl")
        for name, value in (("PROC", self.proc), ("TAIL", reminder.TAIL)):
            self.addCleanup(setattr, reminder, name, getattr(reminder, name))
            setattr(reminder, name, value)
        was = {k: os.environ.get(k) for k in ("XDG_STATE_HOME", "CLAUDE_CONFIG_DIR")}

        def restore():
            for k, v in was.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
        self.addCleanup(restore)
        os.environ["XDG_STATE_HOME"] = self.xdg
        os.environ["CLAUDE_CONFIG_DIR"] = self.config

    def process(self, pid, parent, start, args):
        root = os.path.join(self.proc, str(pid))
        os.makedirs(root, exist_ok=True)
        fields = ["S", str(parent)] + ["0"] * 17 + [start]
        with open(os.path.join(root, "stat"), "w", encoding="utf-8") as f:
            f.write(f"{pid} (claude) {' '.join(fields)}\n")
        with open(os.path.join(root, "cmdline"), "wb") as f:
            f.write(b"\0".join(a.encode() for a in args) + b"\0")


class TestTheSharedCases(Place):

    def test_every_shared_case(self):
        self.assertTrue(CASES["cases"], "the shared cases are empty: the test checks nothing")
        for case in CASES["cases"]:
            with self.subTest(case["what"]):
                self.setUp()
                self.play(case)

    def play(self, case):
        # A shell stands between claude and the hook, and has no file of a session.
        self.process(SHELL["pid"], CLAUDE, SHELL["start"], SHELL["args"])
        self.claude(case.get("claude", {}))
        for i, stop in enumerate(case["stops"]):
            at = f"stop {i}"
            for checklist in stop.get("checklists", []):
                self.checklist(checklist)
            transcript = stop.get("transcript", "worked")
            path = os.path.join(self.tmp, "none.jsonl")
            if transcript is not None:
                path = self.transcript
                with open(path, "w", encoding="utf-8") as f:
                    f.write("\n".join(CASES["transcripts"][transcript]) + "\n")
            reminder.TAIL = stop.get("tail", 4 * 1024 * 1024)
            payload = {"hook_event_name": "Stop", "session_id": CASES["session"], "transcript_path": path,
                       "stop_hook_active": False, **stop.get("payload", {})}
            for key in stop.get("drop", []):
                payload.pop(key)
            got = self.run_hook(stop["input"] if "input" in stop else json.dumps(payload))
            if stop["holds"]:
                self.assertEqual(got, {"decision": "block", "reason": CASES["reason"]}, at)
            else:
                self.assertIsNone(got, at)

    def claude(self, over):
        """Puts the claude running the hook in place: its process and the file it keeps of itself."""
        args = over.get("args", "withTool")
        self.process(CLAUDE, 1, CASES["claude"]["start"], CASES["args"][args] if isinstance(args, str) else args)
        name = over.get("name", CASES["name"])
        data = {"pid": CLAUDE, "sessionId": over.get("session", CASES["session"]), "cwd": CASES["place"],
                "procStart": over.get("start", CASES["claude"]["start"]), "kind": "interactive",
                **({"name": name} if name is not None else {}), **over.get("over", {})}
        os.makedirs(os.path.join(self.config, "sessions"), exist_ok=True)
        with open(os.path.join(self.config, "sessions", f"{CLAUDE}.json"), "w", encoding="utf-8") as f:
            json.dump(data, f)

    def checklist(self, c):
        """Writes the checklist of a session of the place; without a name, the one of the place."""
        cwd, name = c.get("cwd", CASES["place"]), c.get("name", CASES["name"])
        root = os.path.join(self.xdg, "aacpanel", "checklists")
        os.makedirs(root, exist_ok=True)
        path = os.path.join(root, reminder.checklist_name(self.config, cwd, name))
        items = [{"text": f"step {i}", "status": s} for i, s in enumerate(c["statuses"])]
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"configDir": self.config, "dir": cwd, "sessionId": c.get("session", CASES["session"]),
                       "pid": c.get("pid", CLAUDE), "at": "2026-09-28T09:59:00Z", "items": items,
                       **({"name": name} if name is not None else {}), **c.get("over", {})}, f)
        written = CASES["began"] + c.get("written", -60)
        os.utime(path, (written, written))

    def run_hook(self, text):
        out = io.StringIO()
        self.addCleanup(setattr, reminder, "ancestors", reminder.ancestors)
        reminder.ancestors = lambda: [SHELL["pid"], CLAUDE]
        try:
            sys.stdin = io.StringIO(text)
            with contextlib.redirect_stdout(out):
                reminder.main()
        finally:
            sys.stdin = sys.__stdin__
        said = out.getvalue()
        if not said:
            return None
        self.assertTrue(said.endswith("\n") and said.count("\n") == 1, said)
        return json.loads(said)


class TestTheParts(Place):

    def test_the_name_of_the_checklist_is_the_one_the_executor_computes(self):
        # Pinned beside the executor's own test of the same session.
        for config, cwd, name, file in CASES["names"]:
            self.assertEqual(reminder.checklist_name(config, cwd, name), file, (config, cwd, name))

    def test_the_name_of_the_session_is_taken_in_either_form_claude_takes(self):
        for c in CASES["sessionNames"]:
            self.process(7, 1, "1", c["args"])
            session = {"name": c["file"]} if "file" in c else {}
            self.assertEqual(reminder.session_name(7, session), c["name"], c)

    def test_the_time_of_a_record_is_read_as_python_reads_it(self):
        for at, seconds in CASES["stamps"]:
            self.assertEqual(reminder._stamp({"timestamp": at}), seconds, at)

    def test_the_ancestors_are_read_up_the_chain_of_parents(self):
        for pid, parent in ((os.getppid(), 777), (777, 1)):
            os.makedirs(os.path.join(self.proc, str(pid)))
            with open(os.path.join(self.proc, str(pid), "stat"), "w", encoding="utf-8") as f:
                f.write(f"{pid} (a (b) c) S {parent} 0 0\n")
        self.assertEqual(reminder.ancestors(), [os.getppid(), 777])


if __name__ == "__main__":
    unittest.main()
