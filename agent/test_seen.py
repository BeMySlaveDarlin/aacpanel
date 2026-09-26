import json
import os
import socket
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import chat  # noqa: E402
import seen  # noqa: E402

TALK = "6b0a0f2c-3d1e-4a5b-8c7d-9e0f1a2b3c4d"


def user_line(text):
    return json.dumps({"type": "user", "message": {"role": "user",
                                                   "content": [{"type": "text", "text": text}]}},
                      ensure_ascii=False)


def queue_line(text):
    return json.dumps({"type": "queue-operation", "operation": "enqueue", "content": text},
                      ensure_ascii=False)


def assistant_line(text):
    return json.dumps({"type": "assistant", "message": {"role": "assistant",
                                                        "content": [{"type": "text", "text": text}]}},
                      ensure_ascii=False)


class Seen(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.was = chat.PROJECTS_DIR
        chat.PROJECTS_DIR = os.path.join(self.dir.name, "projects")
        self.addCleanup(self.restore)
        self.talk = os.path.join(chat.PROJECTS_DIR, "-srv-proj-panel", TALK + ".jsonl")
        os.makedirs(os.path.dirname(self.talk))
        open(self.talk, "w", encoding="utf-8").close()

    def restore(self):
        chat.PROJECTS_DIR = self.was
        with seen._paths_lock:
            seen._paths.clear()

    def append(self, line):
        with open(self.talk, "a", encoding="utf-8") as f:
            f.write(line + "\n")

    def start(self):
        reply = seen.answer({"session": TALK})
        self.assertTrue(reply["ok"])
        self.assertTrue(reply["found"])
        self.assertFalse(reply["seen"])
        return reply["pos"]

    def test_a_prompt_to_a_free_session_is_seen(self):
        pos = self.start()
        self.append(user_line("a delivery check, a prompt longer than the mark"))
        reply = seen.answer({"session": TALK, "pos": pos, "mark": "adeliverycheck,aprompt"})
        self.assertTrue(reply["seen"])
        self.assertFalse(reply["queued"])
        self.assertGreater(reply["pos"], pos)

    def test_a_queued_prompt_is_named_queued(self):
        pos = self.start()
        self.append(queue_line("a prompt into a busy session"))
        reply = seen.answer({"session": TALK, "pos": pos, "mark": "apromptintoabusysession"})
        self.assertTrue(reply["seen"])
        self.assertTrue(reply["queued"])

    def test_a_prompt_with_a_file_is_known_by_its_chip(self):
        pos = self.start()
        self.append(queue_line("[Image #1]"))
        reply = seen.answer({"session": TALK, "pos": pos,
                             "mark": "/srv/files/20260908-035522-shot.png"})
        self.assertTrue(reply["seen"])

    def test_an_earlier_prompt_does_not_count_as_delivery(self):
        text = "this prompt was already sent an hour ago"
        self.append(user_line(text))
        pos = self.start()
        reply = seen.answer({"session": TALK, "pos": pos, "mark": seen.squeeze(text)})
        self.assertFalse(reply["seen"])

    def test_a_quote_by_the_model_does_not_count_as_delivery(self):
        text = "count how much two plus two is"
        pos = self.start()
        self.append(assistant_line(text))
        reply = seen.answer({"session": TALK, "pos": pos, "mark": seen.squeeze(text)})
        self.assertFalse(reply["seen"])

    def test_someone_elses_prompt_does_not_pass_for_ours(self):
        pos = self.start()
        self.append(user_line("a completely different conversation about something else"))
        reply = seen.answer({"session": TALK, "pos": pos, "mark": "ourpromptthatisnotthere"})
        self.assertFalse(reply["seen"])

    def test_text_split_across_lines_is_found(self):
        pos = self.start()
        self.append(user_line("look at the logs\nof the stack and say,\nwhat fell over"))
        reply = seen.answer({"session": TALK, "pos": pos, "mark": "lookatthelogsofthestackandsay"})
        self.assertTrue(reply["seen"])

    def test_a_half_written_record_does_not_move_the_position(self):
        pos = self.start()
        half = user_line("a prompt the core did not finish writing")
        with open(self.talk, "a", encoding="utf-8") as f:
            f.write(half[: len(half) // 2])
        reply = seen.answer({"session": TALK, "pos": pos, "mark": "apromptthecoredidnot"})
        self.assertFalse(reply["seen"])
        self.assertEqual(reply["pos"], pos)
        with open(self.talk, "a", encoding="utf-8") as f:
            f.write(half[len(half) // 2:] + "\n")
        again = seen.answer({"session": TALK, "pos": reply["pos"], "mark": "apromptthecoredidnot"})
        self.assertTrue(again["seen"])

    def test_the_conversation_is_not_there(self):
        reply = seen.answer({"session": "0b0a0f2c-3d1e-4a5b-8c7d-9e0f1a2b3c4d"})
        self.assertTrue(reply["ok"])
        self.assertFalse(reply["found"])
        self.assertFalse(reply["seen"])

    def test_the_conversation_is_not_a_uuid(self):
        reply = seen.answer({"session": "../../../etc/passwd", "pos": 0, "mark": "x"})
        self.assertFalse(reply["found"])

    def test_the_conversation_is_not_named(self):
        self.assertFalse(seen.answer({"pos": 0})["ok"])

    def test_a_long_mark_is_rejected(self):
        reply = seen.answer({"session": TALK, "pos": 0, "mark": "a" * (seen.MAX_MARK + 1)})
        self.assertFalse(reply["ok"])

    def test_the_reply_carries_only_the_fact(self):
        secret = "the bank key hunter2 and a private conversation"
        pos = self.start()
        self.append(user_line(secret))
        reply = seen.answer({"session": TALK, "pos": pos, "mark": "ourpromptthatisnotthere"})
        body = json.dumps(reply, ensure_ascii=False)
        self.assertNotIn("hunter2", body)
        self.assertNotIn(self.talk, body)
        self.assertEqual(set(reply), {"ok", "found", "pos", "seen", "queued"})

    def test_the_path_of_the_conversation_is_remembered_and_checked(self):
        self.assertEqual(seen.locate(TALK), self.talk)
        os.unlink(self.talk)
        self.assertEqual(seen.locate(TALK), "")

    def test_the_socket_answers_json(self):
        pos = self.start()
        self.append(user_line("a prompt that arrived over the socket"))
        here, there = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
        with here:
            here.sendall(json.dumps({"session": TALK, "pos": pos,
                                     "mark": "apromptthatarrivedoverthesocket"}).encode("utf-8"))
            seen.handle(there)
            reply = json.loads(here.recv(seen.MAX_REQUEST).decode("utf-8"))
        self.assertTrue(reply["seen"])

    def test_garbage_in_the_socket_does_not_break_the_reply(self):
        here, there = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
        with here:
            here.sendall("not json at all".encode("utf-8"))
            seen.handle(there)
            reply = json.loads(here.recv(seen.MAX_REQUEST).decode("utf-8"))
        self.assertFalse(reply["ok"])


if __name__ == "__main__":
    unittest.main()


class TurnEnded(Seen):
    """Whether the session's own turn is over, for a console busy with the agents it sent off."""

    def turn(self):
        reply = seen.answer({"session": TALK, "ask": "turn"})
        self.assertTrue(reply["ok"])
        return reply["found"], reply["ended"]

    def test_an_answer_that_ended_the_turn_and_the_hooks_after_it(self):
        self.append(json.dumps({"type": "user", "message": {"content": "launch an agent"}}))
        self.append(json.dumps({"type": "assistant", "message": {"stop_reason": "end_turn"}}))
        self.append(json.dumps({"type": "system", "subtype": "stop_hook_summary"}))
        self.assertEqual(self.turn(), (True, True))

    def test_a_prompt_the_news_of_a_task_or_a_call_is_a_turn_going_on(self):
        for last in ({"type": "user", "message": {"content": "write about rivers"}},
                     {"type": "user", "message": {"content": "<task-notification>done</task-notification>"}},
                     {"type": "assistant", "message": {"stop_reason": "tool_use"}}):
            self.append(json.dumps({"type": "assistant", "message": {"stop_reason": "end_turn"}}))
            self.append(json.dumps(last))
            self.assertEqual(self.turn(), (True, False), f"{last} read as a turn that is over")

    def test_the_words_of_an_agent_are_its_own(self):
        self.append(json.dumps({"type": "user", "message": {"content": "write about rivers"}}))
        self.append(json.dumps({"type": "assistant", "isSidechain": True, "message": {"stop_reason": "end_turn"}}))
        self.assertEqual(self.turn(), (True, False))

    def test_a_conversation_with_no_word_yet_is_not_known(self):
        self.assertEqual(self.turn(), (False, False))


class LastMode(Seen):
    """The mode a console was last in, for a move of it to the feed."""

    START = 1790000000000  # 2026-09-21T14:13:20Z

    def mode(self, since=START):
        reply = seen.answer({"session": TALK, "ask": "mode", "since": since})
        self.assertTrue(reply["ok"])
        return reply["mode"]

    def test_the_last_mode_said_since_the_start(self):
        self.append(json.dumps({"type": "user", "permissionMode": "default", "timestamp": "2026-09-21T14:14:00Z"}))
        self.append(json.dumps({"type": "assistant", "message": {"stop_reason": "end_turn"}}))
        self.append(json.dumps({"type": "user", "permissionMode": "plan", "timestamp": "2026-09-21T14:15:00.5Z"}))
        self.append(json.dumps({"type": "assistant", "message": {"stop_reason": "end_turn"}}))
        self.assertEqual(self.mode(), "plan")

    def test_a_mode_said_before_the_start_is_another_process(self):
        self.append(json.dumps({"type": "user", "permissionMode": "auto", "timestamp": "2026-09-21T14:13:19.999Z"}))
        self.assertEqual(self.mode(), "")

    def test_a_fraction_of_a_second_is_read_as_time_not_as_text(self):
        self.append(json.dumps({"type": "user", "permissionMode": "auto", "timestamp": "2026-09-21T14:13:20.5Z"}))
        self.assertEqual(self.mode(), "auto")

    def test_a_record_of_the_mode_is_dated_by_the_word_before_it(self):
        self.append(json.dumps({"type": "user", "permissionMode": "default", "timestamp": "2026-09-21T14:14:00Z"}))
        self.append(json.dumps({"type": "assistant", "timestamp": "2026-09-21T14:14:05Z"}))
        self.append(json.dumps({"type": "permission-mode", "permissionMode": "plan"}))
        self.assertEqual(self.mode(), "plan", "the mode changed in the console after the last message is lost")

    def test_a_record_of_the_mode_before_the_start_is_another_process(self):
        self.append(json.dumps({"type": "assistant", "timestamp": "2026-09-21T14:13:00Z"}))
        self.append(json.dumps({"type": "permission-mode", "permissionMode": "plan"}))
        self.assertEqual(self.mode(), "")

    def test_a_start_that_is_not_a_time_is_refused(self):
        for since in (None, "2026-09-21", True, -1):
            reply = seen.answer({"session": TALK, "ask": "mode", "since": since})
            self.assertFalse(reply["ok"], f"since {since!r} was taken")
