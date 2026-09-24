"""Transcript record to feed items."""
import html
import re

import sesstate

from .cards import artifact_card, ask_round, brief_card, sent_card, wake_item
from .harness import classify, service, strip_panel_note, unwrap_pasted
from .mail import peer_name, peer_pid
from .limits import MAX_TEXT, cut
from .queue import delivered
from .tools import edited_path, tool_arg, tool_kind, tool_label


def service_once(text, at, pos, pending):
    """Returns items for a harness insert, and exactly once per text."""
    items = service(text, at, pos)
    if items is None:
        return None
    if pending is None:
        return items
    if pending.service_seen(text):
        return []
    pending.service_remember(text)
    return items


SHELL_IN_RE = re.compile(r"\A<bash-input>(.*)</bash-input>\Z", re.S)
SHELL_OUT_RE = re.compile(
    r"\A<bash-stdout>(.*)</bash-stdout>\s*<bash-stderr>(.*)</bash-stderr>\Z", re.S)


def shell(text, at, pos):
    """Returns the item for a command the human ran from the console, or None.

    The console writes the command and what it printed as two prompts of the
    human: the command as typed, the streams escaped for markup.
    """
    found = SHELL_IN_RE.match(text)
    if found:
        body, trimmed = cut(found.group(1).strip(), MAX_TEXT)
        return {"role": "shell", "text": body, "cut": trimmed, "at": at, "pos": pos}
    found = SHELL_OUT_RE.match(text)
    if not found:
        return None
    body, trimmed = cut(html.unescape(found.group(1)).strip(), MAX_TEXT)
    err, err_trimmed = cut(html.unescape(found.group(2)).strip(), MAX_TEXT)
    item = {"role": "shellout", "text": body, "cut": trimmed or err_trimmed,
            "at": at, "pos": pos}
    if err:
        item["err"] = err
    return item


# A brief is published by running a script, not by a tool of its own: the
# document is a file of tens of kilobytes, and that does not go on a command
# line. Which shell call did it is read from what the call printed and never
# from the command, because one command does several things — a session checks
# the document and publishes it in the same line, and both are shell.


