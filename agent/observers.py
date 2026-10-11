"""What a session asked the panel to wake it with: its observers and its alarms.

An observer is a command the session handed the panel to watch with
observer_start; an alarm is a moment it set with alarm_set. The executor keeps
every observer in a directory of its own under the owner's state — the record
of what it was asked, whose it is and how often it has woken its session, the
log of its command, and how the command ended once it has — and every alarm
in a file beside them: when it rings next, every how long, and how often it
has rung. The executor writes them and nothing else does; the collector reads
the same files into the state of a conversation and the row of its session,
in the small shape the screens take, and the tail of a log for the person who
taps an observer. Starting one is left to a session alone: the files are read
here, and a stop or a cancel is an action of the executor.

Both belong to the conversation of the session that started them — a claude
by its conversation, a codex by its thread — and the executor takes them away
when that session ends.
"""
import json
import os
import re

# The id the executor names the directory of an observer by.
ID = re.compile(r"[0-9a-f]{16}")

# What wakes the session: every line, a line the pattern finds, the end alone.
WAKES = ("line", "match", "exit")

# The files of one observer, as the executor names them.
RECORD = "observer.json"
LOG = "log"
OLD_LOG = "log.1"
END = "exit.json"

# How much of the end of a log a tap reads: as much as the output of a
# background command of claude, and a log moved aside joins the one there now
# while that holds fewer.
TAIL = 24 * 1024


def _state():
    return os.path.join(os.environ.get("XDG_STATE_HOME") or os.path.expanduser("~/.local/state"), "aacpanel")


def observers_dir():
    """Returns where the executor keeps the observers: the owner's state."""
    return os.path.join(_state(), "observers")


def alarms_dir():
    """Returns where the executor keeps the alarms: beside the observers, a file each."""
    return os.path.join(_state(), "alarms")


def _load(path):
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def _text(value):
    return value if isinstance(value, str) else ""


def _count(value):
    return value if isinstance(value, int) and not isinstance(value, bool) and value > 0 else 0


def _ending(data):
    """Returns how a command ended in the words of the record, or None when it says nothing a screen can tell."""
    if not isinstance(data, dict):
        return None
    out = {}
    code = data.get("code")
    if isinstance(code, int) and not isinstance(code, bool):
        out["code"] = code
    signal = _text(data.get("signal"))
    if signal:
        out["signal"] = signal
    if data.get("memory") is True:
        out["memory"] = True
    if data.get("lost") is True:
        out["lost"] = True
    return out or None


def _view(oid, rec, end):
    """Returns an observer as the screens take it, or None for a record that names none.

    The end of the command is the one its unit wrote, as soon as it did; the
    record says it only once the session was told, and alone says a unit
    that ended with no word of how.
    """
    spec = rec.get("spec") if isinstance(rec.get("spec"), dict) else {}
    name, command, wake = _text(spec.get("name")), _text(spec.get("command")), spec.get("wake")
    if rec.get("id") != oid or not name or not command or wake not in WAKES:
        return None
    view = {"id": oid, "name": name, "command": command, "dir": _text(spec.get("dir")), "wake": wake,
            "at": _text(rec.get("started")), "woken": _count(rec.get("woken"))}
    if wake == "match" and _text(spec.get("pattern")):
        view["pattern"] = spec["pattern"]
    if _count(rec.get("woken")) and _text(rec.get("lastWake")):
        view["lastWake"] = rec["lastWake"]
    told = rec.get("ended") if isinstance(rec.get("ended"), dict) else None
    ended = end if isinstance(end, dict) else told
    if ended is not None:
        view["done"] = True
        if _text(ended.get("at")):
            view["doneAt"] = ended["at"]
        how = _ending(ended)
        if how:
            view["ending"] = how
    return view


def _owner(rec):
    owner = rec.get("owner") if isinstance(rec.get("owner"), dict) else {}
    return _text(owner.get("conversation"))


