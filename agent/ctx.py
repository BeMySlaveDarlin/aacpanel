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
from sesstate import ordered_agents
from sesstate.background import ACTIVE
from sesstate.limits import MAX_ITEMS

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

# Where the processes are read: procfs, or a tree laid out the same way.
PROC = "/proc"


def proc_start(pid):
    """Returns the start time of a process in ticks, from /proc/<pid>/stat."""
    try:
        with open(f"{PROC}/{pid}/stat") as f:
            return f.read().rsplit(")", 1)[1].split()[19]
    except (OSError, IndexError):
        return None


def parent_pid(pid):
    """Returns the parent of a process, or None when it is gone."""
    try:
        with open(f"{PROC}/{pid}/stat") as f:
            return int(f.read().rsplit(")", 1)[1].split()[1])
    except (OSError, IndexError, ValueError):
        return None


def proc_args(pid):
    """Returns the arguments a process was started with, or an empty list when it is gone."""
    try:
        with open(f"{PROC}/{pid}/cmdline", "rb") as f:
            raw = f.read().rstrip(b"\0")
    except OSError:
        return []
    return [os.fsdecode(arg) for arg in raw.split(b"\0")] if raw else []


def _comm(pid):
    try:
        with open(f"{PROC}/{pid}/comm") as f:
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
        with open(f"{PROC}/{pid}/cmdline", "rb") as f:
            cmdline = f.read().decode("utf-8", "replace").replace("\0", " ")
    except OSError:
        return False
    if not re.search(r"(^|\s)(-p|--print)(\s|$)", cmdline):
        return False
    return held.summary(sid, pid) is None


def _boot_time():
    """Returns when the machine was booted, in epoch seconds, or None when it is not told."""
    try:
        with open(f"{PROC}/stat") as f:
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


def _fill(data, path):
    """Returns how full the context of a thread is: the fresher of what the daemon said and the rollout.

    The executor hears the daemon only while it is a client of the thread,
    and a turn another client runs reaches it through the rollout alone.
    """
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


def _filled(found):
    """Returns the fields of a codex row that say how full its context is."""
    tokens, limit = found["tokens"], found["limit"]
    out = {"tokens": tokens, "limit": limit, "pct": round(tokens / limit * 100, 1) if limit else 0.0,
           "limitKnown": limit > 0, "lastRequestAt": found["at"] or None}
    if not found["at"]:
        out["noRequests"] = True
    return out


def _account(contour):
    """Returns the config directory of the claude account of a codex contour, or None.

    The map knows a contour by the config directory of its claude account,
    and a codex home is named after the contour it belongs to: the thread
    carries that directory, and the service puts it on the contour the
    claude sessions of the same name are on. A home no claude account is
    named like has none, and lands where a session of no known contour does.
    """
    return dict(contours.profiles()).get(contour) if contour else None


def codex_row(data):
    """Returns the row of a codex thread a live executor follows.

    The executor knows what the thread is doing — busy, what waits for a
    person, the model, the effort, the mode and the plan where the daemon
    told them, the name and the goal of the thread, how many background
    terminals run, the messages that wait in the panel's queue, and how full
    the context is as the daemon last said it; the rollout says the fill when
    the executor heard nothing newer, as a claude row reads its transcript,
    and the agents the thread has at work.
    """
    sid = data["sessionId"]
    name = data.get("name") if isinstance(data.get("name"), str) and data["name"] else f"codex-{sid[-8:]}"
    contour = data.get("contour") if isinstance(data.get("contour"), str) else ""
    path = codex.rollout_path(sid, contour)
    row = {
        "session": name, "sessionId": sid, "cwd": data.get("cwd") or "",
        "profile": contour, "agent": "codex", "transport": "stream",
        "model": data.get("model") or "", "effort": data.get("effort") or "",
        "startedAt": data.get("started") or None,
        **_filled(_fill(data, path)),
    }
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
    config_dir = _account(contour)
    if config_dir:
        row["configDir"] = config_dir
    wait = held.waiting_for(data)
    if wait:
        row["status"] = "waiting"
        row["waitingFor"] = wait
    else:
        row["status"] = "busy" if data.get("busy") is True else "idle"
    return _with_crew(row, path)


