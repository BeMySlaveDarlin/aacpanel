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
repository's `.env`, the hook and the status line in the `settings.json` of every
claude account, `loginctl enable-linger` for your user.

---

## 2. Dependencies

| What | Check | Without it |
|---|---|---|
| Linux with systemd | `systemctl --version` | there is nothing to install |
| docker + compose v2 | `docker compose version` | the service and the database will not come up |
| docker without sudo | `docker version` — the call itself, not `command -v` | half the steps will need rights |
| Go | `go version` | the executor cannot be built |
| tmux | `tmux -V` | **no session will open**: the conversation lives in tmux, the window is only attached to it |
| python3 | `python3 -V` | the collector will not start, the panel is blind |
| jq | `jq --version` | the subscription percentages in the header are empty |
| curl, git | `curl -V`, `git --version` | the checks below are browser-only |
| claude | `claude --version` | there is nothing to show |
| a terminal | `command -v konsole` (or your own) | there will be no windows onto sessions — the normal mode for a machine without graphics |

On Debian and Ubuntu:

```bash
sudo apt install docker.io docker-compose-v2 golang-go tmux python3 jq curl git
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
the disk with (`AACP_PROJECT_SCAN`), the port checks (`AACP_PROBE_PORTS`).

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

Two traces are mandatory — without them the panel is blind to questions and to
limits. `settings.json` is edited **as JSON**, not as text: your own hooks are in
that file and they must stay. The files are `<home>/.claude/settings.json` and
one like it in every directory from `AACP_CLAUDE_HOME`, if there are several
accounts.

**The question hook.** A session reports a question to the panel only through
it: the record of the call reaches the transcript after the answer, and without
the hook the card will never arrive.

```json
{"hooks": {"PreToolUse": [
  {"matcher": "AskUserQuestion",
   "hooks": [{"type": "command", "command": "python3 <repo>/agent/ask-hook.py", "timeout": 5}]}
]}}
```

**The status line.** The subscription percentages arrive only in the status line
payload, there is no other source, and every account has its own subscription.
The script goes first in the chain and passes the payload on to the previous
command:

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
| `deploy/claude/prompt-stamp.py` | the `UserPromptSubmit` and `PostToolBatch` hooks | the session knows today's date, how much context is left, the limits of its account and the load of the machine |
| `deploy/claude/cost-snapshot.py` | the `Stop` and `SubagentStop` hooks | a line per turn in `<account>/logs/cost.jsonl`: where the tokens went |
| `deploy/claude/context-guard.py` | the `Stop` hook | past the threshold a session finalizes and restarts itself; on only where a threshold is set — in the panel's launch parameters or in the project's settings |
| `deploy/claude/skills/restart-session/` | `<account>/skills/` | `/restart-session`: restarting the session in place |
| `deploy/claude/skills/cross-profile-message/` | `<account>/skills/` | a message to a session in another account; needed only where there are several accounts |
| `deploy/claude/skills/notify/` | `<account>/skills/` | `/notify`: the session calls the person to it, and the line arrives on their phone |
| `deploy/claude/skills/brief/` | `<account>/skills/` | `/brief`: the session publishes a long piece the person walks through in the panel, and the answers come back as a message |
| `aacpanel-docker-gc.{service,timer}` | `<home>/.config/systemd/user/` | once a week: build cache older than two weeks and untagged images |

Cleaning the cache:

```bash
install -Dm644 deploy/systemd/aacpanel-docker-gc.service ~/.config/systemd/user/aacpanel-docker-gc.service
install -Dm644 deploy/systemd/aacpanel-docker-gc.timer   ~/.config/systemd/user/aacpanel-docker-gc.timer
systemctl --user daemon-reload && systemctl --user enable --now aacpanel-docker-gc.timer
```

An addition that a script of your own already does on this machine is not
installed on top: two hooks on one event give two stamps in every message.

**The context guard.** The hook sits in the account settings, the threshold
with the project: a session finalizes and restarts itself only where the
percentage is named. It is named in one of two places — in the panel, in the
launch parameters of the profile or the project ("finalize when the context
fills up": the launcher puts `AACP_FINALIZE_AT` into the environment of the
session it brings up), or in the project's `.claude/settings.json` (or
`settings.local.json`), for a session started by hand. The panel's field does
nothing where the hook is not installed.

```json
{"hooks": {"Stop": [
  {"hooks": [{"type": "command", "command": "python3 <repo>/deploy/claude/context-guard.py", "timeout": 5}]}
]}}
```

The same threshold in the project's settings instead of the panel:

```json
{"env": {"AACP_FINALIZE_AT": "80"}}
```

It speaks only at the end of a turn, when the session is free: no question and
no permission prompt can be open then, and nothing is typed into the session
from outside. The fill comes from the collector's snapshot, the same one the
stamp reads, and a model whose window is not known does not trigger it. The
restart goes through the restart-session skill, so that one is installed too.
The turn after the block is the finalization itself and is never blocked again;
a session that ignored it is told again at the end of its next turn.

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

The "profile → group → project" map is set up from the phone after signing in,
on the "profiles" screen: until there is a first profile the projects screen is
empty, and creating a profile is the first thing done there.

What cannot be checked with a command:

1. the containers screen: the row of figures and the list;
2. the sessions screen: the live sessions of this machine;
3. opening a session from the panel and seeing it in the list, and the window on
   the desktop;
4. sending a message and seeing it in the feed;
5. asking the session a question through `AskUserQuestion` and seeing the card on
   the phone — the only check of the hook;
6. the subscription percentages in the header — the only check of the status
   line; they appear only while at least one claude session is alive.

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
  not visible from outside. Do not turn on `funnel`.
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
systemctl --user stop aacpanel-exec.service
go build -o ~/bin/aacpanel-exec ./cmd/aacpanel-exec               # the executor as a binary
systemctl --user start aacpanel-exec.service
sudo systemctl restart "aacpanel-agent@$USER.service"             # the collector travels from the tree
```

