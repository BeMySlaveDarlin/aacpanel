"""Codex threads: where a rollout lies and what its records give the feed.

Codex writes a thread into a rollout under the home of its contour,
sessions/YYYY/MM/DD/rollout-<time>-<thread id>.jsonl, a record a line:
{"timestamp", "type", "payload"}. None of its top-level types is a type of a
claude record, so a record is told by its own type, and a reader needs no word
on which kind of file it reads.

The feed is read off the items codex reports finished (an event_msg of type
item_completed). The raw input and output of the model beside them
(response_item) say the same again, and the counts of tokens are no row.
Beside the items, codex writes what a person decided on in records of their
own: a question of plan mode and its answer (retained_context of type
verified_answer), and every goal set or changed (an event_msg of type
thread_goal_updated).

An agent codex starts runs in a thread of its own, with a rollout of its own:
the thread that started it writes only its calls to agents, and the start is
a card that opens the agent's thread by its id.
"""
import datetime as dt
import glob
import json
import os
import shlex

import contours
import held

from .harness import COMPACTED, STOPPED
from .cards import MAX_ASK_QUESTIONS, MAX_ASK_TEXT
from .limits import MAX_ARGS, MAX_RESULT, MAX_TEXT, cut
from .tools import one_line, tool_kind

KINDS = ("session_meta", "world_state", "turn_context", "response_item",
         "token_usage_record", "compacted", "event_msg", "retained_context")

# What the feed says of a turn codex started by itself to go on with its goal:
# its input is codex's own words, not a message of the person's.
GOAL_TURN = "codex goes on with its goal"

# What the feed says of a goal cleared. Codex writes no goal for it: it tells
# the model "User cleared the goal." in a message of its own, and a later
# release may write an event of it besides.
GOAL_CLEARED = "the goal was cleared"
CLEARED_WORDS = "User cleared the goal."

# How many findings of a review the feed carries.
MAX_FINDINGS = 20

# The key under which a reader of a rollout keeps the id of its thread: a
# review runs in a thread of its own, and its items are written into the
# rollout of the thread that asked for it.
OWN = "codex-thread"

# The key under which a reader keeps that another drives the thread: a run of
# codex exec takes its prompts from the claude session that ran it, the thread
# of an agent from the thread that started the agent. Nobody types into either,
# and what they are given is a task, drawn as one rather than as words of the
# person. The head of the rollout says it, and is read this far at most: its
# instructions run to tens of kilobytes.
DRIVEN = "codex-driven"
HEAD = 1024 * 1024
TASK = "task"

# The mark codex's clients put before the words a person adds beside a pick.
NOTE = "user_note: "

# What the panel answers a question put away with, as the executor sends it.
DISMISSED = "The person put the question away and will answer in the conversation."

# A call of codex is drawn as the claude call that does the same, so the feed
# groups and colours it the same way.
BASH = "Bash"
EDIT = "Edit"
SEARCH = "WebSearch"
WEB_SEARCH = "web.search"

# A call that ran and failed, or one the person refused: either is drawn as a
# call that returned an error.
FAILED = ("failed", "declined")

# A call of codex to its agents: it starts one, waits on them, writes to one,
# brings one back or closes it. A start is a card of its own, as an agent of
# claude has its own row; every other call is a call among the calls, named
# the way claude names its calls and of the kind of claude's calls to agents.
COLLAB = "CollabAgentToolCall"
SPAWN = "spawn_agent"
AGENT_CALLS = {"wait": "Wait", "send_input": "SendInput", "resume_agent": "ResumeAgent",
               "close_agent": "CloseAgent"}
AGENTS = "agents"

# How codex says an agent stands. A word with what the agent said — its last
# answer, its error — comes as the word over those words, and they stay out of
# the rows: only the call opened shows them.
AGENT_STATES = ("pending_init", "running", "interrupted", "completed", "errored", "shutdown", "not_found")

