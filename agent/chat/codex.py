"""Codex threads: where a rollout lies and what its records give the feed.

Codex writes a thread into a rollout under the home of its contour,
sessions/YYYY/MM/DD/rollout-<time>-<thread id>.jsonl, a record a line:
{"timestamp", "type", "payload"}. None of its top-level types is a type of a
claude record, so a record is told by its own type, and a reader needs no word
on which kind of file it reads.

The feed is read off the items codex reports finished (an event_msg of type
item_completed). The raw input and output of the model beside them
(response_item) say the same again, and the counts of tokens are no row.
"""
import datetime as dt
import glob
import json
import os
import shlex

import contours
import held

from .harness import COMPACTED, STOPPED
from .limits import MAX_ARGS, MAX_RESULT, MAX_TEXT, cut
from .tools import one_line, tool_kind

KINDS = ("session_meta", "world_state", "turn_context", "response_item",
         "token_usage_record", "compacted", "event_msg")

# A call of codex is drawn as the claude call that does the same, so the feed
# groups and colours it the same way.
BASH = "Bash"
EDIT = "Edit"
SEARCH = "WebSearch"
WEB_SEARCH = "web.search"

# A call that ran and failed, or one the person refused: either is drawn as a
# call that returned an error.
FAILED = ("failed", "declined")

# How far back the context is looked for before the whole rollout is read.
TAIL_STEPS = (64 * 1024, 1024 * 1024)


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


def context(path):
    """Returns how full the context of a thread is: tokens, the window and when, from its rollout.

    Codex reports the input of the last request and the window of the model
    after every request; a thread that has made none yet has the window its
    turn named at the start, and no time. The tail is read wider until it
    holds a report, and the whole file only when none does.
    """
    try:
        size = os.path.getsize(path)
    except OSError:
        return None
    out = {"tokens": 0, "limit": 0, "at": ""}
    for back in TAIL_STEPS + (size,):
        start = max(0, size - back)
        out = {"tokens": 0, "limit": 0, "at": ""}
        try:
            with open(path, "rb") as f:
                f.seek(start)
                for raw in f:
                    # A command's output can make a line of megabytes: only
                    # a line that may be a report is parsed, and the piece of
                    # a line the seek lands in parses as nothing.
                    if b"token_count" not in raw and b"task_started" not in raw:
                        continue
                    _take(raw, out)
        except OSError:
            return None
        if out["at"] or start == 0:
            return out
    return out


def _take(raw, out):
    try:
        record = json.loads(raw)
    except ValueError:
        return
    if not isinstance(record, dict) or record.get("type") != "event_msg":
        return
    payload = record.get("payload")
    if not isinstance(payload, dict):
        return
    if payload.get("type") == "task_started":
        out["limit"] = _count(payload.get("model_context_window")) or out["limit"]
        return
    if payload.get("type") != "token_count":
        return
    info = payload.get("info")
    usage = info.get("last_token_usage") if isinstance(info, dict) else None
    tokens = _count(usage.get("input_tokens")) if isinstance(usage, dict) else None
    if tokens is None:
        return
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


def _call(name, arg, use, index, failed, at, pos, edited=""):
    """Returns a call codex has finished: its row and the mark of its result at once.

    Codex writes a call once it is over, so the result comes with it.
    """
    # Imported here: the parser of records reads this module for its rows.
    from .records import RESULT
    call = {"role": "tool", "name": name, "kind": tool_kind(name), "arg": arg, "at": at,
            "use": use, "pos": pos, "index": index}
    if edited:
        call["edited"] = edited
    mark = {"role": RESULT, "use": use, "at": at, "pos": pos}
    if failed:
        mark["failed"] = True
    return [call, mark]


def rows(record, pos):
    """Returns the feed items of one rollout record, from none to many.

    A change of several files is a call a file, as claude makes them: each
    names the file it changed, and its details open that file's diff.
    """
    at = record.get("timestamp") or ""
    if _stopped(record):
        return [{"role": "note", "text": STOPPED, "at": at, "pos": pos}]
    found = _item(record)
    if found is None:
        return []
    _, item = found
    kind = item.get("type")
    use = item.get("id") if isinstance(item.get("id"), str) else ""
    if kind == "UserMessage":
        return _said("me", _text(item.get("content"), "text"), at, pos)
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
    else:
        return None
    at = record.get("timestamp") or ""
    args, args_cut = cut(shown, MAX_ARGS)
    result, result_cut = cut(said, MAX_RESULT)
    return {"tool": name, "args": args, "argsCut": args_cut,
            "at": _stamp(payload.get("started_at_ms")) or at,
            "result": result, "resultCut": result_cut, "failed": _failed(item),
            "resultAt": _stamp(payload.get("completed_at_ms")) or at}
