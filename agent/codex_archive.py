"""Archive of codex threads: the conversations a person had with codex, from the state database of each home.

Codex keeps a row per thread in the state database of its home: where its
rollout lies, the directory, when it started and last changed, the model, the
effort and the tokens it spent. The archive is read from there and from no
rollout: the rollouts of a busy home run to gigabytes, nearly all of them runs
of `codex exec`.

Only the conversations of a person stand in the archive: a thread started in
codex's terminal (source cli) or by a client of the daemon — codex's terminal
on it or the panel (source vscode). A run of `codex exec` is a claude
session's reviewer or worker rather than a conversation, and a subagent's
thread is part of its parent's turn: neither stands on its own. A run stands
among the agents of the claude session that started it instead, found by the
call of the session that started it (runs). A thread a
person archived in codex is put away from codex's own lists and its rollout
moved out of sessions/, and leaves the panel's archive too, as a claude
transcript that was deleted does.

No words of the conversation go into a row: codex keeps the first message of
a thread and a name it made of it, and the row is named by the name the panel
gave the thread, or by its directory.
"""

import bisect
import datetime
import json
import os
import sqlite3
import sys

import contours
import held

STATE_DB = "state_5.sqlite"

AGENT = "codex"

# The sources of a person's conversations: codex's own terminal, and a client
# of the daemon, which is what the panel and codex's terminal on the daemon
# are. A run of codex exec is "exec", a subagent's thread a JSON object.
PERSON_SOURCES = ("cli", "vscode")

QUERY = """
SELECT id, rollout_path, cwd,
       COALESCE(created_at_ms, created_at * 1000),
       COALESCE(updated_at_ms, updated_at * 1000),
       model, reasoning_effort, tokens_used, name
  FROM threads
 WHERE archived = 0
   AND source IN ({sources})
   AND COALESCE(thread_source, '') <> 'subagent'
   AND id NOT IN (SELECT child_thread_id FROM thread_spawn_edges)
""".format(sources=", ".join("?" for _ in PERSON_SOURCES))

_said = set()


def say(home, err):
    """Logs why a home was not read, once per reason."""
    line = f"{home}: {err}"
    if line in _said:
        return
    _said.add(line)
    print(f"aacpanel-agent: the codex archive of {line}", file=sys.stderr, flush=True)


def connect(path):
    """Opens the state database of a home for reading, in a directory the collector cannot write to.

    Codex keeps the database in WAL mode, and a reader of such a database
    needs its shared-memory file: open read-only, it uses the one that is
    there — the one codex works with — and sees what codex has written to the
    WAL so far. Where the file is not there and the directory takes no new
    one, the read-only open fails. Then the database is read as unchanging,
    without the WAL and without locks, and only when the WAL is empty or gone:
    codex writes no WAL without the shared-memory file beside it, so nobody is
    writing, and the database file holds every row there is. A WAL with rows in
    it and no shared-memory file is left alone: read as unchanging, the rows in
    the WAL would be missing from the archive without a word.
    """
    db = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    try:
        db.execute("SELECT 1 FROM threads LIMIT 1")
        return db
    except sqlite3.OperationalError:
        db.close()
        wal = path + "-wal"
        if os.path.exists(path + "-shm") or (os.path.exists(wal) and os.path.getsize(wal) > 0):
            raise
    return sqlite3.connect(f"file:{path}?immutable=1", uri=True)


def homes(wanted=None):
    """Returns the codex homes of the named contours, every one without names, as pairs of contour and directory."""
    pairs = contours.codex_homes()
    if not wanted:
        return pairs
    return [(contour, home) for contour, home in pairs if contour in wanted]


def threads(wanted=None, under=None, skip=()):
    """Returns the threads of a person in the codex homes, the newest first.

    With a directory, only the threads that ran in it or below it; skip holds
    the ids left out — the conversations live on the list of sessions.
    """
    under = (under or "").rstrip("/")
    skip = set(skip or ())
    out = []
    for contour, home in homes(wanted):
        path = os.path.join(home, STATE_DB)
        if not os.path.isfile(path):
            continue
        try:
            db = connect(path)
            try:
                rows = db.execute(QUERY, PERSON_SOURCES).fetchall()
            finally:
                db.close()
        except (sqlite3.Error, OSError) as e:
            say(home, e)
            continue
        for row in rows:
            found = thread(row, contour, home)
            if found is None or found["id"] in skip:
                continue
            cwd = found["cwd"].rstrip("/")
            if under and cwd != under and not cwd.startswith(under + "/"):
                continue
            out.append(found)
    out.sort(key=lambda t: (t["updatedMs"], t["id"]), reverse=True)
    return out