# How far back the context is looked for before the whole rollout is read.
TAIL_STEPS = (64 * 1024, 1024 * 1024)


def own(path):
    """Returns the state a reader of a file starts with: the id of the thread of a rollout, nothing for a transcript."""
    name = os.path.basename(path or "")
    if not is_rollout(path):
        return {}
    return {OWN: name[:-len(".jsonl")][-36:]}


def reader(path):
    """Returns the state a reader of a file starts with: what own says, and whether another drives the thread."""
    state = own(path)
    if not state:
        return state
    try:
        with open(path, "rb") as f:
            head = json.loads(f.readline(HEAD))
    except (OSError, ValueError):
        return state
    return {**state, DRIVEN: True} if isinstance(head, dict) and driven(head) else state


def driven(record):
    """Reports whether the head of a rollout says another drives the thread: a run of codex exec or an agent's thread."""
    payload = record.get("payload")
    if record.get("type") != "session_meta" or not isinstance(payload, dict):
        return False
    source = payload.get("source")
    return (source == "exec" or payload.get("thread_source") == "subagent"
            or (isinstance(source, dict) and "subagent" in source))


def is_rollout(path):
    """Reports whether a conversation file is a codex rollout rather than a claude transcript."""
    name = os.path.basename(path or "")
    return name.startswith("rollout-") and name.endswith(".jsonl")


def cwd_of(record):
    """Returns the directory a rollout names at its head, empty for any other record."""
    payload = record.get("payload")
    if record.get("type") != "session_meta" or not isinstance(payload, dict):
        return ""
    cwd = payload.get("cwd")
    return cwd if isinstance(cwd, str) else ""


def rollout_path(session_id, profile=None):
    """Returns the rollout of a codex thread, empty when there is none.

    The executor that follows a live thread names its rollout; a thread
    nobody follows any more is found by its id under the codex homes, of
    the contour named or of every one.
    """
    if not held.named(session_id):
        return ""
    data = held.summary(session_id)
    path = data.get("transcript") if isinstance(data, dict) and data.get("agent") == "codex" else None
    if isinstance(path, str) and path.endswith(f"-{session_id}.jsonl") and os.path.isfile(path):
        return path
    name = f"rollout-*-{glob.escape(session_id)}.jsonl"
    for contour, home in contours.codex_homes():
        if profile and contour != profile:
            continue
        found = sorted(glob.glob(os.path.join(glob.escape(home), "sessions", "*", "*", "*", name)))
        if found:
            return found[-1]
    return ""


def _count(value):
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        return None
    return value


# The marks of a rollout the tail is read for: the reports of the context, the
# start and the end of a turn, and what a turn runs with.
TAIL_MARKS = (b"token_count", b"task_started", b"task_complete", b"turn_aborted", b"turn_context")

# How a turn ends in a rollout: done, or broken off.
TURN_ENDS = ("task_complete", "turn_aborted")


def context(path):
    """Returns how full the context of a thread is: tokens, the window and when, from its rollout.

    Codex reports the input of the last request and the window of the model
    after every request; a thread that has made none yet has the window its
    turn named at the start, and no time. The tail is read wider until it
    holds a report, and the whole file only when none does. Beside the fill
    come the model and the effort the last turn ran with, and whether a turn
    still runs: one runs from its start to its end or its abort, and a report
    with neither before it lies inside a turn that started before the tail.
    """
    try:
        size = os.path.getsize(path)
    except OSError:
        return None
    for back in TAIL_STEPS + (size,):
        start = max(0, size - back)
        out = {"tokens": 0, "limit": 0, "at": "", "model": "", "effort": "", "turn": ""}
        try:
            with open(path, "rb") as f:
                f.seek(start)
                for raw in f:
                    # A command's output can make a line of megabytes: only
                    # a line that may be a report is parsed, and the piece of
                    # a line the seek lands in parses as nothing.
                    if not any(mark in raw for mark in TAIL_MARKS):
                        continue
                    _take(raw, out)
        except OSError:
            return None
        if out["at"] or start == 0:
            break
    out["busy"] = out.pop("turn") == "on"
    return out


