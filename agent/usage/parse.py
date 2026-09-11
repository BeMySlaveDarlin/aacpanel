"""One transcript to the rows of an hourly aggregate."""

import datetime
import json
import os

from .scan import head_sum


class Parsed:
    """Everything parsed out of one file."""

    __slots__ = ("rows", "tools", "events", "session", "offset",
                 "head_sum", "records", "since", "rewind")

    def __init__(self, rows, tools, events, session, offset,
                 head_sum=b"", records=0, since="", rewind=False):
        self.rows = rows
        self.tools = tools
        self.events = events
        self.session = session
        self.offset = offset
        self.head_sum = head_sum
        self.records = records
        self.since = since
        self.rewind = rewind

    def __iter__(self):
        return iter((self.rows, self.tools, self.events,
                     self.session, self.offset))


INTERRUPT = "[Request interrupted by user"
INTERRUPT_TOOL = "[Request interrupted by user for tool use]"
SYNTHETIC = "<synthetic>"
LOOKBACK = 1 << 20
IDLE_BUCKETS = (("under30s", 30_000), ("under2m", 120_000),
                ("under10m", 600_000), ("under1h", 3_600_000),
                ("under4h", 14_400_000), ("over4h", None))


def parse_file(path, offset=0, contour=""):
    """Parses the file from byte offset to its end."""
    rows, tools, answers = {}, {}, {}
    hours = {}
    tool_at = {}
    seen_calls = set()
    seen_agents = set()
    kinds = {}
    events = {}
    session = {"sessionId": "", "contour": contour, "cwd": "",
               "gitBranch": "", "version": "", "startedAt": "",
               "endedAt": "", "agents": []}
    last_user_ms = None
    last_answer_ms = None
    first_hour = ""
    records = 0
    pos = offset

    with open(path, "rb") as f:
        if offset:
            last_user_ms, last_answer_ms = _lookback(f, offset)
            f.seek(offset)
        for raw in f:
            start = pos
            pos += len(raw)
            if not raw.endswith(b"\n"):
                break
            try:
                r = json.loads(raw)
            except ValueError:
                continue
            if not isinstance(r, dict):
                continue
            records += 1

            stamp = r.get("timestamp") or ""
            if not first_hour:
                first_hour = _bucket(stamp)
            _fill_session(session, r, stamp)
            kind = r.get("type")
            agent = r.get("agentId") or ("sidechain" if r.get("isSidechain") else "")

            if kind == "user":
                event = _event(events, _bucket(stamp), agent)
                event["messages"] += 1
                if r.get("isCompactSummary"):
                    event["compacts"] += 1
                if _said_by_human(r) and last_answer_ms is not None:
                    ms = _ms(stamp)
                    if ms is not None and ms >= last_answer_ms:
                        _idle(event, ms - last_answer_ms)
                    last_answer_ms = None
                _user_record(r, event, tools, tool_at)
                if stamp:
                    last_user_ms = _ms(stamp)
                _mark(hours, _bucket(stamp), start)
                continue

            if kind != "assistant":
                _mark(hours, _bucket(stamp), start)
                continue

            _event(events, _bucket(stamp), agent)["messages"] += 1
            if stamp:
                last_answer_ms = _ms(stamp) or last_answer_ms
            message = r.get("message") or {}
            if message.get("model") == SYNTHETIC:
                _event(events, _bucket(stamp), agent)["apiErrors"] += 1
                _mark(hours, _bucket(stamp), start)
                continue

            bucket = _bucket(stamp)
            if not bucket:
                continue

            mid = message.get("id") or r.get("uuid") or ""
            answer = answers.get(mid)
            if answer is None:
                answer = answers[mid] = {
                    "bucket": bucket, "model": message.get("model") or "",
                    "agent": agent, "speed": "", "tier": "",
                    "input": 0, "output": 0, "cacheRead": 0, "cacheCreation": 0,
                    "cache1h": 0, "cache5m": 0, "iterations": 0,
                    "thinking": 0, "latency": None,
                }
                if last_user_ms is not None:
                    ms = _ms(stamp)
                    if ms is not None and ms >= last_user_ms:
                        answer["latency"] = ms - last_user_ms
                    last_user_ms = None
            _mark(hours, answer["bucket"], start)
            _mark(hours, bucket, start)

            if r.get("attributionAgent"):
                kinds[agent] = r["attributionAgent"]
            if agent:
                seen_agents.add(agent)
            _usage(answer, message.get("usage"))
            _content(answer, message.get("content"), tools, tool_at,
                     seen_calls, agent)

    for answer in answers.values():
        _fold(rows, answer)
    for row in rows.values():
        row["agentKind"] = kinds.get(row["agent"], "")
    session["agents"] = _agents(path, seen_agents)

    rewind = False
    if offset and first_hour:
        before = len(rows) + len(tools) + len(events)
        rows = {k: v for k, v in rows.items() if v["bucket"] >= first_hour}
        tools = {k: v for k, v in tools.items() if v["bucket"] >= first_hour}
        events = {k: v for k, v in events.items() if v["bucket"] >= first_hour}
        rewind = before != len(rows) + len(tools) + len(events)

    return Parsed(
        rows=sorted(rows.values(), key=lambda x: (x["bucket"], x["model"], x["agent"])),
        tools=sorted(tools.values(), key=lambda x: (x["bucket"], x["agent"], x["tool"])),
        events=sorted(events.values(), key=lambda x: (x["bucket"], x["agent"])),
        session=session,
        offset=_cut(hours, pos, offset),
        head_sum=head_sum(path),
        records=records,
        since=first_hour,
        rewind=rewind,
    )


