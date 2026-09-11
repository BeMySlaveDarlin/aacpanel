import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
from usage import parse_file  # noqa: E402

SESSION = "55555555-5555-4555-8555-555555555555"
AGENT = "a1111111111111111"


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def common(stamp, kind, **extra):
    record = {"parentUuid": None, "isSidechain": False, "type": kind,
              "userType": "external", "entrypoint": "cli",
              "cwd": "/srv/proj/aacpanel",
              "sessionId": SESSION, "version": "2.1.224", "gitBranch": "master",
              "timestamp": stamp, "uuid": "u-" + stamp}
    record.update(extra)
    return record


def usage(input_tokens=6357, output=446, read=0, made=26284,
          hour=0, five=0, speed="standard", tier="standard", steps=1):
    block = {
        "input_tokens": input_tokens,
        "cache_creation_input_tokens": made,
        "cache_read_input_tokens": read,
        "output_tokens": output,
        "server_tool_use": {"web_search_requests": 0, "web_fetch_requests": 0},
        "cache_creation": {"ephemeral_1h_input_tokens": hour,
                           "ephemeral_5m_input_tokens": five},
        "inference_geo": "not_available",
    }
    if tier is not None:
        block["service_tier"] = tier
    if speed is not None:
        block["speed"] = speed
    if steps is not None:
        block["iterations"] = [{"type": "message"}] * steps
    return block


def answer(stamp, mid="msg_01AAAAAAAAAAAAAAAAAAAAAA", content=None,
           model="claude-opus-5", block=None, **extra):
    return common(stamp, "assistant",
                  requestId="req_01BBBBBBBBBBBBBBBBBBBBBB",
                  message={"model": model, "id": mid, "type": "message",
                           "role": "assistant",
                           "content": content if content is not None else [
                               {"type": "text", "text": "done"}],
                           "stop_reason": "tool_use", "stop_sequence": None,
                           "stop_details": None,
                           "usage": block if block is not None else usage(),
                           "diagnostics": None},
                  **extra)


def human(stamp, text="next", **extra):
    return common(stamp, "user",
                  message={"role": "user",
                           "content": [{"type": "text", "text": text}]},
                  **extra)


def result(stamp, call="toolu_01", is_error=False, **extra):
    return common(stamp, "user",
                  message={"role": "user", "content": [
                      {"type": "tool_result", "tool_use_id": call,
                       "content": "ok", "is_error": is_error}]},
                  **extra)


class File:
    def __init__(self, case, name=None, sub=False):
        root = test_barrier.tmp_path(prefix="aacpanel-usage-")
        case.addCleanup(_rmtree, root)
        if sub:
            root = os.path.join(root, SESSION, "subagents")
            os.makedirs(root)
            name = name or ("agent-%s.jsonl" % AGENT)
        self.path = os.path.join(root, name or (SESSION + ".jsonl"))

    def write(self, *records, tail=""):
        with open(self.path, "w", encoding="utf-8") as f:
            for record in records:
                f.write(line(record))
            f.write(tail)
        return self.path


def _rmtree(path):
    import shutil
    shutil.rmtree(path, ignore_errors=True)