def parse(record, pos, pending=None, asks=None, sidechain=False, sent=None,
          briefs=None, shelf=None):
    """Returns the feed items of one transcript record, from none to many.

    Asks, sent and briefs are the calls of their kind still waiting for an
    answer, by call id: the card for a question round, for a delivery and for
    a published brief is drawn from the answer, and the answer is another
    record. Shelf reads a published brief by its name, for what the card says
    about it.
    """
    if not isinstance(record, dict) or (record.get("isSidechain") and not sidechain):
        return []

    if record.get("isMeta") and (record.get("sourceToolUseID") or record.get("turnCompanion")):
        return []

    kind = record.get("type")
    message = record.get("message") or {}
    at = record.get("timestamp") or ""

    if kind == "system":
        if record.get("subtype") != "local_command":
            return []
        role, shown = classify((record.get("content") or "").strip())
        if role == "note":
            return [{"role": "note", "text": shown, "at": at, "pos": pos}]
        if role != "me" or not shown:
            return []
        return [{"role": "me", "text": shown, "at": at, "pos": pos}]

    if kind == "queue-operation":
        operation = record.get("operation")
        if operation != "enqueue":
            if pending is None:
                return []
            text = strip_panel_note(unwrap_pasted((record.get("content") or "").strip()))
            item = pending.by_text(text) if text else pending.head()
            return [delivered(item)] if item else []

        text = strip_panel_note(unwrap_pasted((record.get("content") or "").strip()))
        if not text:
            return []
        service_items = service_once(text, at, pos, pending)
        if service_items is not None:
            return service_items
        if pending is not None and pending.is_wake(text):
            pending.remember(text, pos, wake=True)
            return [wake_item(text, at, pos)]
        if pending is not None:
            pending.remember(text, pos)
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
                for b in content:
                    if not isinstance(b, dict) or b.get("type") != "tool_result":
                        continue
                    use = b.get("tool_use_id") or ""
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
                        briefs.discard(use)
                        card = brief_card(sesstate.result_text(b), shelf, use, at, pos)
                        if card:
                            links.append(card)
                            continue
                    found = sesstate.ARTIFACT_URL_RE.search(sesstate.result_text(b))
                    if found:
                        links.append({"role": "artifactlink", "use": b.get("tool_use_id") or "",
                                      "url": found.group(1), "at": at, "pos": pos})
                return links
            for i, b in enumerate(content):
                if not isinstance(b, dict) or b.get("type") != "image":
                    continue
                src = b.get("source") or {}
                shots.append({"index": i,
                              "media": src.get("media_type") or "image/jpeg",
                              "bytes": len(src.get("data") or "") * 3 // 4})
            if shots:
                out.append({"role": "shots", "at": at, "pos": pos, "shots": shots})
            text = "\n".join(b.get("text", "") for b in content
                             if isinstance(b, dict) and b.get("type") == "text")
        else:
            text = content if isinstance(content, str) else ""
        text = unwrap_pasted(text.strip())
        if not text or "system-reminder" in text[:200]:
            return out
        ran = shell(text, at, pos)
        if ran:
            return out + [ran]
        if sesstate.is_wakeup(record):
            if pending is not None and pending.shown_as_wake(text):
                return out
            shown = pending.shown_at(text) if pending is not None else None
            if shown is None:
                if pending is not None:
                    pending.remember(text, pos, wake=True)
                return out + [wake_item(text, at, pos)]
            item = wake_item(text, at, shown)
            item["fixes"] = "me"
            return out + [item]

        service_items = service_once(text, at, pos, pending)
        if service_items is not None:
            return out + service_items
        role, shown = classify(text)
        if role == "skip":
            return out
        if role == "note":
            out.append({"role": "note", "text": shown, "at": at, "pos": pos})
            return out
        body, trimmed = cut(shown, MAX_TEXT)
        # A message that went through the queue comes back as a prompt of its
        # own: marked queued in a terminal, and sdk on the stream, where every
        # message goes through the queue. Its bubble is the one the queue drew.
        if record.get("promptSource") in ("queued", "sdk") and pending is not None and pending.seen(text):
            item = pending.by_text(text)
            return [delivered(item)] if item else []
        if pending is not None:
            pending.remember(text, pos)
        if (record.get("origin") or {}).get("kind") == "peer":
            return out
        out.append({"role": "me", "text": body, "cut": trimmed, "at": at, "pos": pos})
        return out

    if kind == "attachment":
        block = record.get("attachment") or {}
        if block.get("type") != "queued_command":
            return []
        prompt = block.get("prompt")
        out = []
        shots = []
        if isinstance(prompt, list):
            for i, b in enumerate(prompt):
                if not isinstance(b, dict) or b.get("type") != "image":
                    continue
                src = b.get("source") or {}
                shots.append({"index": i,
                              "media": src.get("media_type") or "image/jpeg",
                              "bytes": len(src.get("data") or "") * 3 // 4})
            text = "\n".join(b.get("text", "") for b in prompt
                              if isinstance(b, dict) and b.get("type") == "text")
        else:
            text = prompt if isinstance(prompt, str) else ""
        text = strip_panel_note(unwrap_pasted(text.strip()))
        if not text and not shots:
            return []
        if shots:
            out.append({"role": "shots", "at": at, "pos": pos, "shots": shots})
        service_items = service_once(text, at, pos, pending)
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
            out.extend(delivered(item) for item in pending.drain())
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
                if name == "ScheduleWakeup" and pending is not None:
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
                if name == "Bash" and briefs is not None:
                    # The call stays in the run as a call: whether a document
                    # reached the shelf is known only from what it printed.
                    briefs.add(block.get("id") or "")
                if name == sesstate.SENT_TOOL and sent is not None:
                    # The call goes into the run as a call: whether anything
                    # reached the human is known only from the answer, and
                    # the window takes the call out once its card is drawn.
                    sent.add(block.get("id") or "")
                if name == "SendMessage":
                    data = block.get("input") or {}
                    said = data.get("message") or data.get("content") or ""
                    body, trimmed = cut(str(said).strip(), MAX_TEXT)
                    if body:
                        to = str(data.get("to") or data.get("recipient") or "")
                        out.append({"role": "mail", "dir": "out",
                                    "from": peer_name(to),
                                    "source": "session" if peer_pid(to) else "agent",
                                    "text": body, "cut": trimmed,
                                    "at": at, "pos": pos})
                        continue
                call = {"role": "tool", "name": tool_label(name, block.get("input")),
                        "kind": tool_kind(name),
                        "arg": tool_arg(block.get("input")), "at": at,
                        "use": block.get("id") or "",
                        "pos": pos, "index": i}
                edited = edited_path(name, block.get("input"))
                if edited:
                    call["edited"] = edited
                out.append(call)
        return out

    return []