def _take(raw, out):
    try:
        record = json.loads(raw)
    except ValueError:
        return
    if not isinstance(record, dict):
        return
    payload = record.get("payload")
    if not isinstance(payload, dict):
        return
    if record.get("type") == "turn_context":
        for key in ("model", "effort"):
            if isinstance(payload.get(key), str) and payload[key]:
                out[key] = payload[key]
        return
    if record.get("type") != "event_msg":
        return
    if payload.get("type") in TURN_ENDS:
        out["turn"] = "off"
        return
    if payload.get("type") == "task_started":
        out["turn"] = "on"
        out["limit"] = _count(payload.get("model_context_window")) or out["limit"]
        return
    if payload.get("type") != "token_count":
        return
    info = payload.get("info")
    usage = info.get("last_token_usage") if isinstance(info, dict) else None
    tokens = _count(usage.get("input_tokens")) if isinstance(usage, dict) else None
    if tokens is None:
        return
    out["turn"] = out["turn"] or "on"
    out["tokens"] = tokens
    out["limit"] = _count(info.get("model_context_window")) or out["limit"]
    out["at"] = record.get("timestamp") or ""


def _stopped(record):
    """Reports whether a record says a person stopped the turn: codex writes it beside the turn's end."""
    payload = record.get("payload")
    return (record.get("type") == "event_msg" and isinstance(payload, dict)
            and payload.get("type") == "turn_aborted" and payload.get("reason") == "interrupted")


def _item(record):
    """Returns the payload and the item of a record that reports a finished item, or None."""
    payload = record.get("payload")
    if record.get("type") != "event_msg" or not isinstance(payload, dict):
        return None
    item = payload.get("item")
    if payload.get("type") != "item_completed" or not isinstance(item, dict):
        return None
    return payload, item


def _text(parts, kind):
    if not isinstance(parts, list):
        return ""
    return "\n".join(p["text"] for p in parts
                     if isinstance(p, dict) and p.get("type") == kind
                     and isinstance(p.get("text"), str)).strip()


def _said(role, text, at, pos):
    if not text:
        return []
    body, trimmed = cut(text, MAX_TEXT)
    return [{"role": role, "text": body, "cut": trimmed, "at": at, "pos": pos}]


def command_line(command):
    """Returns the command a call ran as it was typed: the script of a shell's -c, else the words joined."""
    if isinstance(command, str):
        return command
    if not isinstance(command, list) or not all(isinstance(word, str) for word in command):
        return ""
    if len(command) == 3 and command[1] in ("-c", "-lc"):
        return command[2]
    return shlex.join(command)


def _changes(item):
    """Returns the files a change touches as (path, change), in the order codex names them."""
    changes = item.get("changes")
    if not isinstance(changes, dict):
        return []
    return [(path, change) for path, change in changes.items() if isinstance(change, dict)]


def _failed(item):
    return item.get("status") in FAILED


def _call(name, arg, use, index, failed, at, pos, edited="", kind=""):
    """Returns a call codex has finished: its row and the mark of its result at once.

    Codex writes a call once it is over, so the result comes with it.
    """
    # Imported here: the parser of records reads this module for its rows.
    from .records import RESULT
    call = {"role": "tool", "name": name, "kind": kind or tool_kind(name), "arg": arg, "at": at,
            "use": use, "pos": pos, "index": index}
    if edited:
        call["edited"] = edited
    mark = {"role": RESULT, "use": use, "at": at, "pos": pos}
    if failed:
        mark["failed"] = True
    return [call, mark]


