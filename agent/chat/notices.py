"""What claude and its hooks tell the person beside the conversation.

A terminal prints these as lines of their own — a warning of claude, the
recap after an absence, the length of a turn, a hook's message — and the
transcript keeps them as records nobody wrote. A blocking error of a hook is
not among them: claude hands it back as a prompt, and the feed shows that as
a letter from the hook.
"""
from .limits import MAX_TEXT, cut
from .tools import one_line

# What claude writes when the model turned a message down and says no more.
REFUSED = "the model refused to answer this message"


def notice(text, source, level, at, pos):
    """Returns a line of the feed: what was said, by whom, and how loud."""
    body, trimmed = cut(str(text or "").strip(), MAX_TEXT)
    if not body:
        return []
    item = {"role": "notice", "text": body, "level": level, "at": at, "pos": pos}
    if source:
        item["from"] = source
    if trimmed:
        item["cut"] = True
    return [item]


def system_notice(record, at, pos):
    """Returns the lines of a system record the terminal shows, or None for one it does not."""
    subtype = record.get("subtype")
    content = record.get("content") if isinstance(record.get("content"), str) else ""
    if subtype == "turn_duration":
        ms = record.get("durationMs")
        if isinstance(ms, bool) or not isinstance(ms, (int, float)) or ms <= 0:
            return []
        return [{"role": "turn", "ms": int(ms), "at": at, "pos": pos}]
    if subtype == "away_summary":
        return notice(content, "while you were away", "info", at, pos)
    if subtype == "informational":
        return notice(content, "", "warn" if record.get("level") == "warning" else "info", at, pos)
    if subtype in ("model_refusal_fallback", "model_refusal_no_fallback"):
        return notice(content or REFUSED, "safeguards", "crit", at, pos)
    return None


def hook_said(block):
    """Returns what a hook told the person as (hook, text, failed), or None for any other attachment."""
    kind = block.get("type") if isinstance(block, dict) else None
    hook = str(block.get("hookName") or "").strip() if kind else ""
    who = f"{hook} hook" if hook else "hook"
    if kind == "hook_system_message":
        text = str(block.get("content") or "").strip()
        return (who, text, False) if text else None
    if kind == "hook_non_blocking_error":
        return who, str(block.get("stderr") or "").strip() or "the hook failed", True
    if kind == "hook_cancelled":
        return who, "the hook was cancelled", True
    return None


def hook_call(block, at, pos):
    """Returns what a hook said as a call of the run it arrived in.

    A hook's message rarely means much on its own, and a busy harness says
    one after every call: as a line each it would split every run it lands
    in. As a call it adds to a badge, and the text is a tap away.
    """
    said = hook_said(block)
    if not said:
        return []
    who, text, _ = said
    return [{"role": "tool", "name": who, "kind": "hook", "arg": one_line(text),
             "at": at, "use": "", "pos": pos, "index": 0}]