class Dedup(unittest.TestCase):
    def test_three_records_of_one_answer_give_one_answer(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.100Z"),
            answer("2026-09-08T10:00:01.000Z",
                   content=[{"type": "thinking", "thinking": "", "signature": "x"}],
                   block=usage(output=5)),
            answer("2026-09-08T10:00:02.000Z",
                   content=[{"type": "text", "text": "writing"}],
                   block=usage(output=5)),
            answer("2026-09-08T10:00:03.000Z",
                   content=[{"type": "tool_use", "id": "toolu_01",
                             "name": "Bash", "input": {}}],
                   block=usage(output=358)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(len(rows), 1)
        row = rows[0]
        self.assertEqual(row["answers"], 1)
        self.assertEqual(row["inputTokens"], 6357)
        self.assertEqual(row["cacheCreation"], 26284)

    def test_the_output_is_the_maximum_not_the_first_and_not_the_sum(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", block=usage(output=5)),
            answer("2026-09-08T10:00:02.000Z", block=usage(output=5)),
            answer("2026-09-08T10:00:03.000Z", block=usage(output=358)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["outputTokens"], 358)

    def test_different_answers_are_not_glued_together(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A", block=usage(output=10)),
            answer("2026-09-08T10:05:01.000Z", mid="msg_B", block=usage(output=20)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["answers"], 2)
        self.assertEqual(rows[0]["outputTokens"], 30)


class Hour(unittest.TestCase):
    def test_an_answer_on_the_hour_boundary_lies_in_the_hour_of_its_first_record(self):
        path = File(self).write(
            answer("2026-09-08T10:59:59.500Z", block=usage(output=5)),
            answer("2026-09-08T11:00:00.400Z", block=usage(output=358)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual([r["bucket"] for r in rows], ["2026-09-08T10:00:00Z"])
        self.assertEqual(rows[0]["outputTokens"], 358)

    def test_tool_calls_lie_in_the_hour_of_their_answer(self):
        path = File(self).write(
            answer("2026-09-08T10:59:59.500Z", block=usage(output=5)),
            answer("2026-09-08T11:00:00.400Z", block=usage(output=358),
                   content=[{"type": "tool_use", "id": "toolu_01",
                             "name": "Read", "input": {}}]))
        _, tools, _, _, _ = parse_file(path)
        self.assertEqual([(t["bucket"], t["tool"], t["calls"]) for t in tools],
                         [("2026-09-08T10:00:00Z", "Read", 1)])


class Boundary(unittest.TestCase):
    def file(self, *records, tail=""):
        return File(self).write(*records, tail=tail)

    def test_an_unclosed_hour_is_reread_whole(self):
        path = self.file(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T11:00:01.000Z", mid="msg_B"),
            answer("2026-09-08T11:30:01.000Z", mid="msg_C"))
        parsed = parse_file(path)
        size = os.path.getsize(path)
        self.assertLess(parsed.offset, size)
        incremental = parse_file(path, parsed.offset)
        self.assertEqual([r["bucket"] for r in incremental.rows],
                         ["2026-09-08T11:00:00Z"])
        self.assertEqual(incremental.rows[0]["answers"], 2)

    def test_an_incremental_read_brings_the_hour_whole_not_its_tail(self):
        full = self.file(
            answer("2026-09-08T11:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T11:20:01.000Z", mid="msg_B"),
            answer("2026-09-08T11:40:01.000Z", mid="msg_C"))
        first = self.file(
            answer("2026-09-08T11:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T11:20:01.000Z", mid="msg_B"))
        before = parse_file(first)
        after = parse_file(full, before.offset)
        self.assertEqual(after.rows[0]["answers"], 3)

    def test_the_boundary_does_not_cut_an_hour_in_half(self):
        path = self.file(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T11:00:01.000Z", mid="msg_B"),
            answer("2026-09-08T10:59:59.900Z", mid="msg_C"),
            answer("2026-09-08T11:10:01.000Z", mid="msg_D"))
        parsed = parse_file(path)
        incremental = parse_file(path, parsed.offset)
        hours = {r["bucket"]: r["answers"] for r in incremental.rows}
        self.assertEqual(hours.get("2026-09-08T10:00:00Z"), 2)

    def test_an_unfinished_line_is_not_parsed_and_does_not_move_the_boundary(self):
        tail = json.dumps(answer("2026-09-08T12:00:01.000Z", mid="msg_X"))[:80]
        path = self.file(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T11:00:01.000Z", mid="msg_B"),
            tail=tail)
        parsed = parse_file(path)
        self.assertEqual(len(parsed.rows), 2)
        self.assertLess(parsed.offset, os.path.getsize(path) - len(tail) + 1)

    def test_a_broken_line_in_the_middle_does_not_stop_the_file(self):
        path = self.file()
        with open(path, "w", encoding="utf-8") as f:
            f.write(line(answer("2026-09-08T10:00:01.000Z", mid="msg_A")))
            f.write('{"type":"assistant","message":{"id":"msg_B"\n')
            f.write(line(answer("2026-09-08T11:00:01.000Z", mid="msg_C")))
        parsed = parse_file(path)
        self.assertEqual(len(parsed.rows), 2)
        self.assertGreater(parsed.offset, 0)

    def test_a_file_without_timed_records_is_not_reread_forever(self):
        path = File(self).write(
            {"type": "queue-operation", "uuid": "q1"},
            {"type": "mode", "uuid": "m1"})
        parsed = parse_file(path)
        self.assertEqual(parsed.offset, os.path.getsize(path))
        self.assertEqual(parsed.rows, [])

    def test_an_incremental_read_does_not_return_the_tail_of_an_earlier_hour(self):
        start = [answer("2026-09-08T06:00:01.000Z", mid="msg_A"),
                  answer("2026-09-08T06:30:01.000Z", mid="msg_B"),
                  answer("2026-09-08T07:00:01.000Z", mid="msg_C")]
        before = File(self, name="before.jsonl").write(*start)
        after = File(self, name="after.jsonl").write(
            *start,
            answer("2026-09-08T08:00:01.000Z", mid="msg_D"),
            answer("2026-09-08T06:45:01.000Z", mid="msg_E"),
            answer("2026-09-08T08:30:01.000Z", mid="msg_F"))
        previous = parse_file(before)
        self.assertEqual({r["bucket"]: r["answers"] for r in previous.rows},
                         {"2026-09-08T06:00:00Z": 2, "2026-09-08T07:00:00Z": 1})

        incremental = parse_file(after, previous.offset)
        returned = {r["bucket"] for r in incremental.rows}
        self.assertNotIn("2026-09-08T06:00:00Z", returned)
        self.assertEqual(incremental.since, "2026-09-08T07:00:00Z")
        self.assertTrue(incremental.rewind)

    def test_an_ordinary_incremental_read_does_not_ask_for_a_rewind(self):
        path = File(self).write(
            answer("2026-09-08T06:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T07:00:01.000Z", mid="msg_B"),
            answer("2026-09-08T08:00:01.000Z", mid="msg_C"))
        parsed = parse_file(path)
        self.assertFalse(parsed.rewind)
        self.assertFalse(parse_file(path, parsed.offset).rewind)

    def test_a_read_from_zero_never_asks_for_a_rewind(self):
        path = File(self).write(
            answer("2026-09-08T08:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T06:00:01.000Z", mid="msg_B"))
        self.assertFalse(parse_file(path).rewind)

    def test_since_names_the_first_reread_hour(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T11:00:01.000Z", mid="msg_B"))
        parsed = parse_file(path)
        self.assertEqual(parsed.since, "2026-09-08T10:00:00Z")
        incremental = parse_file(path, parsed.offset)
        self.assertEqual(incremental.since, "2026-09-08T11:00:00Z")


class Subagent(unittest.TestCase):
    def test_subagent_events_go_under_its_name(self):
        path = File(self, sub=True).write(
            human("2026-09-08T10:00:00.100Z", isSidechain=True, agentId=AGENT),
            answer("2026-09-08T10:00:01.000Z", isSidechain=True, agentId=AGENT,
                   attributionAgent="Explore"))
        rows, _, events, session, _ = parse_file(path, 0, "personal")
        self.assertEqual([e["agent"] for e in events], [AGENT])
        self.assertEqual(session["sessionId"], SESSION)
        self.assertEqual(rows[0]["agent"], AGENT)
        self.assertEqual(rows[0]["agentKind"], "Explore")

    def test_parent_and_subagent_events_in_one_file_are_not_glued_together(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.100Z"),
            human("2026-09-08T10:00:02.000Z", isSidechain=True))
        _, _, events, _, _ = parse_file(path)
        self.assertEqual(sorted((e["agent"], e["messages"]) for e in events),
                         [("", 1), ("sidechain", 1)])

    def test_the_agent_kind_comes_from_any_record_of_the_file(self):
        path = File(self, sub=True).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A", agentId=AGENT,
                   isSidechain=True),
            answer("2026-09-08T10:20:01.000Z", mid="msg_B", agentId=AGENT,
                   isSidechain=True, attributionAgent="general-purpose"))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["agentKind"], "general-purpose")

    def test_a_sidechain_in_the_root_file_is_not_the_main_session(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A"),
            answer("2026-09-08T10:00:02.000Z", mid="msg_B", isSidechain=True))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(sorted(r["agent"] for r in rows), ["", "sidechain"])


class Agents(unittest.TestCase):
    def test_the_root_file_keeps_the_main_session_and_the_sidechain(self):
        path = File(self).write(answer("2026-09-08T10:00:01.000Z"))
        _, _, _, session, _ = parse_file(path)
        self.assertEqual(session["agents"], ["", "sidechain"])

    def test_a_subagent_file_keeps_its_own_agent(self):
        path = File(self, sub=True).write(
            answer("2026-09-08T10:00:01.000Z", agentId=AGENT, isSidechain=True))
        _, _, _, session, _ = parse_file(path)
        self.assertEqual(session["agents"], [AGENT])

    def test_an_empty_subagent_file_names_its_agent_by_the_file_name(self):
        path = File(self, sub=True).write()
        _, rows, _, session, _ = parse_file(path)
        self.assertEqual(rows, [])
        self.assertEqual(session["agents"], [AGENT])


class Failure(unittest.TestCase):
    def test_a_missing_file_fails_the_parse(self):
        with self.assertRaises(OSError):
            parse_file("/no/such/dir/file.jsonl")

    def test_the_parse_survives_being_passed_between_processes(self):
        import pickle
        path = File(self).write(answer("2026-09-08T10:00:01.000Z"))
        parsed = parse_file(path)
        again = pickle.loads(pickle.dumps(parsed))
        self.assertEqual(again.rows, parsed.rows)
        self.assertEqual(again.offset, parsed.offset)
        self.assertEqual(again.since, parsed.since)


class Events(unittest.TestCase):
    def test_events_are_laid_out_by_hour(self):
        path = File(self).write(
            human("2026-09-08T10:00:01.000Z"),
            human("2026-09-08T11:00:01.000Z"),
            human("2026-09-08T11:30:01.000Z"))
        _, _, events, _, _ = parse_file(path)
        self.assertEqual([(e["bucket"], e["messages"]) for e in events],
                         [("2026-09-08T10:00:00Z", 1), ("2026-09-08T11:00:00Z", 2)])

    def test_a_synthetic_answer_is_an_error_not_usage(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A"),
            common("2026-09-08T10:00:02.000Z", "assistant",
                   message={"model": "<synthetic>", "role": "assistant",
                            "content": [{"type": "text",
                                         "text": "API Error: 529 Overloaded"}],
                            "usage": {"inputTokens": 0, "outputTokens": 0,
                                      "cache_read_input_tokens": 0,
                                      "cache_creation_input_tokens": 0}}))
        rows, _, events, _, _ = parse_file(path)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["answers"], 1)
        self.assertEqual(events[0]["apiErrors"], 1)

    def test_interrupts_are_counted_apart_from_each_other(self):
        path = File(self).write(
            human("2026-09-08T10:00:01.000Z", text="[Request interrupted by user]"),
            human("2026-09-08T10:00:02.000Z",
                  text="[Request interrupted by user for tool use]"),
            human("2026-09-08T10:00:03.000Z", text="[Request interrupted by user]"))
        _, _, events, _, _ = parse_file(path)
        self.assertEqual(events[0]["interrupts"], 2)
        self.assertEqual(events[0]["interruptsTool"], 1)

    def test_an_interrupt_written_as_a_plain_string_is_counted_too(self):
        path = File(self).write(
            common("2026-09-08T10:00:01.000Z", "user",
                   message={"role": "user",
                            "content": "[Request interrupted by user]"}))
        _, _, events, _, _ = parse_file(path)
        self.assertEqual(events[0]["interrupts"], 1)

    def test_a_context_compaction_is_counted(self):
        path = File(self).write(
            common("2026-09-08T10:00:01.000Z", "user", isCompactSummary=True,
                   message={"role": "user", "content": [
                       {"type": "text", "text": "conversation summary"}]}))
        _, _, events, _, _ = parse_file(path)
        self.assertEqual(events[0]["compacts"], 1)


class Tools(unittest.TestCase):
    def test_an_mcp_tool_name_is_kept_whole(self):
        name = "mcp__plugin_toolkit_playwright__browser_console_messages"
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z",
                   content=[{"type": "tool_use", "id": "toolu_01",
                             "name": name, "input": {}}]))
        _, tools, _, _, _ = parse_file(path)
        self.assertEqual(tools[0]["tool"], name)

    def test_a_call_is_not_multiplied_along_with_the_answer_records(self):
        block = [{"type": "tool_use", "id": "toolu_01", "name": "Bash", "input": {}}]
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", content=block),
            answer("2026-09-08T10:00:02.000Z", content=block))
        _, tools, _, _, _ = parse_file(path)
        self.assertEqual(tools[0]["calls"], 1)

    def test_an_error_belongs_to_the_call_not_to_the_answer(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z",
                   content=[{"type": "tool_use", "id": "toolu_01",
                             "name": "Bash", "input": {}}]),
            result("2026-09-08T10:00:05.000Z", call="toolu_01", is_error=True),
            answer("2026-09-08T10:00:06.000Z", mid="msg_B",
                   content=[{"type": "tool_use", "id": "toolu_02",
                             "name": "Read", "input": {}}]),
            result("2026-09-08T10:00:07.000Z", call="toolu_02", is_error=False))
        _, tools, _, _, _ = parse_file(path)
        by_name = {t["tool"]: t for t in tools}
        self.assertEqual(by_name["Bash"]["errors"], 1)
        self.assertEqual(by_name["Read"]["errors"], 0)

    def test_parent_and_subagent_calls_are_not_added_together(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A",
                   content=[{"type": "tool_use", "id": "toolu_01",
                             "name": "Read", "input": {}}]),
            answer("2026-09-08T10:00:02.000Z", mid="msg_B", isSidechain=True,
                   content=[{"type": "tool_use", "id": "toolu_02",
                             "name": "Read", "input": {}}]))
        _, tools, _, _, _ = parse_file(path)
        self.assertEqual(sorted((t["agent"], t["calls"]) for t in tools),
                         [("", 1), ("sidechain", 1)])


