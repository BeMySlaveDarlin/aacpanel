# Install

Step by step, from scratch. The order of the steps is mandatory: the machine
description is read by all three processes at startup, `.env` is needed by
compose, the database comes before the application role, linger before the first
`systemctl --user` command, the units before building the executor.

It takes half an hour, of which the first image build is minutes.

The notation below: `<repo>` is where the repository is cloned, `<user>` is your
unix user (`id -un`), `<home>` is the home directory, `<uid>`/`<gid>` are `id -u`
and `id -g`, `<runtime>` is `$XDG_RUNTIME_DIR` (usually `/run/user/<uid>`),
`<state>` is the state directory, `/var/lib/aacpanel` by default.

---

## 1. What appears on the machine

| What | Where | Without it |
|---|---|---|
| `aacpanel` | container | there is no panel |
| `aacpanel-db` | container, Postgres | the service will not come up |
| `aacpanel-socket-proxy` | container | the container tree is empty |
| `aacpanel-agent` | systemd system unit, python from the working tree | every screen is empty |
| `aacpanel-exec` | systemd user unit, the Go binary `<home>/bin/aacpanel-exec` | the panel only shows, not a single button |

Smaller traces: the machine description `<state>/host.env`, the secrets in the
repository's `.env`, the question hook and the status line in the
`settings.json` of every claude account (with the hooks from `deploy/claude`
you choose, §10), the panel's MCP server in the accounts whose sessions are
started by hand, the executor's state in `<home>/.local/state/aacpanel` and
`<home>/.local/state/aacpanel-stream`, `loginctl enable-linger` for your user.

---

## 2. Dependencies

| What | Check | Without it |
|---|---|---|
| Linux with systemd | `systemctl --version` | there is nothing to install |
| docker + compose v2 | `docker compose version` | the service and the database will not come up |
| docker without sudo | `docker version` — the call itself, not `command -v` | half the steps will need rights |
| Go | `go version` | the executor cannot be built |
| tmux | `tmux -V` | **no session will open**, in the console or in the feed: a console lives in tmux and the window is only attached to it, and a feed session goes into tmux when it is moved to the console |
| python3 | `python3 -V` | the collector will not start, the panel is blind |
| jq | `jq --version` | the status line writes nothing: the limits of an account wait for the executor's probe, and a model changed in a console shows only with its next request |
| curl, git, openssl | `curl -V`, `git --version`, `openssl version` | the checks below are browser-only; the secrets in §6 are drawn by `openssl` |
| claude | `claude --version` | there is nothing to show |
| a terminal | `command -v konsole` (or your own) | there will be no windows onto sessions — the normal mode for a machine without graphics |

On Debian and Ubuntu:

```bash
sudo apt install docker.io golang-go tmux python3 jq curl git openssl
sudo apt install docker-compose-v2   # Ubuntu; on Debian the package is docker-compose
sudo usermod -aG docker "$USER"      # then log in again
```

**About Go.** `go.mod` names a recent language version, and distributions ship an
older one. With the default `GOTOOLCHAIN=auto` the first build downloads the
toolchain it needs; with `GOTOOLCHAIN=local` it fails complaining about the
version.

**About claude.** It is installed by Anthropic's official instructions, and it
needs Node 22 or newer — npm will install the package on an older version too,
warning with `EBADENGINE`, and the failure surfaces later, in use. After
installing, one run of `claude` is needed to sign in: the panel sees only a
signed-in claude.

---

## 3. The clone goes somewhere permanent

```bash
git clone <repository address> ~/aacpanel
cd ~/aacpanel
```

The path is remembered in the machine description and in the collector's unit:
**the collector starts straight from this tree**, not from an image. Moving the
directory is a reinstall, so clone it where the repository is going to stay.

---

## 4. Decide where the panel is opened from

One answer sets several variables at once.

| Way | What is needed beforehand | Sign-in | Address |
|---|---|---|---|
| **From this machine only** | nothing | passkey (localhost is a trusted context) or token | `http://localhost:8776` |
| **Local network without a certificate** | the machine's address on the network | **token only** | `http://192.168.x.x:8776`; the cookie has no `Secure`, PWA and pushes do not work |
| **Tailscale** | a tailnet account with MagicDNS and HTTPS, the tailnet name, a one-time auth key; the Tailscale app on the phone | passkey and token | `https://<node>.<tailnet>.ts.net` |
| **Your own domain** | DNS pointing at the machine or at a proxy, TLS on the proxy | passkey and token | `https://panel.example`, PWA, pushes |

