"""Feed window: one pass over the end of the file, folding calls into badges."""
from .cards import mark_outside
from .disk import attach_files
from .limits import DEFAULT_LIMIT, MAX_LIMIT
from .locate import transcript_cwd
from .records import MARKS
from .uploads import attach_uploads
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


def unsettled(item):
    """Reports whether a row waits for a later record: a message or a command in the queue, a command for its answer.

    A command taken back from the queue waits for nothing.
    """
    if item.get("state") == "queued":
        return True
    return item["role"] == "command" and "text" in item and not item.get("done") and not item.get("state")


def fold(rows, limit, before, after):
    """Folds parsed records into a window of at most this many rows.

    A window after a position holds what the reader does not have yet, and a
    record past the position may settle a call the reader already has. So
    the rows up to the position are folded as well, the way they were folded
    for the reader, and kept only to find such a call in them: a group of
    calls a later record changed goes out again whole, under the position
    the reader knows it by, ahead of the new rows.
    """
    window = []
    total = 0
    more_before = False
    last_pos = None
    # After a position: the rows the reader already has, whether the fold has
    # reached the rows past it, and the groups among the known ones that a
    # record past it changed.
    known = []
    crossed = False
    touched = []

    def place(item):
        for i, was in enumerate(window):
            if was["pos"] != item["pos"]:
                continue
            if was["role"] == item["role"] or was["role"] == item.get("fixes"):
                window[i] = dict(item)
                # A row the reader already has comes again under its own
                # position, and the reader finds its row by the mark: the
                # queue hands a bubble over and the next record fixes it
                # within one read.
                if after is None or item["pos"] > after:
                    window[i].pop("fixes", None)
                return True
        return False

    def find_call(use):
        """Returns the group holding the call with this id, the rows it is in and the call.

        A letter is a call that stands as a row of its own: it is its own group.
        """
        if not use:
            return None, None, None
        for rows_of in (window, known):
            for was in reversed(rows_of):
                if was["role"] == "mail" and was.get("use") == use:
                    return was, rows_of, was
                if was["role"] != "tools":
                    continue
                for call in was["calls"]:
                    if call["use"] == use:
                        return was, rows_of, call
        return None, None, None

    def touch(group, rows_of):
        if rows_of is known and not any(group is was for was in touched):
            touched.append(group)

    def settle(mark):
        # The mark only changes the call it names, wherever it stands in the
        # window: the result of a call is often a few rows after the call.
        group, rows_of, call = find_call(mark.get("use"))
        if call is None:
            return
        call.pop("open", None)
        if mark.get("failed"):
            call["failed"] = True
        if mark.get("shots"):
            call["shots"] = [dict(shot) for shot in mark["shots"]]
        if mark.get("undelivered"):
            call["undelivered"] = mark["undelivered"]
        touch(group, rows_of)

    def open_calls():
        return any(c.get("open") for was in window if was["role"] == "tools" for c in was["calls"]) \
            or any(was.get("open") for was in window if was["role"] == "mail")

    def drop_call(use):
        # A delivery is shown once. Whether anything reached the human is
        # known only from the answer, so the call goes into the run first and
        # leaves it when its card is drawn; a call that failed gets no card
        # and stays, with the error readable in its details.
        nonlocal total
        was, rows_of, _ = find_call(use)
        if was is None:
            return
        calls = [c for c in was["calls"] if c["use"] != use]
        if calls:
            was["calls"] = calls
            touch(was, rows_of)
            return
        # A group left with no calls leaves the window. One the reader already
        # has cannot be taken back from its screen, only drawn again, so there
        # it stays as it was.
        rows_of[:] = [row for row in rows_of if row is not was]
        touched[:] = [row for row in touched if row is not was]
        if rows_of is window:
            total -= 1

    def keep(item):
        nonlocal total
        if item["role"] in MARKS:
            settle(item)
            return
        if place(item):
            return
        if item["role"] == "sent":
            drop_call(item["use"])
        if item["role"] == "taskdone":
            # Where the session read the news of a finished task is where the
            # terminal shows it: a later line of the same task takes its place.
            for i, was in enumerate(window):
                if was["role"] == "taskdone" and was["use"] == item["use"]:
                    del window[i]
                    total -= 1
                    break
        if item["role"] == "mail":
            # A letter that arrives twice is shown once; two letters sent are
            # two calls, however alike their words.
            if any(was["role"] == "mail" and was["from"] == item["from"]
                   and was["text"] == item["text"] and was.get("use") == item.get("use")
                   for was in window):
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
            if item.get("open"):
                call["open"] = True
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
                for item in items:
                    keep(item)
                if len(window) > limit:
                    window = window[-limit:]
                continue
            if not crossed:
                known, window, total, crossed = window, [], 0, True
            for item in items:
                keep(item)
            last_pos = line_pos
            if len(window) >= limit:
                break
            continue

        if before is not None and line_pos >= before:
            # The page is done, but what it holds may still change: a message
            # queued in it is delivered later, a call or a command in it
            # answered later.
            if not open_calls() and not any(unsettled(item) for item in window):
                break
            for item in items:
                if item["role"] in MARKS:
                    settle(item)
                elif item["pos"] != line_pos:
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
    if after is not None and not crossed:
        known, window, total = window, [], 0
    attach_files(window, cwd)
    attach_uploads(window)
    mark_outside(window, cwd)
    if after is not None:
        again = [was for was in known if any(was is group for group in touched)]
        return {"items": again + window, "moreBefore": False, "total": total, "last": last_pos}
    return {"items": window, "moreBefore": more_before, "total": total, "last": last_pos}
