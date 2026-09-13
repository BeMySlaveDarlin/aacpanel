"""Feed window: one pass over the file, folding calls into badges."""
import json

from .cards import mark_outside
from .disk import attach_files
from .limits import DEFAULT_LIMIT, MAX_LIMIT
from .queue import Pending
from .records import parse


def feed(path, limit=DEFAULT_LIMIT, before=None, after=None, sidechain=False):
    """Returns a feed window: the tail, a piece before a position or a piece after it."""
    limit = max(1, min(int(limit or DEFAULT_LIMIT), MAX_LIMIT))
    window = []
    total = 0
    cwd = ""
    more_before = False
    pos = 0
    last_pos = None
    pending = Pending()
    asks = {}
    sent = set()

    def place(item):
        for i, was in enumerate(window):
            if was["pos"] != item["pos"]:
                continue
            if was["role"] == item["role"] or was["role"] == item.get("fixes"):
                window[i] = dict(item)
                window[i].pop("fixes", None)
                return True
        return False

    def drop_call(use):
        # A delivery is shown once. Whether anything reached the human is
        # known only from the answer, so the call goes into the run first and
        # leaves it when its card is drawn; a call that failed gets no card
        # and stays, with the error readable in its details.
        nonlocal total
        if not use:
            return
        for i, was in enumerate(window):
            if was["role"] != "tools":
                continue
            calls = [c for c in was["calls"] if c["use"] != use]
            if len(calls) == len(was["calls"]):
                continue
            if calls:
                was["calls"] = calls
            else:
                del window[i]
                total -= 1
            return

    def keep(item):
        nonlocal total
        if place(item):
            return
        if item["role"] == "sent":
            drop_call(item["use"])
        if item["role"] == "taskdone":
            if any(was["role"] == "taskdone" and was["use"] == item["use"] for was in window):
                return
        if item["role"] == "mail":
            if any(was["role"] == "mail" and was["from"] == item["from"]
                   and was["text"] == item["text"] for was in window):
                return
        if item["role"] == "think":
            tail = run_tail()
            run = tail[0]["run"] if tail else item["pos"]
            spot = {"seq": next_seq(tail), "at": item.get("at", ""),
                    "tokens": item.get("tokens", 0), "pos": item["pos"],
                    "index": 0}
            for group in tail:
                if group["role"] == "think":
                    group["count"] += item["count"]
                    group["tokens"] += item.get("tokens", 0)
                    group["spots"].append(spot)
                    return
            item = dict(item, run=run, spots=[spot])
        elif item["role"] == "tool":
            tail = run_tail()
            run = tail[0]["run"] if tail else item["pos"]
            seq = next_seq(tail)
            call = {"name": item["name"], "arg": item.get("arg", ""), "seq": seq,
                    "at": item.get("at", ""), "pos": item["pos"], "index": item["index"],
                    "use": item.get("use", "")}
            if item.get("edited"):
                call["edited"] = item["edited"]
            kind = item.get("kind", "other")
            for group in tail:
                if group["role"] == "tools" and group["kind"] == kind:
                    group["calls"].append(call)
                    return
            item = {"role": "tools", "kind": kind, "run": run, "pos": item["pos"],
                    "at": item.get("at", ""), "calls": [call]}
        window.append(item)
        total += 1

    def next_seq(tail):
        used = [c["seq"] for group in tail if group["role"] == "tools" for c in group["calls"]]
        used += [spot["seq"] for group in tail if group["role"] == "think" for spot in group["spots"]]
        return max(used, default=-1) + 1

    def run_tail():
        tail = []
        for was in reversed(window):
            if was["role"] in ("taskdone", "artifactlink"):
                continue
            if was["role"] not in ("tools", "think"):
                break
            tail.append(was)
        tail.reverse()
        return tail

    with open(path, "rb") as f:
        for raw in f:
            line_pos = pos
            pos += len(raw)
            try:
                record = json.loads(raw.decode("utf-8", "replace"))
            except ValueError:
                continue
            if not cwd and isinstance(record.get("cwd"), str):
                cwd = record["cwd"]
            items = parse(record, line_pos, pending, asks, sidechain, sent)
            if not items:
                continue

            if after is not None:
                if line_pos <= after:
                    continue
                for item in items:
                    keep(item)
                last_pos = line_pos
                if len(window) >= limit:
                    break
                continue

            if before is not None and line_pos >= before:
                if not any(item.get("state") == "queued" for item in window):
                    break
                for item in items:
                    if item["pos"] != line_pos:
                        place(item)
                continue

            for item in items:
                keep(item)
            last_pos = line_pos
            if len(window) > limit:
                window = window[-limit:]
                more_before = True

    attach_files(window, cwd)
    mark_outside(window, cwd)
    if after is not None:
        return {"items": window, "moreBefore": False, "total": total, "last": last_pos}
    return {"items": window, "moreBefore": more_before, "total": total, "last": last_pos}
