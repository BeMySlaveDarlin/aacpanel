#!/usr/bin/env python3
"""Tells a starting session that a brief of this project is answered and waiting.

A brief is answered over hours, and the session that published it is usually
gone by then: restarted after a finalize, or closed for the night. The answers
sit in the panel with nobody to send them to, and the next session in the same
directory knows nothing about them — it is the one that would act on them.

This is a reading of the panel, not of the shelf on disk: what the person typed
into a document lives in the panel's database, and only the panel can say how
far they got. The local listener answers without a login, which is what it is
for; a panel that is down, or one not installed here, leaves the session with
nothing said rather than an error at its first breath.
"""

import json
import os
import sys
import urllib.error
import urllib.request

PANEL = os.environ.get("AACP_PANEL_URL", "http://127.0.0.1:8777")

TIMEOUT = 2.0

# How many briefs are worth naming at the start of a session. Past this the
# line says how many there are and stops: a start banner is not a shelf.
MAX_NAMED = 3


def shelf(base=PANEL, timeout=TIMEOUT):
    """Returns the cards of the panel's shelf, or an empty list."""
    try:
        with urllib.request.urlopen(base.rstrip("/") + "/api/briefs", timeout=timeout) as answer:
            body = json.load(answer)
    except (urllib.error.URLError, OSError, ValueError, TimeoutError):
        return []
    cards = body.get("briefs") if isinstance(body, dict) else None
    return cards if isinstance(cards, list) else []


def waiting(cards, cwd):
    """Returns the briefs of this directory that are answered and not sent."""
    out = []
    for card in cards:
        if not isinstance(card, dict) or card.get("cwd") != cwd:
            continue
        if card.get("sent"):
            continue
        if not card.get("answered"):
            continue
        out.append(card)
    out.sort(key=lambda c: str(c.get("at") or ""), reverse=True)
    return out


def line(cards):
    """Returns what to tell the session, or an empty string."""
    if not cards:
        return ""
    named = cards[:MAX_NAMED]
    parts = []
    for card in named:
        title = str(card.get("title") or card.get("id") or "a brief")
        answered, asked = card.get("answered") or 0, card.get("questions") or 0
        how = f"{answered} of {asked}" if asked else str(answered)
        parts.append(f"«{title}» ({how} answered)")
    rest = len(cards) - len(named)
    if rest:
        parts.append(f"and {rest} more")
    what = ", ".join(parts)
    return ("A brief of this project is answered and the answers have not been sent: "
            f"{what}. The person filled it in the panel, in Briefs — they may not know "
            "the session that asked is gone. Say so if it bears on what you are about to do.")


def main(argv=None):
    cwd = os.getcwd()
    base = PANEL
    if argv:
        base = argv[0]
    said = line(waiting(shelf(base), cwd))
    if said:
        print(said)
    return 0


if __name__ == "__main__":
    sys.exit(main())
