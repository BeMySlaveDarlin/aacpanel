"""Where a transcript lives: contours, conversation uuid, path and directory."""
import json
import os
import re
import subprocess
import threading

import contours

import chat


def profile_dirs():
    """Returns pairs of profile and conversation directory, personal one first."""
    if chat.PROJECTS_DIR:
        return [("", chat.PROJECTS_DIR)]
    return [(name, os.path.join(d, "projects")) for name, d in contours.profiles()]


def dirs_for(profile):
    """Returns the directories to search for a transcript, all of them without a profile."""
    pairs = profile_dirs()
    if not profile:
        return [d for _, d in pairs]
    return [d for name, d in pairs if name == profile]

UUID_RE = re.compile(r"^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$")


def transcript_path(session_id, profile=None):
    """Returns the conversation file found by its uuid."""
    if not UUID_RE.match(session_id or ""):
        return ""
    name = f"{session_id}.jsonl"
    for root in dirs_for(profile):
        try:
            projects = os.listdir(root)
        except OSError:
            continue
        for project in projects:
            path = os.path.join(root, project, name)
            if os.path.isfile(path):
                return path
    return ""


SUBAGENT_ID_RE = re.compile(r"^a[A-Za-z0-9-]{1,64}$")


def subagent_path(session_id, agent_id, profile=None):
    """Returns the subagent feed stored next to its parent transcript."""
    if not SUBAGENT_ID_RE.match(agent_id or ""):
        return ""
    base = transcript_path(session_id, profile)
    if not base:
        return ""
    path = os.path.join(base[: -len(".jsonl")], "subagents", f"agent-{agent_id}.jsonl")
    return path if os.path.isfile(path) else ""


def transcript_cwd(path):
    """Returns the directory the conversation ran in, taken from the transcript itself."""
    try:
        with open(path, "rb") as f:
            for raw in f:
                try:
                    record = json.loads(raw.decode("utf-8", "replace"))
                except ValueError:
                    continue
                cwd = record.get("cwd")
                if isinstance(cwd, str) and cwd:
                    return cwd
    except OSError:
        return ""
    return ""


# The address a session has on claude.ai while its Remote Control is up, and
# the trailer it leaves in the commits it writes.
REMOTE_URL = "https://claude.ai/code/"
BRIDGE_RE = re.compile(r"^session_[A-Za-z0-9]{8,64}$")

# How many transcripts one grep is handed, and how long it may take.
GREP_CHUNK = 400
GREP_TIMEOUT = 20

_closed = {}
_closed_lock = threading.Lock()


def bridge_of(address):
    """Returns the id of a bridge out of its address on claude.ai, empty when it is none."""
    bridge = (address or "").strip()
    if bridge.startswith(REMOTE_URL):
        bridge = bridge[len(REMOTE_URL):]
    return bridge if BRIDGE_RE.match(bridge) else ""


def conversation_of(address, near=""):
    """Returns the conversation a claude.ai address belongs to: its uuid, name and whether it is live.

    A live session keeps the id of its bridge in its file. A closed one left
    it in its transcript, in the record of the bridge coming up — the field
    itself, unescaped, so a conversation that merely quotes the address is not
    taken for the one that had it. The transcripts of the directory the address
    was found in are searched first: a commit of a repository is written by a
    session working in it. What was found is kept: a conversation does not
    change its address.
    """
    bridge = bridge_of(address)
    if not bridge:
        return None
    for data in _live_files():
        if data.get("bridgeSessionId") == bridge and data.get("sessionId"):
            return {"id": data["sessionId"], "name": data.get("name") or "", "live": True}
    with _closed_lock:
        hit = _closed.get(bridge)
    if hit is None:
        hit = _closed_with(bridge, near)
        if hit:
            with _closed_lock:
                _closed[bridge] = hit
    return dict(hit, live=False) if hit else None


def _live_files():
    from collect.live import live_session_files
    try:
        return live_session_files()
    except OSError:
        return []


def _slug(path):
    return re.sub(r"[^A-Za-z0-9]", "-", path or "")


def _closed_with(bridge, near):
    needle = f'"url":"{REMOTE_URL}{bridge}"'
    mine, rest = [], []
    slug = _slug(near.rstrip("/")) if near else ""
    for _, root in profile_dirs():
        try:
            projects = sorted(os.listdir(root))
        except OSError:
            continue
        for project in projects:
            folder = os.path.join(root, project)
            try:
                names = os.listdir(folder)
            except OSError:
                continue
            found = [os.path.join(folder, n) for n in names if n.endswith(".jsonl")]
            (mine if slug and project.startswith(slug) else rest).extend(found)
    for files in (mine, rest):
        path = _grep(needle, files)
        if path:
            sid = os.path.basename(path)[: -len(".jsonl")]
            return {"id": sid, "name": _title(path) or os.path.basename(transcript_cwd(path).rstrip("/")) or sid[:8]}
    return None


def _grep(needle, files):
    for at in range(0, len(files), GREP_CHUNK):
        chunk = files[at:at + GREP_CHUNK]
        try:
            done = subprocess.run(["grep", "-lF", "-m1", "-e", needle, "--", *chunk],
                                  capture_output=True, text=True, timeout=GREP_TIMEOUT)
        except (OSError, subprocess.TimeoutExpired):
            continue
        for line in done.stdout.splitlines():
            if line.strip():
                return line.strip()
    return ""


TITLE_MARK = '"type":"custom-title"'


def _title(path):
    """Returns the last name a conversation was given, empty when it had none."""
    title = ""
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                if TITLE_MARK not in line:
                    continue
                try:
                    record = json.loads(line)
                except ValueError:
                    continue
                if isinstance(record.get("customTitle"), str) and record["customTitle"]:
                    title = record["customTitle"]
    except OSError:
        return ""
    return title