If in doubt — **tailscale**: the only way to get real https without a domain and
a public address, and everything the panel can do works there. And never turn on
`tailscale funnel` with it: it puts out into the open internet something
that can type text into the terminal of a live session.

**This is a door, not the only address there will ever be.** The panel answers on several
addresses at once, and the client chooses between them itself. The steps below
set up one address; the rest are added later, each with its own variables.

A passkey lives only on https and on localhost — that is a browser limitation.
The passkey domain (`AACP_RP_ID`) is baked into every key enrolled: change it
after the first sign-in and you void every key at once.

---

## 5. The state directory and the machine description

The machine description is the single place all three processes look at: the
service in the container, the collector and the executor.

```bash
sudo install -d -m 0755 -o "$USER" -g "$USER" /var/lib/aacpanel
install -m 0644 deploy/host.env.example /var/lib/aacpanel/host.env
$EDITOR /var/lib/aacpanel/host.env
```

The template is commented for every key. What must be filled in:

| Key | Value |
|---|---|
| `AACP_HOST` | what to call the machine in the panel's texts; inside a container `hostname` is the container identifier, so this field is not left empty |
| `AACP_REPO` | `<repo>` — the collector starts from here |
| `AACP_UNIX_USER` | `<user>` — whose home directory and graphical session |
| `AACP_HOME_SESSION` | the name of the machine's home session — the one that lives in the home directory |
| `AACP_STATE_DIR` | `<state>`; if it moved, it is changed in `.env` as well |
| `AACP_TERMINAL` | the terminal invocation template; **an empty value is a lawful answer** meaning "do not open windows", and it is written explicitly |
| `AACP_LANG` | the locale of sessions, from `locale -a`; empty gives `C.UTF-8` |
| `DISPLAY` | the display of the graphical session; empty on a machine without graphics |
| `AACP_PROJECT_ROOTS` | the roots inside which sessions may be opened, separated by `:` |

A missing key and an empty one are different things: without `AACP_TERMINAL`
windows are opened by the built-in default, while an empty value means "there
are no windows". That is why both keys are always written.

The rest of the keys in the template are optional: whether to open a window
together with a session (`AACP_TERMINAL_AUTO`; a disabled auto-start does not
touch the button in the panel), the directories of several claude accounts
(`AACP_CLAUDE_HOME`), your own launch wrapper (`AACP_CLAUDE`), the roots to walk
the disk with (`AACP_PROJECT_SCAN`), the port checks (`AACP_PROBE_PORTS`), how
many processes parse transcripts for usage (`AACP_USAGE_WORKERS`).

One key the executor reads is not in the template: `AACP_LIMITS_EVERY`, how old
the snapshot of an account's subscription limits may grow before the executor
renews it with a probe — a `claude -p` on haiku that says one word, a share of
the very limit it reads. Ten minutes by default, in Go duration form (`30m`),
never under a minute; an account with a terminal open keeps its snapshot fresh
through the status line and is not probed.

`AACP_CLAUDE_REGISTRY` points at the registry of a contour router, if the
machine has one: a line an account, `name | prefix | config dir | token file`,
where the prefix is the directory a session must start under to go into that
account and `*` takes every directory no other line does. The collector reads
the accounts from it after `AACP_CLAUDE_HOME`, and a new contour on the map is
taken from them — without a registry a contour's paths are typed by hand.

---

## 6. Secrets and variables in `.env`

```bash
cp .env.example .env && chmod 0600 .env
sed -i "s|^#\?AACP_SECRET=.*|AACP_SECRET=$(openssl rand -hex 32)|" .env
sed -i "s|^#\?AACP_DB_PASSWORD=.*|AACP_DB_PASSWORD=$(openssl rand -hex 24)|" .env
```

Further on in the same file:

| Key | Value |
|---|---|
| `AACP_UID`, `AACP_GID` | `<uid>` and `<gid>`: the service runs under them, and they must match the owner of the executor's socket |
| `AACP_EXEC_DIR` | `<runtime>/aacpanel-exec` |
| `AACP_STATE_DIR` | `<state>`, if it is not the default: compose does not read the machine description |
| `AACP_TERM_PUBLIC` | `1` or `0` — whether to show the live session terminal to someone who signed in from outside |

