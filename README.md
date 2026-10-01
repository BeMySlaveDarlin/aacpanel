# aacpanel

A control panel for one machine and the [Claude Code](https://claude.com/claude-code)
sessions on it — from a phone and from a monitor.

Not a dashboard: the panel does not only show containers, load and live
sessions, it **runs** them — brings containers up and down, opens and closes
sessions, writes messages into them, answers the model's questions, sends files
and slash commands.

One Go binary in a container, the front end embedded with `embed`, no npm in
the project.

- [What it does](#what-it-does)
- [How it works](#how-it-works)
- [Sign-in](#sign-in)
- [What the machine needs](#what-the-machine-needs)
- [Stack](#stack)
- [The tree](#the-tree)
- [Next](#next)

## What it does

| Screen | What is on it |
|---|---|
| **Sessions** | live sessions by contour and project — who waits for you, who works, who is quiet — with the model, how full the context is and what each one does; the conversation as a feed — messages, thinking, tool calls, letters from other sessions and subagents, attachments — with the timeline of its work beside it; the composer, typed or dictated, with files, a queue and a command after `!`; the question sheet — an option, several, your own words, a note, or dismiss it; the permission dock; stopping the turn, a piece of background work or a subagent, and the feeds of subagents and workflows; the checklist the session keeps; its briefs and published pages; slash commands and the screens of `/mcp`, `/status`, `/hooks` and the like; the model, the effort and the mode picked from a list; Remote Control switched on and off; the session terminal; a window on the host desktop; an archive of closed conversations, with resume. A session lives in tmux or on the stream (`claude -p` under a holder) and moves between them; one in tmux is watched as a feed or as its terminal |
| **Containers** | the "stack → containers" tree with processor, memory and size on disk; logs, starting and stopping a container, bringing a whole stack up and down |
| **Machine** | processor, memory, disks, network, top processes, checks on ports and external services; history with rollups by minute and by hour, charts from half an hour to a month |
| **Profile map** | "contour → group → project": where sessions may be opened, with what parameters, in which claude account. Each has a settings page drawn from a schema of the launch parameters, with the command the next launch runs; every change goes into a journal of the map that can take a deleted project back |
| **Repository** | the code of a project, read from the conversation that writes it: the run of what the branch changed, the tree of the working tree, a file in windows, the diff of one file with the two layers told apart — what is in a commit and what is not — who wrote each line and in which conversation, and notes on lines handed to the session |
| **Briefs** | long pieces a session publishes with questions in them, read and answered at whatever length it takes; the answers go back to the session as a message |
| **Usage** | transcripts broken down: what went out over a day, by model, by tool, by account, by project and session; the 5-hour and weekly limits of every account |
| **Alerts** | rules with thresholds and degradations against a container's own norm, acknowledged from the screen; pushes to devices (Web Push, VAPID), chosen by kind and by source, with a button in the push that quiets its source |
| **Journal** | everything the panel did on the host: who, when, from which device, how it ended |

The panel's own container is not shut down from here, and a push about a
session does not go to the device where that session is open right now.

**Dictation into the composer** is off until it is switched on for a device, in
Settings. With nothing to send the button is a switch — one press starts
listening, the next stops it, and what was heard stays in the field to be read
over before it goes; with words in it the button sends, and only a press held
talks. The recognition is the browser's own, which in Chrome means the recording
goes to Google: it is the one thing a page of the panel sends out of the
machine, and the switch says so before it is flipped. The panel reaches out on
its own for three things: notifications through the push service, each account's
model catalogue from the Anthropic API with that account's token, and the probes
of external services it watches — liveness checks without a login.

## How it works

Three processes, and this is the main decision of the project: **their sets of
rights are opposite, so they have no business being in one process**.

| Process | Where it lives | What it can do |
|---|---|---|
| `aacpanel` | container, distroless | nothing on the host; docker read-only through socket-proxy |
| `aacpanel-agent` | host, system unit | reading only, no graphics and no docker |
| `aacpanel-exec` | host, user unit | `docker.sock` and the graphical session; transcripts closed off explicitly |

The service faces the internet — so it must not read the conversation with the
model. The collector reads transcripts — so it must not run programs. The
executor runs programs — so transcripts are closed to it.

```
phone ──https──► aacpanel (container)
                    │
                    ├──► socket-proxy (GET-only) ──► docker.sock
                    ├──► Postgres (history, profile map, journal, devices)
                    ├──► host snapshot  ◄── aacpanel-agent (host)
                    └──► unix socket    ──► aacpanel-exec (host) ──► docker, tmux, claude
                                                       │
                                                       └──► aacpanel-exec -hold ──► claude -p (the stream)

claude (any session) ──stdio──► aacpanel-exec -mcp: the panel's tools for the session
```

The service passes the executor a structure, not a string for a shell. An
unknown action is rejected, a container name is checked against the list docker
gives. Even full capture of the container yields exactly the list of actions
written down in `internal/action/action.go`.

The boundaries between the processes, and what the panel deliberately does not
have, are in [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Sign-in

There are no passwords. The main door is a **passkey (WebAuthn)**: the first
device is enrolled with a one-time code from the machine itself, the next ones
with a code from the panel. The second door, a **long-lived token**, is for
where a passkey cannot work at all: a browser hands out a key only over https or
on localhost.

The session lives in a signed cookie: two deadlines, idle and absolute, both
mandatory. Devices are revoked one at a time; changing the signing key signs
everyone out at once.

The panel answers on several addresses at once — the domain, the local network
with its own certificate, a tailnet node, localhost — and after signing in the
client goes for data to the nearest one that answered.

## What the machine needs

| What | Without it |
|---|---|
| Linux with systemd | there is nothing to install |
| docker + compose v2 | there is no panel |
| tmux | no session will open |
| python3 | every screen is empty |
| jq | the status line writes nothing: the limits of an account wait for the executor's probe, and a model changed in tmux shows only with its next request |
| claude | there is nothing to show |
| a terminal (`konsole`, `gnome-terminal`, `alacritty`…) | no windows; the normal mode for a machine without graphics |

Postgres does not have to be installed, it arrives as a container, and neither
does Go: the installer downloads its own into a cache and builds itself and the
executor with it. On Debian, Ubuntu and their kin it offers to install docker,
compose, tmux and jq with apt as well:

```bash
git clone https://github.com/BeMySlaveDarlin/aacpanel ~/aacpanel && ~/aacpanel/install.sh
```

What it asks, what it changes, and each of its steps done by hand —
[`INSTALL.md`](INSTALL.md).

## Stack

**The service** — Go, the standard library plus `pgx`, `go-webauthn`, `esbuild`
as a library, and two the viewer needs: `go-gitdiff` to cut a diff into hunks and
`chroma` to colour it. The colouring is done here rather than in the browser —
it costs about three megabytes of the image and nothing on the wire, where a
line travels as a run length and a class number. The image is multi-stage and ends at `distroless`;
`govulncheck` sits inside the build, not in the memory of whoever builds it.

**The front end** — preact and htm, vendored as files; uPlot for charts, xterm
for the terminal. There is no `package.json` and there will not be: the bundle
is assembled by `cmd/webbuild` with the same toolchain as the service and
embedded into the binary. The fonts lie next to it and go into the service worker
precache — the page has no external domains at all. The panel installs as a PWA
and works offline as far as that makes sense.

**The database** — Postgres in its own container. Metrics are partitioned,
retention detaches whole partitions. The service works under a role that cannot
wipe its own journal; DDL goes through a second connection under the schema
owner.

**The collector** — python3 with no dependencies, reads `/proc` and claude
transcripts, writes a snapshot that the container mounts read-only.

## The tree

| Path | What for |
|---|---|
| `cmd/aacpanel/` | the service: pages, API, streams, the action gate |
| `cmd/aacpanel-exec/` | the executor of actions on the host |
| `cmd/webbuild/` | the front-end build |
| `cmd/aacpanel-install/` | the installer behind `./install.sh`: its commands and flags |
| `internal/install/` | the installer's engine: the check of the machine, the questions, the steps with their manifest and undo; `view/` and `ui/` draw it |
| `internal/action/` | the protocol between the service and the executor: the closed list of actions |
| `internal/executor/` | the actions themselves: containers, stacks, sessions, windows |
| `internal/launcher/` | starting a claude session: environment, tmux, window, the panel's MCP server |
| `internal/stream/` | the holder of a session on the stream: `claude -p` on the stream protocol |
| `internal/mcp/`, `internal/toolset/` | the panel's MCP server and the one list of its tools: the checklist, the brief, the call, the restart, the letter, the new session |
| `internal/schema/` | the launch parameters of the map as data: levels, options, effective values with their layers |
| `internal/contours/` | the layout of claude contours: accounts and their directories |
| `internal/repo/` | the repository of a project for a screen: tree, files, history, notes |
| `internal/auth/` | passkeys, enrolment codes, cookie, token |
| `internal/store/` | Postgres: pool, migrations, partitions, writing metrics |
| `internal/chat/`, `internal/usage/` | the conversation feed and token usage |
| `internal/rules/`, `internal/notify/` | alert rules and pushes |
| `web/` | the front end and `embed.go`, which embeds what was built |
| `web/check/` | front-end checks: sources and styles are read as text |
| `agent/` | the host collector of metrics and sessions |
| `deploy/` | schema migrations, systemd units, additions to claude, the stand |

## Next

- [`INSTALL.md`](INSTALL.md) — `./install.sh`, and what each of its steps does, for doing it by hand.
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — how it is built, the boundaries and what the panel does not have.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — build, checks, what is expected of a change.
- [`AGENTS.md`](AGENTS.md) — the same for a coding agent, plus the traps of this code.

## License

MIT — [`LICENSE`](LICENSE).
