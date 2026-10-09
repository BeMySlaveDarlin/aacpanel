"""Search of a conversation: what is found, where it stands and what the screen gets of it."""
import json
import os
import unittest

import test_barrier

import chat
from chat import search as found
from chat import tail


UUID = "55555555-5555-5555-5555-555555555555"
AGENT = "aalpha-0123456789abcdef"

# A word in Cyrillic, the same word in capitals and one more letter: two bytes
# a letter in UTF-8, one unit in UTF-16, and a case of their own.
WORD = "\u043f\u0430\u043d\u0435\u043b\u044c"
WORD_UP = "\u041f\u0410\u041d\u0415\u041b\u042c"
LETTER = "\u0436"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def at(n):
    return f"2026-10-09T10:{n // 60:02d}:{n % 60:02d}Z"


def user(text, n=0, **extra):
    return line({"type": "user", "message": {"content": text}, "timestamp": at(n), **extra})


def assistant(*blocks, n=0, **extra):
    return line({"type": "assistant", "message": {"content": list(blocks)}, "timestamp": at(n), **extra})


def text(said):
    return {"type": "text", "text": said}


def call(tool, use, **data):
    return {"type": "tool_use", "name": tool, "id": use, "input": data}


def result(use, said, n=0):
    return line({"type": "user", "timestamp": at(n), "message": {"content": [
        {"type": "tool_result", "tool_use_id": use, "content": [{"type": "text", "text": said}]}]}})


def enqueue(said, n=0):
    return line({"type": "queue-operation", "operation": "enqueue", "content": said, "timestamp": at(n)})


def dequeue(n=0):
    return line({"type": "queue-operation", "operation": "dequeue", "timestamp": at(n)})


def letter(who, said):
    body = json.dumps({"from": who, "result": said}, ensure_ascii=False)
    return f'<teammate-message teammate_id="{who}">{body}</teammate-message>'


def hit_of(match):
    """Returns the words the hit marks, read the way the screen reads them: in UTF-16 units."""
    raw = match["snippet"].encode("utf-16-le")
    start, size = match["hit"]
    return raw[2 * start:2 * (start + size)].decode("utf-16-le")


def units(said):
    return len(said.encode("utf-16-le")) // 2


# The rows of the feed a role of a match stands for.
ROWS = {"me": ("me",), "assistant": ("ai",), "letter": ("mail",), "card": ("brief", "artifact", "secret")}

SHELF = {"seven": {"title": "Panel: seven questions", "questions": [{"kind": "pick"}]}}