And the variables of the way chosen in §4:

| Way | What to set |
|---|---|
| this machine only | `AACP_RP_ID=localhost`, `AACP_RP_ORIGINS=http://localhost:8776` |
| tailscale | `AACP_RP_ID=<node>.<tailnet>.ts.net`, `AACP_RP_ORIGINS=https://<the same>`, `AACP_TAILSCALE=1`, `AACP_TS_HOSTNAME=<the first label of the name>` |
| local network | `AACP_BIND=<the interface address>`, `AACP_SECURE=0`, `AACP_TOKEN=$(openssl rand -hex 24)` |
| your own domain | `AACP_RP_ID=<domain>`, `AACP_RP_ORIGINS=https://<domain>`; if the proxy is on another machine, `AACP_BIND` as well, with the address it reaches |

`AACP_SECURE=0` means the cookie will travel over plain http too. It only has to
be set where the panel is opened at an address without a certificate: otherwise
the token is accepted, 200 comes back, and an empty sign-in screen returns — the
browser dropped the cookie silently.

`AACP_TOKEN` is the second door next to the passkey. An empty value means there
is no such door; on a domain that is how it should be.

The local listener on `127.0.0.1:8777` is on unless `.env` says
`AACP_LOCAL_ADDR=` (empty). It lets in any process of the machine without a
sign-in, and the sessions rely on it: their restart, their letters to one
another and the note about an unsent brief go through it. Close it only on a
machine whose other users must not drive its sessions, and those go with it.

---

## 7. Linger and the socket directory

Compose mounts the executor's socket directory into the service, and it must
exist before the bring-up. It lives in `<runtime>`, and that one is created by
logind — on a machine nobody has logged into it does not exist until linger is
on. It is also needed by any `systemctl --user` command.

```bash
loginctl show-user "$USER" --property=Linger     # Linger=yes — already on
sudo loginctl enable-linger "$USER"
mkdir -m 0700 -p "$XDG_RUNTIME_DIR/aacpanel-exec"
stat -c '%u %a' "$XDG_RUNTIME_DIR/aacpanel-exec" # <uid> 700
```

If the directory is there but belongs to another uid, docker created it as root
during an early bring-up: `sudo rmdir`, create it again and after §8 recreate
the container (`docker compose up -d --force-recreate aacpanel`).

---

## 8. Bring the panel up

```bash
docker compose up -d                 # the first image build takes minutes
docker compose ps                    # aacpanel, aacpanel-db, aacpanel-socket-proxy: running/healthy
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8776/healthz   # 200
curl -s http://127.0.0.1:8776/auth/methods                               # which doors are open
```

The service rolls out the database schema itself on the first start. If it is
not 200 — look at `docker compose logs aacpanel` and go no further.

---

## 9. The application role in the database

The service works under this role in production: it has neither DDL nor
`TRUNCATE`, that is, it cannot wipe the action journal — and the immutability
triggers do not hold the table owner.

```bash
p=$(openssl rand -hex 24)
AACP_APP_PASSWORD=$p ./deploy/create-app-role.sh
sed -i "s|^#\?AACP_APP_ROLE=.*|AACP_APP_ROLE=monitor_app|" .env
sed -i "s|^#\?AACP_APP_PASSWORD=.*|AACP_APP_PASSWORD=$p|" .env
unset p
docker compose up -d aacpanel        # recreate: the service DSN changes to the role
docker compose logs --since 30s aacpanel | grep -ci 'authentication failed'   # 0
```

The existing lines of the template are edited, new ones are not appended: with
two lines for one key the last one wins, and they part silently.

The role name and the password are set **together**: compose assembles the DSN
from them separately, and a password without a name gives a connection as the
owner with somebody else's password — the service comes up, `healthz` answers
200, and it is not connected to the database.

The role's rights are issued by the service at every startup, not by a
migration. An empty `AACP_APP_PASSWORD` is a working state: the service goes as
the owner.

---

## 10. Wiring up claude

Two lines go into every account. `settings.json` is edited **as JSON**, not as
text: your own hooks are in that file and they must stay. The files are
`<home>/.claude/settings.json` and one like it in every account directory from
`AACP_CLAUDE_HOME` or the contour registry, if there are several accounts.

