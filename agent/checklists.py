"""The checklist of a session: the list of steps the model keeps through the panel's checklist tool.

The executor serves the tool to every session the panel starts and writes one
file a session, told as its role is: by the config directory of the account,
the repository the session works in and its name, so the sessions of one
repository keep checklists of their own, and a session started again under its
name — in the main checkout or in a worktree of it — finds the checklist it
left. A session without a name is told by its account and its repository
alone, and a thread of codex by its thread. The collector reads the file into
the row of the session and into the state of its conversation, in the small
shape the screens take: every step with its status and, for a step at work or
done, since when.
"""
import json
import os
import re

from sessionfile import clean as _clean, name as _name
import roles
import sessionfile

STATUSES = ("pending", "active", "done", "dropped")

# The id codex gives a thread, which names the file of its checklist.
THREAD_ID = re.compile(r"[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}")


def checklists_dir():
    """Returns where the executor keeps the checklists: the owner's state."""
    return sessionfile.state_dir("checklists")


def name_of(config_dir, cwd, name=None):
    """Returns the file name of the checklist of a session, or None for a place that is not one.

    A checklist is kept under the key of the role of the session: the account,
    the repository the directory works in and the name — the one the executor
    computes — so a session moving between the main checkout and a worktree of
    it keeps its checklist, and sessions of one name in two worktrees of a
    repository share one.
    """
    return roles.file_name(config_dir, cwd, name)


def _keys(config_dir, cwd, name=None):
    """Returns where the checklist of a session is looked for: each file with the field that tells it is the session's.

    The file kept under the repository comes first, then the one of the
    account, the directory and the name, which a server keying checklists by
    the directory writes until its session starts again: a session with no
    checklist under its repository reads that one, and the executor moves it
    under the repository at the session's next write. The executor looks in
    the same order.
    """
    return ((name_of(config_dir, cwd, name), "repo", roles.repo(cwd)),
            (sessionfile.file_name(config_dir, cwd, name), "dir", _clean(cwd)))


def thread_file(thread):
    """Returns the file name of the checklist of a codex thread, or None for what is no id of one.

    A thread is told by its id alone, the name the executor gives the file:
    codex keeps the id for the life of the thread, and the place of a thread
    is named by paths the server of the thread and the executor need not
    write alike.
    """
    if not isinstance(thread, str) or not THREAD_ID.fullmatch(thread):
        return None
    return "codex-" + thread + ".json"


def of_thread(thread):
    """Returns the checklist of a codex thread for the screens, or None when it keeps none."""
    file = thread_file(thread)
    if file is None:
        return None
    data = _load(os.path.join(checklists_dir(), file))
    if data is None or data.get("sessionId") != thread:
        return None
    return _shape(data)


def _text(value):
    return value.strip() if isinstance(value, str) else ""


def _load(path):
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def _read(config_dir, cwd, name):
    """Returns the file of the checklist of a session as it lies, or None when it keeps none."""
    for file, field, where in _keys(config_dir, cwd, name):
        if file is None:
            return None
        data = _load(os.path.join(checklists_dir(), file))
        if (data is not None and data.get("configDir") == _clean(config_dir) and data.get(field) == where
                and _name(data.get("name")) == _name(name)):
            return data
    return None


def of(config_dir, cwd, name=None, sid=None):
    """Returns the checklist of a live session for the screens, or None when it keeps none.

    A named session without a file of its own shows the checklist kept without
    a name — the one a server that does not tell the sessions of a place apart
    keeps writing — only while its conversation `sid` sent it last: which
    session sent the checklist of another conversation is not to be told.
    """
    data = _read(config_dir, cwd, name)
    if data is None and _name(name) and sid:
        placed = _read(config_dir, cwd, None)
        data = placed if placed is not None and placed.get("sessionId") == sid else None
    return _shape(data)


def of_conversation(config_dir, cwd, sid):
    """Returns the checklist a conversation that is over left, or None.

    It is the checklist of its account and repository — under whatever name,
    kept under the repository or by the directory — that the conversation sent
    last: once a later conversation of the session sends the checklist, the one
    before has none.
    """
    if name_of(config_dir, cwd) is None or not sid:
        return None
    root = checklists_dir()
    try:
        files = os.listdir(root)
    except OSError:
        return None
    found = None
    for file in files:
        if not file.endswith(".json"):
            continue
        data = _load(os.path.join(root, file))
        if data is None or data.get("configDir") != _clean(config_dir) or data.get("sessionId") != sid:
            continue
        if not any(file == kept and data.get(field) == where
                   for kept, field, where in _keys(config_dir, cwd, data.get("name"))):
            continue
        if found is None or _text(data.get("at")) > _text(found.get("at")):
            found = data
    return _shape(found)


def _shape(data):
    """Returns a checklist as it lies in the shape the screens take, or None when no step of it is drawn."""
    if data is None:
        return None
    items = []
    for item in data.get("items") if isinstance(data.get("items"), list) else []:
        if not isinstance(item, dict):
            continue
        text, status = _text(item.get("text")), item.get("status")
        if not text or status not in STATUSES:
            continue
        step = {"text": text, "status": status}
        since = _text(item.get("since"))
        if since:
            step["since"] = since
        items.append(step)
    if not items:
        return None
    checklist = {"items": items, "at": _text(data.get("at"))}
    note = _text(data.get("note"))
    if note:
        checklist["note"] = note
    return checklist