def codex_sessions():
    """Returns the rows of the codex threads live executors follow."""
    return [codex_row(data) for data in held.codex_threads()]


# What the rollouts of codex threads say of the agents they started, by the
# rollout: read on at every look, as a claude transcript is for its agents.
_crews = {}
MAX_CREWS = 256

# How the panel says an agent of a codex thread stands, by the word codex says
# it with: at work, or how it ended, in the words the list of agents knows.
CREW_STATUSES = {"pending_init": ACTIVE, "running": ACTIVE, "completed": "completed", "interrupted": "stopped",
                 "errored": "failed", "shutdown": "closed", "not_found": "gone"}


def codex_crew(path):
    """Returns the agents a codex thread started, as the agents of a session are listed: the ones at work first.

    An agent stands as the last word of it in the rollout of the thread: at
    work from its start until its turn is over, it is interrupted or it
    fails, and at work again when the thread gives it a task more. It is
    named by its nickname, else by its path or its role, with the role under
    the name, and its conversation is its own thread, by the id the agent
    carries. The rollout of the thread is all that is read: an agent whose
    thread died with the daemon stays at work until the thread hears of it.
    """
    if not path:
        return []
    found = codex.crew(path, _crews.get(path))
    if found is None:
        return []
    if path not in _crews and len(_crews) >= MAX_CREWS:
        _crews.clear()
    _crews[path] = found
    agents = []
    for one in found["agents"].values():
        agent = {"agent": CODEX, "kind": "subagent", "id": one["id"], "name": one["name"],
                 "status": CREW_STATUSES.get(one["state"], one["state"]), "at": one["at"]}
        if one["role"] and one["role"] != one["name"]:
            agent["text"] = one["role"]
        if one["model"]:
            agent["model"] = one["model"]
        if one.get("doneAt"):
            agent["doneAt"] = one["doneAt"]
        agents.append(agent)
    return ordered_agents(agents)[:MAX_ITEMS]


def _with_crew(row, path):
    """Returns the row of a codex thread with the agents it has at work, busy while they work.

    Codex lets a thread go once its own turn is over, while the agents it
    started work on; the row is then busy with the turn over, as a claude
    session waiting for its agents is, and counts them as a claude row does.
    """
    working = sum(1 for agent in codex_crew(path) if agent["status"] == ACTIVE)
    if not working:
        return row
    row["work"] = {"tasks": 0, "agents": working}
    if row.get("status") == "idle":
        row["status"], row["turnOver"] = "busy", True
    return row


# A codex is found by its process too, beside the threads the executors
# follow: a run of codex exec a claude session started — a reviewer, a
# worker — and a codex in a terminal with the server of its threads built in.
# Codex calls its process codex whoever started it. A process of the daemon of
# a home — the daemon itself, or a client that reaches it with --remote — runs
# threads the executor follows through that daemon, and is passed by.
CODEX = "codex"
CODEX_DAEMON = "app-server"
CODEX_REMOTE = "--remote"

# Codex holds a lock on a thread while it writes it: a file named by the
# thread, under its home.
CODEX_LOCKS = "thread-writer-locks"

# What the collector reads of the environment of a codex: its home, and the
# claude session and the role a run of codex exec was started with.
CODEX_VARS = ("CODEX_HOME", "CLAUDE_CODE_SESSION_ID", "CODEX_AGENT_ROLE")

# How much of the head of a rollout is read for what codex says the thread
# is: the head carries the instructions of the model too.
CODEX_HEAD = 1024 * 1024

# What a run of codex exec is called when it was started with no role.
CODEX_EXEC = "codex exec"


