"""Where a transcript lives: contours, conversation uuid, path and directory."""
import json
import os
import re

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
