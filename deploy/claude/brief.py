#!/usr/bin/env python3
"""Publishes a brief: a long piece the person walks through in the panel.

The document is a file, not an argument. A brief runs to tens of kilobytes of
prose, and prose handed over on a command line loses its line breaks to the
shell and its quotes to whoever quotes it next.
"""

import argparse
import json
import os
import socket
import sys

SOCKET = os.environ.get("AACP_BRIEF_SOCKET", "/run/aacpanel-agent/brief.sock")

TIMEOUT = 10.0

# The same ceiling the collector keeps, so a document is refused in one place
# and for one reason. A client that stops short of the shelf refuses what the
# shelf would have taken, and the session is told a number nobody set.
MAX_BYTES = 4 * 1024 * 1024


def say(mark, text):
    print(f"{mark} {text}")


def load(path):
    """Reads the document from yaml or json and returns it as an object."""
    with open(path, encoding="utf-8") as f:
        raw = f.read()
    if path.endswith((".yaml", ".yml")):
        try:
            import yaml
        except ImportError:
            raise ValueError(
                "this machine has no pyyaml, so a yaml document cannot be read here: "
                "write the same document as .json") from None
        try:
            doc = yaml.safe_load(raw)
        except yaml.YAMLError as e:
            # A colon inside an unquoted title is the usual one, and a parser
            # traceback is a poor way to say "put quotes around the title".
            raise ValueError(f"the document is not valid yaml: {e}") from None
    else:
        doc = json.loads(raw)
    if not isinstance(doc, dict):
        raise ValueError("the document is an object with a title and, if it asks anything, questions")
    return doc


def publish(doc, session, cwd, path=SOCKET, timeout=TIMEOUT):
    """Hands the brief to the collector and returns what it answered."""
    payload = json.dumps({"sessionId": session, "cwd": cwd, "doc": doc}, ensure_ascii=False)
    if len(payload.encode("utf-8")) > MAX_BYTES:
        raise ValueError(f"the document is longer than {MAX_BYTES // (1024 * 1024)} MB")
    conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    conn.settimeout(timeout)
    try:
        conn.connect(path)
        conn.sendall(payload.encode("utf-8"))
        conn.shutdown(socket.SHUT_WR)
        raw = b""
        while True:
            chunk = conn.recv(8192)
            if not chunk:
                break
            raw += chunk
    finally:
        conn.close()
    reply = json.loads(raw.decode("utf-8"))
    if not isinstance(reply, dict):
        raise ValueError("the collector answered with something other than an object")
    return reply


def drop(brief_id, path=SOCKET):
    """Takes a brief of this directory off the shelf.

    The directory travels with the request and the shelf holds the removal to
    it: a session puts down the documents of the work it is carrying on, and a
    brief written elsewhere is somebody else's to remove.
    """
    try:
        conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        conn.settimeout(TIMEOUT)
        conn.connect(path)
        conn.sendall(json.dumps({"drop": brief_id, "cwd": os.getcwd()}, ensure_ascii=False).encode("utf-8"))
        conn.shutdown(socket.SHUT_WR)
        raw = conn.recv(64 * 1024)
        conn.close()
        reply = json.loads(raw.decode("utf-8"))
    except FileNotFoundError:
        say("STOP", f"the panel's collector is not listening on {path}")
        return 1
    except (OSError, ValueError) as e:
        say("STOP", f"the removal did not go through: {e}")
        return 1

    if not reply.get("ok"):
        say("STOP", reply.get("error") or "the collector refused to remove the brief")
        return 1
    say("OK", f"{reply.get('dropped')} is off the shelf: the document and the answers to it are gone")
    return 0


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Publishes a brief to the panel for the person to walk through.")
    parser.add_argument("path", nargs="?", help="the document, .yaml or .json")
    parser.add_argument("--delete", metavar="ID",
                        help="take a brief of this directory off the shelf, by its id")
    parser.add_argument("--socket", default=SOCKET, help="the collector's socket")
    parser.add_argument("--check", action="store_true",
                        help="read the document and say what is in it, publishing nothing")
    args = parser.parse_args(argv)

    if args.delete:
        return drop(args.delete, args.socket)

    if not args.path:
        say("STOP", "name the document to publish, or --delete <id> to take one off the shelf")
        return 2

    try:
        doc = load(args.path)
    except OSError as e:
        say("STOP", f"the document was not read: {e}")
        return 2
    except ValueError as e:
        say("STOP", str(e))
        return 2

    questions = doc.get("questions") or []
    asking = [q for q in questions if isinstance(q, dict) and q.get("kind", "pick") != "none"]

    if args.check:
        say("OK", f"{doc.get('title') or 'untitled'}: {len(questions)} questions, {len(asking)} of them ask something")
        say("..", "nothing was published; drop --check to send it")
        return 0

    session = os.environ.get("CLAUDE_CODE_SESSION_ID", "")
    if not session:
        say("STOP", "CLAUDE_CODE_SESSION_ID is empty - this is not running inside a session")
        say("..", "nothing was published: the answer has nowhere to come back to")
        return 2

    try:
        reply = publish(doc, session, os.getcwd(), args.socket)
    except FileNotFoundError:
        say("STOP", f"the panel's collector is not listening on {args.socket}")
        say("..", "nothing was published; ask in the conversation instead")
        return 1
    except (OSError, ValueError) as e:
        say("STOP", f"the brief did not go through: {e}")
        return 1

    if not reply.get("ok"):
        say("STOP", reply.get("error") or "the collector refused the brief")
        return 1

    say("OK", f"published as {reply.get('id')}: the person sees it in the panel")
    if asking:
        say("..", "the answers arrive in this session as a message when they send them, "
                  "which may be hours from now - do not wait for them")
    return 0


if __name__ == "__main__":
    sys.exit(main())
