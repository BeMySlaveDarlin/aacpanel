#!/usr/bin/env python3
"""Context fill of live sessions."""
import datetime as dt
import glob
import json
import os
import re

import archive
import chat
import contours
import held
from chat import codex

SESSION_MODELS = os.environ.get("AACP_SESSION_MODELS")

# What a process of the panel's own sessions is started under: the server of
# tmux for a console, the holder for a session on the stream.
TMUX_SERVER = "tmux: server"
HOLDER = "aacpanel-exec"

# The servers of tmux the executor finds consoles on, as tmux_server names
# them: the user's own, and the one the terminals of the panel live on, where a
# person may type claude into a shell. The executor names the socket of the
# terminals too, and a test of the executor holds the two names together.
PANEL_TMUX = "aacpanel-term"
PANEL_SERVERS = ("", "-L " + PANEL_TMUX)

# The options of tmux that take a value, as its getopt string has them.
TMUX_VALUED = "cfLST"


def proc_start(pid):
    """Returns the start time of a process in ticks, from /proc/<pid>/stat."""
    try:
        with open(f"/proc/{pid}/stat") as f:
            return f.read().rsplit(")", 1)[1].split()[19]
    except (OSError, IndexError):
        return None


def parent_pid(pid):
    """Returns the parent of a process, or None when it is gone."""
    try:
        with open(f"/proc/{pid}/stat") as f:
            return int(f.read().rsplit(")", 1)[1].split()[1])
    except (OSError, IndexError, ValueError):
        return None


def proc_args(pid):
    """Returns the arguments a process was started with, or an empty list when it is gone."""
    try:
        with open(f"/proc/{pid}/cmdline", "rb") as f:
            raw = f.read().rstrip(b"\0")
    except OSError:
        return []
    return [os.fsdecode(arg) for arg in raw.split(b"\0")] if raw else []


def _comm(pid):
    try:
        with open(f"/proc/{pid}/comm") as f:
            return f.read().strip()
    except OSError:
        return ""


def tmux_server(pid):
    """Says how a server of tmux is reached, the way its start named it.

    tmux names its server through the name of the process and leaves the
    command line as the client that started the server gave it, so the socket
    is read off that: "-L <name>", "-S <path>", or "" for the user's own server,
    started with neither or as -L default. The price of reading the start: a
    server whose socket directory was moved with TMUX_TMPDIR passes for the
    socket of the same name in the usual directory.
    """
    args = proc_args(pid)[1:]
    label, path = "default", ""
    i = 0
    while i < len(args):
        arg = args[i]
        if arg == "--" or len(arg) < 2 or not arg.startswith("-"):
            break
        for at, flag in enumerate(arg[1:], 1):
            if flag not in TMUX_VALUED:
                continue
            value = arg[at + 1:]
            if not value:
                i += 1
                value = args[i] if i < len(args) else ""
            if flag == "L":
                label = value
            elif flag == "S":
                path = value
            break
        i += 1
    if path:
        return "-S " + path
    return "" if label == "default" else "-L " + label


def lineage(pid, owners):
    """Says what a session's process was started under.

    The first parent up the chain that tells decides: another live session —
    this one is a run inside its work; the user's server of tmux, the server of
    the panel's terminals or the holder of the stream — a session of the panel.
    A server of tmux on a socket of its own is out of the panel's reach: the
    claude in it is named with the server and only read. A chain that reaches
    the top with none of them is a claude started outside the panel, in a
    terminal of its own. `owners` maps the pids of the live sessions to what
    names them.
    """
    seen = set()
    at = parent_pid(pid) if pid else None
    while at and at > 1 and at not in seen:
        seen.add(at)
        owner = owners.get(at)
        if owner:
            return {"outside": True, "parent": dict(owner)}
        comm = _comm(at)
        if comm == TMUX_SERVER:
            server = tmux_server(at)
            return {} if server in PANEL_SERVERS else {"outside": True, "tmuxServer": server}
        if comm == HOLDER:
            return {}
        at = parent_pid(at)
    return {"outside": True} if pid else {}


