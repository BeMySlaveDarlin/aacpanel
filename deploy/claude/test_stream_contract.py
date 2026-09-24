#!/usr/bin/env python3

import importlib.util
import json
import os
import pathlib
import shutil
import sys
import tempfile
import textwrap
import unittest

HERE = pathlib.Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), HERE / f"{name}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


contract = load("stream-contract")

# A claude that speaks just enough of the stream protocol: it answers
# initialize, writes a transcript under the id it was given, answers a message
# with a word, and asks for a permission before a `touch`. How it behaves after
# the answer is set by FAKE_MODE, so the checks can be shown both a binary that
# keeps the contract and one that does not.
FAKE = textwrap.dedent("""\
    #!/usr/bin/env python3
    import json, os, sys
    args = sys.argv[1:]
    sid = args[args.index("--session-id") + 1]
    mode = os.environ.get("FAKE_MODE", "")
    config = os.environ["CLAUDE_CONFIG_DIR"]
    os.makedirs(os.path.join(config, "projects", "-fake"), exist_ok=True)
    open(os.path.join(config, "projects", "-fake", sid + ".jsonl"), "a").close()

    def out(obj):
        sys.stdout.write(json.dumps(obj) + "\\n"); sys.stdout.flush()

    def result(text):
        out({"type": "assistant", "message": {"content": [{"type": "text", "text": text}]}})
        out({"type": "result", "subtype": "success", "result": text})

    for line in sys.stdin:
        msg = json.loads(line)
        if msg.get("type") == "control_request" and msg["request"]["subtype"] == "initialize":
            out({"type": "control_response", "response": {"subtype": "success", "request_id": msg["request_id"],
                 "response": {"commands": [{"name": "context"}], "models": [{"value": "haiku"}],
                              "current_permission_mode": "default"}}})
            continue
        if msg.get("type") != "user":
            continue
        text = msg["message"]["content"]
        if "touch " not in text:
            result("ready")
            continue
        name = text.split("touch ", 1)[1].split()[0]
        req = {"subtype": "can_use_tool", "tool_name": "Bash", "tool_use_id": "t1",
               "input": {"command": "touch " + name}, "permission_suggestions": []}
        if mode == "bare-request":
            del req["permission_suggestions"]
        out({"type": "control_request", "request_id": "ask-1", "request": req})
        answer = json.loads(sys.stdin.readline())["response"]["response"]
        if answer["behavior"] == "allow":
            if mode != "ignores-allow":
                open(name, "w").close()
            said = "ran"
        else:
            said = "denied" if mode == "drops-reason" else answer.get("message", "")
        out({"type": "user", "message": {"content": [{"type": "tool_result", "content": said}]}})
        result("done")
""")


