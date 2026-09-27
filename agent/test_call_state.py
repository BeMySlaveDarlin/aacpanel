"""Whether a call of the feed is still running, and whether it failed."""
import json
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402

CWD = "/srv/proj"
AT = "2026-09-27T10:00:00Z"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def call(*uses, name="Bash"):
    """Returns an answer of the model that makes these calls, one id each."""
    return {"type": "assistant", "timestamp": AT, "cwd": CWD,
            "message": {"content": [{"type": "tool_use", "id": use, "name": name,
                                     "input": {"command": f"run {use}"}} for use in uses]}}


def result(*uses, error=False, text="ok"):
    """Returns the record that brings the results of these calls."""
    blocks = []
    for use in uses:
        block = {"type": "tool_result", "tool_use_id": use, "content": text}
        if error:
            block["is_error"] = True
        blocks.append(block)
    return {"type": "user", "timestamp": AT, "cwd": CWD, "message": {"content": blocks}}


def turn_end(subtype="turn_duration"):
    return {"type": "system", "subtype": subtype, "durationMs": 4200, "timestamp": AT, "cwd": CWD}


def prompt(text, **fields):
    return {"type": "user", "timestamp": AT, "cwd": CWD, "message": {"content": text}, **fields}


def answer(text):
    return {"type": "assistant", "timestamp": AT, "cwd": CWD,
            "message": {"content": [{"type": "text", "text": text}]}}


def calls_of(items):
    """Returns every call of the window as (id, open, failed)."""
    return [(c["use"], c.get("open"), c.get("failed"))
            for item in items if item["role"] == "tools" for c in item["calls"]]


# What claude answers a letter it could not deliver, and one it could.
REASON = ("No agent named 'coordinator' is reachable.\n"
          "Check the spelling, or use the agent ID from a background agent's spawn result.")
UNREACHABLE = json.dumps({"success": False, "message": REASON})
QUEUED = json.dumps({"success": True, "message": "Message queued for the main conversation's next turn."})


def letter(use, to="coordinator", text="salta"):
    """Returns an answer of the model that sends a letter."""
    return {"type": "assistant", "timestamp": AT, "cwd": CWD,
            "message": {"content": [{"type": "tool_use", "id": use, "name": "SendMessage",
                                     "input": {"to": to, "message": text}}]}}


def letters_of(items):
    """Returns every letter sent in the window as (id, why it reached nobody)."""
    return [(i.get("use"), i.get("undelivered")) for i in items
            if i["role"] == "mail" and i.get("dir") == "out"]


class Parse(unittest.TestCase):
    def test_a_reader_that_keeps_calls_gets_a_call_open(self):
        got = chat.parse(call("t1"), 0, calls={})
        self.assertEqual([(i["role"], i.get("open")) for i in got], [("tool", True)])

    def test_a_reader_that_keeps_no_calls_is_told_nothing_of_it(self):
        # Only a reader that keeps the calls hears of their results, so only
        # it may be told a call is open: otherwise the call would never close.
        got = chat.parse(call("t1"), 0)
        self.assertNotIn("open", got[0])

    def test_a_result_is_a_mark_of_its_call(self):
        calls = {}
        chat.parse(call("t1"), 0, calls=calls)
        got = chat.parse(result("t1"), 10, calls=calls)
        self.assertEqual(got, [{"role": "result", "use": "t1", "at": AT, "pos": 10}])

    def test_an_error_result_marks_its_call_failed(self):
        calls = {}
        chat.parse(call("t1"), 0, calls=calls)
        got = chat.parse(result("t1", error=True), 10, calls=calls)
        self.assertEqual([(i["role"], i.get("failed")) for i in got], [("result", True)])

    def test_the_result_of_a_call_never_seen_gives_no_mark(self):
        self.assertEqual(chat.parse(result("t1"), 10, calls={}), [])

    def test_a_turn_ends_a_call_once(self):
        calls = {}
        chat.parse(call("t1"), 0, calls=calls)
        first = chat.parse(turn_end(), 10, calls=calls)
        self.assertEqual([(i["role"], i.get("use")) for i in first], [("turn", None), ("cutoff", "t1")])
        again = chat.parse(turn_end(), 20, calls=calls)
        self.assertEqual([i["role"] for i in again], ["turn"],
                         "every later turn marked the same dead call again")

    def test_a_cutoff_keeps_the_call_for_its_late_result(self):
        calls = {}
        chat.parse(call("t1"), 0, calls=calls)
        chat.parse(turn_end(), 10, calls=calls)
        got = chat.parse(result("t1", error=True), 20, calls=calls)
        self.assertEqual([(i["role"], i.get("failed")) for i in got], [("result", True)])


