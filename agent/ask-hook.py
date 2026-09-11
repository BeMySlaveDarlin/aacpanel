#!/usr/bin/env python3
"""Hook that forwards a session question to the panel."""

import json
import os
import socket
import sys

SOCKET = os.environ.get("AACP_ASK_SOCKET", "/run/aacpanel-agent/ask.sock")
TIMEOUT = 1.25


def main():
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        return
    if not isinstance(payload, dict) or payload.get("tool_name") != "AskUserQuestion":
        return
    try:
        conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        conn.settimeout(TIMEOUT)
        conn.connect(SOCKET)
        conn.sendall(json.dumps(payload, ensure_ascii=False).encode("utf-8"))
        conn.shutdown(socket.SHUT_WR)
        conn.recv(4096)
        conn.close()
    except OSError:
        return


if __name__ == "__main__":
    main()
    sys.exit(0)
