"""Letters from sessions and subagents, and instructions from the panel."""
import json
import os
import re

import contours
import ctx

from .limits import MAX_NOTE, MAX_RESULT, cut


MAIL_RE = re.compile(
    r"<(teammate|cross-session|agent)-message\s+([^>]*)>(.*?)</\1-message>", re.S)
MAIL_ATTR_RE = re.compile(r'(\w[\w-]*)\s*=\s*"([^"]*)"')
PANEL_MESSAGE_RE = re.compile(r'<panel-message\s+from="([^"]*)"(?:\s+until="([^"]*)")?\s*>(.*?)</panel-message>', re.S)


def panel_message(text):
    """Returns the sender and body of a whole panel prompt, or None for ordinary text.

    The wrapper identifies an instruction sent as a user prompt. A person
    discussing the tag in their own words still wrote an ordinary message.
    """
    text = text.strip()
    found = PANEL_MESSAGE_RE.match(text)
    if found is None or found.end() != len(text):
        return None
    return found.group(1), found.group(3).strip(), found.group(2) or ""


# What a subagent's final report is wrapped in before it reaches the session,
# and where the report itself begins inside that wrapping. The preamble says
# the same thing every time — that the words below carry no authority of the
# person — and it is longer than most of the reports it introduces.
HANDBACK_MARK = "[Subagent hand-back]"
HANDBACK_AT = "The report follows:"

# What the panel signs a letter of an alarm or an observer of the session
# with. The executor sends it in the envelope of a letter between sessions,
# but with no address to answer, which a letter of a session always carries.
PANEL_SENDERS = ("alarm", "observer")


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
        panel = panel_sender(tag, fields)
        if panel:
            source, who = panel
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


def panel_sender(tag, fields):
    """Returns (kind, name) of a letter the panel wrote for an alarm or an observer, or None.

    The panel signs it "alarm <name>" or "observer <name>" and gives it no
    address: a session next door signed so would still carry the address of
    its socket.
    """
    if tag != "cross-session" or fields.get("from"):
        return None
    kind, _, name = (fields.get("from-name") or "").partition(" ")
    if kind not in PANEL_SENDERS or not name.strip():
        return None
    return kind, name.strip()


def report_of(body):
    """Returns the report a hand-back carries, without the wrapping."""
    at = body.find(HANDBACK_AT)
    if at < 0:
        return body
    return body[at + len(HANDBACK_AT):].strip()


def undelivered(said, failed):
    """Returns why a letter reached nobody, empty when it went or the answer does not say.

    The call succeeds either way: claude answers a letter it could not deliver
    with a report of its own, and only that report says the letter went
    nowhere. A call that failed outright delivered nothing either.
    """
    said = (said or "").strip()
    if failed:
        return cut(said, MAX_NOTE)[0] or "the call failed"
    if not said.startswith("{"):
        return ""
    try:
        data = json.loads(said)
    except ValueError:
        return ""
    if not isinstance(data, dict) or data.get("success") is not False:
        return ""
    why = str(data.get("message") or "").strip()
    return cut(why, MAX_NOTE)[0] or "the letter was not delivered"


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


# The panel's tool that writes to a live session of any account on the
# machine: a letter as SendMessage writes one, to a session by its name.
LETTER_TOOL = "mcp__aacpanel__send_to_session"


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