class Fields(unittest.TestCase):
    def test_the_answer_speed_is_part_of_the_key(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A",
                   block=usage(speed="standard")),
            answer("2026-09-08T10:10:01.000Z", mid="msg_B",
                   block=usage(speed="fast")))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(sorted(r["speed"] for r in rows), ["fast", "standard"])

    def test_no_speed_in_the_record_means_an_empty_field(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", block=usage(speed=None, tier=None)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["speed"], "")
        self.assertEqual(rows[0]["serviceTier"], "")

    def test_iterations_are_the_length_of_the_list(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A", block=usage(steps=2)),
            answer("2026-09-08T10:10:01.000Z", mid="msg_B", block=usage(steps=None)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["iterations"], 2)

    def test_the_cache_is_split_by_lifetime(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z",
                   block=usage(made=1500, hour=1000, five=500)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual((rows[0]["cacheCreation"], rows[0]["cache1h"],
                          rows[0]["cache5m"]), (1500, 1000, 500))

    def test_thinking_is_counted_in_blocks(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z",
                   content=[{"type": "thinking", "thinking": "", "signature": "a"}]),
            answer("2026-09-08T10:00:02.000Z",
                   content=[{"type": "thinking", "thinking": "", "signature": "b"}]),
            answer("2026-09-08T10:00:03.000Z",
                   content=[{"type": "text", "text": "done"}]))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["thinking"], 2)

    def test_the_model_splits_the_rows(self):
        path = File(self).write(
            answer("2026-09-08T10:00:01.000Z", mid="msg_A", model="claude-opus-5"),
            answer("2026-09-08T10:10:01.000Z", mid="msg_B", model="claude-sonnet-5"))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(sorted(r["model"] for r in rows),
                         ["claude-opus-5", "claude-sonnet-5"])

    def test_the_session_card_is_built_from_the_records(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.100Z"),
            answer("2026-09-08T10:00:01.000Z", gitBranch="feature/usage"))
        _, _, _, session, _ = parse_file(path, 0, "personal")
        self.assertEqual(session["sessionId"], SESSION)
        self.assertEqual(session["contour"], "personal")
        self.assertEqual(session["cwd"], "/srv/proj/aacpanel")
        self.assertEqual(session["gitBranch"], "feature/usage")
        self.assertEqual(session["version"], "2.1.224")
        self.assertEqual(session["startedAt"], "2026-09-08T10:00:00.100Z")
        self.assertEqual(session["endedAt"], "2026-09-08T10:00:01.000Z")


class Timing(unittest.TestCase):
    def test_latency_is_counted_from_the_human_record_to_the_answer(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.000Z"),
            answer("2026-09-08T10:00:07.500Z", mid="msg_A", block=usage(output=5)),
            answer("2026-09-08T10:00:31.000Z", mid="msg_A", block=usage(output=50)))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["latencyMsSum"], 7500)
        self.assertEqual(rows[0]["latencyMsMax"], 7500)

    def test_a_tool_result_also_opens_a_wait(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.000Z"),
            answer("2026-09-08T10:00:02.000Z", mid="msg_A"),
            result("2026-09-08T10:00:30.000Z"),
            answer("2026-09-08T10:00:33.000Z", mid="msg_B"))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["latencyMsSum"], 5000)
        self.assertEqual(rows[0]["latencyMsMax"], 3000)

    def test_the_wait_does_not_go_to_the_second_answer(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.000Z"),
            answer("2026-09-08T10:00:02.000Z", mid="msg_A"),
            answer("2026-09-08T10:00:40.000Z", mid="msg_B"))
        rows, _, _, _, _ = parse_file(path)
        self.assertEqual(rows[0]["latencyMsSum"], 2000)

    def test_a_human_pause_is_not_confused_with_waiting_for_a_tool(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.000Z"),
            answer("2026-09-08T10:00:02.000Z", mid="msg_A"),
            result("2026-09-08T10:20:00.000Z"),
            answer("2026-09-08T10:20:03.000Z", mid="msg_B"),
            human("2026-09-08T10:21:00.000Z"))
        _, _, events, _, _ = parse_file(path)
        idle = events[0]["idle"]
        self.assertEqual(idle["under2m"], 1)
        self.assertEqual(idle["under30s"], 0)
        self.assertEqual(idle["maxMs"], 57000)

    def test_the_pause_is_counted_from_the_last_record_of_the_answer(self):
        path = File(self).write(
            human("2026-09-08T10:00:00.000Z"),
            answer("2026-09-08T10:00:02.000Z", mid="msg_A", block=usage(output=5)),
            answer("2026-09-08T10:00:50.000Z", mid="msg_A", block=usage(output=99)),
            human("2026-09-08T10:01:00.000Z"))
        _, _, events, _, _ = parse_file(path)
        self.assertEqual(events[0]["idle"]["maxMs"], 10000)

    def test_client_inserts_do_not_close_a_pause(self):
        path = File(self).write(
            answer("2026-09-08T10:00:00.000Z", mid="msg_A"),
            common("2026-09-08T10:00:10.000Z", "user", isMeta=True,
                   message={"role": "user", "content": [
                       {"type": "text", "text": "Base directory for this skill: /x"}]}),
            human("2026-09-08T10:00:40.000Z"))
        _, _, events, _, _ = parse_file(path)
        self.assertEqual(events[0]["idle"]["maxMs"], 40000)

    def test_pauses_fall_into_their_own_buckets(self):
        path = File(self).write(
            answer("2026-09-08T10:00:00.000Z", mid="msg_A"),
            human("2026-09-08T10:00:10.000Z"),
            answer("2026-09-08T10:00:11.000Z", mid="msg_B"),
            human("2026-09-08T10:05:11.000Z"),
            answer("2026-09-08T10:05:12.000Z", mid="msg_C"),
            human("2026-09-08T15:05:12.000Z"))
        _, _, events, _, _ = parse_file(path)
        by_hour = {e["bucket"]: e["idle"] for e in events}
        self.assertEqual(by_hour["2026-09-08T10:00:00Z"]["under30s"], 1)
        self.assertEqual(by_hour["2026-09-08T10:00:00Z"]["under10m"], 1)
        self.assertEqual(by_hour["2026-09-08T15:00:00Z"]["over4h"], 1)

    def test_an_incremental_read_finds_a_human_who_left_across_the_boundary(self):
        path = File(self).write(
            answer("2026-09-08T10:30:00.000Z", mid="msg_A"),
            human("2026-09-08T11:00:40.000Z"),
            answer("2026-09-08T11:00:41.000Z", mid="msg_B"),
            answer("2026-09-08T11:40:00.000Z", mid="msg_C"))
        parsed = parse_file(path)
        incremental = parse_file(path, parsed.offset)
        whole = {e["bucket"]: e for e in parsed.events}["2026-09-08T11:00:00Z"]
        again = {e["bucket"]: e for e in incremental.events}["2026-09-08T11:00:00Z"]
        self.assertEqual(again["idle"], whole["idle"])
        self.assertEqual(again["idle"]["maxMs"], 1840000)

    def test_an_incremental_read_finds_a_reply_across_the_boundary(self):
        path = File(self).write(
            answer("2026-09-08T10:30:00.000Z", mid="msg_A"),
            human("2026-09-08T10:59:58.000Z"),
            answer("2026-09-08T11:00:03.000Z", mid="msg_B"),
            answer("2026-09-08T11:30:00.000Z", mid="msg_C"))
        parsed = parse_file(path)
        incremental = parse_file(path, parsed.offset)
        whole = {r["bucket"]: r for r in parsed.rows}["2026-09-08T11:00:00Z"]
        self.assertEqual(incremental.rows[0]["latencyMsSum"], whole["latencyMsSum"])
        self.assertEqual(incremental.rows[0]["latencyMsSum"], 5000)


if __name__ == "__main__":
    unittest.main()
