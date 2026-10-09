"""Transcript record to feed items."""
import html
import re

import sesstate
from sesstate.feed import TURN_ENDS

from . import commands
from .cards import (BRIEF_TOOL, SECRET_TOOL, artifact_card, ask_round, brief_card, permit_card, permit_row,
                    secret_call, secret_card, sent_card, wake_item)
from .harness import (AGENT_STOPPED, classify, coordinator_letter, interrupted,
                      service, strip_panel_note, unwrap_pasted)
from .mail import LETTER_TOOL, peer_name, peer_pid, undelivered
from .notices import hook_call, system_notice
from .limits import MAX_NOTE, MAX_TEXT, cut
from .queue import delivered, withdrawn
from .tools import edited_path, tool_arg, tool_kind, tool_label


def service_once(text, at, pos, pending, read=False):
    """Returns items for a harness insert, and exactly once per text.

    A finished task is the exception: the terminal shows it where the session
    read the news, not where the news was queued, so the record that hands it
    to the session (read) draws it again there, and the feed keeps the later.
    """
    items = service(text, at, pos)
    if items is None:
        return None
    if pending is None:
        return items
    if pending.service_seen(text):
        return [i for i in items if i["role"] == "taskdone"] if read else []
    pending.service_remember(text)
    return items


SHELL_IN_RE = re.compile(r"\A<bash-input>(.*)</bash-input>\Z", re.S)
SHELL_OUT = (r"<bash-stdout>(.*)</bash-stdout>\s*<bash-stderr>(.*)</bash-stderr>"
             r"\s*(?:<bash-exit-code>(-?\d+)</bash-exit-code>)?")
SHELL_OUT_RE = re.compile(r"\A" + SHELL_OUT + r"\Z", re.S)
SHELL_RAN_RE = re.compile(r"\A<bash-input>(.*?)</bash-input>" + SHELL_OUT + r"\Z", re.S)


def shell(text, at, pos):
    """Returns the items for a command the human ran with "!", or None.

    A console writes the command and what it printed as two prompts of the
    human: the command as typed, the streams escaped for markup. A session on
    the stream gets both in one prompt the panel hands it when the command
    ends, and the exit code after the streams.
    """
    found = SHELL_IN_RE.match(text)
    if found:
        return [command_item(found.group(1), at, pos)]
    found = SHELL_OUT_RE.match(text)
    if found:
        return [output_item(found.group(1), found.group(2), found.group(3), at, pos)]
    found = SHELL_RAN_RE.match(text)
    if not found:
        return None
    ran = command_item(found.group(1), at, pos)
    out = output_item(found.group(2), found.group(3), found.group(4), at, pos)
    # The output names its command for the sheet that shows the whole of it.
    out["command"] = ran["text"]
    if "code" in out:
        ran["code"] = out["code"]
    return [ran, out]


def command_item(command, at, pos):
    body, trimmed = cut(command.strip(), MAX_TEXT)
    return {"role": "shell", "text": body, "cut": trimmed, "at": at, "pos": pos}


def output_item(stdout, stderr, code, at, pos):
    body, trimmed = cut(html.unescape(stdout).strip(), MAX_TEXT)
    err, err_trimmed = cut(html.unescape(stderr).strip(), MAX_TEXT)
    item = {"role": "shellout", "text": body, "cut": trimmed or err_trimmed,
            "at": at, "pos": pos}
    if err:
        item["err"] = err
    if code is not None:
        item["code"] = int(code)
    return item


# Marks of what became of a call, for the fold and never for the screen: the
# fold finds the call by its id among the rows it holds and changes it there.
# A result says the call is over and whether it failed; a cutoff says the turn
# the call was made in is over and no result is coming any more.
RESULT = "result"
CUTOFF = "cutoff"
MARKS = (RESULT, CUTOFF)