def _lookback(f, offset):
    start = max(0, offset - LOOKBACK)
    try:
        f.seek(start)
        chunk = f.read(offset - start)
    except OSError:
        return None, None
    if start:
        cut = chunk.find(b"\n")
        if cut < 0:
            return None, None
        chunk = chunk[cut + 1:]
    last_ms = None
    last_answer_ms = None
    answer = None
    for raw in chunk.split(b"\n"):
        if not raw:
            continue
        try:
            r = json.loads(raw)
        except ValueError:
            continue
        if not isinstance(r, dict):
            continue
        kind = r.get("type")
        if kind == "user":
            stamp = r.get("timestamp")
            if stamp:
                last_ms = _ms(stamp)
            if _said_by_human(r):
                last_answer_ms = None
        elif kind == "assistant":
            stamp = r.get("timestamp")
            if stamp:
                last_answer_ms = _ms(stamp) or last_answer_ms
            message = r.get("message") or {}
            if message.get("model") == SYNTHETIC:
                continue
            mid = message.get("id") or r.get("uuid") or ""
            if mid != answer:
                answer = mid
                last_ms = None
    return last_ms, last_answer_ms


def _agents(path, seen):
    if not _subagent(path):
        return ["", "sidechain"]
    if seen:
        return sorted(seen)
    name = os.path.basename(path)
    if name.startswith("agent-") and name.endswith(".jsonl"):
        return [name[len("agent-"):-len(".jsonl")]]
    return []


def _subagent(path):
    return (os.sep + "subagents" + os.sep) in path


def _fill_session(session, record, stamp):
    if not session["sessionId"] and record.get("sessionId"):
        session["sessionId"] = record["sessionId"]
    if not session["cwd"] and record.get("cwd"):
        session["cwd"] = record["cwd"]
    if record.get("gitBranch"):
        session["gitBranch"] = record["gitBranch"]
    if record.get("version"):
        session["version"] = record["version"]
    if stamp:
        if not session["startedAt"] or stamp < session["startedAt"]:
            session["startedAt"] = stamp
        if stamp > session["endedAt"]:
            session["endedAt"] = stamp


def _user_record(record, event, tools, tool_at):
    content = (record.get("message") or {}).get("content")
    if isinstance(content, str):
        _interrupt(content, event)
        return
    if not isinstance(content, list):
        return
    for block in content:
        if not isinstance(block, dict):
            continue
        btype = block.get("type")
        if btype == "text":
            _interrupt(block.get("text") or "", event)
        elif btype == "tool_result" and block.get("is_error"):
            key = tool_at.get(block.get("tool_use_id"))
            if key in tools:
                tools[key]["errors"] += 1


def _interrupt(text, event):
    if not text.startswith(INTERRUPT):
        return
    if text.startswith(INTERRUPT_TOOL):
        event["interruptsTool"] += 1
    else:
        event["interrupts"] += 1


def _event(events, bucket, agent):
    key = (bucket, agent)
    row = events.get(key)
    if row is None:
        row = events[key] = {
            "bucket": bucket, "agent": agent,
            "messages": 0, "compacts": 0, "interrupts": 0,
            "interruptsTool": 0, "apiErrors": 0,
            "idle": {name: 0 for name, _ in IDLE_BUCKETS},
        }
        row["idle"]["maxMs"] = 0
    return row


