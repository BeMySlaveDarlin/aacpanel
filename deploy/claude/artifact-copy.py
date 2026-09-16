#!/usr/bin/env python3
"""Keeps a copy of a published artifact, so the panel can show it without an account.

An artifact goes out into the account the session works under. The person
reads the panel from a phone under one account, and a contour publishes under
another: the link on the card then opens for whoever matches and refuses
everyone else. The page is a file on this machine at the moment it is
published, and this hands that file to the panel's collector.

It runs as a PostToolUse hook on the Artifact tool and says nothing on success:
a hook that prints on every publish turns into noise the session learns to
ignore. A panel that is down, or one not installed here, leaves the publish
exactly as it was.
"""

import json
import os
import socket
import sys

SOCKET = os.environ.get("AACP_PAGE_SOCKET", "/run/aacpanel-agent/page.sock")

TIMEOUT = 5.0

# The page itself is copied. Anything larger is left to its link: the copy is
# for reading on a phone, and the shelf refuses it anyway.
MAX_HTML = 2 * 1024 * 1024

TOOL = "Artifact"

# Only a publish makes a page. Reading, listing and deleting carry no file, and
# an action this hook does not know is not a publish either.
PUBLISH = ("", "publish")


def url_of(response):
    """Returns the address the artifact was published at, if the answer names one."""
    if isinstance(response, dict):
        for key in ("url", "artifact_url", "link"):
            value = response.get(key)
            if isinstance(value, str) and value.startswith(("http://", "https://")):
                return value
        text = " ".join(str(v) for v in response.values() if isinstance(v, str))
    elif isinstance(response, str):
        text = response
    else:
        return ""
    for word in text.split():
        if word.startswith("https://") or word.startswith("http://"):
            return word.rstrip(".,;)")
    return ""


def page_of(payload):
    """Returns what to send about this publish, or None when there is nothing to send."""
    if not isinstance(payload, dict) or payload.get("tool_name") != TOOL:
        return None
    data = payload.get("tool_input")
    if not isinstance(data, dict):
        return None
    if str(data.get("action") or "").strip() not in PUBLISH:
        return None
    # An upload into the asset store is not a page, though it publishes a file.
    if data.get("asset"):
        return None
    path = str(data.get("file_path") or "").strip()
    if not path or not path.lower().endswith((".html", ".htm")):
        return None

    try:
        if os.path.getsize(path) > MAX_HTML:
            return None
        with open(path, encoding="utf-8") as f:
            html = f.read()
    except (OSError, UnicodeDecodeError):
        return None
    if not html.strip():
        return None

    return {
        "session": str(payload.get("session_id") or ""),
        "cwd": str(payload.get("cwd") or ""),
        "path": path,
        "title": str(data.get("title") or ""),
        "desc": str(data.get("description") or ""),
        "icon": str(data.get("favicon") or ""),
        "url": url_of(payload.get("tool_response")),
        "html": html,
    }


def send(page, path=SOCKET, timeout=TIMEOUT):
    """Hands the copy to the collector and returns its answer."""
    conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    conn.settimeout(timeout)
    try:
        conn.connect(path)
        conn.sendall(json.dumps(page, ensure_ascii=False).encode("utf-8"))
        conn.shutdown(socket.SHUT_WR)
        chunks = []
        while True:
            chunk = conn.recv(64 * 1024)
            if not chunk:
                break
            chunks.append(chunk)
    finally:
        conn.close()
    try:
        return json.loads(b"".join(chunks).decode("utf-8"))
    except ValueError:
        return {"ok": False, "error": "the collector answered with something other than json"}


def main(argv=None):
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        return 0
    page = page_of(payload)
    if not page or not page["session"]:
        return 0
    try:
        send(page, (argv or [SOCKET])[0])
    except OSError:
        # The panel is not here, or its collector is down. A publish is not
        # worth a word of complaint over a copy nobody asked for.
        return 0
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