def thread(row, contour, home):
    """Returns a thread of the archive out of a row of the database, or None for one it has no rollout of."""
    tid, rollout, cwd, created, updated, model, effort, used, name = row
    if not isinstance(tid, str) or not tid or not isinstance(rollout, str) or not os.path.isfile(rollout):
        return None
    return {
        "id": tid, "contour": contour, "home": home, "rollout": rollout,
        "cwd": cwd if isinstance(cwd, str) else "",
        "createdMs": _ms(created), "updatedMs": _ms(updated),
        "model": model if isinstance(model, str) else "",
        "effort": effort if isinstance(effort, str) else "",
        "tokensUsed": _ms(used),
        "called": name if isinstance(name, str) else "",
    }


def _ms(value):
    return value if isinstance(value, int) and not isinstance(value, bool) and value > 0 else 0


def stamp(ms):
    """Returns a time in milliseconds as the archive writes times, empty for none."""
    if not ms:
        return ""
    return datetime.datetime.fromtimestamp(ms / 1000, datetime.timezone.utc) \
        .isoformat(timespec="milliseconds").replace("+00:00", "Z")


def given_name(tid):
    """Returns the name the panel gave a thread, as the executor keeps it, empty when it gave none."""
    if not held.named(tid):
        return ""
    try:
        with open(os.path.join(held.kept_dir(), "named", tid + ".json"), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return ""
    name = data.get("name") if isinstance(data, dict) else None
    return name.strip() if isinstance(name, str) else ""


def name_of(found):
    """Returns the name a thread reads by and whether it was guessed.

    The name the panel gave the thread, for as long as codex calls the thread
    by it, as the live session reads; the directory it ran in otherwise, as a
    claude conversation the panel never named reads. The name codex keeps for
    a thread is not shown: codex makes it of the first request.
    """
    given = given_name(found["id"])
    if given and found["called"].strip() == given:
        return given, False
    cwd = found["cwd"]
    return os.path.basename(cwd.rstrip("/")) or cwd or "codex-" + found["id"].replace("-", "")[-8:], True


def present(found):
    """Returns the archive row of a thread, in the shape of a claude conversation's."""
    name, guessed = name_of(found)
    cwd = found["cwd"]
    used = found["tokensUsed"]
    return {
        "sessionId": found["id"],
        "agent": AGENT,
        "name": name,
        "nameGuessed": guessed,
        "cwd": cwd,
        "profile": found["contour"],
        "slug": "",
        "home": bool(cwd) and cwd.rstrip("/") == os.path.expanduser("~"),
        "model": found["model"],
        "effort": found["effort"],
        "pct": 0.0,
        "pctMax": 0.0,
        "tokens": 0,
        "tokensMax": 0,
        "tokensUsed": used,
        "limit": 0,
        "limitKnown": False,
        "messages": 0,
        "compacts": 0,
        "stale": False,
        "startedAt": stamp(found["createdMs"]),
        "lastAt": stamp(found["updatedMs"]),
        "lastRequestAt": "",
        "noRequests": not used,
        "prompts": [],
    }


# A run of codex exec a claude session started is found by the call of the
# shell that started it: codex makes the thread of a run within a minute of
# the call, or later while the call is still out — a script that runs codex
# several times in turn — in the directory the call ran in or one below it, or
# in one its command names. A run that works on a task does it in a directory
# of the task under the project's own.
RUN_WINDOW_MS = 60_000
TASKS = "/.agents/tasks/"

# The threads of runs made within a span of time. The span is the narrow part,
# and the other columns are kept off their indexes: on the one of the source
# the database would walk every run the home ever had.
RUNS = """
SELECT id, rollout_path, cwd, created_at_ms, updated_at_ms, model, agent_role
  FROM threads
 WHERE created_at_ms BETWEEN ? AND ?
   AND +source = 'exec'
   AND +archived = 0
 ORDER BY created_at_ms, id
"""


def runs(calls, contour, until=0):
    """Returns the runs of codex exec the calls of a claude session started, the newest first.

    The calls are the ones the state of a session keeps (sesstate.runs); the
    runs are threads of the codex home of the session's contour, made where a
    call ran, within the window after it or while it was still out. A call
    that never came back is out until `until`, in milliseconds: now for a
    live session, its last word for one that is over. A contour with no codex
    home of its own runs codex in the home codex takes when none is named.
    Each run says its thread, its rollout, when it was made and last written,
    its model and role, and what the session called the call that started it.
    A call that resumes a thread makes none, and none is put down to it: the
    thread it resumes is the run of the call that started it. Nothing the run
    was asked comes along: the database keeps the first message of a thread,
    and it is not read.
    """
    home = None
    if contour:
        home = dict(contours.codex_homes()).get(contour) or os.path.expanduser(contours.CODEX_HOME)
    starts = sorted((c for c in calls if c.get("at") and not c.get("resume")), key=lambda c: c["at"])
    if not home or not starts:
        return []
    path = os.path.join(home, STATE_DB)
    if not os.path.isfile(path):
        return []
    till = {c["id"]: c.get("back") or max(until, c["at"]) for c in starts}
    # The calls out for longer than the window: the only ones a thread made
    # past the window of every call can be put down to.
    long = [c for c in starts if till[c["id"]] - c["at"] > RUN_WINDOW_MS]
    last = max([starts[-1]["at"] + RUN_WINDOW_MS] + [till[c["id"]] for c in long])
    try:
        db = connect(path)
        try:
            rows = db.execute(RUNS, (starts[0]["at"], last)).fetchall()
        finally:
            db.close()
    except (sqlite3.Error, OSError) as e:
        say(home, e)
        return []
    times = [c["at"] for c in starts]
    told = set()
    out = []
    for tid, rollout, cwd, made, written, model, role in rows:
        if not isinstance(tid, str) or not tid or not isinstance(rollout, str) or not _ms(made):
            continue
        cwd = cwd if isinstance(cwd, str) else ""
        near = [c for c in starts[bisect.bisect_left(times, made - RUN_WINDOW_MS):bisect.bisect_right(times, made)]
                if _ran_in(c, cwd)]
        if not near:
            near = [c for c in long if c["at"] < made <= till[c["id"]] and _ran_in(c, cwd)]
        if not near or not os.path.isfile(rollout):
            continue
        call = _starter(near, made, told, till)
        told.add(call["id"])
        out.append({"thread": tid, "rollout": rollout, "createdMs": made, "updatedMs": _ms(written) or made,
                    "model": model if isinstance(model, str) else "",
                    "role": role if isinstance(role, str) else "", "text": call.get("text") or ""})
    out.sort(key=lambda r: (r["createdMs"], r["thread"]), reverse=True)
    return out


def _under(path, root):
    root = root.rstrip("/")
    return bool(root) and (path == root or path.startswith(root + "/"))


def _ran_in(call, cwd):
    """Says whether a thread ran where a call may have started it."""
    own = cwd.split(TASKS, 1)[0].rstrip("/")
    if not own:
        return False
    return _under(own, call.get("cwd") or "") or any(_under(named, own) for named in call.get("paths") or ())


def _starter(near, made, told, till):
    """Returns the call that started a thread, of the calls that may have, in the order they went out.

    A call still out when codex made the thread started it rather than one
    that had come back: a command that reads codex's files names codex too. A
    call can be back before its thread is made, when it sends codex off on its
    own, so with none out every call is weighed. Of those, the earliest no
    thread is put down to yet: calls sent off one after another make their
    threads in that order. When every one has a thread already, one call
    started several, and it is the latest.
    """
    out = [c for c in near if till[c["id"]] >= made] or near
    fresh = [c for c in out if c["id"] not in told]
    return fresh[0] if fresh else out[-1]