def _oneshot(pid, sid=None):
    """Says whether a process is a run of its own: a `-p` that no holder keeps."""
    try:
        with open(f"/proc/{pid}/cmdline", "rb") as f:
            cmdline = f.read().decode("utf-8", "replace").replace("\0", " ")
    except OSError:
        return False
    if not re.search(r"(^|\s)(-p|--print)(\s|$)", cmdline):
        return False
    return held.summary(sid, pid) is None


def _boot_time():
    """Returns when the machine was booted, in epoch seconds, or None when it is not told."""
    try:
        with open("/proc/stat") as f:
            for row in f:
                if row.startswith("btime "):
                    return float(row.split()[1])
    except (OSError, IndexError, ValueError):
        return None
    return None


def started_at(pid):
    """Returns when a process was born, in epoch seconds, or None when it is gone.

    The birth is counted from the ticks of /proc/<pid>/stat against the boot
    time, and never from the directory of the process: procfs stamps that
    directory when it builds the inode, at the first look, so a long living
    process nobody has looked at until now would pass for a newborn. Sessions
    lose their background tasks and their agents to such a birth.
    """
    ticks, boot = proc_start(pid), _boot_time()
    if ticks is None or boot is None:
        return None
    try:
        return boot + int(ticks) / os.sysconf("SC_CLK_TCK")
    except (TypeError, ValueError, ZeroDivisionError):
        return None


def _iso(ts):
    return dt.datetime.fromtimestamp(ts).astimezone().isoformat() if ts else None


def live_sessions():
    """Returns the live claude sessions of every contour: name, uuid, directory, transcript."""
    out = []
    files = []
    for root in archive.live_dirs():
        try:
            files.extend(glob.glob(os.path.join(root, "*.json")))
        except OSError:
            continue
    for path in sorted(files):
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        pid, sid, cwd = data.get("pid"), data.get("sessionId"), data.get("cwd") or ""
        if not pid or not sid or not cwd:
            continue
        start = proc_start(pid)
        if start is None or (data.get("procStart") and str(data["procStart"]) != start):
            continue
        if _oneshot(pid, sid) or archive.background(data):
            continue
        name = data.get("name") or os.path.basename(cwd.rstrip("/")) or cwd
        out.append({
            "name": name, "sessionId": sid, "cwd": cwd, "pid": pid,
            "transcript": chat.claude_path(sid),
            "procStartedAt": started_at(pid),
        })
    return out


def session_model_dirs():
    """Returns the directories of the status line snapshots of every contour."""
    return contours.dirs("session-models", SESSION_MODELS)