class Window(unittest.TestCase):
    def setUp(self):
        chat.tail.PIECES.forget()
        self.root = os.path.realpath(test_barrier.tmp_path(prefix="chat-call-state-"))
        self.addCleanup(shutil.rmtree, self.root, True)
        self.addCleanup(os.environ.pop, "XDG_STATE_HOME", None)
        os.environ["XDG_STATE_HOME"] = os.path.join(self.root, "state")
        self.path = os.path.join(self.root, "feed.jsonl")
        self.written = 0

    def write(self, *records):
        """Appends the records and returns the position of the first of them."""
        at = self.written
        raw = "".join(line(r) for r in records)
        with open(self.path, "a", encoding="utf-8") as f:
            f.write(raw)
        self.written += len(raw.encode("utf-8"))
        return at


class Tail(Window):
    def test_a_call_without_a_result_is_open(self):
        self.write(prompt("go"), call("t1"))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", True, None)])

    def test_a_result_closes_the_call(self):
        self.write(prompt("go"), call("t1"), result("t1"))
        got = [c for i in chat.feed(self.path)["items"] if i["role"] == "tools" for c in i["calls"]]
        self.assertNotIn("open", got[0], "a closed call has no open field at all")
        self.assertNotIn("failed", got[0])

    def test_an_error_result_marks_the_call_failed(self):
        self.write(prompt("go"), call("t1"), result("t1", error=True))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, True)])

    def test_only_the_answered_call_of_a_group_closes(self):
        self.write(prompt("go"), call("t1", "t2"), result("t1"))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, None), ("t2", True, None)])

    def test_no_mark_reaches_the_reader(self):
        self.write(prompt("go"), call("t1", "t2"), result("t1"), turn_end())
        roles = [i["role"] for i in chat.feed(self.path)["items"]]
        self.assertEqual(roles, ["me", "tools", "turn"])

    def test_a_hook_line_in_a_run_is_never_open(self):
        self.write(prompt("go"), call("t1"), result("t1"),
                   {"type": "attachment", "timestamp": AT, "cwd": CWD,
                    "attachment": {"type": "hook_system_message", "hookName": "PostToolUse",
                                   "content": "formatted"}})
        got = [c for i in chat.feed(self.path)["items"] if i["role"] == "tools" for c in i["calls"]]
        self.assertEqual([(c["name"], c.get("open")) for c in got],
                         [("Bash", None), ("PostToolUse hook", None)])

    def test_the_end_of_the_turn_closes_a_call_left_without_a_result(self):
        self.write(prompt("go"), call("t1"), turn_end())
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, None)])

    def test_the_summary_of_stop_hooks_ends_the_turn_too(self):
        self.write(prompt("go"), call("t1"), turn_end("stop_hook_summary"))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, None)])

    def test_an_interruption_closes_the_call(self):
        self.write(prompt("go"), call("t1"), prompt("[Request interrupted by user]"))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, None)])

    def test_the_next_prompt_closes_a_call_a_dead_session_left(self):
        self.write(prompt("go"), call("t1"), prompt("Continue from where you left off."))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, None)])

    def test_a_prompt_from_the_queue_closes_it_too(self):
        # On the stream every message goes through the queue and comes back
        # as a prompt of its own once the turn before it is over.
        self.write(prompt("go"), call("t1"),
                   {"type": "queue-operation", "operation": "enqueue", "content": "next",
                    "timestamp": AT},
                   prompt("next", promptSource="sdk"))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, None)])

    def test_a_result_written_after_the_end_of_the_turn_still_settles_the_call(self):
        # claude writes the end of a turn a moment before the result of a call
        # that stops a background task.
        self.write(prompt("go"), call("t1", name="TaskStop"), turn_end(), result("t1", error=True))
        self.assertEqual(calls_of(chat.feed(self.path)["items"]), [("t1", None, True)])


