#!/usr/bin/env python3
"""Asks the panel to restart this session as its project from the map.

The panel closes the session the gentle way and brings it up again with the
project's parameters — the console or the feed, the model, the account — and
the message after a restart as its first. The session is named by its
conversation: from inside it the name the panel calls it by is not known.

Exit 0: the panel took the restart, or is doing it. Exit 1: it refused or did
not answer — the caller restarts the old way.
"""

import argparse
import json
import os
import socket
import sys
import urllib.error
import urllib.request

URL = os.environ.get("AACP_PANEL_URL", "http://127.0.0.1:8777")

# How long the answer is waited for. A refusal comes at once; a restart that
# was taken answers only once the session is closed, and the session closes
# after this script is done — on the stream at the end of the turn it runs in.
# No answer by then is a restart under way.
WAIT = 3.0


def say(mark, text):
    print(f"{mark} {text}")


def ask(url, conversation, wait=WAIT):
    """Returns (taken, what): whether the panel took the restart, and its words."""
    body = json.dumps({"kind": "session.restart", "params": {"conversation": conversation}}).encode()
    req = urllib.request.Request(url.rstrip("/") + "/api/actions", data=body, method="POST",
                                 headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=wait) as resp:
            reply = json.loads(resp.read() or b"{}")
            return True, reply.get("detail") or "restarted"
    except urllib.error.HTTPError as e:
        return False, e.read().decode(errors="replace").strip() or f"HTTP {e.code}"
    except (socket.timeout, TimeoutError):
        return True, "the panel is restarting the session"
    except urllib.error.URLError as e:
        if isinstance(e.reason, (socket.timeout, TimeoutError)):
            return True, "the panel is restarting the session"
        return False, f"the panel does not answer at {url}: {e.reason}"
    except (OSError, ValueError) as e:
        return False, f"the panel does not answer at {url}: {e}"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--url", default=URL, help="the panel's local address")
    args = parser.parse_args(argv)

    conversation = os.environ.get("CLAUDE_CODE_SESSION_ID", "")
    if not conversation:
        say("STOP", "CLAUDE_CODE_SESSION_ID is empty - this is not running inside a session")
        return 1
    taken, what = ask(args.url, conversation)
    if not taken:
        say("STOP", what)
        return 1
    say("OK", what)
    say("..", "this session closes in a few seconds; do nothing more in it")
    return 0


if __name__ == "__main__":
    sys.exit(main())
