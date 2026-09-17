#!/usr/bin/env python3
"""Sends a message to a Claude session running under another account."""

import argparse
import json
import os
import pathlib
import subprocess
import sys

HOME = pathlib.Path.home()
DEFAULT_HOST_ENV = pathlib.Path("/var/lib/aacpanel/host.env")


def say(mark, text):
    print(f"{mark} {text}")


def host_env_path():
    """Returns the path of the host description file."""
    if os.environ.get("AACP_HOSTCFG"):
        return pathlib.Path(os.environ["AACP_HOSTCFG"])
    if os.environ.get("AACP_STATE_DIR"):
        return pathlib.Path(os.environ["AACP_STATE_DIR"]) / "host.env"
    return DEFAULT_HOST_ENV


def host_env_var(key, path=None):
    """Returns a variable from the host description, or an empty string."""
    path = path or host_env_path()
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return ""
    value = ""
    for line in lines:
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        if k.strip() == key:
            value = v.strip().strip('"').strip("'")
    return value


def setting(key, host_env=None):
    """Returns a setting from the environment, falling back to the host description."""
    return os.environ.get(key) or host_env_var(key, host_env)


def profiles(host_env=None):
    """Returns the accounts of this machine as (name, config directory) pairs."""
    raw = setting("AACP_CLAUDE_HOME", host_env)
    dirs = []
    for item in raw.split(":"):
        item = item.strip()
        if item:
            dirs.append(pathlib.Path(os.path.expanduser(item)))
    if not dirs:
        dirs = [HOME / ".claude"]
    out = []
    seen = set()
    for d in dirs:
        key = str(d)
        if key in seen:
            continue
        seen.add(key)
        name = "personal" if d == HOME / ".claude" else d.name.lstrip(".") or str(d)
        out.append((name, d))
    return out


def alive(pid):
    return pathlib.Path(f"/proc/{pid}").exists()


def sessions(config_dir):
    """Returns the live named sessions of an account."""
    out = []
    for f in sorted((config_dir / "sessions").glob("*.json")):
        try:
            d = json.loads(f.read_text())
        except (OSError, ValueError):
            continue
        pid = d.get("pid")
        if not pid or not alive(pid):
            continue
        out.append(
            {
                "name": d.get("name") or f"(no name, pid {pid})",
                "pid": pid,
                "cwd": d.get("cwd") or "",
                "status": d.get("status") or "-",
                "kind": d.get("kind") or "-",
            }
        )
    return out


def my_config():
    return pathlib.Path(os.environ.get("CLAUDE_CONFIG_DIR") or (HOME / ".claude"))


def build_map(host_env=None):
    """Returns the live sessions of every account, keyed by account."""
    return {name: sessions(cfg) for name, cfg in profiles(host_env) if (cfg / "sessions").is_dir()}


def cmd_list(host_env=None):
    mine = my_config().resolve()
    for name, cfg in profiles(host_env):
        here = "  <- you are here" if cfg.resolve() == mine else ""
        rows = sessions(cfg) if (cfg / "sessions").is_dir() else []
        print(f"\n{name}  ({cfg}){here}")
        if not rows:
            print("   - no live sessions")
            continue
        for s in rows:
            print(f"   {s['name']:<22} pid {s['pid']:<8} {s['status']:<6} {s['cwd']}")
    print()


def find(target, host_env=None):
    """Finds an account and a session by name, or explains the refusal."""
    hits = [
        (prof, s)
        for prof, rows in build_map(host_env).items()
        for s in rows
        if s["name"] == target
    ]
    if not hits:
        say("STOP", f"no live session named '{target}' in any account")
        say("..", "the map: cross-profile-msg.py --list")
        sys.exit(2)
    if len(hits) > 1:
        say("STOP", f"the name '{target}' is taken in several accounts:")
        for prof, s in hits:
            say("..", f"{prof}: pid {s['pid']} - {s['cwd']}")
        say("..", "rename one of them, or write from inside its account")
        sys.exit(2)
    return hits[0]


def prompt_for(target, text):
    """Builds the prompt for the headless session."""
    return (
        f'Call SendMessage with to="{target}" and pass the message between the '
        "markers verbatim: no additions, no greetings, no signature.\n\n"
        "<<<MESSAGE START>>>\n"
        f"{text}\n"
        "<<<MESSAGE END>>>\n\n"
        "Do nothing else: no files, no commands, no tasks. Sent means done."
    )


def claude_bin(host_env=None):
    """Returns the binary that starts the headless session."""
    return setting("AACP_CLAUDE", host_env) or "claude"


def main():
    ap = argparse.ArgumentParser(
        description="Message a claude session that lives in another account",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    ap.add_argument("target", nargs="?", help="recipient session name")
    ap.add_argument("-m", "--message", help="message text (otherwise read from stdin)")
    ap.add_argument("--from", dest="sender", default="bridge",
                    help="sender name the recipient will see (default: bridge)")
    ap.add_argument("--list", action="store_true", help="map of live sessions by account")
    ap.add_argument("--dry-run", action="store_true", help="print the command and exit")
    ap.add_argument("--timeout", type=int, default=180, help="seconds for delivery (default 180)")
    args = ap.parse_args()

    if args.list:
        cmd_list()
        return

    if not args.target:
        ap.error("a recipient name is required (or --list)")

    text = args.message if args.message is not None else sys.stdin.read()
    text = text.strip()
    if not text:
        say("STOP", "empty message - nothing to send")
        sys.exit(2)

    profile, target = find(args.target)
    cfg = dict(profiles())[profile]

    if cfg.resolve() == my_config().resolve():
        say("STOP", f"'{args.target}' lives in your own account ({profile})")
        say("..", "use SendMessage directly - a model call is wasted here")
        sys.exit(3)

    binary = claude_bin()
    workdir = pathlib.Path(target["cwd"])
    if not workdir.is_dir():
        say("STOP", f"the recipient's directory is not reachable: {workdir}")
        sys.exit(4)

    cmd = [
        binary, "-p",
        "--allowedTools", "SendMessage",
        "-n", args.sender,
        prompt_for(args.target, text),
    ]
    env = dict(os.environ, CLAUDE_CONFIG_DIR=str(cfg))

    say("..", f"recipient: {args.target} (account {profile}, pid {target['pid']}, {target['status']})")
    say("..", f"sender: {args.sender}, working directory {workdir}")

    if args.dry_run:
        say("..", "dry run, nothing started. The command:")
        print("   cd", workdir, f"&& CLAUDE_CONFIG_DIR={cfg}", " ".join(repr(c) for c in cmd))
        return

    try:
        run = subprocess.run(cmd, cwd=workdir, env=env, capture_output=True, text=True, timeout=args.timeout)
    except FileNotFoundError:
        say("STOP", f"claude is not found: {binary}")
        say("..", "name it in AACP_CLAUDE or put it on PATH")
        sys.exit(4)
    out = (run.stdout or "").strip()
    err = (run.stderr or "").strip()

    if run.returncode != 0:
        say("STOP", f"the headless session exited with code {run.returncode}")
        if err:
            print(err[:2000])
        sys.exit(run.returncode)

    print(out)
    if "msg_id" not in out and "sent" not in out.lower():
        say("..", "the reply names neither msg_id nor sending - check with the recipient")


if __name__ == "__main__":
    main()
