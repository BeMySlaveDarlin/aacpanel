"""Live claude sessions: where their files are, what they are busy with and what goes into the snapshot."""
import json
import os

import archive
import asked
import contours
import ctx
import held
import notes

import agent


def claude_config_dirs():
    """Returns the config directories of every contour, the personal one first."""
    return contours.config_dirs()


def sessions_dirs():
    """Returns the live session directories of every contour, the personal one first."""
    return contours.dirs("sessions", agent.CLAUDE_SESSIONS)


def sessions_sources():
    """Returns pairs of contour name and its live session directory, the personal one first."""
    if agent.CLAUDE_SESSIONS:
        return [("", "", agent.CLAUDE_SESSIONS)]
    return [(name, d, os.path.join(d, "sessions")) for name, d in contours.profiles()]


def session_files():
    """Returns the parsed <pid>.json of every contour as (contour, config directory, content)."""
    out = []
    for profile, config_dir, root in sessions_sources():
        try:
            names = os.listdir(root)
        except OSError:
            continue
        for name in names:
            if not name.endswith(".json"):
                continue
            try:
                with open(os.path.join(root, name)) as f:
                    out.append((profile, config_dir, json.load(f)))
            except (OSError, json.JSONDecodeError):
                continue
    return out


def session_profiles():
    """Maps a sessionId to the contour of the conversation: its name and config directory."""
    out = {}
    for profile, config_dir, data in session_files():
        sid = data.get("sessionId")
        if sid and profile:
            out[sid] = (profile, config_dir)
    return out


def session_births():
    """Maps a sessionId to when the process of the session was born, in epoch seconds."""
    out = {}
    for data in live_session_files():
        sid = data.get("sessionId")
        born = ctx.started_at(data["pid"]) if sid else None
        if born:
            out[sid] = born
    return out


def live_session_files():
    """Returns the parsed files of the sessions whose process is really alive."""
    out = []
    for _, _, data in session_files():
        pid = data.get("pid")
        if not pid:
            continue
        start = ctx.proc_start(pid)
        if start is None or (data.get("procStart") and str(data["procStart"]) != start):
            continue
        if archive.background(data):
            continue
        out.append(data)
    return out


def live_session_waits():
    """Maps a session name to what it is waiting for, when it waits."""
    out, dropped = {}, set()
    for data in live_session_files():
        label = data.get("name")
        if not label or data.get("status") != "waiting":
            continue
        reason = data.get("waitingFor")
        if not isinstance(reason, str) or not reason.strip():
            continue
        reason = reason.strip()
        if label in out and out[label] != reason:
            dropped.add(label)
        out[label] = reason
    for label in dropped:
        out.pop(label, None)
    return out


def live_session_status():
    """Maps a session name to what it is busy with: busy, idle or waiting."""
    return _by_name(lambda data: data.get("status") or None)


def live_session_status_at():
    """Maps a session name to when its status became what it is, in epoch milliseconds."""
    def stamp(data):
        value = data.get("statusUpdatedAt")
        if isinstance(value, bool) or not isinstance(value, (int, float)) or value <= 0:
            return None
        return int(value)
    return _by_name(stamp)


def _by_name(value_of):
    out, dropped = {}, set()
    for data in live_session_files():
        label, value = data.get("name"), value_of(data)
        if not label or value is None:
            continue
        if label in out and out[label] != value:
            dropped.add(label)
        out[label] = value
    for label in dropped:
        out.pop(label, None)
    return out


# Both counts are of what is still going on. A shell that is over stays in the
# state so its output can be read, and an agent that has reported may never be
# heard from again; the card of a session says what it is doing now, not what
# it did.
def work_of(busy):
    """Returns how much of the session's work is still going on."""
    return {
        "tasks": sum(1 for t in busy["tasks"] if not t.get("done")),
        "agents": sum(1 for a in busy["agents"] if a.get("status") != "reported"),
    }


def sessions():
    """Returns the live sessions for the snapshot."""
    try:
        data = ctx.sessions()
    except Exception as e:  # noqa: BLE001
        return {"sessions": [], "notes": [f"the session count did not run: {e}"]}
    home_slug = os.path.expanduser("~").replace("/", "-")
    profiles = session_profiles()
    births = session_births()
    waits = live_session_waits()
    statuses = live_session_status()
    stamps = live_session_status_at()
    seen_transcripts = set()
    for s in data.get("sessions", []):
        transcript = s.get("transcript") or ""
        s["home"] = f"/projects/{home_slug}/" in transcript
        sid = s.get("sessionId") or ""
        if sid:
            found = profiles.get(sid)
            if found:
                s["profile"], config_dir = found
                if config_dir:
                    s["configDir"] = config_dir
        note = notes.BOARD.of(sid) if sid else None
        if note:
            s["note"] = {"text": note.get("text") or "", "at": note.get("at") or ""}
        wait = waits.get(s.get("session") or "")
        if wait:
            s["waitingFor"] = wait
        status = statuses.get(s.get("session") or "")
        if status:
            s["status"] = status
            stamp = stamps.get(s.get("session") or "")
            if stamp:
                s["statusUpdatedAt"] = stamp
        hold = held.summary(sid) if sid else None
        if hold:
            # On the stream the holder is who knows a person is waited for:
            # claude writes busy or idle, and a request sits with the holder.
            s["transport"] = "stream"
            wait = held.waiting_for(hold)
            if wait:
                s["status"] = "waiting"
                s["waitingFor"] = wait
            if isinstance(hold.get("mode"), str) and hold["mode"]:
                s["mode"] = hold["mode"]
            # An effort picked in the feed is known to the holder at once; the
            # transcript of a claude -p does not carry it.
            if isinstance(hold.get("effort"), str) and hold["effort"]:
                s["effort"] = hold["effort"]
            # A compaction is reported by claude on the stream alone: the
            # transcript learns of it only once it is over.
            if isinstance(hold.get("compacting"), str) and hold["compacting"]:
                s["compacting"] = hold["compacting"]
        if transcript:
            seen_transcripts.add(transcript)
            state = agent.SESSION_STATE.state(transcript, born=births.get(sid))
            busy = state.snapshot() if state else None
            if state and sid:
                asked.BOOK.answered(sid, set(state.answered))
            ask = asked.BOOK.of(sid) if sid else None
            if ask:
                first = (ask.get("questions") or [{}])[0]
                s["ask"] = {
                    "header": first.get("header") or "",
                    "text": first.get("text") or "",
                    "count": len(ask.get("questions") or []),
                    "at": ask.get("at") or "",
                }
            if busy:
                work = work_of(busy)
                if work["tasks"] or work["agents"]:
                    s["work"] = work
        s.pop("transcript", None)
    agent.SESSION_STATE.forget(seen_transcripts)
    alive = {s.get("sessionId") for s in data.get("sessions", []) if s.get("sessionId")}
    asked.BOOK.sweep(alive)
    notes.BOARD.sweep(alive)

    return data