**The question hook.** A session in the console reports a question to the panel
only through it: the record of the call reaches the transcript after the
answer, and without the hook the card never arrives. A session on the stream
hands its question to its holder as a request, hook or not.

```json
{"hooks": {"PreToolUse": [
  {"matcher": "AskUserQuestion",
   "hooks": [{"type": "command", "command": "python3 <repo>/agent/ask-hook.py", "timeout": 5}]}
]}}
```

**The status line.** Claude says the 5-hour and weekly percentages of the
subscription to the status line of a terminal, and every account has its own.
The script writes them for its account, at most every twenty seconds, together
with the model and the effort the console runs — what the list of sessions and a
move to the feed read. Without it the limits come only from the executor's
probe (§5, `AACP_LIMITS_EVERY`). The script goes first in the chain and passes
the payload on to the previous command:

```json
{"statusLine": {"type": "command",
  "command": "AACP_STATUSLINE_NEXT='<the previous command>' bash <repo>/agent/rate-snapshot.sh"}}
```

If there was no status line of your own, it is simply
`"bash <repo>/agent/rate-snapshot.sh"`.

The check:

```bash
jq '.hooks.PreToolUse[] | select(.matcher=="AskUserQuestion") | .hooks[].command' ~/.claude/settings.json
jq '.statusLine.command | contains("agent/rate-snapshot.sh")' ~/.claude/settings.json    # true
```

### Optional additions

The panel works entirely without them — this is convenience for sessions, not
the panel's eyes.

| What | Where | What for |
|---|---|---|
| `deploy/claude/prompt-stamp.py` | the `UserPromptSubmit` and `PostToolBatch` hooks | the session knows today's date, how much context is left and where its project's context cap falls, with an alarm past it — or, where the project restarts its sessions, where the restart comes, with no alarm and no word to the person — the limits of its account and the load of the machine |
| `deploy/claude/cost-snapshot.py` | the `Stop` and `SubagentStop` hooks | a line per turn in `<account>/logs/cost.jsonl`: where the tokens went |
| `deploy/claude/artifact-copy.py` | the `PostToolUse` hook on `Artifact` | the panel keeps a copy of every page a session publishes and shows it without the account it went out under; without the hook the card has only its link |
| `deploy/claude/brief-waiting.py` | the `SessionStart` hook | a session that starts in a project where a brief is answered and unsent hears about it, since the session that asked is usually gone by then |
| `deploy/claude/context-guard.py` | the `Stop` hook | past its context cap a session with Auto restart finalizes and restarts itself; the cap and the switch are set in the panel, per contour or per project |
| `deploy/claude/checklist-reminder.py` | the `Stop` hook | a session that keeps a checklist with the panel's checklist tool and did work without touching it is asked once, at the end of the turn, to update the checklist if it changed |
| the panel's MCP server | `<account>/.claude.json`, by `claude mcp add` | the panel's tools in a session started by hand, not by the panel: see **Panel tools** below |
| `aacpanel-docker-gc.{service,timer}` | `<home>/.config/systemd/user/` | once a week: build cache older than two weeks and untagged images |

Cleaning the cache:

```bash
install -Dm644 deploy/systemd/aacpanel-docker-gc.service ~/.config/systemd/user/aacpanel-docker-gc.service
install -Dm644 deploy/systemd/aacpanel-docker-gc.timer   ~/.config/systemd/user/aacpanel-docker-gc.timer
systemctl --user daemon-reload && systemctl --user enable --now aacpanel-docker-gc.timer
```

An addition that a script of your own already does on this machine is not
installed on top: two hooks on one event give two stamps in every message.

`deploy/claude/restart-via-panel.py` is not a hook but the restart of the
`session_restart` tool for a script or a skill run inside a session: it names
the session by `CLAUDE_CODE_SESSION_ID`, asks the panel over its local listener
to bring it back as its project, and asks nothing while the session's agents,
workflows or background commands are at work (`--anyway` asks all the same). It
exits 0 when the panel took the restart, 1 when it refused or did not answer, 2
when the work of the session holds it off.

**Copies of published pages.** The hook goes into the account settings of every
account whose sessions publish artifacts, and it is the same line everywhere:

