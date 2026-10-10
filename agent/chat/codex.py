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
a card that opens the agent's thread by its id. Of those calls, the ones of
the second set of codex's tools for agents are written as the raw records of
the model alone, and the feed reads them there.
"""
import datetime as dt
import glob
import json
import os
import shlex

import contours
import held

from .harness import COMPACTED, STOPPED
from .cards import MAX_ASK_ANSWERS, MAX_ASK_QUESTIONS, MAX_ASK_TEXT
from .limits import MAX_ARGS, MAX_RESULT, MAX_TEXT, cut
from .mail import LETTER_TOOL, mails, peer_name, undelivered
from .tools import one_line, tool_arg, tool_kind, tool_label

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

# The key under which a reader keeps the path of the thread among the agents
# of the second set: a message it wrote itself is one it sent, not received.
PATH = "codex-path"
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
# gives one a task more, brings one back, interrupts one, lists them or closes
# one. A start is a card of its own, as an agent of claude has its own row;
# every other call is a call among the calls, named the way claude names its
# calls and of the kind of claude's calls to agents.
#
# Codex has two sets of these tools, and a thread works with one of them. The
# first writes an item of every call (CollabAgentToolCall), its agents named
# by their threads. The second writes the call and its answer only as the raw
# records of the model — a function_call of the namespace collaboration and
# its function_call_output — and beside them an item of what the call did to
# an agent (SubAgentActivity): started it, spoke to it or interrupted it; the
# agent's turn coming to its end is an item of its own. It names an agent by
# its path, /root/<name>, and a wait of it writes the item of a wait besides.
# So a call of the second set is drawn from its call, and its items only say
# how the agents stand. The words it sends an agent travel encrypted and are
# never shown: only to whom and that it went.
COLLAB = "CollabAgentToolCall"
ACTIVITY = "SubAgentActivity"
SECOND = "collaboration"
SPAWN = "spawn_agent"
AGENT_CALLS = {"spawn_agent": "Agent", "send_input": "SendInput", "resume_agent": "ResumeAgent",
               "wait": "Wait", "wait_agent": "Wait", "close_agent": "CloseAgent",
               "send_message": "SendMessage", "followup_task": "FollowupTask",
               "interrupt_agent": "InterruptAgent", "list_agents": "ListAgents"}
AGENTS = "agents"

# A wait codex sets itself (clock.sleep) is drawn from its call as well: its
# item comes only once the wait is over, and the call says codex waits while
# it does.
SLEEP = ("clock", "sleep")
SLEEP_NAME = "Sleep"
TIME = "time"

# The key under which a reader keeps the calls drawn from the raw record of
# the model it has met — of the second set, and the waits — by the id of the
# call: the tool and the role a start asked for. The items and the answer of
# a call come after it and name it by that id. The map is replaced rather
# than changed: a throwaway read of a record still being written works on a
# shallow copy of the reader's state.
RAW_CALLS = "codex-raw-calls"

# How the second set names the thread that started the agents, and the part
# of a path an agent of it is named without.
ROOT = "/root"

# What an item of the second set says happened to an agent, as the word codex
# says an agent stands by, and the line the feed draws of it, when it draws one:
# a start is a card, a word sent is the call that sent it, and a task more —
# followup_task — sets the agent to work again.
ACTIVITIES = {"started": "running", "interrupted": "interrupted", "completed": "completed"}
ACTIVITY_LINES = {"interrupted": ("was interrupted", "warn"), "completed": ("finished", "ok")}
FOLLOWUP = "followup_task"

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
    """Returns the state a reader of a file starts with: what own says, whether another drives the thread, and its path.

    The path is the one an agent of the second set is known by among the
    agents, the thread that started them being the root.
    """
    state = own(path)
    if not state:
        return state
    try:
        with open(path, "rb") as f:
            head = json.loads(f.readline(HEAD))
    except (OSError, ValueError):
        return state
    if not isinstance(head, dict):
        return state
    payload = head.get("payload") if isinstance(head.get("payload"), dict) else {}
    named = payload.get("agent_path")
    state = {**state, PATH: named if isinstance(named, str) and named else ROOT}
    return {**state, DRIVEN: True} if driven(head) else state


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


# A letter of another session reaches a codex thread as a message of the
# panel: the envelope claude sends a letter in, with the panel's words around
# it saying who wrote and that the person did not. The feed draws it as the
# feed of claude draws one — a card of a letter from the session that wrote —
# and leaves the panel's words out.
LETTER_TAG = "<cross-session-message"


def _letters(text, at, pos):
    """Returns the cards of the letters a message of the thread carries, none for a message of the person."""
    if LETTER_TAG not in text:
        return []
    out = []
    for who, source, said in mails(text):
        body, trimmed = cut(said, MAX_TEXT)
        out.append({"role": "mail", "from": who, "source": source, "text": body, "cut": trimmed,
                    "at": at, "pos": pos})
    return out


# A call of a tool of an MCP server. It is named the way claude names one,
# mcp__<server>__<tool>, so it is drawn as claude's: among the calls, by its
# server and tool, what it answered opened from it. The panel's letter is an
# outgoing letter, as in the feed of claude.
MCP_CALL = "McpToolCall"


def _mcp_name(item):
    server, tool = item.get("server"), item.get("tool")
    if not isinstance(server, str) or not isinstance(tool, str) or not server or not tool:
        return ""
    return f"mcp__{server}__{tool}"


def _mcp_args(item):
    args = item.get("arguments")
    return args if isinstance(args, dict) else {}


def _mcp_said(item):
    """Returns what an MCP call answered: the words of its result, or why it did not run."""
    error = item.get("error")
    if isinstance(error, dict) and isinstance(error.get("message"), str):
        return error["message"]
    result = item.get("result")
    content = result.get("content") if isinstance(result, dict) else None
    if not isinstance(content, list):
        return ""
    return "\n".join(c["text"] for c in content
                     if isinstance(c, dict) and c.get("type") == "text" and isinstance(c.get("text"), str))


def _mcp_call(item, use, at, pos):
    """Returns the rows of a call of an MCP tool: a call among the calls, or an outgoing letter.

    A letter that reached nobody says why, from the answer that came with it.
    """
    name = _mcp_name(item)
    if not name:
        return []
    args = _mcp_args(item)
    said = args.get("text")
    if name == LETTER_TOOL and isinstance(said, str) and said.strip():
        body, trimmed = cut(said.strip(), MAX_TEXT)
        letter = {"role": "mail", "dir": "out", "from": peer_name(str(args.get("to") or "")),
                  "source": "session", "text": body, "cut": trimmed, "use": use, "at": at, "pos": pos}
        lost = undelivered(_mcp_said(item), _failed(item))
        if lost:
            letter["undelivered"] = lost
        return [letter]
    return _call(tool_label(name, args), tool_arg(args, name), use, 0, _failed(item), at, pos,
                 kind=tool_kind(name))


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


# Codex asks outside plan mode too, with a tool that does not wait: the
# question goes to the person as an answer of the turn, delivered apart
# (delivery async), with its questions beside the words, and the turn goes on.
# Nothing in the protocol takes the answer: the person answers with a message,
# into the turn that runs or as a turn of its own, and the next message of the
# person is the answer.
ASYNC = "async"


def _questions(item):
    """Returns the questions of an answer codex delivered apart — each its title and its options — or none."""
    if item.get("delivery") != ASYNC:
        return []
    out = []
    for raw in _list(item.get("questions"))[:MAX_ASK_QUESTIONS]:
        title = raw.get("title") if isinstance(raw, dict) else None
        if not isinstance(title, str) or not title.strip():
            continue
        options = [cut(" ".join(one.split()), MAX_ASK_TEXT)[0] for one in _list(raw.get("options"))
                   if isinstance(one, str) and one.strip()]
        out.append({"title": cut(title.strip(), MAX_ASK_TEXT)[0], "options": options[:MAX_ASK_ANSWERS]})
    return out


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


def _card(agents, model, effort, use, at, pos, task="", failed=False):
    """Returns the card of agents codex started: who each is, on what model, how it stands, and their task.

    The task is what the thread that started the agent asked of it; the feed
    keeps it folded, as the call of an agent of claude keeps its prompt.
    """
    body, trimmed = cut(task, MAX_TEXT)
    card = {"role": "spawn", "use": use, "spawned": [dict(agent, model=model, effort=effort) for agent in agents],
            "text": body, "cut": trimmed, "at": at, "pos": pos}
    if failed:
        card["status"] = "failed"
    return card


def _spawn(item, use, at, pos):
    """Returns the card of an agent codex started with the first set of its tools."""
    return _card(_agents(item), _word(item.get("model")), _word(item.get("reasoning_effort")), use, at, pos,
                 item.get("prompt") if isinstance(item.get("prompt"), str) else "", _failed(item))


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
    name = AGENT_CALLS.get(tool) or tool or "Agent"
    return name, json.dumps(called, ensure_ascii=False, indent=2, sort_keys=True), "\n\n".join(lines)


def agent_name(path):
    """Returns how the feed names an agent of the second set: its path under the thread that started it."""
    if not isinstance(path, str):
        return ""
    return one_line(path[len(ROOT) + 1:] if path.startswith(ROOT + "/") else path)


def _arguments(payload):
    """Returns what a call of the model was called with, or nothing when it does not read."""
    raw = payload.get("arguments")
    try:
        args = json.loads(raw) if isinstance(raw, str) else raw
    except ValueError:
        return {}
    return args if isinstance(args, dict) else {}


def _raw_call(payload):
    """Returns the tool and the id of a call drawn from the record of the model, or None for any other record.

    That is a call of the second set and a wait codex set itself.
    """
    tool, use = payload.get("name"), payload.get("call_id")
    if payload.get("type") != "function_call" or not isinstance(use, str) or not use:
        return None
    if payload.get("namespace") == SECOND and tool in AGENT_CALLS:
        return tool, use
    if (payload.get("namespace"), tool) == SLEEP:
        return tool, use
    return None


def _called(tool):
    """Returns the name and the kind a call drawn from the record of the model is drawn under."""
    return (SLEEP_NAME, TIME) if tool == SLEEP[1] else (AGENT_CALLS[tool], AGENTS)


def duration(ms):
    """Returns a wait in words: seconds, or minutes once it is longer than two."""
    if isinstance(ms, bool) or not isinstance(ms, (int, float)) or ms < 0:
        return ""
    sec = round(ms / 1000)
    return f"{sec} s" if sec < 120 else f"{round(sec / 60)} min"


def _raw_call_row(payload, at, pos, state):
    """Returns the row of a call drawn from the record of the model as it goes out, and keeps the call in the state.

    The call is open until its answer comes: a wait says how long codex
    waits, a call to agents whom it goes to. A start is no call: the agent it
    started is a card, drawn from the item that names its thread.
    """
    tool, use = _raw_call(payload)
    args = _arguments(payload)
    state[RAW_CALLS] = {**state.get(RAW_CALLS, {}), use: (tool, _word(args.get("agent_type")))}
    if tool == SPAWN:
        return []
    name, kind = _called(tool)
    arg = agent_name(args.get("target")) if kind == AGENTS else duration(args.get("duration_ms"))
    return [{"role": "tool", "name": name, "kind": kind, "arg": arg,
             "at": at, "use": use, "pos": pos, "index": 0, "open": True}]


def _refused(output):
    """Reports whether the answer of a call of the second set says it did not go.

    A call that went answers with an object or with nothing; words in place
    of either are codex saying why it did not — the person stopped the turn
    it waited in, or the call failed.
    """
    if not isinstance(output, str) or not output.strip():
        return False
    try:
        return not isinstance(json.loads(output), dict)
    except ValueError:
        return True


def _raw_answer(payload, at, pos, state):
    """Returns the mark of the answer of a call drawn from the record of the model, or None for any other answer."""
    from .records import RESULT
    use = payload.get("call_id")
    known = (state or {}).get(RAW_CALLS, {})
    if payload.get("type") != "function_call_output" or not isinstance(use, str) or use not in known:
        return None
    if known[use][0] == SPAWN:
        return []
    mark = {"role": RESULT, "use": use, "at": at, "pos": pos}
    if _refused(payload.get("output")):
        mark["failed"] = True
    return [mark]


def _activity(item, at, pos, state):
    """Returns the rows of an item of the second set: a card of an agent started, how an agent stands now.

    A start names the agent's thread, its model and its effort, and the role
    its call asked for; an agent interrupted or done with its turn is a line
    of the feed besides, since nothing else in it says so.
    """
    from .locate import UUID_RE
    thread, kind = item.get("agent_thread_id"), item.get("kind")
    if not isinstance(thread, str) or not UUID_RE.match(thread):
        return []
    use = item.get("id") if isinstance(item.get("id"), str) else ""
    tool, role = (state or {}).get(RAW_CALLS, {}).get(use, ("", ""))
    name = agent_name(item.get("agent_path")) or thread[-8:]
    if kind == "started":
        agents = [{"id": thread, "name": name, "role": role, "state": ACTIVITIES[kind]}]
        return [_card(agents, _word(item.get("model")), _word(item.get("reasoning_effort")), use, at, pos)]
    word = ACTIVITIES.get(kind) or ("running" if kind == "interacted" and tool == FOLLOWUP else "")
    if not word:
        return []
    out = [{"role": "agentstates", "spawned": [{"id": thread, "state": word}], "at": at, "pos": pos}]
    if kind in ACTIVITY_LINES:
        said, level = ACTIVITY_LINES[kind]
        out.append({"role": "notice", "from": f"agent {name}", "text": said, "level": level, "at": at, "pos": pos})
    return out


def _raw_details(record, payload, f):
    """Returns a call drawn from the record of the model opened, the way the feed opens a claude one.

    It was called with everything but its words to the agent; its answer is
    read on in the rollout, a few records after the call, as claude's result
    is read after its call, and a call whose answer has not come is pending.
    """
    tool, use = _raw_call(payload)
    called = {key: value for key, value in _arguments(payload).items() if key != "message"}
    at = record.get("timestamp") or ""
    args, args_cut = cut(json.dumps(called, ensure_ascii=False, indent=2, sort_keys=True) if called else "", MAX_ARGS)
    out = {"tool": _called(tool)[0], "args": args, "argsCut": args_cut, "at": at,
           "result": "", "resultCut": False, "failed": False, "resultAt": ""}
    answer = _answer(f, use) if f is not None else None
    if answer is None:
        out["pending"] = True
        return out
    output, answered = answer
    try:
        said = json.loads(output) if isinstance(output, str) and output.strip() else output
    except ValueError:
        said = output
    if isinstance(said, dict):
        said = json.dumps(said, ensure_ascii=False, indent=2, sort_keys=True)
    out["result"], out["resultCut"] = cut(said if isinstance(said, str) else "", MAX_RESULT)
    out["failed"], out["resultAt"] = _refused(output), answered or at
    return out


# The bytes a record that may say something of the agents of a thread or of
# a question it left hanging holds: an item of a call to agents of either set,
# a call of the second set, which names the role a start asked for and
# whether a word to an agent gave it a task more, an answer delivered apart
# and a message of the person. Only such a line of the rollout is parsed: a
# command's output can make a line of megabytes.
CREW_MARKS = tuple(json.dumps(word).encode() for word in (COLLAB, ACTIVITY, SECOND, ASYNC, "UserMessage"))


def crew(path, known=None):
    """Returns what a thread has going beside its turn, as its rollout says, read on from what was known.

    That is the agents it started, each as it stands, and the question it
    asked without waiting while no message of the person has come after it.
    Known is what an earlier call returned: the place the reading stopped,
    the agents by their threads, the calls of the second set met so far and
    the question that hangs. An agent stands as the last word codex wrote of
    it — a start, an item of the second set, the states a call of the first
    set came back with — and at the moment it was written; a rollout shorter
    than the place is another file, read from its start. An item of another
    thread — a review, the history an agent's thread takes over from its
    parent — is passed by.
    """
    try:
        size = os.path.getsize(path)
    except OSError:
        return known
    if known is None or known["pos"] > size:
        known = {"pos": 0, "agents": {}, RAW_CALLS: {}, OWN: own(path).get(OWN, ""), "asked": None}
    if known["pos"] == size:
        return known
    known = {**known, "agents": dict(known["agents"]), RAW_CALLS: dict(known[RAW_CALLS])}
    try:
        with open(path, "rb") as f:
            f.seek(known["pos"])
            for raw in f:
                if not raw.endswith(b"\n"):
                    break
                known["pos"] += len(raw)
                if any(mark in raw for mark in CREW_MARKS):
                    _crew_take(raw, known)
    except OSError:
        return known
    return known


def _crew_take(raw, known):
    try:
        record = json.loads(raw)
    except ValueError:
        return
    payload = record.get("payload") if isinstance(record, dict) else None
    if not isinstance(payload, dict):
        return
    at = record.get("timestamp") or ""
    if record.get("type") == "response_item":
        if _raw_call(payload):
            tool, use = _raw_call(payload)
            known[RAW_CALLS][use] = (tool, _word(_arguments(payload).get("agent_type")))
        return
    found = _item(record)
    if found is None or found[0].get("thread_id") not in (None, "", known[OWN]):
        return
    item = found[1]
    if item.get("type") == "UserMessage":
        known["asked"] = None
        return
    asked = _questions(item) if item.get("type") == "AgentMessage" else []
    if asked:
        was = known["asked"] or {"count": 0}
        known["asked"] = {"id": item.get("id") if isinstance(item.get("id"), str) else "",
                          "text": asked[0]["title"], "count": was["count"] + len(asked), "at": at}
        return
    if item.get("type") == COLLAB and item.get("tool") == SPAWN:
        for agent in _agents(item):
            known["agents"][agent["id"]] = {
                "id": agent["id"], "name": agent["name"] or agent["role"] or agent["id"][-8:], "role": agent["role"],
                "model": _word(item.get("model")), "state": agent["state"] or "pending_init", "at": at}
        return
    if item.get("type") == COLLAB:
        for agent in _agents(item):
            _crew_stands(known, agent["id"], agent["state"], at)
        return
    if item.get("type") != ACTIVITY:
        return
    for row in _activity(item, at, 0, known):
        if row["role"] == "spawn":
            for agent in row["spawned"]:
                known["agents"][agent["id"]] = {"id": agent["id"], "name": agent["name"], "role": agent["role"],
                                                "model": agent["model"], "state": agent["state"], "at": at}
        elif row["role"] == "agentstates":
            for agent in row["spawned"]:
                _crew_stands(known, agent["id"], agent["state"], at)


def _crew_stands(known, thread, state, at):
    """Sets how an agent of the thread stands, and since when it is over when it is."""
    agent = known["agents"].get(thread)
    if agent is None or not state or state == agent["state"]:
        return
    agent = {**agent, "state": state}
    agent.pop("doneAt", None)
    if state not in ("pending_init", "running"):
        agent["doneAt"] = at
    known["agents"][thread] = agent


# How many records after a call its answer is looked for: what the other
# agents write meanwhile stands between a wait and its answer.
ANSWER_LIMIT = 512


def _answer(f, use):
    """Returns the output of the call with this id and when it came, read on from where f stands, or None."""
    mark = json.dumps(use).encode()
    for _ in range(ANSWER_LIMIT):
        raw = f.readline()
        if not raw:
            return None
        if mark not in raw:
            continue
        try:
            record = json.loads(raw)
        except ValueError:
            continue
        payload = record.get("payload") if isinstance(record, dict) else None
        if (record.get("type") == "response_item" and isinstance(payload, dict)
                and payload.get("type") == "function_call_output" and payload.get("call_id") == use):
            return payload.get("output"), record.get("timestamp") or ""
    return None


# What else codex does that the feed draws as a call among the calls: a wait
# it set itself, a picture it looked at — the kind of claude's reading of a
# file — and what a hook put into the turn, as claude's hook speaks in the
# feed of claude.
CLOCK_SLEEP = "clock.sleep"
VIEW_IMAGE = "ViewImage"
FILES = "files"
HOOK = "hook"

# How many places a web search found the call opened lists.
MAX_FOUND = 20


def _searched(item):
    """Returns what a web search looked for: the words, the page it opened, or what it looked for in a page."""
    action = item.get("action") if isinstance(item.get("action"), dict) else {}
    url = action.get("url") if isinstance(action.get("url"), str) else ""
    pattern = action.get("pattern") if isinstance(action.get("pattern"), str) else ""
    if action.get("type") == "findInPage" and url:
        return f"{pattern} in {url}" if pattern else url
    if action.get("type") == "openPage" and url:
        return url
    query = item.get("query")
    return query if isinstance(query, str) else ""


def _found(item):
    """Returns the places a web search found, a title and an address each."""
    lines = []
    for one in _list(item.get("results"))[:MAX_FOUND]:
        if not isinstance(one, dict) or not isinstance(one.get("url"), str):
            continue
        title = one.get("title") if isinstance(one.get("title"), str) else ""
        lines.append(f"{title}\n{one['url']}" if title else one["url"])
    return "\n\n".join(lines)


def _hook_said(item):
    """Returns what a hook put into the turn as (the hook, its words), or None.

    The hook is named by the event it ran on — the run of a hook names it
    before anything else — as claude's hook is named by its own name.
    """
    texts, event = [], ""
    for fragment in _list(item.get("fragments")):
        if not isinstance(fragment, dict) or not isinstance(fragment.get("text"), str):
            continue
        texts.append(fragment["text"].strip())
        run = fragment.get("hookRunId")
        event = event or (run.split(":", 1)[0] if isinstance(run, str) else "")
    said = "\n\n".join(t for t in texts if t)
    if not said:
        return None
    return (f"{event} hook" if event else "hook"), said


# A message between a thread and its agents of the second set: a raw record
# of the model, its author and its recipient by their paths, its words after
# a head codex puts before them — the kind, the task it is about, the sender.
# Codex keeps every one but the last answer of an agent encrypted, so the
# feed shows that one as a letter from the agent and the others as calls
# among the calls: from whom it came, and that its words are not shown.
AGENT_LETTER = "agent_message"
LETTER_HEAD = "Payload:\n"
FINAL_ANSWER = "FINAL_ANSWER"
LETTER_NAME = "Letter"


def _letter_parts(raw):
    """Returns the kind of a message between agents and its words, empty when they travel encrypted."""
    texts = [part["text"] for part in _list(raw.get("content"))
             if isinstance(part, dict) and part.get("type") == "input_text" and isinstance(part.get("text"), str)]
    whole = "".join(texts)
    head, _, said = whole.partition(LETTER_HEAD)
    kind = ""
    for line in head.splitlines():
        if line.startswith("Message Type:"):
            kind = line.split(":", 1)[1].strip()
    return kind, said.strip()


def _letter_details(record, raw):
    """Returns a message between agents opened: its kind, from whom and to whom; its words travel encrypted."""
    kind, said = _letter_parts(raw)
    called = {"type": kind, "from": raw.get("author"), "to": raw.get("recipient")}
    if not said:
        called["words"] = "encrypted"
    at = record.get("timestamp") or ""
    args, args_cut = cut(json.dumps(called, ensure_ascii=False, indent=2, sort_keys=True), MAX_ARGS)
    result, result_cut = cut(said, MAX_RESULT)
    return {"tool": LETTER_NAME, "args": args, "argsCut": args_cut, "at": at, "result": result,
            "resultCut": result_cut, "failed": False, "resultAt": at}


def _agent_letter(raw, at, pos, state):
    """Returns the rows of a message between a thread and its agents: a letter, or a call whose words are not shown.

    A message the thread wrote itself is one it sent: a call to whom it went.
    """
    author, recipient = agent_name(raw.get("author")), agent_name(raw.get("recipient"))
    sent = raw.get("author") == (state or {}).get(PATH)
    kind, said = _letter_parts(raw)
    if kind == FINAL_ANSWER and not sent:
        body, trimmed = cut(said, MAX_TEXT)
        return [{"role": "mail", "from": author, "source": "agent", "text": body, "cut": trimmed,
                 "at": at, "pos": pos}]
    use = raw.get("id") if isinstance(raw.get("id"), str) else ""
    arg = f"to {recipient}" if sent else f"from {author}"
    return _call(LETTER_NAME, one_line(arg), use, 0, False, at, pos, kind=AGENTS)


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
    rollout is of, and the calls to agents of the second set it has met.
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
    raw = record.get("payload")
    if record.get("type") == "response_item" and isinstance(raw, dict):
        if _raw_call(raw):
            return _raw_call_row(raw, at, pos, state if state is not None else {})
        if raw.get("type") == AGENT_LETTER:
            return _agent_letter(raw, at, pos, state)
        return _raw_answer(raw, at, pos, state) or []
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
        text = _text(item.get("content"), "text")
        letters = _letters(text, at, pos)
        if letters:
            return letters
        role = TASK if (state or {}).get(DRIVEN) else "me"
        return _said(role, text, at, pos)
    if kind == "AgentMessage":
        asked = _questions(item)
        if asked:
            return [{"role": "question", "use": use, "asks": asked, "at": at, "pos": pos}]
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
        return _call(SEARCH, one_line(_searched(item)), use, 0, _failed(item), at, pos)
    if kind == "Extension" and item.get("kind") == CLOCK_SLEEP:
        # A wait drawn from its call already; one whose call fell before the
        # window, or a rollout with no records of the model, is drawn here.
        if use in (state or {}).get(RAW_CALLS, {}):
            return []
        return _call(SLEEP_NAME, duration(item.get("durationMs")), use, 0, False, at, pos, kind=TIME)
    if kind == "ImageView":
        path = item.get("path") if isinstance(item.get("path"), str) else ""
        return _call(VIEW_IMAGE, one_line(path), use, 0, False, at, pos, kind=FILES)
    if kind == "HookPrompt":
        said = _hook_said(item)
        return _call(said[0], one_line(said[1]), use, 0, False, at, pos, kind=HOOK) if said else []
    if kind == "ContextCompaction":
        return [{"role": "note", "text": COMPACTED, "at": at, "pos": pos}]
    if kind == COLLAB:
        if item.get("tool") == SPAWN:
            return [_spawn(item, use, at, pos)]
        rows_of_call = _agent_call(item, use, at, pos)
        # The item of a call of the second set — the wait — says only how the
        # agents stand: the call is drawn from the call.
        if use in (state or {}).get(RAW_CALLS, {}):
            return [row for row in rows_of_call if row["role"] == "agentstates"]
        return rows_of_call
    if kind == ACTIVITY:
        return _activity(item, at, pos, state)
    if kind == MCP_CALL:
        return _mcp_call(item, use, at, pos)
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


def details(record, index, f=None):
    """Returns one codex call whole, the way the feed opens a claude one, or None.

    A command is what it ran and where, and what it printed; a change is the
    diff of its file at index, and what codex said applying it. A call to
    agents of the second set has its answer in a later record: f is the
    rollout read up to the end of this one.
    """
    raw = record.get("payload")
    if record.get("type") == "response_item" and isinstance(raw, dict) and _raw_call(raw):
        return _raw_details(record, raw, f) if index == 0 else None
    if record.get("type") == "response_item" and isinstance(raw, dict) and raw.get("type") == AGENT_LETTER:
        return _letter_details(record, raw) if index == 0 else None
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
        called = {key: item[key] for key in ("query", "action") if item.get(key)}
        shown = json.dumps(called, ensure_ascii=False, indent=2, sort_keys=True) if called else ""
        name, said = SEARCH, _found(item)
    elif kind == "Extension" and item.get("kind") == CLOCK_SLEEP and index == 0:
        name, shown, said = SLEEP_NAME, json.dumps({"duration_ms": item.get("durationMs")}), ""
    elif kind == "ImageView" and index == 0:
        name, shown, said = VIEW_IMAGE, json.dumps({"path": item.get("path")}, ensure_ascii=False), ""
    elif kind == "HookPrompt" and index == 0 and _hook_said(item):
        (name, said), shown = _hook_said(item), ""
    elif kind == COLLAB and index == 0:
        name, shown, said = _agent_spot(item)
    elif kind == MCP_CALL and index == 0 and _mcp_name(item):
        name = _mcp_name(item)
        shown = json.dumps(_mcp_args(item), ensure_ascii=False, indent=2, sort_keys=True)
        said = _mcp_said(item)
    else:
        return None
    at = record.get("timestamp") or ""
    args, args_cut = cut(shown, MAX_ARGS)
    result, result_cut = cut(said, MAX_RESULT)
    return {"tool": name, "args": args, "argsCut": args_cut,
            "at": _stamp(payload.get("started_at_ms")) or at,
            "result": result, "resultCut": result_cut, "failed": _failed(item),
            "resultAt": _stamp(payload.get("completed_at_ms")) or at}