def _all():
    """Returns (conversation, view) of every observer the executor keeps."""
    root = observers_dir()
    try:
        names = os.listdir(root)
    except OSError:
        return []
    out = []
    for oid in names:
        if not ID.fullmatch(oid):
            continue
        rec = _load(os.path.join(root, oid, RECORD))
        if rec is None:
            continue
        view = _view(oid, rec, _load(os.path.join(root, oid, END)))
        conversation = _owner(rec)
        if view is not None and conversation:
            out.append((conversation, view))
    out.sort(key=lambda pair: pair[1]["at"])
    return out


def of(conversation):
    """Returns the observers of a conversation, the oldest first: a claude's by its conversation, a codex's by its thread."""
    if not isinstance(conversation, str) or not conversation:
        return []
    return [view for owner, view in _all() if owner == conversation]


# A span of Go's, in nanoseconds, is told to the screens in seconds.
NS = 1_000_000_000


def _alarm(oid, rec):
    """Returns an alarm as the screens take it, or None for a record that names none."""
    name, due = _text(rec.get("name")), _text(rec.get("next"))
    if rec.get("id") != oid or not name or not due:
        return None
    view = {"id": oid, "name": name, "at": _text(rec.get("set")), "next": due, "rang": _count(rec.get("rang"))}
    note = _text(rec.get("note")).strip()
    if note:
        view["note"] = note
    every = _count(rec.get("every")) // NS
    if every:
        view["every"] = every
    if view["rang"] and _text(rec.get("lastRing")):
        view["lastRing"] = rec["lastRing"]
    return view


def _all_alarms():
    """Returns (conversation, view) of every alarm the executor keeps, the soonest first."""
    root = alarms_dir()
    try:
        names = os.listdir(root)
    except OSError:
        return []
    out = []
    for file in names:
        oid, dot, ext = file.partition(".")
        if not dot or ext != "json" or not ID.fullmatch(oid):
            continue
        rec = _load(os.path.join(root, file))
        if rec is None:
            continue
        view, conversation = _alarm(oid, rec), _owner(rec)
        if view is not None and conversation:
            out.append((conversation, view))
    out.sort(key=lambda pair: pair[1]["next"])
    return out


def alarms_of(conversation):
    """Returns the alarms of a conversation, the soonest first."""
    if not isinstance(conversation, str) or not conversation:
        return []
    return [view for owner, view in _all_alarms() if owner == conversation]


def held_by_conversation():
    """Maps a conversation to how much of what it asked the panel for is still ahead.

    observers counts the ones whose command still runs, alarms the ones that
    have a ring ahead: an alarm that rang its last is gone from the disk.
    """
    out = {}
    for owner, view in _all():
        if "done" not in view:
            counts = out.setdefault(owner, {})
            counts["observers"] = counts.get("observers", 0) + 1
    for owner, _ in _all_alarms():
        counts = out.setdefault(owner, {})
        counts["alarms"] = counts.get("alarms", 0) + 1
    return out


def tail(conversation, oid):
    """Returns the end of the log of an observer of a conversation, or None when it has no such observer.

    It is read the way the output of a background command is: the last bytes
    of it, with a line cut at their start, and whether the log holds more.
    """
    if not isinstance(oid, str) or not ID.fullmatch(oid) or not isinstance(conversation, str) or not conversation:
        return None
    path = os.path.join(observers_dir(), oid)
    rec = _load(os.path.join(path, RECORD))
    if rec is None or rec.get("id") != oid or _owner(rec) != conversation:
        return None
    data, size = b"", 0
    for name in (LOG, OLD_LOG):
        chunk, whole = _last(os.path.join(path, name), TAIL - len(data))
        data, size = chunk + data, size + whole
        if len(data) >= TAIL:
            break
    return {"text": data.decode("utf-8", "replace"), "cut": size > len(data), "size": size}


def _last(path, room):
    """Returns the last room bytes of a file and how long it is; nothing for a file that is not there."""
    try:
        with open(path, "rb") as f:
            size = os.fstat(f.fileno()).st_size
            if room <= 0:
                return b"", size
            f.seek(max(size - room, 0))
            return f.read(room), size
    except OSError:
        return b"", 0