def _asked(record, at, pos):
    """Returns the card of a question of plan mode and its answer, as a claude question is drawn, or None.

    Codex keeps the pair in one record: each question with its options under
    it, a line an option, and the answer a line a label, the words a person
    added beside a pick marked as a note. A question put away from the panel
    is answered with the note that the person will answer in the
    conversation, and the card says it was put away.
    """
    payload = record.get("payload")
    if (record.get("type") != "retained_context" or not isinstance(payload, dict)
            or payload.get("type") != "verified_answer" or not isinstance(payload.get("questions"), list)):
        return None
    rows, put_away = [], True
    for item in payload["questions"][:MAX_ASK_QUESTIONS]:
        if not isinstance(item, dict) or not isinstance(item.get("question"), str):
            continue
        text = cut(" ".join(item["question"].split("\n", 1)[0].split()), MAX_ASK_TEXT)[0]
        if not text:
            continue
        row = {"text": text}
        lines = [line.strip() for line in str(item.get("answer") or "").split("\n") if line.strip()]
        notes = [line[len(NOTE):].strip() for line in lines if line.startswith(NOTE)]
        answer = [cut(line, MAX_ASK_TEXT)[0] for line in lines if not line.startswith(NOTE)]
        if answer:
            row["answer"] = answer
        if notes and notes != [DISMISSED]:
            row["note"] = cut(" ".join(notes), MAX_ASK_TEXT)[0]
        put_away = put_away and not answer and notes == [DISMISSED]
        rows.append(row)
    if not rows:
        return None
    card = {"role": "asked", "use": payload.get("call_id") if isinstance(payload.get("call_id"), str) else "",
            "asked": rows, "at": at, "pos": pos}
    if put_away:
        card["status"] = "rejected"
    return card


def _goal(record, at, pos):
    """Returns the row of a goal set or changed, or None.

    Codex writes the goal whole at every change a client makes: what it works
    towards, how it stands, what it has spent and its budget.
    """
    payload = record.get("payload")
    goal = payload.get("goal") if isinstance(payload, dict) else None
    if (not isinstance(goal, dict) or record.get("type") != "event_msg"
            or payload.get("type") != "thread_goal_updated" or not isinstance(goal.get("objective"), str)):
        return None
    body, trimmed = cut(goal["objective"], MAX_TEXT)
    return {"role": "goal", "text": body, "cut": trimmed, "status": str(goal.get("status") or ""),
            "tokensUsed": _count(goal.get("tokensUsed")) or 0, "tokenBudget": _count(goal.get("tokenBudget")),
            "timeUsedSeconds": _count(goal.get("timeUsedSeconds")) or 0, "at": at, "pos": pos}


def _goal_cleared(record):
    """Reports whether a record says the goal of the thread was cleared."""
    payload = record.get("payload")
    if not isinstance(payload, dict):
        return False
    if record.get("type") == "event_msg":
        return payload.get("type") == "thread_goal_cleared"
    if (record.get("type") != "response_item" or payload.get("type") != "message"
            or payload.get("role") == "assistant" or not isinstance(payload.get("content"), list)):
        return False
    return any(isinstance(part, dict) and isinstance(part.get("text"), str) and CLEARED_WORDS in part["text"]
               for part in payload["content"])


def _goal_turn(record):
    """Reports whether a record starts a turn codex began by itself to go on with its goal."""
    payload = record.get("payload")
    started = payload.get("turn_attribution") if isinstance(payload, dict) else None
    return (isinstance(started, dict) and record.get("type") == "event_msg"
            and payload.get("type") == "task_started" and started.get("turn_trigger") == "goal")


def _finding(raw):
    if not isinstance(raw, dict):
        return None
    where = raw.get("code_location") if isinstance(raw.get("code_location"), dict) else {}
    span = where.get("line_range") if isinstance(where.get("line_range"), dict) else {}
    found = {"title": cut(str(raw.get("title") or ""), MAX_ASK_TEXT)[0],
             "body": cut(str(raw.get("body") or ""), MAX_TEXT)[0],
             "priority": _count(raw.get("priority")),
             "path": where.get("absolute_file_path") if isinstance(where.get("absolute_file_path"), str) else "",
             "lines": [_count(span.get("start")), _count(span.get("end"))]}
    score = raw.get("confidence_score")
    if isinstance(score, (int, float)) and not isinstance(score, bool):
        found["confidence"] = score
    return found