```json
{"hooks": {"PostToolUse": [
  {"matcher": "Artifact",
   "hooks": [{"type": "command", "command": "python3 <repo>/deploy/claude/artifact-copy.py", "timeout": 10}]}
]}}
```

Without it the panel still shows the card and its link; with it the card opens
the page itself, for anyone who can see the panel. The copies are swept by age,
by count and by the room they take together, and the hook says nothing on a
machine where the panel is not installed.

**The context guard.** The hook sits in the account settings; the context cap
and Auto restart are set in the panel, in the launch parameters of the contour
or the project. The executor keeps what the map says of every project and
contour in `~/.local/state/aacpanel/guards.tsv` (under `$XDG_STATE_HOME` where
it is set), a line a place, and the hook finds a session's place by the project
directory claude names for its hooks — not by the working directory, which
follows every `cd` of the agent: a session started by hand in a project is
guarded like one the panel brought up, and a change in the panel holds from the
next turn. The cap alone
restarts nothing — the prompt stamp names it as the point to wrap up. With Auto
restart on, the stamp names it as where the restart comes instead, raises no
alarm and does not speak up between prompts as the context grows, and a session
past it is told to finalize and restart itself silently, without a word to the
person about the context. The switch does nothing where the account does not
have the hook.

```json
{"hooks": {"Stop": [
  {"hooks": [{"type": "command", "command": "python3 <repo>/deploy/claude/context-guard.py", "timeout": 5}]}
]}}
```

It speaks only at the end of a turn, when the session is free: no question and
no permission prompt can be open then, and nothing is typed into the session
from outside. The fill comes from the collector's snapshot, the same one the
stamp reads, and a model whose window is not known does not trigger it. The
restart goes through the panel's `session_restart` tool, which asks the panel
over its local listener (`AACP_PANEL_URL`) to bring the session back as its
project, without `continue`, so the new session starts with an empty context.
A session the panel started has the tool and is allowed it; one started by
hand has it once its account has the panel's server, and is allowed it once
the account says so (**Panel tools** below) — until then the restart past the
cap waits on a permission prompt. With the local listener off, a session cannot
restart itself. The turn after the block is the finalization itself and is
never blocked again; a session that ignored it is told again at the end of its
next turn.

**Panel tools.** Every session the panel starts gets them from the launcher
unless the Panel tools parameter of its contour or project is off: the
checklist, the brief (`brief_publish`, `brief_delete`), the call to the person
(`notify`), the restart of the session (`session_restart`) and the letter to
another session (`send_to_session`). They are the tools of one MCP server, the
executor started as `aacpanel-exec -mcp`. Rules of the machine's own for the
text of a brief go into `${XDG_CONFIG_HOME:-~/.config}/aacpanel/brief-guide.md`:
the brief tool gives them to a session after the shipped ones, and without the
file there are none.

A session started by hand, not by the panel, gets the same tools from its
account. One command an account adds the server at the user level, for every
project of that account; it only writes the command down, so it may go before
the executor is built in §12, and the server comes up with the next session:

```bash
claude mcp add --scope user aacpanel -- <home>/bin/aacpanel-exec -mcp
CLAUDE_CONFIG_DIR=<account> claude mcp add --scope user aacpanel -- <home>/bin/aacpanel-exec -mcp   # every other account
claude mcp get aacpanel          # Scope: User config, and Connected once the executor is built
```

The name is `aacpanel` and no other: claude calls the tools
`mcp__aacpanel__<tool>`, and the permission rules below name them so. Where the
state directory is not `/var/lib/aacpanel`, the server is told it after the
name, `-e AACP_STATE_DIR=<state>`. A second `add` of the same name is refused,
so a new path goes in after `claude mcp remove aacpanel -s user`. A session the
panel starts in such an account still has one server: claude keeps one server
of a name, and the launcher's, given on the command line, takes the place of
the account's.

A session the panel starts is allowed the checklist, `brief_publish`,
`brief_delete`, `notify` and `session_restart`, and asks the person before
`send_to_session` as before any tool nobody allowed. A session started by hand
is allowed nothing of the server until its account says so, in
`permissions.allow` of `<account>/settings.json`:

```json
{"permissions": {"allow": [
  "mcp__aacpanel__checklist", "mcp__aacpanel__brief_publish",
  "mcp__aacpanel__brief_delete", "mcp__aacpanel__notify",
  "mcp__aacpanel__session_restart"]}}
```