def status_line(sid):
    """Returns the model and the effort the status line last saw for the session, or None."""
    for d in session_model_dirs():
        try:
            with open(os.path.join(d, f"{sid}.json"), encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        if isinstance(data, dict) and isinstance(data.get("at"), (int, float)):
            return data
    return None


def _epoch(stamp):
    try:
        return dt.datetime.fromisoformat(stamp.replace("Z", "+00:00")).timestamp()
    except (AttributeError, ValueError):
        return None


def _model_and_effort(found, sid):
    """Returns the model and the effort of a live session.

    The transcript learns of a change of model or effort only with the next
    request; the status line snapshot knows at once. The snapshot wins while it
    is fresher than the last request, after that the transcript is the truth.
    """
    model, effort = found.get("model") or "", found.get("effort") or ""
    seen = status_line(sid)
    if not seen:
        return model, effort
    last = _epoch(found.get("lastRequestAt") or "")
    if last is not None and seen["at"] <= last:
        return model, effort
    picked = seen.get("model")
    picked = picked.get("id") if isinstance(picked, dict) else ""
    if "effort" in seen:
        effort = str(seen["effort"] or "")
    return picked or model, effort


def _mode(found):
    mode = found.get("mode") or ""
    if not mode:
        return {}
    return {"mode": mode, "modeAt": found.get("lastAt") or ""}


def _row(live):
    transcript = live["transcript"]
    found = archive.scan(transcript) if transcript else {}
    model, effort = _model_and_effort(found, live["sessionId"])
    limit, known = archive.limit_for(model)
    started = found.get("startedAt") or _iso(live["procStartedAt"])

    if not found.get("lastRequestAt"):
        return {
            "session": live["name"], "sessionId": live["sessionId"], "cwd": live["cwd"],
            "transcript": transcript,
            "tokens": 0, "limit": limit, "pct": 0.0, "limitKnown": known,
            "tokensIn": 0, "tokensOut": 0,
            "stale": False, "noRequests": True, "model": model,
            "effort": effort, "messages": found.get("messages") or 0,
            "compacts": found.get("compacts") or 0, "startedAt": started,
            "lastRequestAt": None,
            **_mode(found),
        }

    total = found.get("tokens") or 0
    if total > limit:
        limit, known = archive.DEFAULT_LIMIT_TOKENS, False
    pct = round(total / limit * 100, 1) if limit else 0.0
    return {
        "session": live["name"], "sessionId": live["sessionId"], "cwd": live["cwd"],
        "transcript": transcript,
        "tokens": total, "limit": limit, "pct": pct, "limitKnown": known,
        "tokensIn": found.get("tokensIn") or 0, "tokensOut": found.get("tokensOut") or 0,
        "stale": bool(found.get("stale")), "model": model,
        "effort": effort, "messages": found.get("messages") or 0,
        "compacts": found.get("compacts") or 0, "startedAt": started,
        "lastRequestAt": found.get("lastRequestAt"),
        **_mode(found),
    }


def _heard(data):
    """Returns how full the context of a thread is as the executor heard it from the daemon, or None.

    The daemon tells it to the clients of the thread with every request, in
    the same count the rollout keeps: the input of the last request against
    the window of the model.
    """
    heard = data.get("context")
    if not isinstance(heard, dict):
        return None
    tokens, window, at = heard.get("tokens"), heard.get("window"), heard.get("at")
    if (isinstance(tokens, bool) or not isinstance(tokens, int) or tokens < 0
            or isinstance(window, bool) or not isinstance(window, int) or window < 0
            or not isinstance(at, str) or _epoch(at) is None):
        return None
    return {"tokens": tokens, "limit": window, "at": at}


def _fill(data, sid, contour):
    """Returns how full the context of a thread is: the fresher of what the daemon said and the rollout.

    The executor hears the daemon only while it is a client of the thread,
    and a turn another client runs reaches it through the rollout alone.
    """
    path = codex.rollout_path(sid, contour)
    found = (codex.context(path) if path else None) or {"tokens": 0, "limit": 0, "at": ""}
    heard = _heard(data)
    if heard and (not found["at"] or _epoch(heard["at"]) >= (_epoch(found["at"]) or 0)):
        if not heard["limit"]:
            heard["limit"] = found["limit"]
        return heard
    return found


def _goal(raw):
    """Returns the goal of a thread as a row carries it, or None.

    What codex works towards across turns: the objective, how it stands —
    active, paused, blocked, usageLimited, budgetLimited or complete — the
    tokens spent, the budget (None for none) and the seconds it has taken.
    """
    if not isinstance(raw, dict):
        return None
    objective, status = raw.get("objective"), raw.get("status")
    if not isinstance(objective, str) or not objective or not isinstance(status, str) or not status:
        return None

    def count(value):
        return value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else None

    return {"objective": objective, "status": status, "tokensUsed": count(raw.get("tokensUsed")) or 0,
            "tokenBudget": count(raw.get("tokenBudget")),
            "timeUsedSeconds": count(raw.get("timeUsedSeconds")) or 0,
            "updatedAt": count(raw.get("updatedAt"))}


def codex_row(data):
    """Returns the row of a codex thread a live executor follows.

    The executor knows what the thread is doing — busy, what waits for a
    person, the model, the effort, the mode and the plan where the daemon
    told them, the name and the goal of the thread, how many background
    terminals run, the messages that wait in the panel's queue, and how full
    the context is as the daemon last said it; the rollout says the fill when
    the executor heard nothing newer, as a claude row reads its transcript.
    """
    sid = data["sessionId"]
    name = data.get("name") if isinstance(data.get("name"), str) and data["name"] else f"codex-{sid[-8:]}"
    contour = data.get("contour") if isinstance(data.get("contour"), str) else ""
    found = _fill(data, sid, contour)
    tokens, limit = found["tokens"], found["limit"]
    row = {
        "session": name, "sessionId": sid, "cwd": data.get("cwd") or "",
        "profile": contour, "agent": "codex", "transport": "stream",
        "model": data.get("model") or "", "effort": data.get("effort") or "",
        "startedAt": data.get("started") or None,
        "tokens": tokens, "limit": limit, "pct": round(tokens / limit * 100, 1) if limit else 0.0,
        "limitKnown": limit > 0, "lastRequestAt": found["at"] or None,
    }
    if not found["at"]:
        row["noRequests"] = True
    # A thread codex runs in a tmux session the panel started lives in that
    # terminal, as a claude session in tmux does; one the daemon alone holds is
    # reached the way a session on the stream is, through the panel.
    terminal = data.get("terminal")
    if isinstance(terminal, str) and terminal:
        row["transport"], row["tmux"] = "tmux", terminal
    if isinstance(data.get("mode"), str) and data["mode"]:
        row["mode"] = data["mode"]
    if isinstance(data.get("plan"), bool):
        row["plan"] = data["plan"]
    # The name the panel gave the thread, while codex still calls the thread
    # by it — the executor leaves out a name codex made up or was given in its
    # own terminal: the session keeps the name the panel addresses it by, and
    # the screen shows this in its place.
    if isinstance(data.get("title"), str) and data["title"]:
        row["title"] = data["title"]
    goal = _goal(data.get("goal"))
    if goal:
        row["goal"] = goal
    # Background terminals a turn left running, as the executor last read them.
    processes = data.get("processes")
    if isinstance(processes, int) and not isinstance(processes, bool) and processes > 0:
        row["processes"] = processes
    # Messages that wait in the panel's queue, counted as a claude row counts
    # the queue of its holder.
    queued = data.get("queue")
    if isinstance(queued, int) and not isinstance(queued, bool) and queued > 0:
        row["queued"] = queued
    # The map knows a contour by the config directory of its claude account,
    # and a codex home is named after the contour it belongs to: the thread
    # carries that directory, and the service puts it on the contour the
    # claude sessions of the same name are on. A home no claude account is
    # named like has none, and lands where a session of no known contour does.
    config_dir = dict(contours.profiles()).get(contour) if contour else None
    if config_dir:
        row["configDir"] = config_dir
    wait = held.waiting_for(data)
    if wait:
        row["status"] = "waiting"
        row["waitingFor"] = wait
    else:
        row["status"] = "busy" if data.get("busy") is True else "idle"
    return row


def codex_sessions():
    """Returns the rows of the codex threads live executors follow."""
    return [codex_row(data) for data in held.codex_threads()]


def sessions():
    """Returns the live sessions as {"sessions": [...], "notes": []}, the fullest first."""
    lives = live_sessions()
    owners = {live["pid"]: {"session": live["name"], "sessionId": live["sessionId"]}
              for live in lives if live.get("pid")}
    rows = []
    for live in lives:
        row = _row(live)
        row.update(lineage(live.get("pid"), owners))
        rows.append(row)
    rows.sort(key=lambda r: -r["pct"])
    return {"sessions": rows, "notes": []}