def _review(item, at, pos):
    """Returns the row a review begins or ends with.

    A review begins with what it looks at, in codex's own words, and ends with
    its verdict, the explanation and the findings, each with its place in the
    code. Codex then writes the findings as an answer as well, which the feed
    draws as one.
    """
    if item.get("type") == "EnteredReviewMode":
        hint = item.get("user_facing_hint") if isinstance(item.get("user_facing_hint"), str) else ""
        return {"role": "review", "state": "start", "text": hint, "at": at, "pos": pos}
    out = item.get("review_output") if isinstance(item.get("review_output"), dict) else {}
    body, trimmed = cut(str(out.get("overall_explanation") or ""), MAX_TEXT)
    findings = [f for f in (_finding(raw) for raw in (out.get("findings") or [])[:MAX_FINDINGS]) if f]
    row = {"role": "review", "state": "end", "verdict": str(out.get("overall_correctness") or ""),
           "text": body, "cut": trimmed, "findings": findings, "at": at, "pos": pos}
    score = out.get("overall_confidence_score")
    if isinstance(score, (int, float)) and not isinstance(score, bool):
        row["confidence"] = score
    return row


def _list(value):
    return value if isinstance(value, list) else []


def _word(value):
    return one_line(value) if isinstance(value, str) else ""


def _state(raw):
    """Returns how an agent stands, in codex's word, and the words it said with it.

    A word codex does not have is no state: what stands in its place could be
    words of the conversation.
    """
    word, said = raw, ""
    if isinstance(raw, dict) and len(raw) == 1:
        (word, said), = raw.items()
    if word not in AGENT_STATES:
        return "", ""
    return word, said if isinstance(said, str) else ""


def _agents(item):
    """Returns the agents a call to agents names: each by its thread, with its nickname, its role and how it stands."""
    # Imported here: the module that finds transcripts reads this one.
    from .locate import UUID_RE
    states = item.get("agents_states") if isinstance(item.get("agents_states"), dict) else {}
    named = {agent["thread_id"]: agent for agent in _list(item.get("receiver_agents"))
             if isinstance(agent, dict) and isinstance(agent.get("thread_id"), str)}
    ids = [thread for thread in _list(item.get("receiver_thread_ids")) if isinstance(thread, str)]
    out = []
    for thread in dict.fromkeys(ids + list(named)):
        if not UUID_RE.match(thread):
            continue
        agent = named.get(thread, {})
        out.append({"id": thread, "name": _word(agent.get("agent_nickname")), "role": _word(agent.get("agent_role")),
                    "state": _state(states.get(thread))[0]})
    return out


def _spawn(item, use, at, pos):
    """Returns the card of an agent codex started: who it is, on what model, how it stands, and its task.

    The task is what the thread that started the agent asked of it; the feed
    keeps it folded, as the call of an agent of claude keeps its prompt.
    """
    model, effort = _word(item.get("model")), _word(item.get("reasoning_effort"))
    agents = [dict(agent, model=model, effort=effort) for agent in _agents(item)]
    body, trimmed = cut(item.get("prompt") if isinstance(item.get("prompt"), str) else "", MAX_TEXT)
    card = {"role": "spawn", "use": use, "spawned": agents, "text": body, "cut": trimmed, "at": at, "pos": pos}
    if _failed(item):
        card["status"] = "failed"
    return card