class After(Window):
    def test_a_result_past_the_position_sends_the_group_again(self):
        self.write(prompt("go"), call("t1"))
        first = chat.feed(self.path, limit=40)
        group = first["items"][-1]
        self.assertEqual(calls_of([group]), [("t1", True, None)])

        self.write(result("t1"))
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual([(i["role"], i["pos"]) for i in more["items"]], [("tools", group["pos"])],
                         "the reader finds the group by its position and role and redraws it")
        again = more["items"][0]
        self.assertEqual(calls_of([again]), [("t1", None, None)])
        self.assertEqual((again["run"], again["kind"], [c["seq"] for c in again["calls"]]),
                         (group["run"], group["kind"], [c["seq"] for c in group["calls"]]))

    def test_an_error_past_the_position_arrives_as_a_failed_call(self):
        self.write(prompt("go"), call("t1"))
        first = chat.feed(self.path, limit=40)
        self.write(result("t1", error=True))
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual(calls_of(more["items"]), [("t1", None, True)])

    def test_the_end_of_a_turn_past_the_position_sends_the_group_again(self):
        self.write(prompt("go"), call("t1"))
        first = chat.feed(self.path, limit=40)
        self.write(prompt("[Request interrupted by user]"))
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual([i["role"] for i in more["items"]], ["tools", "note"])
        self.assertEqual(calls_of(more["items"]), [("t1", None, None)])

    def test_the_group_goes_again_whole_with_calls_the_reader_got_later(self):
        self.write(prompt("go"), call("t1"))
        first = chat.feed(self.path, limit=40)
        self.write(call("t2"))
        second = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual(calls_of(second["items"]), [("t2", True, None)],
                         "a new call past the position arrives as it always did")

        self.write(result("t1", "t2"))
        third = chat.feed(self.path, limit=40, after=second["last"])
        self.assertEqual([(i["role"], i["pos"]) for i in third["items"]],
                         [("tools", first["items"][-1]["pos"])])
        self.assertEqual(calls_of(third["items"]), [("t1", None, None), ("t2", None, None)])

    def test_the_group_goes_ahead_of_the_new_rows(self):
        self.write(prompt("go"), call("t1"))
        first = chat.feed(self.path, limit=40)
        self.write(result("t1"), answer("done"))
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual([i["role"] for i in more["items"]], ["tools", "ai"])

    def test_a_group_the_reader_has_whole_is_not_sent_again(self):
        self.write(prompt("go"), call("t1"), result("t1"))
        first = chat.feed(self.path, limit=40)
        self.write(answer("done"))
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual([i["role"] for i in more["items"]], ["ai"])

    def test_nothing_new_is_nothing_at_all(self):
        self.write(prompt("go"), call("t1"), result("t1"))
        first = chat.feed(self.path, limit=40)
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual((more["items"], more["total"]), ([], 0))

    def test_a_delivery_past_the_position_takes_its_call_out_of_the_group(self):
        self.write(prompt("go"), {
            "type": "assistant", "timestamp": AT, "cwd": CWD,
            "message": {"content": [
                {"type": "tool_use", "id": "t0", "name": "Read", "input": {"file_path": "/srv/proj/a.pdf"}},
                {"type": "tool_use", "id": "t1", "name": "SendUserFile",
                 "input": {"files": ["/srv/proj/a.pdf"]}}]}}, result("t0"))
        first = chat.feed(self.path, limit=40)
        self.assertEqual([c["use"] for c in first["items"][-1]["calls"]], ["t0", "t1"])
        self.write({"type": "user", "timestamp": AT, "cwd": CWD,
                    "message": {"content": [{"type": "tool_result", "tool_use_id": "t1",
                                             "content": "1 file delivered to user."}]},
                    "toolUseResult": {"caption": "", "attachments": [
                        {"path": "/srv/proj/a.pdf", "size": 10, "media_type": "application/pdf"}]}})
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual([i["role"] for i in more["items"]], ["tools", "sent"])
        self.assertEqual([c["use"] for c in more["items"][0]["calls"]], ["t0"])


