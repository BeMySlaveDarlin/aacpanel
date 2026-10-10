"""Prints the state of a claude session with a run of codex exec among its agents, as the collector makes it.

    python3 codexrun.py AGENT_DIR

The run is made by the collector's own functions out of a rollout of a thread
written here, so a key the collector puts on a run is a key the service is
shown.
"""
import json
import os
import sys
import tempfile

sys.path.insert(0, sys.argv[1])

import ctx  # noqa: E402

THREAD = "01a12600-0000-7000-8000-0000000000e1"

RECORDS = [
    {"timestamp": "2026-10-10T12:00:00.000Z", "type": "session_meta",
     "payload": {"id": THREAD, "cwd": "/srv/lab", "source": "exec"}},
    {"timestamp": "2026-10-10T12:00:00.100Z", "type": "event_msg",
     "payload": {"type": "task_started", "turn_id": "t1", "model_context_window": 258400}},
    {"timestamp": "2026-10-10T12:00:00.200Z", "type": "turn_context",
     "payload": {"turn_id": "t1", "model": "gpt-6-sol", "effort": "high"}},
    {"timestamp": "2026-10-10T12:00:05.000Z", "type": "event_msg",
     "payload": {"type": "token_count",
                 "info": {"last_token_usage": {"input_tokens": 25840}, "model_context_window": 258400}}},
]

with tempfile.TemporaryDirectory() as root:
    rollout = os.path.join(root, f"rollout-2026-10-10T12-00-00-{THREAD}.jsonl")
    with open(rollout, "w", encoding="utf-8") as f:
        for record in RECORDS:
            f.write(json.dumps(record) + "\n")
    run = ctx.codex_agent({"thread": THREAD, "rollout": rollout, "born": 1791640005.0, "role": "reviewer"})
    print(json.dumps(ctx.with_codex_runs({"tasks": [], "agents": []}, [run])))
