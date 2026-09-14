#!/usr/bin/env python3
"""Calls the person to this session: a line to the panel, a push to their phone."""

import argparse
import json
import os
import socket
import sys

SOCKET = os.environ.get("AACP_NOTIFY_SOCKET", "/run/aacpanel-agent/notify.sock")

TIMEOUT = 2.0

MAX_TEXT = 300


def say(mark, text):
    print(f"{mark} {text}")


def call(text, session, path=SOCKET, timeout=TIMEOUT):
    """Hands the call to the collector and returns what it answered."""
    payload = json.dumps({"sessionId": session, "text": text}, ensure_ascii=False)
    conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    conn.settimeout(timeout)
    try:
        conn.connect(path)
        conn.sendall(payload.encode("utf-8"))
        conn.shutdown(socket.SHUT_WR)
        raw = conn.recv(4096)
    finally:
        conn.close()
    reply = json.loads(raw.decode("utf-8"))
    if not isinstance(reply, dict):
        raise ValueError("the collector answered with something other than an object")
    return reply


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Calls the person to this session through the panel.")
    parser.add_argument("text", nargs="*", help="what to tell them, in one line")
    parser.add_argument("--socket", default=SOCKET, help="the collector's socket")
    args = parser.parse_args(argv)

    text = " ".join(" ".join(args.text).split())
    if not text:
        say("STOP", "nothing to say - a call carries a line the person can act on")
        return 2
    if len(text) > MAX_TEXT:
        say("..", f"the line is {len(text)} characters, the panel keeps the first {MAX_TEXT}")

    session = os.environ.get("CLAUDE_CODE_SESSION_ID", "")
    if not session:
        say("STOP", "CLAUDE_CODE_SESSION_ID is empty - this is not running inside a session")
        say("..", "nothing was sent: the panel names the caller by that identifier")
        return 2

    try:
        reply = call(text, session, args.socket)
    except FileNotFoundError:
        say("STOP", f"the panel's collector is not listening on {args.socket}")
        say("..", "nobody was called; say it in the conversation instead")
        return 1
    except (OSError, ValueError) as e:
        say("STOP", f"the call did not go through: {e}")
        return 1

    if not reply.get("ok"):
        say("STOP", reply.get("error") or "the collector refused the call")
        return 1

    say("OK", "the person has been called; the push carries this line and opens this session")
    return 0


if __name__ == "__main__":
    sys.exit(main())