These five are the ones the panel allows. The restart is among them because
the restart past the context cap is done with nobody at the screen; the price
is that the model restarts its own session without asking whenever it decides
to, while its agents and background commands still hold the restart off.

**The checklist reminder.** The checklist tool needs nothing installed: every
session the panel starts gets it from the launcher, unless the Panel tools
parameter of its contour or project is off. The hook is the soft half of it,
in the account settings:

```json
{"hooks": {"Stop": [
  {"hooks": [{"type": "command", "command": "python3 <repo>/deploy/claude/checklist-reminder.py", "timeout": 5}]}
]}}
```

It holds the end of a turn only when the session — its account, its directory
and its name — has a checklist with steps pending or at work, the turn called
tools, and the checklist was not written during it; the model is told to send
the checklist if it changed, to clear it if it no longer applies, and otherwise
to end the turn, and the turn after the hold is never held. Its price is one
short turn more when the model forgot. Only a session with the checklist tool is
asked: one that sent the checklist itself, or one started with the tool allowed,
as the panel starts every session — so a session started again under its name
is asked about the checklist it found. A claude started by hand in the same
place, without the tool, is asked nothing.

**Restart and letters.** The tools `session_restart` and `send_to_session` ask
the panel over its local listener (`AACP_PANEL_URL`, `http://127.0.0.1:8777` by
default), so with the listener off a session can neither restart itself through
them nor write to another.

---

## 11. systemd units

**The executor is a user unit**, because it needs the session bus and graphics.
**The collector is a system one**, because it reads transcripts and is cut down
to reading.

```bash
install -Dm644 deploy/systemd/aacpanel-exec.service ~/.config/systemd/user/aacpanel-exec.service
sudo install -Dm644 deploy/systemd/aacpanel-agent@.service /etc/systemd/system/aacpanel-agent@.service
```

If the state directory is not `/var/lib/aacpanel`, fix the `EnvironmentFile=`
line in **both** installed units, and `ReadWritePaths=` in the collector's unit
as well. Only those lines, everything else as shipped.

```bash
systemctl --user daemon-reload && systemctl --user enable aacpanel-exec.service   # without --now: there is no binary yet
sudo systemctl daemon-reload && sudo systemctl enable --now "aacpanel-agent@$USER.service"
sleep 10 && stat -c %y /var/lib/aacpanel/state.json      # the file appeared and is fresh
```

If the collector did not come up — `journalctl -u "aacpanel-agent@$USER" -n 30`;
most often it is a wrong `AACP_REPO` in the machine description.

---

## 12. Build and start the executor

The service travels as an image, the executor as a separate binary. Built from
the wrong version of the tree, it answers the panel with "unknown action" while
the button is alive.

```bash
go build -o ~/bin/aacpanel-exec ./cmd/aacpanel-exec
systemctl --user start aacpanel-exec.service
sleep 2 && ~/bin/aacpanel-exec -list          # the list of actions of this machine
```

The list is shorter than the full one — the executor says what it is missing:
without tmux all the actions over sessions and windows go away.

---

## 13. The check