def shot_of(block, index):
    """Returns a picture of a record by its place, without its bytes, or None."""
    if not isinstance(block, dict) or block.get("type") != "image":
        return None
    src = block.get("source") or {}
    return {"index": index,
            "media": src.get("media_type") or "image/jpeg",
            "bytes": len(src.get("data") or "") * 3 // 4}


def result_mark(use, block, at, pos, index):
    """Returns the mark of a call whose result has come.

    A picture the call returned — a file it read, a page it took a shot of —
    rides on the mark to the call: the result is the record at pos, the
    picture is at part in the result that stands at index.
    """
    mark = {"role": RESULT, "use": use, "at": at, "pos": pos}
    if block.get("is_error"):
        mark["failed"] = True
    body = block.get("content")
    shots = []
    for part, piece in enumerate(body if isinstance(body, list) else []):
        shot = shot_of(piece, index)
        if shot:
            shots.append(dict(shot, pos=pos, part=part))
    if shots:
        mark["shots"] = shots
    return mark


def cutoff(calls, at, pos):
    """Returns a mark for every call the turn now over left without a result.

    A call is shown running until its result comes, and a turn can end before
    it: the person stops the answer, or the session dies in the middle of a
    call and the next prompt starts over. The entry stays, marked, so a result
    that does come later still settles the call and a card of permissions
    still names it; it is replaced rather than changed, because a throwaway
    read of a record still being written works on a shallow copy of calls.
    """
    if not calls:
        return []
    marks = []
    for use, call in list(calls.items()):
        if not use or call.get("cut"):
            continue
        calls[use] = dict(call, cut=True)
        marks.append({"role": CUTOFF, "use": use, "at": at, "pos": pos})
    return marks


def command_card(record, typed, at, pos, pending, unanswered, calls):
    """Returns the card of a local command the person typed.

    A command typed into a session that was busy is drawn by the queue as the
    person typed it, and the card stays there: the record of the command only
    says which record its answer will name as its parent. Unanswered keeps
    the cards still waiting for an answer, by that name.
    """
    drawn = pending.drawn(typed, waiting=False) if pending is not None else None
    if drawn is not None and drawn["role"] == "command":
        card, items = delivered(drawn), []
    else:
        card = {"role": "command", "text": typed, "at": at, "pos": pos}
        items = [card] + cutoff(calls, at, pos)
        if drawn is not None:
            # The queue drew it as a message: the card takes its place.
            card = dict(card, at=drawn["at"], pos=drawn["pos"])
            items[0] = dict(card, fixes=drawn["role"])
    if unanswered is not None and record.get("uuid"):
        unanswered[record["uuid"]] = card
    return items


def command_answer(record, text, at, pos, unanswered):
    """Returns the card of the command this record answers, with the answer on it, or None for no answer.

    The answer names the record of its command as its parent, and the card
    comes again in the place of the command with the answer on it. An answer
    whose command the feed never saw is a card of its own.
    """
    parent = record.get("parentUuid")
    if commands.grid(text):
        # The markdown after the grid names the grid as its parent.
        if unanswered is not None and parent in unanswered and record.get("uuid"):
            unanswered[record["uuid"]] = unanswered.pop(parent)
        return []
    said = commands.reply(record, text)
    if said is None:
        return None
    command = unanswered.pop(parent, None) if unanswered is not None and parent else None
    if command is not None:
        card = dict(command, **said)
    elif said.get("out") or said.get("err") or "data" in said:
        card = dict(said, at=at, pos=pos)
    else:
        return []
    card["done"] = True
    return [card]


# A brief is published by the panel's brief_publish tool, or by a shell call
# that hands a document to the collector. Whether a call published one is read
# from its answer and never from what it was given: the tool also checks a
# document without publishing it, and one command line may check a document
# and publish it too.


def parse(record, pos, pending=None, asks=None, sidechain=False, sent=None,
          briefs=None, shelf=None, calls=None, permits=None, unanswered=None):
    """Returns the feed items of one transcript record, from none to many.

    A row is found again by its place and its role: a later record draws a
    bubble the queue drew again in its place — delivered, taken back, found
    to be an alarm — and the fold and the screen put the new row where the
    old one stood. A record may also give several rows of one role beside
    one another — the letters of several agents at once, an answer of
    several blocks of text — and those are rows of their own: each after the
    first carries its number among them in nth, so none is taken for another
    drawn again. A row drawn again stands at the place of an earlier record
    and is never numbered: the queue draws one row of a role a record.
    """
    seen, out = {}, []
    for item in rows_of(record, pos, pending, asks, sidechain, sent, briefs, shelf,
                        calls, permits, unanswered):
        role = item.get("role")
        if role not in MARKS and item.get("pos") == pos:
            nth = seen.get(role, 0)
            seen[role] = nth + 1
            if nth:
                item = dict(item, nth=nth)
        out.append(item)
    return out


def rows_of(record, pos, pending=None, asks=None, sidechain=False, sent=None,
            briefs=None, shelf=None, calls=None, permits=None, unanswered=None):
    """Returns the feed items of one transcript record before parse() numbers them.

    Asks, sent and briefs are the calls of their kind still waiting for an
    answer, by call id: the card for a question round, for a delivery and for
    a published brief is drawn from the answer, and the answer is another
    record. Briefs keeps a call that asks for a secret as well, by what its
    card takes from the call. Shelf reads a published brief by its name, for
    what the card says about it. Calls are all the calls still waiting, for
    what a card of permissions says a call was about and for whether a call
    is still running: a reader that keeps them gets a call marked open and a
    mark when its result comes or its turn ends without one. Permits are the
    answers a person gave to permissions, by call, and the card stands by the
    result of the call. Unanswered is the cards of local commands still
    waiting for an answer, by the record the answer will name as its parent.
    """
    if not isinstance(record, dict) or (record.get("isSidechain") and not sidechain):
        return []

    if record.get("isMeta") and (record.get("sourceToolUseID") or record.get("turnCompanion")):
        return []

    kind = record.get("type")
    message = record.get("message") or {}
    at = record.get("timestamp") or ""

    if kind == "system":
        # claude may write the end of a turn a moment before the last result
        # of it: the result that follows still settles its call.
        ended = cutoff(calls, at, pos) if record.get("subtype") in TURN_ENDS else []
        said = system_notice(record, at, pos)
        if said is not None:
            return said + ended
        if record.get("subtype") != "local_command":
            return ended
        text = (record.get("content") or "").strip()
        typed = commands.recorded(text)
        if typed:
            return command_card(record, typed, at, pos, pending, unanswered, calls)
        answer = command_answer(record, text, at, pos, unanswered)
        if answer is not None:
            return answer
        role, shown = classify(text)
        if role == "note":
            return [{"role": "note", "text": shown, "at": at, "pos": pos}]
        if role != "me" or not shown:
            return []
        return [{"role": "me", "text": shown, "at": at, "pos": pos}]

    if kind == "queue-operation":
        operation = record.get("operation")
        if operation == "popAll":
            # Esc in a terminal pulls the queue back into the composer: nothing
            # of it was read, and what goes out after is a message of its own.
            if pending is None:
                return []
            text = strip_panel_note(unwrap_pasted((record.get("content") or "").strip()))
            item = pending.by_text(text) if text else None
            return [withdrawn(i) for i in ([item] if item else pending.drain())]
        if operation != "enqueue":
            if pending is None:
                return []
            text = strip_panel_note(unwrap_pasted((record.get("content") or "").strip()))
            item = pending.by_text(text) if text else pending.head()
            return [delivered(item)] if item else []

        text = strip_panel_note(unwrap_pasted((record.get("content") or "").strip()))
        if not text:
            return []
        # The output of a command run on the stream is drawn where it is
        # queued: the command is over, and claude may read it long after, in
        # the middle of an answer. It stays in the queue all the same, so the
        # word that the queue handed it over finds it there.
        ran = shell(text, at, pos)
        if ran and len(ran) == 2:
            if pending is not None:
                pending.remember(text, pos)
                pending.enqueue(ran[0])
            return ran
        service_items = service_once(text, at, pos, pending)
        if service_items is not None:
            return service_items
        if pending is not None and pending.is_wake(text):
            pending.remember(text, pos, wake=True)
            return [wake_item(text, at, pos)]
        if pending is not None:
            pending.remember(text, pos)
        typed = commands.typed(text)
        if typed:
            # A command waits for the turn to end, and its card says so.
            item = {"role": "command", "text": typed, "at": at, "pos": pos, "state": "queued"}
            if pending is not None:
                pending.enqueue(item)
            return [item]
        role, shown = classify(text)
        if role == "skip":
            return []
        if role == "note":
            return [{"role": "note", "text": shown, "at": at, "pos": pos}]
        body, trimmed = cut(shown, MAX_TEXT)
        item = {"role": "me", "text": body, "cut": trimmed, "at": at,
                "pos": pos, "state": "queued"}
        if pending is not None:
            pending.enqueue(item)
        return [item]

    if kind == "user":
        content = message.get("content")
        out = []
        shots = []
        if isinstance(content, list):
            if any(isinstance(b, dict) and b.get("type") == "tool_result" for b in content):
                links = []
                allowed = []
                settled = []
                for i, b in enumerate(content):
                    if not isinstance(b, dict) or b.get("type") != "tool_result":
                        continue
                    use = b.get("tool_use_id") or ""
                    call = calls.pop(use, None) if calls is not None else None
                    if use and call is not None:
                        mark = result_mark(use, b, at, pos, i)
                        if call.get("letter"):
                            lost = undelivered(sesstate.result_text(b), b.get("is_error"))
                            if lost:
                                mark["undelivered"] = lost
                        settled.append(mark)
                    if permits and use in permits:
                        allowed.append(permit_row(permits[use], call))
                    if asks is not None and use in asks:
                        card = ask_round(asks.pop(use), record.get("toolUseResult"), use, at, pos)
                        if card:
                            links.append(card)
                        continue
                    if sent is not None and use in sent:
                        sent.discard(use)
                        card = sent_card(record.get("toolUseResult"), use, at, pos)
                        if card:
                            links.append(card)
                        continue
                    if briefs is not None and use in briefs:
                        by = briefs.pop(use)
                        if isinstance(by, dict):
                            card = secret_card(sesstate.result_text(b), by, use, at, pos)
                        else:
                            card = brief_card(sesstate.result_text(b), by, shelf, use, at, pos)
                        if card:
                            links.append(card)
                            continue
                    found = sesstate.ARTIFACT_URL_RE.search(sesstate.result_text(b))
                    if found:
                        links.append({"role": "artifactlink", "use": b.get("tool_use_id") or "",
                                      "url": found.group(1), "at": at, "pos": pos})
                if allowed:
                    # The person answered before the call ran, so the answer
                    # stands before whatever its result brought.
                    links.insert(0, permit_card(allowed, at, pos))
                # The marks go last: a card that takes the place of its call
                # takes the call out first, and the mark then finds nothing.
                return links + settled
            shots = [shot for shot in (shot_of(b, i) for i, b in enumerate(content)) if shot]
            if shots:
                out.append({"role": "shots", "at": at, "pos": pos, "shots": shots})
            text = "\n".join(b.get("text", "") for b in content
                             if isinstance(b, dict) and b.get("type") == "text")
        else:
            text = content if isinstance(content, str) else ""
        text = unwrap_pasted(text.strip())
        letter = coordinator_letter(record.get("origin"), text, at, pos) if text else None
        if letter is not None:
            return out + letter
        if not text or "system-reminder" in text[:200]:
            return out
        # Only a record the harness wrote can be an answer: a person pasting
        # the same markdown into a message wrote a message.
        if record.get("isMeta") or text.startswith("<local-command-std"):
            answer = command_answer(record, text, at, pos, unanswered)
            if answer is not None:
                return out + answer
        typed = commands.recorded(text)
        if typed:
            return out + command_card(record, typed, at, pos, pending, unanswered, calls)
        ran = shell(text, at, pos)
        if ran:
            # The prompt the queue drew is already in the feed.
            if len(ran) == 2 and record.get("promptSource") in ("queued", "sdk") \
                    and pending is not None and pending.seen(text):
                pending.by_text(text)
                return out
            return out + ran
        if sesstate.is_wakeup(record):
            if pending is not None and pending.shown_as_wake(text):
                return out
            drawn = pending.drawn(text) if pending is not None else None
            if drawn is None:
                if pending is not None:
                    pending.remember(text, pos, wake=True)
                return out + [wake_item(text, at, pos)]
            item = wake_item(text, at, drawn["pos"])
            item["fixes"] = "me"
            return out + [item]

        service_items = service_once(text, at, pos, pending, read=True)
        if service_items is not None:
            return out + service_items
        role, shown = classify(text)
        if role == "skip":
            return out
        if role == "note":
            stop = interrupted(text)
            if stop and sidechain:
                shown = AGENT_STOPPED
            out.append({"role": "note", "text": shown, "at": at, "pos": pos})
            if stop:
                out += cutoff(calls, at, pos)
            return out
        body, trimmed = cut(shown, MAX_TEXT)
        # A command the model answers — a skill — comes back from the queue as
        # a record of the command, with no mark of the queue on it. The queue
        # drew it as a command, and it is a prompt of the person: the bubble
        # takes the place of the card.
        drawn = pending.drawn(commands.typed(shown) or shown, waiting=False) \
            if pending is not None and text.startswith("<command-message>") else None
        if drawn is not None:
            return out + [{"role": "me", "text": body, "cut": trimmed, "at": drawn["at"],
                           "pos": drawn["pos"], "fixes": drawn["role"]}] + cutoff(calls, at, pos)
        # A message that went through the queue comes back as a prompt of its
        # own: marked queued in a terminal, and sdk on the stream, where every
        # message goes through the queue. Its bubble is the one the queue drew.
        # A prompt of the person starts a turn, so the one before it is over.
        if record.get("promptSource") in ("queued", "sdk") and pending is not None and pending.seen(text):
            item = pending.by_text(text)
            return ([delivered(item)] if item else []) + cutoff(calls, at, pos)
        if pending is not None:
            pending.remember(text, pos)
        if (record.get("origin") or {}).get("kind") == "peer":
            return out
        # claude wrote this prompt itself — to go on once the limit reset,
        # after a resume — and a prompt of claude is a line, never a bubble of
        # the person. The queue draws it as one before the record says whose it
        # is, and the line then takes that bubble's place.
        if record.get("isMeta"):
            said, trimmed = cut(shown, MAX_NOTE)
            drawn = pending.drawn(text) if pending is not None else None
            item = {"role": "note", "text": said + "…" if trimmed else said, "at": at,
                    "pos": drawn["pos"] if drawn else pos}
            if drawn:
                item["fixes"] = "me"
            return out + [item] + cutoff(calls, at, pos)
        out.append({"role": "me", "text": body, "cut": trimmed, "at": at, "pos": pos})
        return out + cutoff(calls, at, pos)

    if kind == "attachment":
        block = record.get("attachment") or {}
        if block.get("type") != "queued_command":
            return hook_call(block, at, pos)
        prompt = block.get("prompt")
        out = []
        shots = []
        if isinstance(prompt, list):
            shots = [shot for shot in (shot_of(b, i) for i, b in enumerate(prompt)) if shot]
            text = "\n".join(b.get("text", "") for b in prompt
                              if isinstance(b, dict) and b.get("type") == "text")
        else:
            text = prompt if isinstance(prompt, str) else ""
        text = strip_panel_note(unwrap_pasted(text.strip()))
        if not text and not shots:
            return []
        if shots:
            out.append({"role": "shots", "at": at, "pos": pos, "shots": shots})
        letter = coordinator_letter(block.get("origin"), text, at, pos) if text else None
        if letter is not None:
            return out + letter
        service_items = service_once(text, at, pos, pending, read=True)
        if service_items is not None:
            return out + service_items
        if pending is not None and pending.seen(text):
            return []
        if pending is not None:
            pending.remember(text, pos)
        role, shown = classify(text)
        if role == "skip":
            return []
        if role == "note":
            return [{"role": "note", "text": shown, "at": at, "pos": pos}]
        body, trimmed = cut(shown, MAX_TEXT)
        item = {"role": "me", "text": body, "cut": trimmed, "at": at, "pos": pos,
                "state": "queued"}
        if pending is not None:
            pending.enqueue(item)
        out.append(item)
        return out

    if kind == "assistant":
        out = []
        if pending is not None:
            out.extend(delivered(item) for item in pending.read())
        silent = 0
        said = []
        for b in message.get("content", []):
            if not isinstance(b, dict) or b.get("type") != "thinking":
                continue
            text = (b.get("thinking") or "").strip()
            if text:
                said.append(text)
            else:
                silent += 1
        if said:
            body, trimmed = cut("\n\n".join(said), MAX_TEXT)
            out.append({"role": "mind", "text": body, "cut": trimmed,
                        "at": at, "pos": pos})
        if silent:
            usage = message.get("usage") or {}
            details = usage.get("output_tokens_details") or {}
            out.append({"role": "think", "count": silent,
                        "tokens": int(details.get("thinking_tokens") or 0),
                        "at": at, "pos": pos})

        for i, block in enumerate(message.get("content", [])):
            if not isinstance(block, dict):
                continue
            if block.get("type") == "text":
                text = (block.get("text") or "").strip()
                if not text:
                    continue
                body, trimmed = cut(text, MAX_TEXT)
                out.append({"role": "ai", "text": body, "cut": trimmed, "at": at, "pos": pos})
            elif block.get("type") == "tool_use":
                name = block.get("name") or "?"
                if calls is not None:
                    calls[block.get("id") or ""] = {"tool": tool_label(name, block.get("input")),
                                                    "subject": tool_arg(block.get("input"), name)}
                # An alarm comes back through the queue in its own words, and
                # the queue draws it as an alarm rather than as the person.
                if name in ("ScheduleWakeup", "CronCreate") and pending is not None:
                    data = block.get("input")
                    if isinstance(data, dict):
                        pending.schedule(data.get("prompt"))
                if name == "AskUserQuestion" and asks is not None:
                    data = block.get("input")
                    round_qs = data.get("questions") if isinstance(data, dict) else None
                    if isinstance(round_qs, list) and round_qs:
                        asks[block.get("id") or ""] = data
                        continue
                if name == "Artifact":
                    card = artifact_card(block.get("input"), block.get("id"), at, pos)
                    if card:
                        out.append(card)
                        continue
                if name in ("Bash", BRIEF_TOOL) and briefs is not None:
                    # The call stays in the run as a call: whether a document
                    # reached the shelf is known only from its answer.
                    briefs[block.get("id") or ""] = "tool" if name == BRIEF_TOOL else "shell"
                if name == SECRET_TOOL and briefs is not None:
                    # The call stays in the run as a call: whether anything
                    # was asked is known only from its answer, and the card
                    # takes the title and the notepad from the call.
                    briefs[block.get("id") or ""] = secret_call(block.get("input"))
                if name == sesstate.SENT_TOOL and sent is not None:
                    # The call goes into the run as a call: whether anything
                    # reached the human is known only from the answer, and
                    # the window takes the call out once its card is drawn.
                    sent.add(block.get("id") or "")
                if name in ("SendMessage", LETTER_TOOL):
                    data = block.get("input") or {}
                    said = data.get("message") or data.get("content") or data.get("text") or ""
                    body, trimmed = cut(str(said).strip(), MAX_TEXT)
                    # The panel's tool asked for the list of sessions sends
                    # nothing, and stays a call.
                    if body:
                        to = str(data.get("to") or data.get("recipient") or "")
                        letter = {"role": "mail", "dir": "out",
                                  "from": peer_name(to),
                                  "source": "session" if name == LETTER_TOOL or peer_pid(to) else "agent",
                                  "text": body, "cut": trimmed,
                                  "use": block.get("id") or "",
                                  "at": at, "pos": pos}
                        # Whether the letter reached anyone is known only
                        # from the answer: the letter waits for it as a call
                        # does, and the fold marks it when it comes.
                        if calls is not None and letter["use"]:
                            calls[letter["use"]]["letter"] = True
                            letter["open"] = True
                        out.append(letter)
                        continue
                call = {"role": "tool", "name": tool_label(name, block.get("input")),
                        "kind": tool_kind(name),
                        "arg": tool_arg(block.get("input"), name), "at": at,
                        "use": block.get("id") or "",
                        "pos": pos, "index": i}
                if calls is not None and call["use"]:
                    call["open"] = True
                edited = edited_path(name, block.get("input"))
                if edited:
                    call["edited"] = edited
                out.append(call)
        return out

    return []
