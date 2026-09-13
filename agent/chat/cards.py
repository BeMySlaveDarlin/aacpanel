"""Feed cards: artifact, files sent to the human, answered question round, alarm firing."""
import sesstate

from .limits import MAX_TEXT, cut


def artifact_card(data, use, at, pos):
    """Returns an artifact card built from call arguments, or None when it is not a publish."""
    fields = sesstate.artifact_fields(data)
    if not fields:
        return None
    card = {"role": "artifact", "use": use or "",
            "title": fields.get("title") or fields["file"],
            "file": fields["file"], "at": at, "pos": pos}
    for field in ("desc", "label", "note", "icon"):
        if fields.get(field):
            card[field] = fields[field]
    return card


def sent_card(result, use, at, pos):
    """Returns a card for the files a call delivered to the human, or None when none went.

    The card is built from the answer, not from the call: the call names what
    it wants sent, and only the answer says what reached the human. A call
    that failed answers with an error and no attachments — it stays a plain
    call in the run, with the error readable in its details, and gives no card.
    """
    files = [{"path": f["path"], "name": f["file"],
              **{k: f[k] for k in ("size", "media") if k in f}}
             for f in sesstate.sent_files(result)]
    if not files:
        return None
    body, trimmed = cut(str(result.get("caption") or "").strip(), MAX_TEXT)
    card = {"role": "sent", "use": use or "", "files": files, "at": at, "pos": pos}
    if body:
        card["text"] = body
        card["cut"] = trimmed
    return card


def mark_outside(items, cwd):
    """Marks in every card of sent files the ones the reader cannot open.

    The reader opens a file only inside the directory of the conversation,
    and a session may send one from anywhere — its scratchpad, say. Such a
    file did reach the human, so its card stays; but a tap on it would end
    in a refusal, and the row has to say so instead of promising an opening.
    """
    for item in items:
        if item.get("role") != "sent":
            continue
        for entry in item.get("files") or []:
            if sesstate.inside(entry.get("path"), cwd) is None:
                entry["outside"] = True
    return items


MAX_ASK_QUESTIONS = 8
MAX_ASK_TEXT = 400
MAX_ASK_ANSWERS = 12

ASK_REJECTED = ("reject", "doesn't want to proceed")


def ask_round(data, result, use, at, pos):
    """Returns a card for an answered question round: what was asked and what was picked."""
    questions = data.get("questions") if isinstance(data, dict) else None
    if not isinstance(questions, list) or not questions:
        return None

    status = ""
    answers = {}
    if isinstance(result, dict):
        if result.get("afkTimeoutMs") is not None:
            status = "afk"
        got = result.get("answers")
        if isinstance(got, dict):
            answers = got
    else:
        text = str(result or "")
        status = "rejected" if any(mark in text for mark in ASK_REJECTED) else "failed"

    rows = []
    for item in questions[:MAX_ASK_QUESTIONS]:
        if not isinstance(item, dict):
            continue
        text = cut(" ".join(str(item.get("question") or "").split()), MAX_ASK_TEXT)[0]
        if not text:
            continue
        row = {"text": text}
        header = cut(" ".join(str(item.get("header") or "").split()), 60)[0]
        if header:
            row["header"] = header
        picked = answers.get(item.get("question"))
        if not isinstance(picked, list):
            picked = [picked] if picked else []
        answer = [cut(" ".join(str(one).split()), MAX_ASK_TEXT)[0] for one in picked[:MAX_ASK_ANSWERS]]
        answer = [one for one in answer if one]
        if answer:
            row["answer"] = answer
        rows.append(row)
    if not rows:
        return None

    card = {"role": "asked", "use": use or "", "asked": rows, "at": at, "pos": pos}
    if status:
        card["status"] = status
    return card


def wake_item(text, at, pos):
    """Returns a feed item for an alarm firing."""
    body, trimmed = cut(text, MAX_TEXT)
    return {"role": "wake", "text": body, "cut": trimmed, "at": at, "pos": pos}