class Search(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.projects = os.path.join(self.dir.name, "projects")
        os.makedirs(os.path.join(self.projects, "-srv-proj-x"))
        self.path = os.path.join(self.projects, "-srv-proj-x", f"{UUID}.jsonl")
        self.old, chat.PROJECTS_DIR = chat.PROJECTS_DIR, self.projects
        self.addCleanup(lambda: setattr(chat, "PROJECTS_DIR", self.old))
        # The titles of briefs come from the shelf the parser reads them off.
        self.shelf, tail._shelf = tail._shelf, SHELF.get
        self.addCleanup(lambda: setattr(tail, "_shelf", self.shelf))

    def write(self, *raws, path=None):
        with open(path or self.path, "w", encoding="utf-8") as f:
            f.write("".join(raws))

    def ask(self, q, **more):
        reply = chat.answer({"session": UUID, "search": {"q": q, **more}})
        self.assertTrue(reply["ok"], reply.get("error"))
        return reply

    def conversation(self):
        """Writes a conversation where one word stands in every kind of row, shown or not."""
        self.write(
            user("Fix the panel on the phone", n=1),
            assistant({"type": "thinking", "thinking": "hidden: the panel in a thought"}, n=2),
            assistant(text("The panel is ready.\nTake a look."), n=3),
            assistant(call("Bash", "toolu_grep", command="grep -r 'hidden panel' ."), n=4),
            result("toolu_grep", "hidden: the panel in the output", n=5),
            line({"type": "attachment", "timestamp": at(6), "attachment": {
                "type": "hook_system_message", "hookName": "Stop", "content": "hidden: the panel of a hook"}}),
            user("Stop hook feedback:\n[checklist] hidden: the panel of a hook", n=7),
            user(letter("helper", "Panel built, the tests are green"), n=8),
            assistant(call("Artifact", "toolu_art", file_path="/tmp/x/shots.html", title="Shots of the panel"), n=9),
            assistant(call("mcp__aacpanel__secret_ask", "toolu_secret", name="deploy-token",
                           title="Token of the panel", template="TOKEN=\n"), n=10),
            result("toolu_secret", "Asked as deploy-token.\nThe person fills the notepad in the panel.", n=11),
            assistant(call("mcp__aacpanel__brief_publish", "toolu_brief", doc={"id": "seven"}), n=12),
            result("toolu_brief", "Published as seven: the person sees it in the panel", n=13),
        )

    def test_what_the_feed_shows_as_the_conversation_is_found(self):
        self.conversation()
        got = self.ask("panel")
        self.assertEqual([(m["role"], hit_of(m)) for m in got["matches"]],
                         [("me", "panel"), ("assistant", "panel"), ("letter", "Panel"),
                          ("card", "panel"), ("card", "panel"), ("card", "Panel")])
        self.assertEqual(got["total"], 6)
        self.assertFalse(got["cut"])
        named = self.ask("deploy-token")["matches"]
        self.assertEqual([(m["role"], m["snippet"]) for m in named], [("card", "deploy-token")],
                         "a secret is found by its name, and its call and answer are not")

    def test_calls_their_output_thinking_and_hooks_are_not_found(self):
        self.conversation()
        got = self.ask("hidden")
        self.assertEqual((got["matches"], got["total"]), ([], 0),
                         "the words of a call, its output, a thought or a hook were searched")

    def test_case_is_folded_on_both_sides(self):
        self.write(user(f"Fix the {WORD_UP} and the PANEL", n=1), assistant(text("Die Straße ist lang"), n=2))
        self.assertEqual([hit_of(m) for m in self.ask(WORD)["matches"]], [WORD_UP])
        self.assertEqual([hit_of(m) for m in self.ask("Panel")["matches"]], ["PANEL"])
        self.assertEqual([hit_of(m) for m in self.ask("STRASSE")["matches"]], ["Straße"],
                         "one letter that folds into two is found and marked whole")

    def test_a_phrase_across_a_line_break_is_found(self):
        self.write(assistant(text("The first line ends here\n\n  and the second\tstarts there"), n=1))
        got = self.ask("here and the second")
        self.assertEqual([hit_of(m) for m in got["matches"]], ["here and the second"])
        self.assertEqual(got["matches"][0]["snippet"],
                         "The first line ends here and the second starts there",
                         "the whitespace of the snippet is not folded")
        self.assertEqual(len(self.ask("  here \n and  the second ")["matches"]), 1,
                         "the question is folded the way the text is")

    def test_a_snippet_holds_160_units_around_the_hit_counted_in_utf16(self):
        self.write(user("😀" * 200 + f" {WORD_UP} " + LETTER * 300, n=1),
                   user(f"briefly on the {WORD}", n=2),
                   user(WORD + "😀" * 200, n=3))
        got = self.ask(WORD)["matches"]
        self.assertEqual(len(got), 3)
        for match in got:
            self.assertLessEqual(units(match["snippet"]), found.SNIPPET, match["snippet"][:20])
            self.assertEqual(hit_of(match).casefold(), WORD)
        middle = got[0]
        self.assertEqual(units(middle["snippet"]), found.SNIPPET)
        self.assertTrue(middle["snippet"].startswith("😀"), "the snippet takes the text before the hit")
        self.assertTrue(middle["snippet"].endswith(LETTER), "the snippet takes the text after the hit")
        self.assertEqual(middle["hit"][1], 6)
        self.assertEqual(got[1]["snippet"], f"briefly on the {WORD}")
        self.assertEqual(got[1]["hit"], [15, 6])
        self.assertEqual(got[2]["hit"], [0, 6])
        self.assertEqual(units(got[2]["snippet"]), found.SNIPPET, "an emoji is two units and is not split")

    def test_a_hit_after_an_emoji_is_placed_in_utf16_units(self):
        self.write(user(f"😀😀 {WORD}", n=1))
        got = self.ask(WORD)["matches"]
        self.assertEqual(got[0]["hit"], [5, 6], "two emoji are four units, not two characters")
        self.assertEqual(hit_of(got[0]), WORD)

    def test_a_hit_longer_than_the_snippet_keeps_its_head(self):
        long = ("panel " * 34).strip()
        self.write(user(long, n=1))
        got = self.ask(long[:200])["matches"]
        self.assertEqual(len(got), 1)
        self.assertEqual(got[0]["hit"], [0, found.SNIPPET])
        self.assertEqual(got[0]["snippet"], long[:found.SNIPPET])

    def test_matches_come_in_the_order_of_the_transcript_one_per_hit(self):
        self.write(user("panel one, panel two", n=1),
                   assistant(text("panel three"), n=2),
                   user("panel four", n=3))
        got = self.ask("panel")["matches"]
        self.assertEqual([m["snippet"][m["hit"][0] + 6:] for m in got],
                         ["one, panel two", "two", "three", "four"])
        positions = [m["pos"] for m in got]
        self.assertEqual(positions, sorted(positions))
        self.assertEqual(positions[0], positions[1], "two hits of one row stand at the place of the row")
        self.assertEqual([m["at"] for m in got], [at(1), at(1), at(2), at(3)])

    def test_over_the_limit_the_newest_are_kept_and_the_answer_says_it_is_cut(self):
        self.write(*(user(f"panel number {n}", n=n) for n in range(10)))
        got = self.ask("panel", limit=3)
        self.assertEqual([m["snippet"] for m in got["matches"]],
                         ["panel number 7", "panel number 8", "panel number 9"],
                         "the screen starts from the newest match, so the newest are the ones kept")
        self.assertEqual(got["total"], 10)
        self.assertTrue(got["cut"])
        whole = self.ask("panel", limit=10)
        self.assertEqual((len(whole["matches"]), whole["total"], whole["cut"]), (10, 10, False))

    def test_the_limit_is_capped(self):
        self.write(*(user("panel " * 50, n=n) for n in range(10)))
        got = self.ask("panel", limit=10_000)
        self.assertEqual(len(got["matches"]), found.MAX_MATCHES)
        self.assertEqual(got["total"], 500)
        self.assertTrue(got["cut"])

    def test_a_short_or_a_long_question_is_refused(self):
        self.write(user("panel", n=1))
        for q in ("", "  ", " p ", "\np\t", "p" * 201, " " + WORD * 34 + " ", None, 42):
            with self.subTest(q=q):
                reply = chat.answer({"session": UUID, "search": {"q": q}})
                self.assertFalse(reply["ok"])
                self.assertTrue(reply["error"])
        for q in ("pa", " pa ", "p" * 200, " " + WORD * 33 + "pa\n"):
            with self.subTest(q=q):
                self.assertTrue(chat.answer({"session": UUID, "search": {"q": q}})["ok"])

    def test_the_answer_has_the_shape_the_service_reads(self):
        self.write(user("Fix the panel", n=1))
        reply = self.ask("panel")
        self.assertEqual(set(reply), {"ok", "session", "matches", "total", "cut"})
        self.assertEqual(reply["session"], UUID)
        self.assertEqual(reply["matches"], [{"pos": 0, "at": at(1), "role": "me",
                                             "snippet": "Fix the panel", "hit": [8, 5]}])
        self.assertEqual(self.ask("nothing like it")["matches"], [], "no match is an empty list, not an absent one")

    def test_the_place_of_a_match_is_the_place_of_its_row_in_the_feed(self):
        self.conversation()
        with open(self.path, "a", encoding="utf-8") as f:
            f.write("".join([enqueue("the panel in the queue", n=20), assistant(text("thinking"), n=21),
                             dequeue(n=22), user("the panel in the queue", n=22, promptSource="sdk"),
                             assistant(text("and one more on the panel"), n=23)]))
        got = self.ask("panel")["matches"]
        self.assertEqual(len(got), 8)
        tail_rows = chat.feed(self.path, limit=200)["items"]
        for match in got:
            with self.subTest(match=match["snippet"]):
                rows = [item for item in tail_rows if item["pos"] == match["pos"]
                        and item.get("nth") == match.get("nth") and item["role"] in ROWS[match["role"]]]
                self.assertEqual(len(rows), 1, "the place names no row of the feed")
                # The screen opens the window that ends with the match.
                window = chat.feed(self.path, limit=5, before=match["pos"] + 1)["items"]
                self.assertEqual(window[-1]["pos"], match["pos"],
                                 "the window before the next place does not end with the match")

    def test_a_message_through_the_queue_is_found_once(self):
        self.write(enqueue("fix the panel", n=1), assistant(text("looking"), n=2),
                   dequeue(n=3), user("fix the panel", n=3, promptSource="sdk"))
        got = self.ask("panel")
        self.assertEqual([(m["role"], m["pos"]) for m in got["matches"]], [("me", 0)],
                         "the bubble the queue drew and the prompt it became are one row")
        self.assertEqual(got["total"], 1)

    def test_a_bubble_that_turns_out_an_alarm_is_not_found(self):
        prompt = "Check the panel and pick up a task"
        self.write(assistant(call("ScheduleWakeup", "toolu_wake", delaySeconds=300,
                                  prompt="<<autonomous-loop-dynamic>>"), n=1),
                   enqueue(prompt, n=2), dequeue(n=3),
                   user(prompt, n=4, isMeta=True, promptSource="system", turnOrigin="scheduled"))
        rows = chat.feed(self.path)["items"]
        self.assertIn("wake", [item["role"] for item in rows], "the alarm did not take the place of the bubble")
        got = self.ask("panel")
        self.assertEqual((got["matches"], got["total"]), ([], 0),
                         "the bubble the alarm took the place of is still found")

    def test_every_row_of_a_record_of_several_is_found_and_named_by_its_number(self):
        self.write(assistant(text("first panel"), text("second panel"), n=1),
                   user(letter("a", "panel one") + "\n" + letter("b", "panel two"), n=2))
        shown = [item for item in chat.feed(self.path)["items"] if item["role"] in ("ai", "mail")]
        got = self.ask("panel")["matches"]
        self.assertEqual([(m["role"], m["snippet"], m.get("nth")) for m in got],
                         [("assistant", "first panel", None), ("assistant", "second panel", 1),
                          ("letter", "panel one", None), ("letter", "panel two", 1)])
        for match in got:
            row = [item for item in shown if item["pos"] == match["pos"] and item.get("nth") == match.get("nth")
                   and item["role"] in ROWS[match["role"]]]
            self.assertEqual(len(row), 1, "the place and the number name no row of the feed")
            self.assertEqual(match["snippet"], row[0]["text"])

    def test_a_letter_that_arrives_twice_is_found_once(self):
        said = letter("helper", "the panel is built")
        self.write(user("Another Claude session sent a message:\n" + said, n=1), user(said, n=2))
        self.assertEqual(len([i for i in chat.feed(self.path)["items"] if i["role"] == "mail"]), 1)
        self.assertEqual(self.ask("panel")["total"], 1)

    def test_the_feed_of_an_agent_is_searched_in_place_of_the_conversation(self):
        self.write(user("the panel of the parent", n=1))
        subs = os.path.join(self.projects, "-srv-proj-x", UUID, "subagents")
        os.makedirs(subs)
        self.write(assistant(text("the panel of the agent"), n=1, isSidechain=True, agentId=AGENT),
                   path=os.path.join(subs, f"agent-{AGENT}.jsonl"))
        reply = chat.answer({"session": UUID, "subagent": AGENT, "search": {"q": "panel"}})
        self.assertTrue(reply["ok"], reply.get("error"))
        self.assertEqual([m["snippet"] for m in reply["matches"]], ["the panel of the agent"])
        self.assertEqual(reply["subagent"], AGENT)

    def test_a_conversation_not_on_disk_is_refused_as_the_feed_refuses_it(self):
        reply = chat.answer({"session": "11111111-2222-3333-4444-555555555555", "search": {"q": "panel"}})
        self.assertFalse(reply["ok"])
        self.assertIn("no transcript", reply["error"])


if __name__ == "__main__":
    unittest.main()