def _environ(pid, names):
    """Returns the variables of a process among names, none when they cannot be read."""
    try:
        with open(f"{PROC}/{pid}/environ", "rb") as f:
            raw = f.read()
    except OSError:
        return {}
    out = {}
    for entry in raw.split(b"\0"):
        key, _, value = entry.partition(b"=")
        key = os.fsdecode(key)
        if key in names:
            out[key] = os.fsdecode(value)
    return out


def _open_files(pid):
    """Returns the paths a process holds open."""
    root = f"{PROC}/{pid}/fd"
    try:
        fds = os.listdir(root)
    except OSError:
        return []
    out = []
    for fd in fds:
        try:
            out.append(os.readlink(os.path.join(root, fd)))
        except OSError:
            continue
    return out


def _codex_thread(pid, home):
    """Returns the thread a codex process writes and its rollout, by the files it holds open.

    A codex with subagents writes their threads too and holds a lock on each;
    its own is the eldest: a codex id grows with time, and a subagent is
    started by a thread that already runs.
    """
    locks = os.path.join(home, CODEX_LOCKS)
    sessions = os.path.join(home, "sessions") + os.sep
    threads, rollouts = [], {}
    for path in _open_files(pid):
        folder, name = os.path.split(path)
        if folder == locks and name.endswith(".lock"):
            threads.append(name[:-len(".lock")])
        elif path.startswith(sessions) and codex.is_rollout(path):
            rollouts[codex.own(path)[codex.OWN]] = path
    ids = sorted(t for t in (threads or rollouts) if chat.UUID_RE.match(t))
    if not ids:
        return "", ""
    thread = ids[0]
    if thread in rollouts:
        return thread, rollouts[thread]
    found = sorted(glob.glob(os.path.join(glob.escape(home), "sessions", "*", "*", "*",
                                          f"rollout-*-{thread}.jsonl")))
    return thread, found[-1] if found else ""


def _codex_head(path):
    """Returns what the head of a rollout says of its thread: how it was started and where, or None.

    A thread of codex exec says exec; one of a terminal, of an editor or of a
    subagent says otherwise. A head not written whole yet says nothing.
    """
    try:
        with open(path, "rb") as f:
            record = json.loads(f.readline(CODEX_HEAD))
    except (OSError, ValueError):
        return None
    if not isinstance(record, dict) or record.get("type") != "session_meta":
        return None
    payload = record.get("payload")
    if not isinstance(payload, dict):
        return None
    return {"exec": payload.get("source") == "exec", "cwd": codex.cwd_of(record)}


def _cwd(pid):
    try:
        return os.readlink(f"{PROC}/{pid}/cwd")
    except OSError:
        return ""


def _codex_process(pid, followed):
    """Returns what a process says of the codex thread it runs, or None for any other.

    The contour is that of the home CODEX_HOME names, ~/.codex with none, and
    never the program's: one program serves every home. A codex that writes no
    thread yet, or one whose rollout has no head yet, has nothing to read.
    """
    if _comm(pid) != CODEX:
        return None
    try:
        if os.stat(f"{PROC}/{pid}").st_uid != os.getuid():
            return None
    except OSError:
        return None
    args = proc_args(pid)[1:]
    if CODEX_DAEMON in args or any(a == CODEX_REMOTE or a.startswith(CODEX_REMOTE + "=") for a in args):
        return None
    env = _environ(pid, CODEX_VARS)
    home = os.path.normpath(os.path.expanduser(env.get("CODEX_HOME") or contours.CODEX_HOME))
    thread, rollout = _codex_thread(pid, home)
    if not thread or thread in followed or not rollout:
        return None
    head = _codex_head(rollout)
    if head is None:
        return None
    return {
        "pid": pid, "home": home, "contour": contours.codex_contour(home),
        "thread": thread, "rollout": rollout, "born": started_at(pid),
        "cwd": head["cwd"] or _cwd(pid), "exec": head["exec"],
        "claude": env.get("CLAUDE_CODE_SESSION_ID") or "", "role": env.get("CODEX_AGENT_ROLE") or "",
    }


