"""State of a claude session: background commands, subagents, artifacts, sent files."""

import json
import os
import threading

import contours

from .artifacts import (ARTIFACT_PUBLISH, ARTIFACT_URL_RE, DOC_EXT,  # noqa: F401
                        DOC_TOOLS, SENT_TOOL, artifact_fields, inside, result_text,
                        sent_files)
from .feed import State, _feed_record
from .limits import MAX_ITEMS, MAX_TEXT  # noqa: F401
from .subagents import (AGENT_ID_RE, TERMINATED_RE, _drop_terminated,  # noqa: F401
                        _lose_older_than, _mark_reported, agent_meta, letter_text)
from .tasks import (DONE_STATUSES, MAYBE_BACKGROUND, MONITOR_OVER_RE,  # noqa: F401
                    NOTIF_BLOCK_RE, NOTIF_EVENT_RE, NOTIF_STATUS_RE,
                    NOTIF_TASK_RE, NOTIF_USE_RE, STOPPERS, TASK_AGENT,
                    TASK_BASH, TASK_ID_KEYS, TASK_KIND_BY_KEY, TASK_MONITOR,
                    _finish_older_than)
from .wake import TASK_WAKE, WAKE_ID, is_wakeup  # noqa: F401

TEAMS_DIR = os.environ.get("AACP_CLAUDE_TEAMS")


def teams_dirs():
    """Returns the team directories of every contour, the personal one first."""
    return contours.dirs("teams", TEAMS_DIR)


_team_cache = {}
_team_lock = threading.Lock()


def team_names(path):
    """Returns who is in the team of this session, or None when no team is found."""
    session = os.path.basename(path)[: -len(".jsonl")] if path.endswith(".jsonl") else ""
    if len(session) < 8:
        return None
    cfg = ""
    for root in teams_dirs():
        candidate = os.path.join(root, "session-" + session[:8], "config.json")
        if os.path.exists(candidate):
            cfg = candidate
            break
    if not cfg:
        return None
    try:
        stat = os.stat(cfg)
    except OSError:
        return None
    key = (stat.st_mtime_ns, stat.st_size)
    with _team_lock:
        hit = _team_cache.get(cfg)
        if hit and hit[0] == key:
            return hit[1]
    try:
        with open(cfg, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    members = data.get("members")
    if not isinstance(members, list):
        return None
    names = {m.get("name") for m in members if isinstance(m, dict) and m.get("name")}
    with _team_lock:
        _team_cache[cfg] = (key, names)
    return names


def _let_go_before_birth(state):
    """Lets go what the previous process of the session took with it."""
    _lose_older_than(state, state.born)
    _finish_older_than(state, state.born)


def read(path, state=None, size=None, born=None):
    """Returns the state of a transcript, reading a ready state on from its position.

    Born is when the process of the session started, in epoch seconds: the
    subagents spawned and the background work started before it died with
    the process that ran them.
    """
    if size is None:
        size = os.path.getsize(path)
    if state is None or state.pos > size:
        state = State()
    if born:
        state.born = born
    if state.pos == size:
        _let_go_before_birth(state)
        return state

    with open(path, encoding="utf-8", errors="replace") as f:
        f.seek(state.pos)
        for line in f:
            if not line.endswith("\n"):
                break
            state.pos += len(line.encode("utf-8"))
            try:
                record = json.loads(line)
            except ValueError:
                continue
            if not isinstance(record, dict):
                continue
            _mark_reported(state, record)
            _drop_terminated(state, record)
            try:
                _feed_record(state, record, line)
            except (AttributeError, TypeError, ValueError):
                continue

    names = team_names(path)
    if names is not None:
        for gone in [name for name in state.agents if name not in names]:
            del state.agents[gone]
    _let_go_before_birth(state)

    meta = agent_meta(path)
    for name, agent in state.agents.items():
        known = meta.get(name)
        if not known:
            continue
        agent["model"] = known["model"]
        agent["color"] = known["color"]
        agent["last"] = known["last"]
        agent["id"] = known["id"]
        agent["kind"] = known["kind"]
        agent["tokens"] = known["tokens"]
        agent["limit"] = known["limit"]
        agent["limitKnown"] = known["limitKnown"]
        if known["text"]:
            agent["text"] = known["text"]
    return state


class Cache:
    """Parsed states of live sessions, keyed by transcript path."""

    def __init__(self):
        self._states = {}
        self._lock = threading.Lock()

    def state(self, path, born=None):
        try:
            size = os.path.getsize(path)
        except OSError:
            return None
        with self._lock:
            state = read(path, self._states.get(path), size, born)
            self._states[path] = state
            return state

    def of(self, path):
        """Returns the state for the panel, or None when there is no transcript."""
        state = self.state(path)
        return state.snapshot() if state else None

    def forget(self, keep):
        """Forgets the states of sessions that are gone."""
        with self._lock:
            for path in list(self._states):
                if path not in keep:
                    del self._states[path]


SHARED = Cache()
