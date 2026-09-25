#!/usr/bin/env python3
"""The contract of the stream protocol of claude, checked against the installed binary.

A session the panel keeps on `claude -p` with stream-json talks to claude over
its control protocol: a message is a line of JSON, a permission and a question
are requests with an id, an answer is a reply to that id. Part of that protocol
is public and part of it is what the claude.ai clients use, and the second part
may change with any release. This runs every request the panel leans on against
the binary on this machine and says what still holds — before sessions move to
a new version, not after a button stops working.

It spends tokens and goes to the network, so it is not part of `make check`:
one long-lived session on the cheapest model, a dozen short turns, in a
directory of its own with hooks of its own and the user's settings left out.
The transcript it leaves is removed at the end.

    python3 deploy/claude/stream-contract.py [--claude PATH] [--model haiku] [--json] [--keep]

Exit code 0 when every required check holds, 1 when one does not, 2 when the
session could not be started at all.
"""

import argparse
import glob
import json
import os
import queue
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import uuid

TURN = 150          # seconds a turn may take
REQUEST = 30        # seconds a control request may take

# What the panel needs in the environment of a session it starts, and nothing
# else. A variable of the session running this script — its id, its messaging
# socket, CLAUDECODE — would make the child believe it is nested and quietly
# stop writing its transcript.
PASSED_ENV = ("HOME", "PATH", "USER", "LOGNAME", "LANG", "XDG_RUNTIME_DIR", "TMPDIR",
              "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_TMPDIR",
              "ANTHROPIC_API_KEY", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY")

# The flags a stream session is started with. Without the permission prompt
# tool pointed at the host, everything that would ask is denied on the spot and
# AskUserQuestion is not offered to the model at all.
STREAM_FLAGS = ("--input-format", "--output-format", "--permission-prompt-tool",
                "--session-id", "--replay-user-messages", "--include-partial-messages")

OK, FAIL, INFO = "ok", "FAIL", "info"


class Result:
    def __init__(self, name, status, detail="", required=True):
        self.name, self.status, self.detail, self.required = name, status, detail, required

    def as_dict(self):
        return {"check": self.name, "status": self.status, "detail": self.detail, "required": self.required}


def clean_env(env):
    """Returns the environment a session is started in: the passed names only."""
    out = {k: env[k] for k in PASSED_ENV if env.get(k)}
    out.setdefault("LANG", "C.UTF-8")
    return out


def config_dir(env):
    return env.get("CLAUDE_CONFIG_DIR") or os.path.join(env.get("HOME", "~"), ".claude")


def find_transcript(config, sid):
    """Returns the transcript of a session by its id, wherever its project directory is named."""
    found = glob.glob(os.path.join(config, "projects", "*", f"{sid}.jsonl"))
    return found[0] if found else ""


def help_flags(text):
    """Returns which of the stream flags the help of the binary still names."""
    return {flag: flag in text for flag in STREAM_FLAGS}


def direct_connect(text):
    """Says whether the help names a way for a console to attach to a running session.

    A console that is a client of a session on the stream protocol would make
    switching between the console and the panel unnecessary. The client is in
    the binary with its entry cut; the day the entry opens, the help says so.
    """
    marks = ("cc://", "cc+unix://", "claude connect", "claude server")
    return [m for m in marks if m in text]


def exit_code(results):
    return 1 if any(r.required and r.status == FAIL for r in results) else 0


def table(results):
    width = max((len(r.name) for r in results), default=10)
    rows = []
    for r in results:
        mark = r.status if r.required or r.status != OK else "ok"
        rows.append(f"{r.name.ljust(width)}  {mark.ljust(4)}  {'' if r.required else '(info) '}{r.detail}")
    return "\n".join(rows)


class Session:
    """One long-lived `claude -p` on the stream protocol, and what it said."""

    def __init__(self, claude, cwd, env, model, log_path):
        self.sid = str(uuid.uuid4())
        self.cwd = cwd
        self.events = queue.Queue()
        self.log = open(log_path, "w", encoding="utf-8")
        # What claude says on stderr goes to a file beside the events: a pipe
        # nobody reads fills up, and claude stops mid-turn waiting for it.
        self.err = open(os.path.splitext(log_path)[0] + ".stderr", "w", encoding="utf-8")
        self.seen = []
        cmd = [claude, "-p", "--input-format", "stream-json", "--output-format", "stream-json",
               "--verbose", "--replay-user-messages", "--model", model,
               "--permission-prompt-tool", "stdio", "--setting-sources", "project",
               "--session-id", self.sid]
        self.proc = subprocess.Popen(cmd, cwd=cwd, env=env, stdin=subprocess.PIPE,
                                     stdout=subprocess.PIPE, stderr=self.err,
                                     text=True, bufsize=1)
        threading.Thread(target=self._read, daemon=True).start()
        self.counter = 0

    def _read(self):
        for line in self.proc.stdout:
            self.log.write(line)
            self.log.flush()
            try:
                self.events.put(json.loads(line))
            except ValueError:
                continue
        self.events.put(None)

    def send(self, obj):
        self.proc.stdin.write(json.dumps(obj) + "\n")
        self.proc.stdin.flush()

    def user(self, text, message_uuid=""):
        msg = {"type": "user", "session_id": self.sid, "parent_tool_use_id": None,
               "message": {"role": "user", "content": text}}
        if message_uuid:
            msg["uuid"] = message_uuid
        self.send(msg)

    def pump(self, until, timeout, on_event=None):
        """Reads events until one satisfies `until`, answering along the way; returns it or None."""
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            try:
                ev = self.events.get(timeout=0.5)
            except queue.Empty:
                continue
            if ev is None:
                return None
            self.seen.append(ev)
            if on_event:
                on_event(ev)
            if until(ev):
                return ev
        return None

    def request(self, subtype, timeout=REQUEST, **fields):
        """Sends a control request and returns the body of its response, or None."""
        self.counter += 1
        rid = f"contract-{self.counter}"
        self.send({"type": "control_request", "request_id": rid, "request": {"subtype": subtype, **fields}})
        ev = self.pump(lambda e: e.get("type") == "control_response"
                       and (e.get("response") or {}).get("request_id") == rid, timeout)
        return None if ev is None else ev["response"]

    def answer(self, ev, response):
        self.send({"type": "control_response", "response": {
            "subtype": "success", "request_id": ev["request_id"], "response": response}})

    def turn(self, text, on_ask=None, timeout=TURN, message_uuid=""):
        """Sends one message and reads to the end of the turn; returns the result event and the turn's events."""
        start = len(self.seen)

        def on(ev):
            if is_ask(ev):
                (on_ask or allow)(self, ev)
        self.user(text, message_uuid)
        result = self.pump(lambda e: e.get("type") == "result", timeout, on)
        return result, self.seen[start:]

    def close(self):
        try:
            self.proc.stdin.close()
        except OSError:
            pass
        try:
            self.proc.wait(timeout=30)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait()
        self.proc.stdout.close()
        self.log.close()
        self.err.close()


def is_ask(ev):
    return ev.get("type") == "control_request" and (ev.get("request") or {}).get("subtype") == "can_use_tool"


def tool_of(ev):
    return (ev.get("request") or {}).get("tool_name", "")


def allow(session, ev, patch=None):
    body = dict((ev.get("request") or {}).get("input") or {})
    if patch:
        body.update(patch)
    session.answer(ev, {"behavior": "allow", "updatedInput": body})


def tool_results(events):
    """Returns the text of every tool result among the events."""
    out = []
    for ev in events:
        if ev.get("type") != "user":
            continue
        content = (ev.get("message") or {}).get("content")
        if not isinstance(content, list):
            continue
        for part in content:
            if part.get("type") != "tool_result":
                continue
            body = part.get("content")
            if isinstance(body, list):
                body = " ".join(x.get("text", "") for x in body if isinstance(x, dict))
            out.append(str(body))
    return out


def assistant_text(events):
    out = []
    for ev in events:
        if ev.get("type") != "assistant":
            continue
        for part in (ev.get("message") or {}).get("content") or []:
            if part.get("type") == "text":
                out.append(part.get("text", ""))
    return "\n".join(out)


def keys(body):
    """Names what an answer carries; for an answer that is one list, what its items carry."""
    if not isinstance(body, dict):
        return type(body).__name__
    if len(body) == 1:
        (name, items), = body.items()
        if isinstance(items, list):
            first = items[0] if items and isinstance(items[0], dict) else {}
            return f"{name}: {len(items)}, each with {', '.join(sorted(first)) or 'nothing'}"
    return ", ".join(sorted(body))


# ------------------------------------------------------------------ the checks

def check_initialize(s):
    body = s.request("initialize")
    if not body or body.get("subtype") != "success":
        return Result("initialize", FAIL, f"no answer: {body}")
    resp = body.get("response") or {}
    commands = [c.get("name") for c in resp.get("commands") or []]
    models = resp.get("models") or []
    if not commands or not models:
        return Result("initialize", FAIL, f"commands {len(commands)}, models {len(models)}")
    return Result("initialize", OK, f"{len(commands)} commands, {len(models)} models, mode {resp.get('current_permission_mode')}")


def check_turn(s, config):
    result, _ = s.turn("Reply with the single word: ready")
    if not result or result.get("subtype") != "success":
        return [Result("turn", FAIL, f"no successful result: {result and result.get('subtype')}")]
    out = [Result("turn", OK, str(result.get("result", ""))[:40])]
    path = find_transcript(config, s.sid)
    out.append(Result("transcript", OK if path else FAIL,
                      "written under the session id" if path else "no transcript under the session id"))
    return out


def check_hooks(cwd):
    try:
        with open(os.path.join(cwd, "hooks.log"), encoding="utf-8") as f:
            fired = set(f.read().split())
    except OSError:
        fired = set()
    want = {"SessionStart", "UserPromptSubmit", "Stop"}
    missing = sorted(want - fired)
    return Result("hooks", FAIL if missing else OK,
                  f"did not fire: {', '.join(missing)}" if missing else "SessionStart, UserPromptSubmit, Stop")


def check_permission(s):
    asked = []

    def on(session, ev):
        asked.append(ev)
        allow(session, ev)
    s.turn("Use the Bash tool to run exactly: touch contract-marker.txt — then reply with the word done.", on)
    bash = [e for e in asked if tool_of(e) == "Bash"]
    if not bash:
        return Result("permission.allow", FAIL, "no can_use_tool for Bash arrived")
    req = bash[0]["request"]
    fields = [k for k in ("input", "tool_use_id", "permission_suggestions") if k not in req]
    if fields:
        return Result("permission.allow", FAIL, f"the request lacks {', '.join(fields)}")
    made = os.path.exists(os.path.join(s.cwd, "contract-marker.txt"))
    return Result("permission.allow", OK if made else FAIL,
                  "asked, allowed, the command ran" if made else "allowed, but the command did not run")


def check_deny(s):
    why = "the contract says no"
    asked = []

    def on(session, ev):
        asked.append(ev)
        session.answer(ev, {"behavior": "deny", "message": why})
    _, events = s.turn("Use the Bash tool to run exactly: touch contract-denied.txt — then reply with the word done.", on)
    if not asked:
        return Result("permission.deny", FAIL, "no can_use_tool arrived")
    heard = any(why in t for t in tool_results(events))
    made = os.path.exists(os.path.join(s.cwd, "contract-denied.txt"))
    ok = heard and not made
    return Result("permission.deny", OK if ok else FAIL,
                  "denied with a reason the model read" if ok else f"reason read {heard}, file made {made}")


ASK_PROMPT = ("Call the AskUserQuestion tool exactly once with two questions. "
              "Question 1: \"Pick a colour\", header \"Colour\", options Red (preview \"RED BLOCK\") "
              "and Blue (preview \"BLUE BLOCK\"). Question 2: \"Pick a size\", header \"Size\", "
              "options Small and Large, no previews. After the answer, reply with the word done.")


def check_ask(s):
    got = []

    def on(session, ev):
        if tool_of(ev) != "AskUserQuestion":
            allow(session, ev)
            return
        got.append(ev)
        questions = ev["request"]["input"].get("questions") or []
        answers = {q["question"]: q["options"][-1]["label"] for q in questions if q.get("options")}
        notes = {questions[0]["question"]: {"notes": "contract-note"}} if questions else {}
        allow(session, ev, {"answers": answers, "annotations": notes})
    _, events = s.turn(ASK_PROMPT, on)
    if not got:
        return [Result("ask", FAIL, "no can_use_tool for AskUserQuestion arrived — the tool was not offered or not called")]
    questions = got[0]["request"]["input"].get("questions") or []
    previews = any(o.get("preview") for q in questions for o in q.get("options") or [])
    said = " ".join(tool_results(events))
    picked = all(q["options"][-1]["label"] in said for q in questions if q.get("options"))
    out = [Result("ask", OK if picked and len(questions) == 2 else FAIL,
                  f"{len(questions)} questions, previews {previews}, answers heard {picked}")]
    out.append(Result("ask.notes", OK if "contract-note" in said else INFO,
                      "a note to an option reaches the model" if "contract-note" in said
                      else "a note to an option did not reach the model", required=False))
    return out


def check_plan(s):
    body = s.request("set_permission_mode", mode="plan")
    if not body or body.get("subtype") != "success":
        return Result("plan", FAIL, f"set_permission_mode plan: {body}")
    got = []

    def on(session, ev):
        if tool_of(ev) == "ExitPlanMode":
            got.append(ev)
        allow(session, ev)
    s.turn("Plan how to create a file named plan.txt with the word hello in it. Keep the plan to one line "
           "and present it with the ExitPlanMode tool right away. Do not do anything else.", on)
    back = s.request("set_permission_mode", mode=manual_mode(s))
    if not got:
        return Result("plan", FAIL, "no can_use_tool for ExitPlanMode arrived")
    body = got[0]["request"].get("input") or {}
    plan = body.get("plan", "")
    # Plan mode keeps the plan as a file among the user's plans; this one was
    # the contract's and goes with it.
    written = body.get("planFilePath", "")
    if written and os.path.basename(os.path.dirname(written)) == "plans":
        try:
            os.remove(written)
        except OSError:
            pass
    ok = bool(plan) and back and back.get("subtype") == "success"
    return Result("plan", OK if ok else FAIL, "the plan arrives as a request with its text" if ok
                  else f"plan text {bool(plan)}, mode restored {back}")


def manual_mode(s):
    return getattr(s, "manual", "default")


def check_interrupt(s):
    started = []

    def on(ev):
        if is_ask(ev):
            allow(s, ev)
        if ev.get("type") == "system" and ev.get("subtype") == "task_started":
            started.append(ev)
    # A bare long sleep is refused by claude itself as a wait better done
    # another way; chained to a command it is an ordinary command.
    s.user("Use the Bash tool to run exactly: sleep 40 && echo woke — then reply with the word done.")
    s.pump(lambda e: bool(started), 60, on)
    if not started:
        return Result("interrupt", FAIL, "the command never started")
    body = s.request("interrupt")
    result = s.pump(lambda e: e.get("type") == "result", 30)
    queued = ((body or {}).get("response") or {}).get("still_queued")
    ok = result is not None and result.get("subtype") == "error_during_execution"
    return Result("interrupt", OK if ok else FAIL,
                  f"the turn ended at once, still_queued is {type(queued).__name__}" if ok
                  else f"result {result and result.get('subtype')}")


def check_queue(s):
    """A message sent while a turn runs waits in a queue; one of two is taken back before it is read."""
    dropped, kept = str(uuid.uuid4()), str(uuid.uuid4())
    started = []

    def on(ev):
        if is_ask(ev):
            allow(s, ev)
        if ev.get("type") == "system" and ev.get("subtype") == "task_started":
            started.append(ev)
    s.user("Use the Bash tool to run exactly: sleep 12 && echo woke — then reply with the word done.")
    s.pump(lambda e: bool(started), 60, on)
    if not started:
        return [Result("queue", FAIL, "the command never started")]
    s.user("Reply with the single word: DROPPEDWORD", dropped)
    s.user("Reply with the single word: KEPTWORD", kept)
    body = s.request("cancel_async_message", message_uuid=dropped)
    cancelled = ((body or {}).get("response") or {}).get("cancelled")
    start = len(s.seen)
    # The turn of the sleep ends, then the kept message is read as a turn of its own.
    deadline = time.monotonic() + TURN
    while time.monotonic() < deadline:
        s.pump(lambda e: e.get("type") == "result", 10, on)
        if "KEPTWORD" in assistant_text(s.seen[start:]):
            break
    said = assistant_text(s.seen[start:])
    out = [Result("queue.cancel", OK if cancelled is True else FAIL,
                  f"cancel_async_message answered cancelled={cancelled}")]
    out.append(Result("queue.deliver", OK if "KEPTWORD" in said and "DROPPEDWORD" not in said else FAIL,
                      f"kept one read {'KEPTWORD' in said}, dropped one read {'DROPPEDWORD' in said}"))
    return out


def check_model(s, model):
    body = s.request("set_model", model="sonnet")
    if not body or body.get("subtype") != "success":
        return Result("set_model", FAIL, f"{body}")
    _, events = s.turn("/context")
    shown = assistant_text(events) + " ".join(str(e.get("message", {}).get("content", "")) for e in events)
    s.request("set_model", model=model)
    ok = "sonnet" in shown.lower()
    return Result("set_model", OK if ok else FAIL, "applied, /context names it" if ok
                  else "accepted, but /context does not name the new model")


def check_effort(s):
    out = []
    body = s.request("set_max_thinking_tokens", max_thinking_tokens=None)
    out.append(Result("set_max_thinking_tokens", OK if body and body.get("subtype") == "success" else FAIL,
                      f"{body and body.get('subtype')}"))
    _, events = s.turn("/effort high")
    text = assistant_text(events) + " ".join(str(e.get("message", {}).get("content", "")) for e in events
                                             if e.get("type") == "user")
    out.append(Result("/effort", OK if "effort" in text.lower() else FAIL, text.strip()[:60]))
    return out


# The requests the screens read their data from. The list of background tasks
# is not among them: asked for, it answers with nothing, and it arrives by
# itself as an event each time it changes.
DATA_REQUESTS = ("list_models", "get_context_usage", "get_usage", "get_session_cost", "mcp_status",
                 "get_hooks_listing", "get_memory_dialog", "get_skills_dialog")


def check_data(s):
    out = []
    for subtype in DATA_REQUESTS:
        body = s.request(subtype)
        if not body or body.get("subtype") != "success":
            out.append(Result(subtype, FAIL, f"{(body or {}).get('error') or body}"))
            continue
        out.append(Result(subtype, OK, keys(body.get("response"))))
    return out


def check_slash(s):
    _, events = s.turn("/cost")
    text = assistant_text(events)
    return Result("/cost", OK if text.strip() else FAIL, text.strip().splitlines()[0][:60] if text.strip() else "no text")


def task_ended(ev, task):
    """Says whether an event reports the end of a task: its notice, or a patch to a final status."""
    if ev.get("type") != "system" or ev.get("task_id") != task:
        return False
    if ev.get("subtype") == "task_notification":
        return True
    return ev.get("subtype") == "task_updated" and (ev.get("patch") or {}).get("status") in (
        "completed", "failed", "killed", "stopped")


def check_background(s):
    """A background task outlives the turn that started it, and the stream says which tasks run.

    The list comes as an event, background_tasks_changed, each time it
    changes; a request for it answers with nothing. The panel follows the
    events rather than asking.
    """
    def on(session, ev):
        allow(session, ev)
    start = len(s.seen)
    s.turn("Use the Bash tool with run_in_background set to true to run exactly: sleep 90 — "
           "then reply with the word done without waiting for it.", on)
    started = [e for e in s.seen[start:] if e.get("type") == "system" and e.get("subtype") == "task_started"]
    if not started:
        return [Result("background", FAIL, "no background task started")]
    task = started[-1].get("task_id", "")
    time.sleep(6)
    s.turn("Reply with the single word: still")
    lists = [e.get("tasks") or [] for e in s.seen[start:]
             if e.get("type") == "system" and e.get("subtype") == "background_tasks_changed"]
    listed = bool(lists) and any(t.get("task_id") == task for t in lists[-1])
    ended = any(task_ended(e, task) for e in s.seen[start:])
    alive = listed and not ended
    out = [Result("background", OK if alive else FAIL,
                  "a background task outlives its turn, listed by background_tasks_changed" if alive
                  else f"listed {listed}, ended before it was stopped {ended}")]
    mark = len(s.seen)
    stop = s.request("stop_task", task_id=task)
    if not any(task_ended(e, task) for e in s.seen[mark:]):
        s.pump(lambda e: task_ended(e, task), 10)
    stopped = any(task_ended(e, task) for e in s.seen[mark:])
    out.append(Result("stop_task", OK if stop and stop.get("subtype") == "success" and stopped else FAIL,
                      "the task is stopped and the stream says so" if stopped else f"{stop and stop.get('subtype')}, no stop seen"))
    return out


def check_resume(claude, cwd, env, sid, model):
    try:
        done = subprocess.run([claude, "-p", "--resume", sid, "--model", model, "--setting-sources", "project",
                               "--output-format", "json", "Reply with the single word: RESUMED"],
                              cwd=cwd, env=env, capture_output=True, text=True, timeout=TURN)
        out = json.loads(done.stdout or "{}")
    except (subprocess.TimeoutExpired, ValueError) as e:
        return Result("resume", FAIL, f"{e}")
    same = out.get("session_id") == sid
    heard = "RESUMED" in str(out.get("result", ""))
    return Result("resume", OK if same and heard else FAIL,
                  "the same id, the same history" if same and heard else f"id kept {same}, answered {heard}")


def wait_gone(pid, limit=15):
    """Waits for a process to end. A terminal writes the last of its transcript
    on the way out, and a directory removed before that comes back with it."""
    deadline = time.time() + limit
    while pid and time.time() < deadline and os.path.exists(f"/proc/{pid}"):
        time.sleep(0.2)


def check_console(claude, cwd, env, sid, model):
    """A session moves to the console by resuming its conversation in a terminal,
    so a terminal has to read what the stream wrote. No request is made: the
    history is drawn from the transcript. The directory is new to claude, and
    the terminal asks whether to trust it with the cursor on "No, exit": a key
    goes in only after the screen shows where the cursor is, and Enter only on
    the line that trusts."""
    tmux = shutil.which("tmux")
    if not tmux:
        return Result("console", INFO, "no tmux on this machine: the console side of a switch was not checked",
                      required=False)
    name = "stream-contract-" + sid[:8]
    pane = dict(env, TERM="screen-256color")
    argv = [tmux, "new-session", "-d", "-s", name, "-x", "160", "-y", "48", "-c", cwd, "--", "env", "-i"]
    argv += [f"{k}={v}" for k, v in pane.items()]
    argv += [claude, "--resume", sid, "--model", model, "--setting-sources", "project"]
    screen = ""
    pid = 0
    try:
        subprocess.run(argv, check=True, capture_output=True, timeout=30)
        listed = subprocess.run([tmux, "list-panes", "-t", name, "-F", "#{pane_pid}"],
                                capture_output=True, text=True, timeout=10).stdout.split()
        pid = int(listed[0]) if listed else 0
        deadline = time.time() + 60
        while time.time() < deadline:
            screen = subprocess.run([tmux, "capture-pane", "-p", "-t", name],
                                    capture_output=True, text=True, timeout=10).stdout
            if "RESUMED" in screen:
                return Result("console", OK, "a terminal resumes what the stream wrote")
            if "Yes, I trust this folder" in screen:
                on_yes = any(line.strip().startswith("❯") and "Yes, I trust this folder" in line
                             for line in screen.splitlines())
                key = "Enter" if on_yes else "Down"
                subprocess.run([tmux, "send-keys", "-t", name, key], capture_output=True, timeout=10)
            time.sleep(1)
    except (OSError, subprocess.SubprocessError) as e:
        return Result("console", FAIL, f"the terminal did not start: {e}")
    finally:
        subprocess.run([tmux, "kill-session", "-t", name], capture_output=True)
        wait_gone(pid)
    last = " | ".join(line.strip() for line in screen.splitlines() if line.strip())[-300:]
    return Result("console", FAIL, f"the terminal did not show the history: {last}")


# ------------------------------------------------------------------ the run

def hooks_settings(cwd):
    log = os.path.join(cwd, "hooks.log")

    def mark(name):
        return [{"hooks": [{"type": "command", "command": f"printf '%s\\n' {name} >> '{log}'"}]}]
    return {"hooks": {name: mark(name) for name in ("SessionStart", "UserPromptSubmit", "Stop")}}


def run(claude, model, keep, say):
    results = []
    env = clean_env(os.environ)
    config = config_dir(env)
    try:
        version = subprocess.run([claude, "--version"], capture_output=True, text=True, env=env, timeout=30).stdout.strip()
        helptext = subprocess.run([claude, "--help"], capture_output=True, text=True, env=env, timeout=30).stdout
    except (OSError, subprocess.TimeoutExpired) as e:
        return [Result("binary", FAIL, f"{claude}: {e}")], 2
    results.append(Result("version", INFO, version, required=False))
    missing = [f for f, there in help_flags(helptext).items() if not there]
    results.append(Result("flags", FAIL if missing else OK,
                          f"gone: {', '.join(missing)}" if missing else "every stream flag is still there"))
    marks = direct_connect(helptext)
    results.append(Result("direct-connect", INFO, f"the help names {', '.join(marks)} — a console may now attach "
                          "to a running session" if marks else "no way for a console to attach yet", required=False))
    manual = "manual" if '"manual"' in helptext else "default"

    cwd = tempfile.mkdtemp(prefix="stream-contract-")
    os.makedirs(os.path.join(cwd, ".claude"))
    with open(os.path.join(cwd, ".claude", "settings.json"), "w", encoding="utf-8") as f:
        json.dump(hooks_settings(cwd), f)

    s = Session(claude, cwd, env, model, os.path.join(cwd, "events.jsonl"))
    s.manual = manual
    say(f"session {s.sid} in {cwd}")
    steps = (
        ("initialize", lambda: check_initialize(s)),
        ("turn", lambda: check_turn(s, config)),
        ("permission", lambda: check_permission(s)),
        ("deny", lambda: check_deny(s)),
        ("ask", lambda: check_ask(s)),
        ("plan", lambda: check_plan(s)),
        ("interrupt", lambda: check_interrupt(s)),
        ("queue", lambda: check_queue(s)),
        ("model", lambda: check_model(s, model)),
        ("effort", lambda: check_effort(s)),
        ("data", lambda: check_data(s)),
        ("slash", lambda: check_slash(s)),
        ("background", lambda: check_background(s)),
    )
    for name, step in steps:
        if s.proc.poll() is not None:
            results.append(Result(name, FAIL, "the session is gone"))
            continue
        say(f"… {name}")
        got = step()
        results.extend(got if isinstance(got, list) else [got])
    s.close()
    results.append(check_hooks(cwd))
    say("… resume")
    results.append(check_resume(claude, cwd, env, s.sid, model))
    say("… console")
    results.append(check_console(claude, cwd, env, s.sid, model))

    transcript = find_transcript(config, s.sid)
    if keep:
        say(f"kept: {cwd}, transcript {transcript}")
    else:
        if transcript:
            shutil.rmtree(os.path.dirname(transcript), ignore_errors=True)
        shutil.rmtree(cwd, ignore_errors=True)
    return results, exit_code(results)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--claude", default=shutil.which("claude") or "claude")
    parser.add_argument("--model", default="haiku")
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--keep", action="store_true", help="keep the directory, the event log and the transcript")
    args = parser.parse_args(argv)

    def say(line):
        if not args.json:
            print(line, file=sys.stderr, flush=True)

    results, code = run(args.claude, args.model, args.keep, say)
    if args.json:
        print(json.dumps([r.as_dict() for r in results], ensure_ascii=False, indent=1))
    else:
        print(table(results))
    return code


if __name__ == "__main__":
    sys.exit(main())