def codex_processes():
    """Returns the live codex processes of the user whose threads no executor follows.

    Each says its pid, its home and the contour of the home, the thread it
    writes and the rollout of it, when it was born and in what directory,
    whether it is a run of codex exec, and the claude session and the role it
    was started with.
    """
    try:
        pids = [int(name) for name in os.listdir(PROC) if name.isdigit()]
    except OSError:
        return []
    followed = {data.get("sessionId") for data in held.codex_threads()}
    return [found for found in (_codex_process(pid, followed) for pid in sorted(pids)) if found]


def live_rollout(thread, contour=None):
    """Returns the rollout of a thread a live codex writes, of the contour named or of any, empty for none."""
    for proc in codex_processes():
        if proc["thread"] == thread and (not contour or proc["contour"] == contour):
            return proc["rollout"]
    return ""


def codex_parent(proc, owners, sessions):
    """Returns the id of the live claude session that started a run of codex exec, empty for none.

    Up the chain of parents, the first live session is it. A codex met on the
    way started the run itself: a daemon keeps the environment of the claude
    it was started from and hands it down to every command its threads run.
    A chain that reaches the top with neither — a run sent off on its own,
    whose parent let it go — is named by the session claude put into its
    environment, while that session lives. `owners` maps the pids of the
    live sessions to what names them, `sessions` holds their ids.
    """
    seen = set()
    at = parent_pid(proc["pid"])
    while at and at > 1 and at not in seen:
        seen.add(at)
        owner = owners.get(at)
        if owner:
            return owner["sessionId"]
        if _comm(at) == CODEX:
            return ""
        at = parent_pid(at)
    return proc["claude"] if proc["claude"] in sessions else ""


def codex_live(lives=None):
    """Returns the codex processes no executor follows, a run of codex exec with the session that started it.

    `lives` are the live claude sessions as live_sessions returns them, read
    afresh when not given.
    """
    procs = codex_processes()
    if not any(proc["exec"] for proc in procs):
        return procs
    if lives is None:
        lives = live_sessions()
    owners = {live["pid"]: {"session": live["name"], "sessionId": live["sessionId"]}
              for live in lives if live.get("pid")}
    sessions = {live["sessionId"] for live in lives}
    for proc in procs:
        if proc["exec"]:
            proc["parent"] = codex_parent(proc, owners, sessions)
    return procs


def _utc(ts):
    if not ts:
        return ""
    return dt.datetime.fromtimestamp(ts, dt.timezone.utc).isoformat(timespec="milliseconds") \
        .replace("+00:00", "Z")


def codex_agent(proc):
    """Returns a run of codex exec as an agent of the claude session that started it.

    It is at work while its process lives, named by its role, and its
    conversation is its thread: the id of the agent is the id of the thread,
    which the feed opens as a conversation of its own. Nothing of what it was
    asked goes with it: the prompt of a role carries the words of the work.
    """
    agent = {"agent": CODEX, "id": proc["thread"], "name": proc["role"] or CODEX_EXEC,
             "status": "active", "at": _utc(proc["born"])}
    return _with_context(agent, codex.context(proc["rollout"]) or {})


def _with_context(agent, found, model=""):
    """Returns the agent of a run with the model it ran on and how full its context was."""
    model = model or found.get("model")
    if model:
        agent["model"] = model
    if found.get("tokens"):
        agent["tokens"], agent["limit"] = found["tokens"], found["limit"]
        agent["limitKnown"] = found["limit"] > 0
    if found.get("at"):
        agent["last"] = found["at"]
    return agent


# How a run of codex exec that is over stands among the agents.
CODEX_DONE = "done"

# What the rollouts of runs that are over say of their context, by the size
# and the time of the file: a run that is over writes no more, and a session
# lists dozens of them on every reading of its state.
_contexts = {}
MAX_CONTEXTS = 512


def _context_over(path):
    try:
        st = os.stat(path)
    except OSError:
        return {}
    key = (st.st_size, st.st_mtime_ns)
    hit = _contexts.get(path)
    if hit and hit[0] == key:
        return hit[1]
    found = codex.context(path) or {}
    if len(_contexts) >= MAX_CONTEXTS:
        _contexts.clear()
    _contexts[path] = (key, found)
    return found