Five links, and different things fix them.

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8776/healthz   # 1. the service: 200
echo $(( $(date +%s) - $(stat -c %Y /var/lib/aacpanel/state.json) ))      # 2. the collector writes: seconds < 60
~/bin/aacpanel-exec -list | head -3                                      # 3. the binary knows the actions
stat -c '%u %F' "$XDG_RUNTIME_DIR/aacpanel-exec/sock"                    # 4. the socket: uid = AACP_UID, "socket"
docker compose logs aacpanel | grep -c 'store: connected'                # 5. the database: not 0
```

| Silent | Where to look |
|---|---|
| 1 | `docker compose ps`, `docker compose logs aacpanel`; the wrong `AACP_BIND` |
| 2 | `systemctl status "aacpanel-agent@$USER"`, `journalctl -u "aacpanel-agent@$USER" -n 50` |
| 3 | the build, §12 |
| 4 | no socket — `systemctl --user status aacpanel-exec`; a foreign uid — `AACP_UID` in `.env` does not match the owner, and the service will not open a `0600` socket |
| 5 | `AACP_APP_ROLE` and `AACP_APP_PASSWORD` — both or neither; `docker compose ps aacpanel-db` healthy |

---

## 14. The first sign-in

The enrolment code for the first device is printed by the binary on the machine
itself — there is no other way in:

```bash
docker compose exec aacpanel /aacpanel -enroll
```

The code lives five minutes and works once. Then open the panel at your address,
enrol a passkey (or sign in with the token) — and the second device is already
enrolled with a code from the panel itself.

The "contour → group → project" map is set up after signing in, on the
Profiles tab of the phone or from Projects in the sessions section at a desk:
until there is a first contour the projects screen is empty, and taking one is
the first thing done there — a contour is taken from an account the machine
already has, found by the collector; its paths are typed by hand only on a
machine without a contour router. A project lives in the console unless its
launch parameters put it in the feed.

What cannot be checked with a command:

1. the containers screen: the row of figures and the list;
2. the sessions screen: the live sessions of this machine;
3. opening a session from the panel and seeing it in the list, and the window on
   the desktop;
4. sending a message and seeing it in the feed;
5. asking a session in the console a question through `AskUserQuestion` and
   seeing the card on the phone — the only check of the question hook;
6. the 5-hour and weekly limits of the account on the sessions screen: the
   executor's probe writes them shortly after its start, and the status
   line keeps them fresh while a terminal of the account is open.

---

## 15. Access from outside

- **Domain.** A reverse proxy: `https://<domain>` → `127.0.0.1:8776` (or the
  address from `AACP_BIND`) with the headers passed through; a sample
  configuration is `deploy/nginx-aacpanel.conf`. Check your own
  `client_max_body_size`: nginx defaults to a megabyte, while a file from a
  phone reaches 32 MB, and it gets cut at the proxy before ever reaching the
  panel.
- **Tailscale.** The `aacpanel-tailscale` container comes up when
  `AACP_TAILSCALE=1`. Signing in works only with a one-time key (`TS_AUTHKEY` in
  `.env` before the container is brought up): signing in by the link from the log
  does not get through, because `containerboot` waits a minute for confirmation
  and starts again with a new node key. The tailnet needs MagicDNS and HTTPS.
  Check it from inside the container (`docker exec aacpanel-tailscale tailscale
  status`) or from a device in the tailnet: the node is in userspace mode and is
  not visible from outside. Do not turn on `funnel`. Where the panel has a
  domain, the node is a further way in and proxies to a listener of its own, so
  the panel knows where a request came from and the address map offers it:
  `AACP_TS_SERVE=./deploy/tailscale/serve-leg.json`, `AACP_TS_ADDR=:8778`,
  `AACP_TS_URL=https://<node>.<tailnet>.ts.net`.
- **Local network over TLS.** The panel can listen on the local network with a
  real certificate alongside the domain, and then both passkeys and PWA work
  there: `AACP_LAN_ADDR`, `AACP_LAN_URL`, `AACP_LAN_BIND`, `AACP_LAN_PORT` plus
  `AACP_TLS_DIR`, `AACP_TLS_CERT`, `AACP_TLS_KEY`. TLS is held by the service
  itself, nginx is not needed. The certificate can only be had over DNS-01: a
  public CA will not issue a certificate for an address on a local network, and
  the name in DNS is needed for exactly that.

---

## 16. Updating

```bash
git pull
docker compose up -d --build                                     # the service as an image
go build -o ~/bin/aacpanel-exec.new ./cmd/aacpanel-exec           # the executor as a binary,
mv ~/bin/aacpanel-exec.new ~/bin/aacpanel-exec                    #   put in place by a rename
systemctl --user restart aacpanel-exec.service
sudo systemctl restart "aacpanel-agent@$USER.service"             # the collector travels from the tree
```

The executor is built beside itself and renamed into place: the holders of
sessions on the stream run from the same file, and building over a file a
process runs from answers "text file busy". A rename leaves the running
holders on the old copy until their sessions end, and the executor restarted
after it is the new one. So a session on the stream takes what the new build
brings to it only after its own restart, which keeps the conversation; until
then an operation its holder does not know is refused with those words.

Claude updates itself, and part of the stream protocol the feed leans on is not
public. When the feed of a session on the stream breaks after an update, the
stream contract says which request changed: it runs every request the feed
needs against the installed claude, in a directory of its own — a few minutes
and a dozen short turns on haiku:

