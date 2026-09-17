"""Mail between claude sessions: from a subagent and from a neighbour session."""
import json
import os
import re

import contours
import ctx

from .limits import MAX_RESULT, cut


MAIL_RE = re.compile(
    r"<(teammate|cross-session|agent)-message\s+([^>]*)>(.*?)</\1-message>", re.S)
MAIL_ATTR_RE = re.compile(r'(\w[\w-]*)\s*=\s*"([^"]*)"')

# What a subagent's final report is wrapped in before it reaches the session,
# and where the report itself begins inside that wrapping. The preamble says
# the same thing every time — that the words below carry no authority of the
# person — and it is longer than most of the reports it introduces.
HANDBACK_MARK = "[Subagent hand-back]"
HANDBACK_AT = "The report follows:"


def mails(text):
    """Returns mail from other claude sessions as (who, source, what was said)."""
    out = []
    for tag, attrs, body in MAIL_RE.findall(text):
        fields = dict(MAIL_ATTR_RE.findall(attrs))
        body = body.strip()
        # The harness wraps two different things in <agent-message>: a letter
        # from a session next door and the report of a subagent this session
        # delegated to. What is inside tells them apart.
        handback = body.startswith(HANDBACK_MARK)
        if tag == "agent":
            source = "agent" if handback else "session"
        else:
            source = "agent" if tag == "teammate" else "session"
        who = (fields.get("teammate_id") or fields.get("from-name")
               or fields.get("from") or "")
        if handback:
            body = report_of(body)
        said = ""
        if body.startswith("{"):
            try:
                data = json.loads(body)
            except ValueError:
                data = None
            if isinstance(data, dict):
                who = data.get("from") or who
                said = (data.get("result") or "").strip()
                if not said:
                    said = (data.get("idleReason") or "").strip()
            else:
                said = body
        else:
            said = body
        if not said:
            said = fields.get("summary") or ""
        if not said:
            continue
        out.append((who, source, said))
    return out


def report_of(body):
    """Returns the report a hand-back carries, without the wrapping."""
    at = body.find(HANDBACK_AT)
    if at < 0:
        return body
    return body[at + len(HANDBACK_AT):].strip()


def agent_mail(path, name):
    """Returns every mail of the named subagent, in order."""
    out = []
    with open(path, "rb") as f:
        for raw in f:
            if b"teammate-message" not in raw:
                continue
            try:
                record = json.loads(raw.decode("utf-8", "replace"))
            except ValueError:
                continue
            content = (record.get("message") or {}).get("content")
            text = content if isinstance(content, str) else ""
            if not text:
                continue
            for who, source, said in mails(text):
                if who != name:
                    continue
                out.append({"at": record.get("timestamp") or "",
                            "text": cut(said, MAX_RESULT)[0]})
    return out


PEER_PID_RE = re.compile(r"cc-socks[/-](?:\w+[/-])?(\d+)\.sock$")

_PEER_NAMES = {}


def peer_pid(address):
    """Returns the pid from a socket address, or zero when the address is not one."""
    found = PEER_PID_RE.search(str(address or "").strip())
    return int(found.group(1)) if found else 0


def peer_name(address):
    """Returns the session name for an address, or the address itself when unknown."""
    pid = peer_pid(address)
    if not pid:
        return str(address or "")
    if pid in _PEER_NAMES:
        return _PEER_NAMES[pid] or str(address)
    name = ""
    for base in contours.dirs("sessions"):
        path = os.path.join(base, f"{pid}.json")
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            continue
        start = ctx.proc_start(pid)
        if start is None or (data.get("procStart") and str(data["procStart"]) != start):
            continue
        name = str(data.get("name") or "")
        break
    _PEER_NAMES[pid] = name
    return name or str(address)


def peer_forget():
    """Drops the cache of peer names."""
    _PEER_NAMES.clear()