def codex_run_over(run):
    """Returns a run of codex exec that is over as an agent of the claude session that started it.

    It reads as the run at work does, over since codex last wrote its thread,
    and is named by what the session called the call that started it, else by
    its role. The run is one codex_archive.runs returns.
    """
    agent = {"agent": CODEX, "id": run["thread"], "name": run["text"] or run["role"] or CODEX_EXEC,
             "status": CODEX_DONE, "at": _utc(run["createdMs"] / 1000), "doneAt": _utc(run["updatedMs"] / 1000)}
    return _with_context(agent, _context_over(run["rollout"]), run["model"])


def codex_runs(procs):
    """Maps a claude session to the runs of codex exec it started, as its agents, the newest first."""
    out = {}
    for proc in sorted(procs, key=lambda p: -(p["born"] or 0)):
        if proc.get("parent"):
            out.setdefault(proc["parent"], []).append(codex_agent(proc))
    return out


def with_codex_runs(state, runs, over=()):
    """Returns the state of a claude session with the runs of codex exec it started among its agents.

    A run at work stands first. A run that is over stands among the agents
    that are, by when it was last heard of; while its process lives the run
    is at work, whatever its thread says.
    """
    runs = list(runs or ())
    live = {run["id"] for run in runs}
    over = [run for run in over if run["id"] not in live]
    if not runs and not over:
        return state
    agents = list(state.get("agents") or [])
    if over:
        agents = ordered_agents(agents + over)
    return {**state, "agents": (runs + agents)[:MAX_ITEMS]}


def codex_own_row(proc):
    """Returns the row of a codex no live claude session started and no executor follows.

    The panel reaches a codex thread through the daemon of its home alone,
    and this one runs in a process of its own: the row is only read, out of
    the panel's reach as a claude typed into a terminal of its own is. It is
    called the way a thread of a daemon is, by the tail of its id, and reads
    by its role for a run of codex exec, else by the name the panel gave the
    thread while a daemon held it.
    """
    thread = proc["thread"]
    found = codex.context(proc["rollout"]) or {"tokens": 0, "limit": 0, "at": "", "model": "",
                                               "effort": "", "busy": False}
    row = {
        "session": f"codex-{thread[-8:]}", "sessionId": thread, "cwd": proc["cwd"],
        "profile": proc["contour"], "agent": CODEX, "outside": True,
        "model": found["model"], "effort": found["effort"], "startedAt": _iso(proc["born"]),
        "status": "busy" if found["busy"] else "idle",
        **_filled(found),
    }
    title = (proc["role"] or CODEX_EXEC) if proc["exec"] else held.given_name(thread)
    if title:
        row["title"] = title
    config_dir = _account(proc["contour"])
    if config_dir:
        row["configDir"] = config_dir
    return _with_crew(row, proc["rollout"])


def codex_own_rows(procs):
    """Returns the rows of the codex processes that are no run of a live claude session."""
    return [codex_own_row(proc) for proc in procs if not proc.get("parent")]


def sessions():
    """Returns the live sessions as {"sessions": [...], "notes": [], "codex": [...]}, the fullest first.

    The codex processes no executor follows go along: the runs of codex exec
    a session started ride on its row under codexRuns, for the reader of its
    state to take off, and the rest are rows of their own under codex.
    """
    lives = live_sessions()
    owners = {live["pid"]: {"session": live["name"], "sessionId": live["sessionId"]}
              for live in lives if live.get("pid")}
    procs = codex_live(lives)
    runs = codex_runs(procs)
    rows = []
    for live in lives:
        row = _row(live)
        row.update(lineage(live.get("pid"), owners))
        if runs.get(live["sessionId"]):
            row["codexRuns"] = runs[live["sessionId"]]
        rows.append(row)
    rows.sort(key=lambda r: -r["pct"])
    return {"sessions": rows, "notes": [], "codex": codex_own_rows(procs)}
