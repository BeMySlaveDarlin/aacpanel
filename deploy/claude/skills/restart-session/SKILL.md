---
name: restart-session
description: "Restarts this Claude Code session in place: the same tmux pane, the same directory, the same account. Use when the user says: \"restart the session\", \"restart yourself\", \"restart\", \"pick up the settings\", \"reread the settings\" — or, in Russian, \"перезапусти сессию\", \"перезапустись\", \"перезапусти себя\", \"рестарт\", \"подхвати настройки\", \"перечитай настройки\". Also after editing settings.json, CLAUDE.md, a hook or an MCP server, when the change is only picked up at startup. NOT about systemd units, docker containers or services — only about the agent session itself."
user-invocable: true
argument-hint: "[--continue]"
---

Restart this session. One command, no reconnaissance.

## The main rule

**"Restart X" is about the agent session.** Never about a systemd unit, a docker
container or a service. If the person meant a container, they will say so:
"container" or "service".

## Before a clean restart

A restart resets the context. Everything worth keeping has to be on disk
**before** the command — notes, a record of where the work stands, a commit.
There is no undo and no second attempt: the command hands the work over to the
tmux server, and a few seconds later this session is gone.

Two cases where there is nothing to save:

- **`--continue`** — the conversation moves into the new session.
- The person said outright not to bother.

Not sure — ask. Losing an hour of reasoning over one unasked question is a bad
trade.

## The command

```bash
<repo>/deploy/claude/restart-session.sh [--continue]
```

| What is needed | Flags |
|---|---|
| restart, start with a clean context | no flags |
| restart and continue the conversation from the same place | `--continue` |
| see what would happen, changing nothing | `--dry-run` |

The rest the script works out by itself, and it is worth knowing what "the rest"
is: every part of it has been stepped on somewhere.

- **Which tmux pane is ours** — found by walking up from this process to the
  `claude` it belongs to and matching its pid against the tmux panes. Not by
  asking tmux for the "current pane": without `$TMUX` in the environment that
  would be the *first* pane on the server, that is, somebody else's
  conversation. A restart is destructive, and the target must not be guessed.
- **Which environment to start with** — the one the working session has, read
  from the process itself. The panel starts sessions through `env -i` plus a
  checked set (the account route, the locale, TERM), and inheriting the
  environment of the skill instead would silently move the restarted session into
  another account.
- **In which directory** — the one this session is in. A restart somewhere else
  is a different session, not this one.
- **Which binary** — `$AACP_CLAUDE` if the machine names one, otherwise `claude`
  from `PATH`.

## When the script refuses

The script stops and says why instead of guessing:

- **`tmux not found`** — the session does not live in tmux, and there is nothing
  to restart it with: there is no pane. Restart it the same way it was started.
- **`no tmux pane with this session`** — the same thing from the other end: the
  process is not inside a pane. A session started by hand in an ordinary terminal
  is restarted by hand.

Both answers are honest, not failures to work around. Do not go looking for
another tmux pane to kill.

## What the skill does not do

- **It does not close the window.** The conversation lives in tmux, and the
  terminal window is only attached to it. Closing the window leaves the session
  running, and this command does not touch the window.
- **It does not touch other sessions.** One tmux pane — the one this session is
  in.
- **It does not restart services.** If the panel itself has to be restarted, that
  is `systemctl --user restart aacpanel-exec` or a reinstall, and that is a
  separate conversation.