def _agent_call(item, use, at, pos):
    """Returns a call to agents other than a start: a call among the calls, and how its agents stand after it.

    How they stand is a row the screen does not draw: it settles the card
    each of them was started with.
    """
    tool = item.get("tool") if isinstance(item.get("tool"), str) else ""
    agents = _agents(item)
    who = ", ".join(agent["name"] or agent["role"] or agent["id"][-8:] for agent in agents)
    out = _call(AGENT_CALLS.get(tool) or _word(tool) or "Agent", one_line(who), use, 0, _failed(item), at, pos,
                kind=AGENTS)
    stood = [{"id": agent["id"], "state": agent["state"]} for agent in agents if agent["state"]]
    if stood:
        out.append({"role": "agentstates", "spawned": stood, "at": at, "pos": pos})
    return out


def _agent_spot(item):
    """Returns a call to agents opened: its name, what it was called with and what came of it.

    It was called with the agents it names — each by its nickname, its role
    and its thread — and, for a start or a word sent, the words and the
    model; what came of it is how each agent stands, with its last answer or
    its error under it.
    """
    tool = item.get("tool") if isinstance(item.get("tool"), str) else ""
    states = item.get("agents_states") if isinstance(item.get("agents_states"), dict) else {}
    agents = _agents(item)
    called = {"agents": [{"nickname": agent["name"], "role": agent["role"], "thread": agent["id"]}
                         for agent in agents]}
    for key, field in (("prompt", "prompt"), ("model", "model"), ("effort", "reasoning_effort")):
        if isinstance(item.get(field), str) and item[field]:
            called[key] = item[field]
    lines = []
    for agent in agents:
        word, said = _state(states.get(agent["id"]))
        head = f"{agent['name'] or agent['id']}: {word.replace('_', ' ') or 'not said'}"
        lines.append(f"{head}\n{said.strip()}" if said.strip() else head)
    name = "Agent" if tool == SPAWN else (AGENT_CALLS.get(tool) or tool or "Agent")
    return name, json.dumps(called, ensure_ascii=False, indent=2, sort_keys=True), "\n\n".join(lines)


# The items a review runs in its own thread that the rollout of the thread
# that asked for it shows: the work, not the words. The words of that thread
# — its prompt and its answers — are codex's to itself, and the review says
# its findings in the thread that asked.
WORK = ("CommandExecution", "FileChange", "Extension")


def rows(record, pos, state=None):
    """Returns the feed items of one rollout record, from none to many.

    A change of several files is a call a file, as claude makes them: each
    names the file it changed, and its details open that file's diff. State
    is what the reader keeps for the whole file: the id of the thread the
    rollout is of.
    """
    at = record.get("timestamp") or ""
    if _stopped(record):
        return [{"role": "note", "text": STOPPED, "at": at, "pos": pos}]
    if _goal_turn(record):
        return [{"role": "note", "text": GOAL_TURN, "at": at, "pos": pos}]
    if _goal_cleared(record):
        return [{"role": "note", "text": GOAL_CLEARED, "at": at, "pos": pos}]
    for card in (_asked(record, at, pos), _goal(record, at, pos)):
        if card:
            return [card]
    found = _item(record)
    if found is None:
        return []
    payload, item = found
    kind = item.get("type")
    use = item.get("id") if isinstance(item.get("id"), str) else ""
    mine = (state or {}).get(OWN)
    if mine and payload.get("thread_id") not in (None, "", mine) and kind not in WORK:
        return []
    if kind == "Plan":
        return _said("plan", item.get("text") if isinstance(item.get("text"), str) else "", at, pos)
    if kind in ("EnteredReviewMode", "ExitedReviewMode"):
        return [_review(item, at, pos)]
    if kind == "UserMessage":
        role = TASK if (state or {}).get(DRIVEN) else "me"
        return _said(role, _text(item.get("content"), "text"), at, pos)
    if kind == "AgentMessage":
        return _said("ai", _text(item.get("content"), "Text"), at, pos)
    if kind == "Reasoning":
        summary = item.get("summary_text")
        parts = [s.strip() for s in summary if isinstance(s, str) and s.strip()] \
            if isinstance(summary, list) else []
        return _said("mind", "\n\n".join(parts), at, pos)
    if kind == "CommandExecution":
        return _call(BASH, one_line(command_line(item.get("command"))), use, 0,
                     _failed(item), at, pos)
    if kind == "FileChange":
        out = []
        for index, (path, change) in enumerate(_changes(item)):
            moved = change.get("move_path") if isinstance(change.get("move_path"), str) else ""
            arg = f"{path} → {moved}" if moved else path
            edited = "" if change.get("type") == "delete" else (moved or path)
            out += _call(EDIT, one_line(arg), f"{use}#{index}", index, _failed(item), at, pos,
                         edited)
        return out
    if kind == "Extension" and item.get("kind") == WEB_SEARCH:
        return _call(SEARCH, "", use, 0, _failed(item), at, pos)
    if kind == "ContextCompaction":
        return [{"role": "note", "text": COMPACTED, "at": at, "pos": pos}]
    if kind == COLLAB:
        if item.get("tool") == SPAWN:
            return [_spawn(item, use, at, pos)]
        return _agent_call(item, use, at, pos)
    return []


