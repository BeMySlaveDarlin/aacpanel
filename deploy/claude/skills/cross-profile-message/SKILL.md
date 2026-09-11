---
name: cross-profile-message
description: "Writes to a Claude session living under another account (another CLAUDE_CONFIG_DIR) on this machine. Use when the user says: \"write to <session>\", \"pass this to session <name>\", \"tell the agent in the work account\", \"send a message to another account\", \"write to the neighbouring account\" — or, in Russian, \"напиши в <сессию>\", \"передай сессии <имя>\", \"скажи агенту в рабочем аккаунте\", \"отправь письмо в другой аккаунт\", \"напиши в соседний аккаунт\". Also when SendMessage answered \"No agent named ... is reachable\" while the session is definitely alive. NOT for sessions of your own account — that is plain SendMessage."
user-invocable: true
argument-hint: "<session-name> <message text>"
---

A message to a session in another account. One command.

## First check whether the skill is needed at all

**`ListAgents`. If the recipient is there — write with plain `SendMessage` and be
done.** The skill is for exactly one case: the session is alive, but your
`SendMessage` does not see it.

The reason, rather than superstition: the registry of live sessions lies in
`<CLAUDE_CONFIG_DIR>/sessions/`, and every account has its own. A session of the
personal account sees personal ones only, a work one sees its own only. The
sockets are shared, so what is missing is the address book, not the transport.

## The command

```bash
<repo>/deploy/claude/cross-profile-msg.py --list                      # map of live sessions by account
<repo>/deploy/claude/cross-profile-msg.py <name> --from <your name> -m "text"
```

A long message is given on stdin, which is easier than quoting it in an argument:

```bash
<repo>/deploy/claude/cross-profile-msg.py <name> --from <your name> <<'EOF'
several lines of text
EOF
```

| What is needed | Flags |
|---|---|
| see who lives where | `--list` |
| sign with your own name | `--from <name>` (otherwise the recipient sees `bridge`) |
| check without sending | `--dry-run` |

The accounts come from `AACP_CLAUDE_HOME` in the host description (`host.env`),
and the claude binary from `AACP_CLAUDE` or `PATH`. The refusals are plain: no
such recipient anywhere (code 2), the name is taken in two accounts at once (2),
the recipient is in your own account (3), no claude or the recipient directory is
unreachable (4).

## What it costs and why

The message is carried by a one-off headless session started **in the account of
the recipient** — for it the addressee is an ordinary neighbour in the registry.
Which means:

- **one model call on the subscription of that account**, about ten seconds. Do
  not send five messages in a row, collect them into one;
- **the model may retell the text in its own words.** The script wraps the message
  in markers and demands it verbatim, but there is no guarantee — put anything
  important in a form a retelling cannot change.

## There will be no answer — write one-way messages

The sender dies right after delivery. The recipient, if they answer, will answer
**into their own account, into a session that no longer exists**. So do not ask
for an answer in the message — ask them to **tell the user**, who sits in both
windows. And do not wait for anything: silence after sending is normal, not a
failure.

## Traps

- **`--allowedTools SendMessage` is set by the script itself** — without it the
  headless session runs into permissions and quietly sends nothing.
- **A session file outlives its process.** The script checks the pid through
  `/proc`; do not go by the list of files by eye.
- **Delivery is gated by `crossSessionInbound`** in the settings of the recipient.
  With the default value a message from a session with a different permission-mode
  class hangs on the recipient's confirmation, and that can only be given from the
  machine where the window is open.