def _idle(event, ms):
    idle = event["idle"]
    for name, edge in IDLE_BUCKETS:
        if edge is None or ms < edge:
            idle[name] += 1
            break
    if ms > idle["maxMs"]:
        idle["maxMs"] = ms


def _said_by_human(record):
    if record.get("isMeta") or record.get("isCompactSummary"):
        return False
    content = (record.get("message") or {}).get("content")
    if isinstance(content, str):
        return bool(content.strip())
    if not isinstance(content, list):
        return False
    return any(isinstance(b, dict) and b.get("type") == "text" for b in content)


def _usage(answer, usage):
    if not isinstance(usage, dict):
        return
    answer["input"] = max(answer["input"], usage.get("input_tokens") or 0)
    answer["output"] = max(answer["output"], usage.get("output_tokens") or 0)
    answer["cacheRead"] = max(answer["cacheRead"],
                              usage.get("cache_read_input_tokens") or 0)
    answer["cacheCreation"] = max(answer["cacheCreation"],
                                  usage.get("cache_creation_input_tokens") or 0)
    made = usage.get("cache_creation")
    if isinstance(made, dict):
        answer["cache1h"] = max(answer["cache1h"],
                                made.get("ephemeral_1h_input_tokens") or 0)
        answer["cache5m"] = max(answer["cache5m"],
                                made.get("ephemeral_5m_input_tokens") or 0)
    steps = usage.get("iterations")
    if isinstance(steps, list):
        answer["iterations"] = max(answer["iterations"], len(steps))
    if not answer["speed"] and usage.get("speed"):
        answer["speed"] = usage["speed"]
    if not answer["tier"] and usage.get("service_tier"):
        answer["tier"] = usage["service_tier"]


def _content(answer, content, tools, tool_at, seen_calls, agent):
    if not isinstance(content, list):
        return
    for block in content:
        if not isinstance(block, dict):
            continue
        btype = block.get("type")
        if btype == "thinking":
            answer["thinking"] += 1
        elif btype == "tool_use":
            call = block.get("id") or ""
            if call and call in seen_calls:
                continue
            if call:
                seen_calls.add(call)
            name = block.get("name") or ""
            if not name:
                continue
            key = (answer["bucket"], agent, name)
            row = tools.get(key)
            if row is None:
                row = tools[key] = {"bucket": answer["bucket"], "agent": agent,
                                    "tool": name, "calls": 0, "errors": 0}
            row["calls"] += 1
            if call:
                tool_at[call] = key


def _fold(rows, answer):
    key = (answer["bucket"], answer["model"], answer["agent"],
           answer["speed"], answer["tier"])
    row = rows.get(key)
    if row is None:
        row = rows[key] = {
            "bucket": answer["bucket"], "model": answer["model"],
            "agent": answer["agent"], "speed": answer["speed"],
            "serviceTier": answer["tier"], "agentKind": "",
            "answers": 0, "inputTokens": 0, "outputTokens": 0,
            "cacheRead": 0, "cacheCreation": 0, "cache1h": 0, "cache5m": 0,
            "iterations": 0, "thinking": 0,
            "latencyMsSum": 0, "latencyMsMax": 0,
        }
    row["answers"] += 1
    row["inputTokens"] += answer["input"]
    row["outputTokens"] += answer["output"]
    row["cacheRead"] += answer["cacheRead"]
    row["cacheCreation"] += answer["cacheCreation"]
    row["cache1h"] += answer["cache1h"]
    row["cache5m"] += answer["cache5m"]
    row["iterations"] += answer["iterations"]
    row["thinking"] += answer["thinking"]
    latency = answer["latency"]
    if latency is not None:
        row["latencyMsSum"] += latency
        row["latencyMsMax"] = max(row["latencyMsMax"], latency)


def _bucket(stamp):
    if len(stamp) < 13 or stamp[10] != "T" or not stamp.endswith("Z"):
        return ""
    return stamp[:13] + ":00:00Z"


def _ms(stamp):
    try:
        return int(datetime.datetime.fromisoformat(stamp).timestamp() * 1000)
    except ValueError:
        return None


def _mark(hours, bucket, start):
    if not bucket:
        return
    span = hours.get(bucket)
    if span is None:
        hours[bucket] = [start, start]
    elif start > span[1]:
        span[1] = start


def _cut(hours, end, floor):
    if not hours:
        return max(end, floor)
    cut = hours[max(hours)][0]
    moved = True
    while moved:
        moved = False
        for first, last in hours.values():
            if last >= cut > first:
                cut = first
                moved = True
    return max(cut, floor)