```bash
python3 deploy/claude/stream-contract.py     # exit 0: every required check holds
```

Compare `.env` with
`.env.example` — keys of the template missing from `.env` are worth carrying over:

```bash
diff <(grep -oE '^#?[A-Z_]+=' .env.example | tr -d '#') <(grep -oE '^[A-Z_]+=' .env) | grep '^<'
```

The units are worth installing again if the shipped ones changed their
directives — with the same `EnvironmentFile=` edits as in §11. After an update — the check
from §13.

---

## 17. Removing the panel

```bash
systemctl --user disable --now aacpanel-exec.service aacpanel-docker-gc.timer
rm -f ~/.config/systemd/user/aacpanel-*.{service,timer} ~/bin/aacpanel-exec
systemctl --user daemon-reload
sudo systemctl disable --now "aacpanel-agent@$USER.service"
sudo rm -f /etc/systemd/system/aacpanel-agent@.service && sudo systemctl daemon-reload
docker compose down --rmi local
```

The rest is optional and one at a time, because it is data:

- `docker volume rm aacpanel_aacpanel-db` — the profile map, the journal, the
  history, the device passkeys;
- `docker volume rm aacpanel_aacpanel-ts-state` — the tailnet node keys;
- `<state>` — the snapshot, the session questions, the conversation index;
- what §10 put into every `settings.json` — the question hook, the status
  line, the hooks from `deploy/claude`, the rules allowing the panel's tools;
  delete the repository only after that — the hooks call files from the tree,
  and every session's question would fall over a path that is gone;
- `claude mcp remove aacpanel -s user` in every account that has the panel's
  server, or each session there starts with a server that fails to connect;
- `<home>/.local/state/aacpanel` and `<home>/.local/state/aacpanel-stream` —
  the guards of the map, the checklists of sessions, the answers to permissions
  kept for the feed;
- `sudo loginctl disable-linger "$USER"`, unless something else of yours needs
  user units without a login.

---

## 18. If it did not work

| Symptom | Where to look |
|---|---|
| the panel does not open | `docker compose logs aacpanel`, `docker compose ps` |
| the screens are empty, "the collector is not writing" | `journalctl -u "aacpanel-agent@$USER" -f`; most often a wrong `AACP_REPO` |
| "the executor is unavailable" | `journalctl --user -u aacpanel-exec -f`; linger; the socket must belong to the uid from `AACP_UID` |
| the panel is down and a session in the feed is needed | `~/bin/aacpanel-exec -sessions` names the sessions on the stream, `~/bin/aacpanel-exec -console <name>` moves one into tmux under the same account, then `tmux attach -t <name>`; the holders outlive a restart of the executor |
| the token is accepted, but the sign-in screen comes back empty | a cookie with `Secure` was dropped over http: an address without a certificate needs `AACP_SECURE=0` |
| the "cookie without Secure" chip in the header | the panel is opened over https while `AACP_SECURE=0` stayed: remove the line and recreate the container |
| a session opens without a window | an empty `AACP_TERMINAL` or a machine without `DISPLAY`; the button in the conversation header will open a window where there are graphics |
| the window opened, but it is not on the screen | `DISPLAY` points at a service display; the right one is named by `systemctl --user show-environment` |
| the limits of an account are empty or old | `journalctl --user -u aacpanel-exec` names a probe that failed; no `jq` or no status line leaves them to the probe alone |
| there is a button, and the journal says "unknown action" | the executor was built from an old tree: §12 |
| a session on the stream answers that it was started before the panel could do this | its holder runs the executor it was started with: restart the session, the conversation is kept |
| the feed of a session on the stream broke after claude updated itself | the stream contract names the request that changed: §16 |
| `Failed to connect to bus` from `systemctl --user` | linger is off, or you signed in without a logind session |
| the feed is visible from the phone, but there is no switch to the terminal | that is by design: `AACP_TERM_PUBLIC=0` |
| the projects screen is empty although there are projects on disk | not a single profile has been created: the button is on that same screen |
| tailscale: the node does not sign in, dead nodes pile up in the admin console | signing in by the link from the log does not work, `TS_AUTHKEY` is needed before the container comes up |

Boundaries that do not get fixed: passkeys, PWA and pushes work only in a secure
context — https or localhost. That is a browser's condition, not the panel's.
