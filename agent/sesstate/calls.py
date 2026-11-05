"""The calls of a conversation still waiting for their result, and the gist of what each runs.

A permission dialog is about a call claude wrote to the transcript and has
not run yet: the card says which tool it is and, in a few words, what it runs.
The gist is the head of a command or the name of a file, never the whole
input: the card is read on a phone's lock screen as much as in the panel.
"""
import os
import re
from urllib.parse import urlsplit

# How many open calls are kept: a turn has a few at a time, and a call whose
# result never came is let go with its turn.
MAX_OPEN = 64

# How much of a call the card says: the first words of a command, a name.
HEAD_WORDS = 3
HEAD_MAX = 40

# A word of a command said as it is: a program, a subcommand, a flag or a
# path. A word with a quote, a variable, an assignment or anything else shell
# makes something of ends the head: what follows it is the input, not the
# command.
WORD_RE = re.compile(r"^[A-Za-z0-9_./:@%+,-]+$")
ASSIGN_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=")

FILE_TOOLS = ("Read", "Write", "Edit", "MultiEdit", "NotebookEdit", "NotebookRead")
AGENT_TOOLS = ("Agent", "Task")


def _cut(text):
    text = " ".join(text.split())
    return text if len(text) <= HEAD_MAX else text[:HEAD_MAX - 1].rstrip() + "…"


def command_head(command):
    """Returns the first words of a shell command, the assignments before it left out."""
    if not isinstance(command, str):
        return ""
    lines = command.strip().splitlines()
    words = lines[0].split() if lines else []
    while words and ASSIGN_RE.match(words[0]):
        words.pop(0)
    out = []
    for word in words[:HEAD_WORDS]:
        if not WORD_RE.match(word):
            break
        out.append(word)
    return _cut(" ".join(out))


def head(name, data):
    """Returns the gist of a call for its card: what a command starts with, the file or the place it is about."""
    if not isinstance(data, dict):
        return ""
    if name in ("Bash", "Monitor"):
        return command_head(data.get("command"))
    if name in FILE_TOOLS:
        path = data.get("file_path") or data.get("notebook_path")
        return _cut(os.path.basename(path.rstrip("/"))) if isinstance(path, str) else ""
    if name == "WebFetch":
        url = data.get("url")
        return _cut(urlsplit(url).hostname or "") if isinstance(url, str) else ""
    if name in ("Glob", "Grep"):
        pattern = data.get("pattern")
        return _cut(pattern) if isinstance(pattern, str) else ""
    if name == "Skill":
        skill = data.get("skill")
        return _cut(skill) if isinstance(skill, str) else ""
    if name in AGENT_TOOLS:
        text = data.get("description")
        return _cut(text) if isinstance(text, str) else ""
    return ""


def opened(state, block, at, agent):
    """Keeps a call written to the transcript until its result comes; agent says it is a subagent's."""
    use = block.get("id")
    if not isinstance(use, str) or not use:
        return
    name = block.get("name") or ""
    state.open_calls.pop(use, None)
    state.open_calls[use] = {"tool": name, "head": head(name, block.get("input")), "agent": agent, "at": at}
    while len(state.open_calls) > MAX_OPEN:
        state.open_calls.pop(next(iter(state.open_calls)))


def closed(state, use):
    """Lets go a call whose result came."""
    if isinstance(use, str):
        state.open_calls.pop(use, None)


def turn_over(state):
    """Lets go the session's own calls when its turn is over: a call whose result never came is not waiting."""
    for use in [u for u, call in state.open_calls.items() if not call["agent"]]:
        del state.open_calls[use]


def older_than(state, stamp):
    """Lets go the calls made before a stamp: the process that made them is gone."""
    if not stamp:
        return
    for use in [u for u, call in state.open_calls.items() if call["at"] < stamp]:
        del state.open_calls[use]


def asking(state, tool, use):
    """Returns the call a permission asks about: by its id, or the newest open call of the tool."""
    call = state.open_calls.get(use) if use else None
    if call is not None:
        return call
    for call in reversed(state.open_calls.values()):
        if call["tool"] == tool:
            return call
    return None


def dialog(state):
    """Returns the call a dialog in the console is about: the session's newest open call.

    The console says only that a dialog is open. A call of an agent the
    session waits on is not it — the dialog then is about a call of the
    agent, which this transcript does not show — and is no answer.
    """
    for call in reversed(state.open_calls.values()):
        if not call["agent"]:
            return None if call["tool"] in AGENT_TOOLS else call
    return None
