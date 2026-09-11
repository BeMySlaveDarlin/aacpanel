#!/usr/bin/env python3
"""Token usage snapshot hook for Claude sessions and the report over it."""

import json
import os
import sys
import time

FIELDS = ("input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens")

SHORT = {
    "input_tokens": "in",
    "output_tokens": "out",
    "cache_creation_input_tokens": "cache+",
    "cache_read_input_tokens": "cache→",
}


def empty():
    return {f: 0 for f in FIELDS} | {"messages": 0}


def sum_usage(path):
    """Sums transcript usage, keeping the main session and subagents apart."""
    main, side = empty(), empty()
    seen = set()
    try:
        fh = open(path, encoding="utf-8")
    except OSError:
        return None
    with fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except ValueError:
                continue
            message = rec.get("message") or {}
            usage = message.get("usage")
            if not isinstance(usage, dict):
                continue
            mid = message.get("id")
            if mid:
                if mid in seen:
                    continue
                seen.add(mid)
            acc = side if rec.get("isSidechain") else main
            for f in FIELDS:
                value = usage.get(f)
                if isinstance(value, int):
                    acc[f] += value
            acc["messages"] += 1
    return {"main": main, "side": side}


def hook():
    """Appends a snapshot for the hook payload."""
    try:
        payload = json.load(sys.stdin)
    except (ValueError, OSError):
        return
    path = payload.get("transcript_path")
    if not path or not os.path.isfile(path):
        return
    sums = sum_usage(path)
    if sums is None:
        return

    config = os.environ.get("CLAUDE_CONFIG_DIR") or os.path.expanduser("~/.claude")
    log_dir = os.path.join(config, "logs")
    record = {
        "ts": int(time.time()),
        "session": payload.get("session_id", ""),
        "event": payload.get("hook_event_name", ""),
        "agent": payload.get("agent_id", ""),
        "main": sums["main"],
        "side": sums["side"],
    }
    try:
        os.makedirs(log_dir, exist_ok=True)
        with open(os.path.join(log_dir, "cost.jsonl"), "a", encoding="utf-8") as f:
            f.write(json.dumps(record, ensure_ascii=False) + "\n")
    except OSError:
        return


def human(n):
    """Formats a token count in thousands."""
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.0f}k"
    return str(n)


def report(path):
    """Prints where tokens went, from the last snapshot of each session."""
    last = {}
    order = []
    try:
        fh = open(path, encoding="utf-8")
    except OSError as err:
        print(f"cost-snapshot: {err}", file=sys.stderr)
        return 1
    with fh:
        for line in fh:
            try:
                rec = json.loads(line)
            except ValueError:
                continue
            sid = rec.get("session") or "?"
            if sid not in last:
                order.append(sid)
            last[sid] = rec
    if not last:
        print("No snapshots yet: the hook writes one on every Stop.")
        return 0

    head = f"{'session':<14}{'main out':>10}{'agents out':>12}{'main in':>10}{'cache read':>12}  agents"
    print(head)
    print("-" * len(head))
    totals = empty()
    agents_total = empty()
    for sid in order:
        rec = last[sid]
        main, side = rec.get("main") or empty(), rec.get("side") or empty()
        for f in FIELDS:
            totals[f] += main.get(f, 0)
            agents_total[f] += side.get(f, 0)
        print(f"{sid[:12]:<14}"
              f"{human(main.get('output_tokens', 0)):>10}"
              f"{human(side.get('output_tokens', 0)):>12}"
              f"{human(main.get('input_tokens', 0)):>10}"
              f"{human(main.get('cache_read_input_tokens', 0)):>12}"
              f"  {side.get('messages', 0)} msgs")

    print()
    out_main = totals["output_tokens"]
    out_side = agents_total["output_tokens"]
    total_out = out_main + out_side
    print(f"Output: {human(out_main)} from the sessions themselves, {human(out_side)} from subagents", end="")
    if total_out:
        print(f" ({out_side * 100 // total_out}% of it)")
    else:
        print()
    print(f"Input:  {human(totals['input_tokens'] + agents_total['input_tokens'])} fresh, "
          f"{human(totals['cache_read_input_tokens'] + agents_total['cache_read_input_tokens'])} read from cache, "
          f"{human(totals['cache_creation_input_tokens'] + agents_total['cache_creation_input_tokens'])} written to it")
    return 0


def main():
    if "--report" in sys.argv:
        args = [a for a in sys.argv[1:] if a != "--report"]
        config = os.environ.get("CLAUDE_CONFIG_DIR") or os.path.expanduser("~/.claude")
        path = args[0] if args else os.path.join(config, "logs", "cost.jsonl")
        sys.exit(report(path))
    hook()


if __name__ == "__main__":
    main()
