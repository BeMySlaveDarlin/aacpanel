"""Reading by address: an attachment and a whole tool call."""
import json

from .limits import MAX_ARGS, MAX_RESULT, cut


def blocks_of(record):
    """Returns the blocks of a prompt record."""
    content = (record.get("message") or {}).get("content")
    if isinstance(content, list):
        return content
    prompt = (record.get("attachment") or {}).get("prompt")
    return prompt if isinstance(prompt, list) else []


def image(path, pos, index):
    """Returns one image from the transcript as its media type and base64 bytes."""
    with open(path, "rb") as f:
        f.seek(pos)
        raw = f.readline()
    try:
        record = json.loads(raw.decode("utf-8", "replace"))
    except ValueError:
        return None
    content = blocks_of(record)
    if not (0 <= index < len(content)):
        return None
    block = content[index]
    if not isinstance(block, dict) or block.get("type") != "image":
        return None
    src = block.get("source") or {}
    data = src.get("data")
    if not isinstance(data, str) or not data:
        return None
    return {"media": src.get("media_type") or "image/jpeg", "data": data}


def tool_result(f, tool_id, limit=64):
    """Returns what the call returned: text, failure mark and answer time."""
    for _ in range(limit):
        raw = f.readline()
        if not raw:
            return None
        try:
            record = json.loads(raw.decode("utf-8", "replace"))
        except ValueError:
            continue
        if record.get("type") != "user":
            continue
        for block in ((record.get("message") or {}).get("content")) or []:
            if not isinstance(block, dict) or block.get("type") != "tool_result":
                continue
            if block.get("tool_use_id") != tool_id:
                continue
            body = block.get("content")
            if isinstance(body, list):
                parts = []
                for piece in body:
                    if not isinstance(piece, dict):
                        continue
                    kind = piece.get("type")
                    if kind == "text":
                        parts.append(piece.get("text") or "")
                    elif kind == "image":
                        parts.append("[image]")
                    elif kind == "tool_reference":
                        parts.append(f"[tool {piece.get('tool_name') or '?'}]")
                body = "\n".join(p for p in parts if p)
            if not isinstance(body, str):
                body = json.dumps(body, ensure_ascii=False)
            text, trimmed = cut(body, MAX_RESULT)
            return {"result": text, "resultCut": trimmed,
                    "failed": bool(block.get("is_error")),
                    "resultAt": record.get("timestamp") or ""}
    return None


def call(path, pos, index):
    """Returns one whole tool call: what it was called with and what came out."""
    with open(path, "rb") as f:
        f.seek(pos)
        raw = f.readline()
        try:
            record = json.loads(raw.decode("utf-8", "replace"))
        except ValueError:
            return None
        content = ((record.get("message") or {}).get("content")) or []
        if not isinstance(content, list) or not (0 <= index < len(content)):
            return None
        block = content[index]
        if not isinstance(block, dict):
            return None
        if block.get("type") != "tool_use":
            return None
        name = block.get("name") or "?"
        shown = json.dumps(block.get("input"), ensure_ascii=False, indent=2,
                           sort_keys=True) if block.get("input") is not None else ""
        args, args_cut = cut(shown, MAX_ARGS)
        out = {"tool": name, "args": args, "argsCut": args_cut,
               "at": record.get("timestamp") or "",
               "result": "", "resultCut": False, "failed": False, "resultAt": ""}
        found = tool_result(f, block.get("id"))
    if found is None:
        out["pending"] = True
        return out
    out.update(found)
    return out