class Before(Window):
    def test_a_page_that_ends_on_a_call_sees_its_result_past_the_page(self):
        self.write(prompt("go"), call("t1"))
        edge = self.write(result("t1", error=True))
        self.write(*[prompt(f"prompt {n}") for n in range(3)])
        page = chat.feed(self.path, limit=10, before=edge)
        self.assertEqual([i["role"] for i in page["items"]], ["me", "tools"],
                         "the page took in rows from past its end")
        self.assertEqual(calls_of(page["items"]), [("t1", None, True)])

    def test_a_page_that_ends_on_a_letter_sees_its_refusal_past_the_page(self):
        self.write(prompt("go"), letter("t1"))
        edge = self.write(result("t1", text=UNREACHABLE))
        self.write(*[prompt(f"prompt {n}") for n in range(3)])
        page = chat.feed(self.path, limit=10, before=edge)
        self.assertEqual(letters_of(page["items"]), [("t1", REASON)],
                         "the page stopped at its end with the letter still on its way")


class Letters(Window):
    def test_a_letter_that_reached_nobody_says_why(self):
        self.write(prompt("go"), letter("t1"), result("t1", text=UNREACHABLE))
        self.assertEqual(letters_of(chat.feed(self.path)["items"]), [("t1", REASON)],
                         "a refused letter is drawn as one sent")

    def test_a_delivered_letter_carries_no_mark(self):
        self.write(prompt("go"), letter("t1", to="main"), result("t1", text=QUEUED))
        got = [i for i in chat.feed(self.path)["items"] if i["role"] == "mail"]
        self.assertEqual(letters_of(got), [("t1", None)])
        self.assertNotIn("open", got[0], "the answer came, the letter is not on its way any more")

    def test_a_call_that_failed_delivered_nothing(self):
        said = "<tool_use_error>InputValidationError: to is required</tool_use_error>"
        self.write(prompt("go"), letter("t1"), result("t1", error=True, text=said))
        self.assertEqual(letters_of(chat.feed(self.path)["items"]), [("t1", said)])

    def test_a_letter_with_no_answer_yet_is_not_called_lost(self):
        self.write(prompt("go"), letter("t1"))
        self.assertEqual(letters_of(chat.feed(self.path)["items"]), [("t1", None)])

    def test_the_answer_of_another_call_is_not_read_as_a_letter(self):
        self.write(prompt("go"), call("t1"), result("t1", text=UNREACHABLE))
        got = chat.feed(self.path)["items"]
        self.assertEqual(calls_of(got), [("t1", None, None)])
        self.assertNotIn("undelivered", json.dumps(got))

    def test_two_letters_alike_are_two_letters(self):
        self.write(prompt("go"), letter("t1"), result("t1", text=UNREACHABLE),
                   letter("t2"), result("t2", text=QUEUED))
        self.assertEqual(letters_of(chat.feed(self.path)["items"]), [("t1", REASON), ("t2", None)],
                         "the second try is folded into the first, and the feed says it failed")

    def test_a_refusal_past_the_position_sends_the_letter_again(self):
        self.write(prompt("go"), letter("t1"))
        first = chat.feed(self.path, limit=40)
        sent = first["items"][-1]
        self.write(result("t1", text=UNREACHABLE))
        more = chat.feed(self.path, limit=40, after=first["last"])
        self.assertEqual([(i["role"], i["pos"]) for i in more["items"]], [("mail", sent["pos"])],
                         "the reader finds the letter by its position and role and redraws it")
        self.assertEqual(letters_of(more["items"]), [("t1", REASON)])


if __name__ == "__main__":
    unittest.main()