def _prefixed(body, mark):
    return "".join(mark + line for line in body.splitlines(keepends=True))


def patch_of(path, change):
    """Returns one file of a change as a diff: the one codex made, or the whole file added or deleted."""
    kind = change.get("type")
    body = change.get("content") if isinstance(change.get("content"), str) else ""
    if kind == "update":
        moved = change.get("move_path") if isinstance(change.get("move_path"), str) else ""
        diff = change.get("unified_diff") if isinstance(change.get("unified_diff"), str) else ""
        return f"--- {path}\n+++ {moved or path}\n{diff}"
    if kind == "add":
        return f"--- /dev/null\n+++ {path}\n{_prefixed(body, '+')}"
    if kind == "delete":
        return f"--- {path}\n+++ /dev/null\n{_prefixed(body, '-')}"
    return ""


def _stamp(ms):
    if _count(ms) is None or not ms:
        return ""
    return dt.datetime.fromtimestamp(ms / 1000, dt.timezone.utc) \
        .isoformat(timespec="milliseconds").replace("+00:00", "Z")


def details(record, index):
    """Returns one codex call whole, the way the feed opens a claude one, or None.

    A command is what it ran and where, and what it printed; a change is the
    diff of its file at index, and what codex said applying it.
    """
    found = _item(record)
    if found is None:
        return None
    payload, item = found
    kind = item.get("type")
    if kind == "CommandExecution" and index == 0:
        name = BASH
        called = {"command": command_line(item.get("command"))}
        if isinstance(item.get("cwd"), str) and item["cwd"]:
            called["cwd"] = item["cwd"]
        shown = json.dumps(called, ensure_ascii=False, indent=2, sort_keys=True)
        said = item.get("aggregated_output") if isinstance(item.get("aggregated_output"), str) else ""
        code = item.get("exit_code")
        if isinstance(code, int) and not isinstance(code, bool) and code:
            said = f"Exit code {code}\n{said}"
    elif kind == "FileChange":
        changes = _changes(item)
        if not 0 <= index < len(changes):
            return None
        name = EDIT
        shown = patch_of(*changes[index])
        said = "\n".join(s.strip() for s in (item.get("stdout"), item.get("stderr"))
                         if isinstance(s, str) and s.strip())
    elif kind == "Extension" and item.get("kind") == WEB_SEARCH and index == 0:
        name, shown, said = SEARCH, "", ""
    elif kind == COLLAB and index == 0:
        name, shown, said = _agent_spot(item)
    else:
        return None
    at = record.get("timestamp") or ""
    args, args_cut = cut(shown, MAX_ARGS)
    result, result_cut = cut(said, MAX_RESULT)
    return {"tool": name, "args": args, "argsCut": args_cut,
            "at": _stamp(payload.get("started_at_ms")) or at,
            "result": result, "resultCut": result_cut, "failed": _failed(item),
            "resultAt": _stamp(payload.get("completed_at_ms")) or at}
