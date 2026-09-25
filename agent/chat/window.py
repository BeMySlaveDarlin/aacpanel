"""Feed window: one pass over the end of the file, folding calls into badges."""
from .cards import mark_outside
from .disk import attach_files
from .limits import DEFAULT_LIMIT, MAX_LIMIT
from .locate import transcript_cwd
from . import tail


def feed(path, limit=DEFAULT_LIMIT, before=None, after=None, sidechain=False):
    """Returns a feed window: the tail, a piece before a position or a piece after it.

    Every window is folded from a piece of the file rather than from the
    whole of it, and the piece grows only while the page it folds into has
    no row to spare: a page with a row to spare is the page the whole file
    gives, since the rows before it were let go by the fold anyway.
    """
    limit = max(1, min(int(limit or DEFAULT_LIMIT), MAX_LIMIT))

    if after is not None:
        rows = tail.PIECES.view(path, tail.FIRST_SPAN, sidechain)
        if not rows.head and (not rows.rows or rows.rows[0][0] > after):
            rows = tail.Stream(path, after - tail.OVERLAP, sidechain)
        return fold(rows, limit, None, after)

    if before is not None:
        span = tail.FIRST_SPAN
        while True:
            rows = tail.Stream(path, before - span, sidechain)
            window = fold(rows, limit, before, None)
            if settled(window, limit, rows):
                return window
            span *= tail.SPAN_STEP

    span = tail.FIRST_SPAN
    while True:
        rows = tail.PIECES.view(path, span, sidechain)
        window = fold(rows, limit, None, None)
        if settled(window, limit, rows):
            return window
        span *= tail.SPAN_STEP


def settled(window, limit, rows):
    """Reports whether a piece is wide enough to give the page the whole file gives.

    The rows at the head of a piece are not the rows a whole read gives
    there: the run they are drawn in began before the piece, and so may
    have the call a card answers or the prompt a delivery repeats. All of
    that lies within a turn or so of the head, so a piece is wide enough
    once the window has let go of as many rows as it holds: every row it
    kept then stands a full window away from the head.
    """
    return rows.head or window["total"] > limit * 2


def fold(rows, limit, before, after):
    """Folds parsed records into a window of at most this many rows."""
    window = []
    total = 0
    more_before = False
    last_pos = None

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
            groups = run_tail()
            run = groups[0]["run"] if groups else item["pos"]
            spot = {"seq": next_seq(groups), "at": item.get("at", ""),
                    "tokens": item.get("tokens", 0), "pos": item["pos"],
                    "index": 0}
            for group in groups:
                if group["role"] == "think":
                    group["count"] += item["count"]
                    group["tokens"] += item.get("tokens", 0)
                    group["spots"].append(spot)
                    return
            item = dict(item, run=run, spots=[spot])
        elif item["role"] == "tool":
            groups = run_tail()
            run = groups[0]["run"] if groups else item["pos"]
            seq = next_seq(groups)
            call = {"name": item["name"], "arg": item.get("arg", ""), "seq": seq,
                    "at": item.get("at", ""), "pos": item["pos"], "index": item["index"],
                    "use": item.get("use", "")}
            if item.get("edited"):
                call["edited"] = item["edited"]
            kind = item.get("kind", "other")
            for group in groups:
                if group["role"] == "tools" and group["kind"] == kind:
                    group["calls"].append(call)
                    return
            item = {"role": "tools", "kind": kind, "run": run, "pos": item["pos"],
                    "at": item.get("at", ""), "calls": [call]}
        window.append(item)
        total += 1

    def next_seq(groups):
        used = [c["seq"] for group in groups if group["role"] == "tools" for c in group["calls"]]
        used += [spot["seq"] for group in groups if group["role"] == "think" for spot in group["spots"]]
        return max(used, default=-1) + 1

    def run_tail():
        """Returns the badge groups the run at the end of the window is drawn as.

        A line that arrives inside a run — a background task done, a warning
        of claude — does not end it: the screen hangs it under the run.
        """
        groups = []
        for was in reversed(window):
            if was["role"] in ("taskdone", "artifactlink", "notice"):
                continue
            if was["role"] not in ("tools", "think"):
                break
            groups.append(was)
        groups.reverse()
        return groups

    for line_pos, items in rows:
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

    cwd = rows.cwd
    if not cwd and not rows.head:
        cwd = transcript_cwd(rows.path)
    attach_files(window, cwd)
    mark_outside(window, cwd)
    if after is not None:
        return {"items": window, "moreBefore": False, "total": total, "last": last_pos}
    return {"items": window, "moreBefore": more_before, "total": total, "last": last_pos}
