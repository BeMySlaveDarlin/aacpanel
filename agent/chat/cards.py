"""Feed cards: artifact, answered question round, alarm firing."""
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