Without the `stop` the build answers "text file busy". Compare `.env` with
`.env.example` — the template's new keys are worth carrying over:

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
- the hook and the status line in every `settings.json`; delete the repository
  only after that — the hook calls a file from the tree, and every session's
  question would fall over a path that no longer exists.

---

## 18. If it did not work

| Symptom | Where to look |
|---|---|
| the panel does not open | `docker compose logs aacpanel`, `docker compose ps` |
| the screens are empty, "the collector is not writing" | `journalctl -u "aacpanel-agent@$USER" -f`; most often a wrong `AACP_REPO` |
| "the executor is unavailable" | `journalctl --user -u aacpanel-exec -f`; linger; the socket must belong to the uid from `AACP_UID` |
| the token is accepted, but the sign-in screen comes back empty | a cookie with `Secure` was dropped over http: an address without a certificate needs `AACP_SECURE=0` |
| the "cookie without Secure" chip in the header | the panel is opened over https while `AACP_SECURE=0` stayed: remove the line and recreate the container |
| a session opens without a window | an empty `AACP_TERMINAL` or a machine without `DISPLAY`; the button in the conversation header will open a window where there are graphics |
| the window opened, but it is not on the screen | `DISPLAY` points at a service display; the right one is named by `systemctl --user show-environment` |
| the subscription percentages are empty | no `jq`, no status line in this account, or no live session at all |
| there is a button, and the journal says "unknown action" | the executor was built from an old tree: §12 |
| `Failed to connect to bus` from `systemctl --user` | linger is off, or you signed in without a logind session |
| the feed is visible from the phone, but there is no switch to the terminal | that is by design: `AACP_TERM_PUBLIC=0` |
| the projects screen is empty although there are projects on disk | not a single profile has been created: the button is on that same screen |
| `the schema does not match the migrations` in the log | an applied migration file was changed; repeating does not cure it |
| tailscale: the node does not sign in, dead nodes pile up in the admin console | signing in by the link from the log does not work, `TS_AUTHKEY` is needed before the container comes up |

Boundaries that do not get fixed: passkeys, PWA and pushes work only in a secure
context — https or localhost. That is a browser's condition, not the panel's.