class Pure(unittest.TestCase):
    def test_the_session_environment_leaves_the_running_session_behind(self):
        env = contract.clean_env({
            "HOME": "/home/u", "PATH": "/bin", "CLAUDECODE": "1",
            "CLAUDE_CODE_SESSION_ID": "abc", "CLAUDE_CODE_MESSAGING_SOCKET": "/run/x.sock",
            "CLAUDE_CONFIG_DIR": "/home/u/.claude-work",
        })
        self.assertNotIn("CLAUDECODE", env, "a child that sees CLAUDECODE stops writing its transcript")
        self.assertNotIn("CLAUDE_CODE_SESSION_ID", env)
        self.assertNotIn("CLAUDE_CODE_MESSAGING_SOCKET", env)
        self.assertEqual(env["CLAUDE_CONFIG_DIR"], "/home/u/.claude-work",
                         "the account of the contour has to travel, or the run goes to another account")
        self.assertEqual(env["LANG"], "C.UTF-8")

    def test_a_flag_gone_from_the_help_is_named(self):
        flags = contract.help_flags("--input-format --output-format --session-id")
        self.assertTrue(flags["--session-id"])
        self.assertFalse(flags["--permission-prompt-tool"])

    def test_an_attaching_console_is_noticed_in_the_help(self):
        self.assertEqual(contract.direct_connect("usage: claude [options]"), [])
        self.assertEqual(contract.direct_connect("  claude connect <cc://host>"), ["cc://", "claude connect"])

    def test_only_a_required_check_fails_the_run(self):
        R = contract.Result
        self.assertEqual(contract.exit_code([R("a", contract.OK), R("b", contract.INFO, required=False)]), 0)
        self.assertEqual(contract.exit_code([R("a", contract.OK), R("b", contract.FAIL, required=False)]), 0)
        self.assertEqual(contract.exit_code([R("a", contract.FAIL)]), 1)

    def test_the_transcript_is_found_by_its_id_whatever_the_project_directory(self):
        config = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, config)
        os.makedirs(os.path.join(config, "projects", "-some-where"))
        path = os.path.join(config, "projects", "-some-where", "sid-1.jsonl")
        open(path, "w").close()
        self.assertEqual(contract.find_transcript(config, "sid-1"), path)
        self.assertEqual(contract.find_transcript(config, "sid-2"), "")

    def test_an_answer_of_one_list_names_what_its_items_carry(self):
        self.assertEqual(contract.keys({"models": [{"value": "haiku", "efforts": []}, {}]}),
                         "models: 2, each with efforts, value")
        self.assertEqual(contract.keys({"b": 1, "a": 2}), "a, b")

    def test_only_a_final_status_ends_a_task(self):
        def upd(status):
            return {"type": "system", "subtype": "task_updated", "task_id": "t", "patch": {"status": status}}
        self.assertTrue(contract.task_ended({"type": "system", "subtype": "task_notification", "task_id": "t"}, "t"))
        self.assertTrue(contract.task_ended(upd("killed"), "t"))
        self.assertFalse(contract.task_ended(upd("running"), "t"), "a progress patch is not an end")
        self.assertFalse(contract.task_ended({"type": "system", "subtype": "task_notification", "task_id": "u"}, "t"))

    def test_the_hooks_that_did_not_fire_are_named(self):
        cwd = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, cwd)
        with open(os.path.join(cwd, "hooks.log"), "w") as f:
            f.write("SessionStart\nStop\n")
        got = contract.check_hooks(cwd)
        self.assertEqual(got.status, contract.FAIL)
        self.assertIn("UserPromptSubmit", got.detail)


class AgainstAFakeClaude(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.dir)
        self.claude = os.path.join(self.dir, "claude")
        with open(self.claude, "w") as f:
            f.write(FAKE)
        os.chmod(self.claude, 0o755)
        self.config = os.path.join(self.dir, "config")
        self.cwd = os.path.join(self.dir, "work")
        os.makedirs(self.cwd)

    def session(self, mode=""):
        env = {"PATH": os.environ.get("PATH", ""), "HOME": self.dir, "CLAUDE_CONFIG_DIR": self.config,
               "FAKE_MODE": mode}
        s = contract.Session(self.claude, self.cwd, env, "haiku", os.path.join(self.dir, "events.jsonl"))
        self.addCleanup(s.close)
        return s

    def test_initialize_and_a_turn_hold(self):
        s = self.session()
        self.assertEqual(contract.check_initialize(s).status, contract.OK)
        turn, transcript = contract.check_turn(s, self.config)
        self.assertEqual(turn.status, contract.OK, turn.detail)
        self.assertEqual(transcript.status, contract.OK, transcript.detail)

    def test_an_allowed_command_that_ran_holds(self):
        got = contract.check_permission(self.session())
        self.assertEqual(got.status, contract.OK, got.detail)

    def test_an_allowed_command_that_never_ran_fails(self):
        got = contract.check_permission(self.session("ignores-allow"))
        self.assertEqual(got.status, contract.FAIL)
        self.assertIn("did not run", got.detail)

    def test_a_request_without_its_suggestions_fails(self):
        got = contract.check_permission(self.session("bare-request"))
        self.assertEqual(got.status, contract.FAIL)
        self.assertIn("permission_suggestions", got.detail)

    def test_a_denial_whose_reason_is_lost_fails(self):
        got = contract.check_deny(self.session("drops-reason"))
        self.assertEqual(got.status, contract.FAIL, "the model has to hear why it was refused")

    def test_a_denial_reaches_the_model_with_its_reason(self):
        got = contract.check_deny(self.session())
        self.assertEqual(got.status, contract.OK, got.detail)
        self.assertFalse(os.path.exists(os.path.join(self.cwd, "contract-denied.txt")))


if __name__ == "__main__":
    unittest.main()
