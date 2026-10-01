# Install

One command puts the panel on a machine:

```bash
git clone https://github.com/BeMySlaveDarlin/aacpanel ~/aacpanel && ~/aacpanel/install.sh
```

It looks the machine over, asks its questions, shows a plan and changes nothing
before the plan is approved. Then it goes step by step, each step checked on the
machine before the next one begins, and writes down every change it makes, so
that `./install.sh uninstall` can take back exactly that.

Part 1 is that command: what to know before running it, what it asks, how it
looks and what to do after it. Part 2 is what each of its steps does, written to
be carried out by hand — by a person or by an agent such as Claude Code — on a
machine where the installer stopped or does not run at all.

The notation below: `<clone>` is where the repository is cloned, `<user>` is
your unix user (`id -un`), `<home>` is the home directory, `<uid>`/`<gid>` are
`id -u` and `id -g`, `<state>` is the state directory, `/var/lib/aacpanel` by
default, and `<install>` is the installer's own directory,
`${XDG_STATE_HOME:-~/.local/state}/aacpanel-install`.

---

# Part 1. The installer

## What appears on the machine

| What | Where | Without it |
|---|---|---|
| `aacpanel` | container | there is no panel |
| `aacpanel-db` | container, Postgres | the service will not come up |
| `aacpanel-socket-proxy` | container | the container tree is empty |
| `aacpanel-agent` | systemd system unit, python from the working tree | every screen is empty |
| `aacpanel-exec` | systemd user unit, the Go binary `<home>/bin/aacpanel-exec` | the panel only shows, not a single button |

Smaller traces: the machine description `<state>/host.env`, the secrets in the
clone's `.env`, the question hook, the status line and the parts of the kit you
choose in the `settings.json` of every claude account, the panel's MCP server in
every account, linger for your user, the executor's state in
`<home>/.local/state/aacpanel` and `<home>/.local/state/aacpanel-stream`.

On request, with the kit: the weekly cleanup of docker, the user units
`aacpanel-docker-gc.timer` and `aacpanel-docker-gc.service`. Where the machine
has no claude and you agree: claude itself, by Anthropic's native installer, in
`<home>/.local/bin/claude` and `<home>/.local/share/claude`.

The installer's own: the manifest, the journals and the copies of the files it
changed in `<install>`, and the Go it builds itself and the executor with in
`${XDG_CACHE_HOME:-~/.cache}/aacpanel-install`.

## Before you run it

**The clone stays where it is, for good.** The collector and the hooks of claude
run straight from the tree, and its path is written into the machine
description and into every account's settings: moving the directory is a
reinstall. It has to belong to you, lie on a disk and not on tmpfs, and its
path takes no spaces or quotes — unit files and hook commands cannot carry them.

**Run it as the user whose claude sessions the panel manages**, not as root: the
executor refuses to run as root, and the panel's container runs under your uid.

**One step runs as root**, in one sudo: the apt packages the machine lacks, the
state directory, the collector's unit and linger. The installer shows the
command and what it does before asking, and the whole of it is one file to read,
`deploy/install/root.sh`. sudo asks for its password at the terminal. Without
sudo rights of your own, answer No at the root command: the run stops with the
command for an administrator, and `./install.sh` goes on from there once it
has run.

**claude** is found with `command -v claude`, or where Anthropic's native
installer puts it, `~/.local/bin/claude`, when that is not on `PATH` yet. On a
machine without it the installer offers to install it with the native
installer, as you and without root: it downloads the script
`https://claude.ai/install.sh` itself and runs it with bash, before the
settings of claude are wired. The script puts the launcher into
`~/.local/bin/claude` and the versions into `~/.local/share/claude`, and
downloads claude with curl or wget: on a machine with neither, the root step
installs curl with apt, with the other packages. A claude that runs on a node
of your shell's `PATH` is refused: the executor's unit does not see that node,
and no panel session would start. Signing in can wait — the installer offers to
hand you the terminal for it.

**The network.** The installer downloads the Go that `go.mod` names from
`go.dev` (checked against the checksum `go.dev` publishes), the modules of the
executor from `proxy.golang.org`, the images from Docker Hub, the packages
with apt, and — where it installs claude — the native installer from
`claude.ai`, which downloads claude from `downloads.claude.ai`. Behind a proxy,
the downloads of Go, of its modules and of claude go through `HTTPS_PROXY`.

**Decide where the panel is opened from.** This machine is always a way in.
The others are parts of the kit, and each asks its own questions:

| Way in | What it needs from you | Sign-in | Address |
|---|---|---|---|
| **this machine** | nothing | passkey (localhost is a trusted context) or token | `http://localhost:8776` |
| **Tailscale** | a tailnet with MagicDNS and HTTPS Certificates on, a one-time auth key, the Tailscale app on the phone | passkey and token | `https://<node>.<tailnet>.ts.net` |
| **home network, TLS** | a certificate for a name that points at the machine's home address, issued and renewed by you | handed over to the home of the passkeys; as the only way in besides this machine, a token | `https://<name>:8443` |
| **own domain** | DNS, a reverse proxy and its certificate, all yours | passkey and token | `https://<domain>` |

A passkey lives only on https and on localhost — a browser's condition. The
passkeys have one home, and the kit decides it: the domain when there is one,
else the tailnet node, else this machine; the other ways in hand the sign-in
over to it. The home is baked into every key enrolled: moving it later voids
every key at once, and the installer asks you to type the new address before
it does that.

If in doubt — **Tailscale**: the one way to real https without a domain and a
public address. Never turn on `tailscale funnel` with it: it puts on the open
internet something that can type into the terminal of a live session.

## The supported machine

The check reads the machine and changes nothing. A line marked `stop` ends the
run before the first change, with the command that fixes it.

| What | How it is checked | Outside it |
|---|---|---|
| not root | the effective uid | stop: run it as the user of the sessions |
| the Debian and Ubuntu family | `ID` or `ID_LIKE` in `/etc/os-release`, and `apt-get` and `dpkg` on `PATH`; the version is not checked, what the machine can do is | stop: part 2 is the way by hand |
| not WSL | `microsoft` in `/proc/sys/kernel/osrelease` | stop |
| systemd as PID 1, 249 or newer | `/proc/1/comm`, `systemctl --version` | stop |
| `x86_64` | `uname -m` | `aarch64` goes with a warning, anything else stops |
| docker, the system daemon, answering you | `docker version`, `docker info`, `/var/run/docker.sock` | missing: offered with apt; `permission denied`: `sudo usermod -aG docker <user>` and a new login; rootless docker, Docker Desktop, docker from snap: stop |
| docker compose v2, 2.18.0 or newer | `docker compose version --short` | missing: offered with apt, when apt has one that new; otherwise stop. The `docker-compose` package of Debian 12 is compose v1: there docker comes from `download.docker.com` |
| claude | `command -v claude`, else `~/.local/bin/claude`; and what its first line runs | missing: offered with Anthropic's native installer, as you, and curl with apt where the machine has neither curl nor wget for it. One that runs on a node the executor's unit does not see: stop |
| tmux and jq | `PATH` | offered with apt |
| python3 3.9 or newer | `python3 -c …` | stop |
| sudo | `sudo -n true`, `sudo -n -l` | a password is asked at the terminal; without a terminal the run stops, unless the root part of an earlier install is in place |
| ports 8776 and 8777 | `/proc/net/tcp`, `ss`, `docker ps` | taken by something else: stop, the ports are fixed. 8443 taken: the home-network listener takes a free port of 18443–18499 |
| the clone | owner, file system, path | not yours, on tmpfs, or a path with spaces: stop |
| container names `aacpanel`, `aacpanel-db`, `aacpanel-socket-proxy`, `aacpanel-tailscale` | `docker ps -a` and their compose project | taken by another project: stop |
| room | free space under docker's root and the state directory, `MemTotal` | under 3 GB for docker, under 1 GB for the state, under 2 GB of memory: stop; under 5 GB and 4 GB: a warning |
| the network | `registry-1.docker.io`, `proxy.golang.org` | a warning only |
| an earlier install | the manifest, the machine description, the `.env`, the units, the containers, the database volume | a manifest makes the run an update, traces without one an install by hand to take over. The database volume without the `.env` that holds its password: stop. A `host.env` that names another clone: stop |

## How it looks

The screen is drawn the way Claude Code draws a session: what is done goes into
the terminal's scrollback and stays there, and only the bottom part lives — a
spinner, a question or a frame to confirm.

The run opens with where and as whom it installs, and the check of the machine:

```
╭──────────────────────────────────────────────────────────────────────────────╮
│ ✻ aacpanel installer                                                         │
│                                                                              │
│   clone    ~/aacpanel  (3f1c2ab)                                             │
│   user     dev (uid 1000)                                                    │
│   mode     fresh install — no trace of the panel here                        │
╰──────────────────────────────────────────────────────────────────────────────╯

⏺ Check the machine
  ⎿  ✓ Ubuntu 24.04.3 LTS · x86_64 · systemd 255
     ✓ docker 27.5.1 · compose 2.33.1 · system daemon
     ✓ claude 2.1.283 · ~/.local/bin/claude
     ⚠ tmux is missing: sessions live in it, and without it the executor turns
       off every action on sessions. The installer offers to install it.
     ✓ ports 8776, 8777, 8443 are free
     … +7 checks (ctrl+o to expand)
```

The questions come in blocks, a tab a question. The first option is what the
machine gives, with where it came from; the last is an answer of your own:

```
────────────────────────────────────────────────────────────────────────────────
 ←  ■ Host  ☐ Session  ☐ Projects  ☐ State  ✔ Submit  →

 What does the panel call this machine?

 ❯ 1. lab
      From hostname. Metrics history is keyed by this name: changing it later
      starts a new history.
   2. Type something.

 Enter to select · Tab/Arrow keys to navigate · Esc to go back
```

The kit is one list. The first group is the panel itself and does not come off,
the quiet group is checked for you, the rest waits to be asked for:

```
 What goes into this install?
 Space checks a part, Enter takes the list as it stands.

   Always
   [✔] Question relay     a session's questions reach your phone
   [✔] Limits snapshot    5-hour and weekly limits, before your status line
   [✔] Panel tools        checklist, briefs, a call to your phone, restart
   Quiet — checked for you
 ❯ [✔] Page copies        a published page opens on the phone under any account
   [✔] Brief reminder     a session learns that an answered brief waits
   [✔] Context cap        stops a session at the cap, restarts it with your line
   [✔] Checklist nudge    reminds a session that forgot its checklist
   [✔] Self-restart       a session may restart itself without asking you
   On request
   [ ] Prompt stamp       time, context, limits and load before every prompt
   [ ] Cost snapshot      spend per session, written after every turn
   [ ] Background work    reminds a session of work left running over an hour
   [ ] Docker cleanup     weekly prune of unused images — of the whole machine
   Ways in besides this machine
   [ ] Tailscale          your phone from anywhere, through your tailnet
   [ ] Home network, TLS  straight at home, with a certificate you issue
   [ ] Own domain         behind your reverse proxy
   For development
   [ ] Test database      for make check: forty tests skip without it
```

The plan says what will be done, by whose hands, and where every value came
from. Nothing is changed before you answer Yes; the third option writes the
plan into `<install>/plan.txt` and quits:

```
╭─ Install plan ───────────────────────────────────────────────────────────────╮
│ As root — one sudo                                                           │
│   · apt install tmux                                                     new │
│   · /var/lib/aacpanel, owner dev                                         new │
│   · /var/lib/aacpanel/host.env                                           new │
│   · aacpanel-agent@.service, enabled for dev                             new │
│   · linger for dev                                                       new │
│ As you                                                                       │
│   · ~/bin/aacpanel-exec, built with the installer's Go                   new │
│   · aacpanel-exec.service, user unit                                     new │
│   · ~/aacpanel/.env                                                      new │
│   · compose: aacpanel, aacpanel-db, socket-proxy                         new │
│   · map: contour personal (~/.claude)                                    new │
│   · map: group Projects, 1 project                                       new │
│ In claude settings — ~/.claude/settings.json                                 │
│   · question hook · status line before yours · MCP server                    │
│   · 5 allow rules · 5 hooks                                                  │
│ Values                                                                       │
│   host lab (hostname) · home session lab (host name)                         │
│   state /var/lib/aacpanel (recommended) · projects under ~/code (found)      │
│   …                                                                          │
╰──────────────────────────────────────────────────────────────────────────────╯

 Would you like to proceed?
 ❯ 1. Yes
   2. No, back to the answers
   3. Save the plan to a file and quit
```

The part as root is framed on its own. After Yes the terminal is sudo's until
it is done — it asks for the password in its own line — and the installer comes
back:

```
╭─ Root command ───────────────────────────────────────────────────────────────╮
│ sudo bash /home/dev/aacpanel/deploy/install/root.sh apply --user dev --state │
│ /var/lib/aacpanel --staged                                                   │
│ /home/dev/.local/state/aacpanel-install/host.env.staged --package tmux       │
│                                                                              │
│ Installs tmux with apt, creates /var/lib/aacpanel for dev, installs the      │
│ collector unit and enables linger. Nothing else runs as root.                │
╰──────────────────────────────────────────────────────────────────────────────╯

 Do you want to run it?

 ❯ 1. Yes
   2. Show the script first
   3. No — print the command for an administrator and stop
```

A long step shows its last lines, its time and the list of steps; Ctrl+O opens
everything it printed:

```
⏺ Build the executor
  ⎿  go: downloading charm.land/bubbletea/v2 v2.0.10
     … +2 lines (ctrl+o to expand)

✻ Build the executor… (41s · ctrl+c stops after a safe point)
  ⎿  ☒ Host description
     ☒ Root part
     ☒ User manager
     ☒ Panel settings
     ■ Build the executor
     ☐ Executor unit
     ☐ Panel stack
```

A step that fails stops the run with a diagnosis in a line, what to do about
it, the last lines of the command and the journal of the run:

```
⏺ Panel stack
  ⎿  ✗ http://127.0.0.1:8776/healthz did not answer with the store connected in
       120 s: docker compose logs aacpanel
     A wrong AACP_BIND in .env keeps the address from answering.
     …
     the full output is in ~/.local/state/aacpanel-install/log/20260102-140211-install.log

⏺ Stopped
  ⎿  Fix it and run ./install.sh again — finished steps are skipped.
     Changed in this run: …
```

The first device gets its code in a frame with a countdown; `r` gives a new
code, `q` goes on:

```
╭─ First device ───────────────────────────────────────────────────────────────╮
│ Open  http://localhost:8776  and enter the code                              │
│                                                                              │
│                                 7KQ2M-X9D4P                                  │
│                                                                              │
│ valid 4:37 · once · r — a new code · q — finish                              │
╰──────────────────────────────────────────────────────────────────────────────╯
```

The run ends with a report: what changed, what was in place already and left
as it was, warnings, what is left to you, and what comes next.

| Key | What it does |
|---|---|
| ↑ ↓, a digit | an option; a digit picks it at once |
| Tab, ← → | the tabs of a block |
| Enter | pick, confirm |
| Space | check an option of a list |
| Esc | back to the tab before, from the first tab to the block before; in a frame, No |
| Ctrl+O | everything the step printed, Esc back |
| Ctrl+C | while it asks: out, nothing changed; while a step works: stop after that step; a second Ctrl+C ends the command at work |

A run that stopped — by a failure or by Ctrl+C — goes on from the step it
stopped at: every step looks at the machine first and passes by what is in
place. There is no file of progress to lose.

`NO_COLOR` turns colour off. Under 60 columns the frames become plain lines.

## Without a terminal

Without a terminal, or with `--plain`, the same lines go out plain: no live part
and no colour. Every question has a flag, and `--yes` takes the suggested answer
of every question no flag answers. A question nothing answers stops the run
with the flag's name:

```
stop: no terminal to ask "Where does the panel keep its state?"; pass --state-dir <value> or --yes
```

A question with nothing to suggest — a domain, the Tailscale key — takes its
flag even with `--yes`. The plain view asks nothing of its own, and with a
terminal at the input `--plain` still gives it to sudo, which asks for its
password there, and to claude for a sign-in the answers agreed to. Without a
terminal at the input sudo has to go through without asking (`sudo -n`), or
the run stops with the command for an administrator. An install by hand is
taken over only with `--adopt` next to `--yes`.

```bash
./install.sh --yes --plain                                     # the suggested answers, all of them
./install.sh --yes --plain --transport tmux --no-windows --kit +stamp,-cap
./install.sh --yes --plain --kit +tailscale --ts-authkey-file ~/ts.key
```

| Flag | Answers |
|---|---|
| `--keep yes\|review\|access` | Keep the current settings? — over an earlier install |
| `--install-docker`, `--install-compose`, `--install-tmux`, `--install-jq` `yes\|no` | whether the root step installs what is missing with apt |
| `--install-claude yes\|no` | claude is not installed: install it with Anthropic's native installer? |
| `--host NAME` | What does the panel call this machine? |
| `--home-session NAME` | Name of the home session? |
| `--projects-root DIR` | Where do your projects live? Again for more |
| `--state-dir DIR` | Where does the panel keep its state? |
| `--terminal TEMPLATE`, `--no-windows` | How should the panel open a window to a session? |
| `--window-on-start yes\|no` | Open a window when a session starts? |
| `--display DISPLAY` | Which display do the windows open on? |
| `--locale LOCALE` | Which locale do sessions run in? |
| `--claude PATH` | What starts claude? |
| `--account DIR` | Which claude accounts does the panel serve? Again for more |
| `--transport stream\|tmux` | How are sessions kept? |
| `--claude-login [DIR=]now\|later` | Sign in to an account now? |
| `--kit PARTS`, `--kit +PART,-PART` | the kit: a list is the whole of it, `+`/`-` change the suggested one. The parts: `relay`, `limits`, `tools`, `copies`, `brief`, `cap`, `nudge`, `restart`, `stamp`, `cost`, `gc`, `tailscale`, `lan`, `domain`, `testdb` |
| `--ts-authkey-file FILE` | the one-time Tailscale key, read from a file: a key never goes on a command line |
| `--ts-hostname NAME` | Node name in the tailnet? |
| `--lan-addr ADDRESS` | Which address should the panel listen on in the home network? |
| `--lan-cert FILE`, `--lan-plain` | the certificate of that address, its `.key` beside it; or plain http |
| `--domain DOMAIN` | Domain of the panel? |
| `--bind ADDRESS` | Where does your reverse proxy reach the panel? |
| `--passkey-home DOMAIN` | agreement to move the passkeys there, which voids every enrolled one |
| `--lan-token yes\|no` | How does a phone sign in over the home network? |
| `--term-public yes\|no` | Terminal of live sessions from other devices? |
| `--more yes\|no` | Tune more settings? |
| `--probe NAME=HOST:PORT` | a local service the Machine screen probes. Again for more |
| `--push-contact CONTACT` | Contact for push services? |
| `--login-life IDLE/MAX` | How long does a sign-in last? Like `30m/12h` |
| `--contour DIR=NAME` | Name of the contour of an account |
| `--group NAME`, `--no-group` | Create a group for your projects? |
| `--project DIR` | a project in that group. Again for more |
| `--check-session yes\|no` | Open a test session to check the whole chain? |
| `--skip-vulncheck` | build the image without the check of its dependencies against published vulnerabilities — knowingly |

`./install.sh plan` asks the same questions, takes the same flags and shows the
plan with the source of every value, and changes nothing.
`./install.sh demo` walks the whole install on a made-up machine and changes
nothing either. `./install.sh <command> -h` lists the flags of a command.

## After the install

**The first sign-in.** The code of the first device is shown by the install
itself; another comes from `./install.sh enroll`, or from the panel's binary in
its container:

```bash
docker compose exec aacpanel /aacpanel -enroll
```

A code lives five minutes and works once. Open the panel at the address the
frame names — `http://localhost:8776` on this machine; on a machine without a
browser, `ssh -L 8776:127.0.0.1:8776 <user>@<host>` from your computer and the
same address there — and enrol a passkey. The next devices are enrolled with a
code from the panel itself.

The installer puts the first contours on the map, a group and its projects, as
answered. More are added on the Profiles tab of the phone or from Projects in
the sessions section at a desk. A project's sessions live on the stream or in
tmux, as its launch parameters say.

What no command checks:

1. the containers screen: the row of figures and the list;
2. the sessions screen: the live sessions of this machine;
3. opening a session from the panel and seeing it in the list, and the window on
   the desktop;
4. sending a message and seeing it in the feed;
5. asking a session in tmux a question through `AskUserQuestion` and seeing the
   card on the phone — the only check of the question hook;
6. the 5-hour and weekly limits of the account on the sessions screen: the
   executor's probe writes them shortly after its start, and the status line
   keeps them fresh while a terminal of the account is open.

**`./install.sh check [--session]`** walks the chain the panel works through, a
line a link: `/healthz` on the panel's address and on the local listener, the
age of the collector's snapshot, `aacpanel-exec -list`, the executor's socket and
whether the panel reaches it, the database under the application role, the
containers, both units and linger, the socket of the questions, the wiring of
claude in every account, the contours on the map, the tailnet node when there
is one, and the timer of the docker cleanup when the kit holds it — a warning,
not a break, when it is off. `--session` opens the test session
`aacpanel-check` through the executor, waits for the collector to see it and
closes it — only it. The check changes nothing else.

**`./install.sh enroll`** gives a code for another device, with its countdown.

**`./install.sh update [--to P]`** moves the clone on and updates the whole
stack. A clone on a branch pulls it (`git pull --ff-only`); a clone on a release
goes to the newest release of its paradigm. Releases are tagged `vP.M.m`:
Paradigm, Major, Minor — a paradigm is a change of approach, and an update
crosses one only when `--to` names it. Changes of your own to the tracked files
of the clone stop the update before anything moves. The installer of the new
tree then pulls the images that are not built here, asks "Keep the current
settings?" and goes on as an install: the image is built and brought up, the
executor is rebuilt and restarted, a changed unit is put in place again, and the
collector is restarted on the new tree. The report names the keys
`.env.example` gained since the tree the clone was on.

**`./install.sh uninstall`** takes back what the manifest lists, and nothing
else, in the order that keeps the machine working to the end: claude's settings
first, while the clone the hooks run from is there; your units; the executor;
the stack with its images; the part as root in one sudo, with linger last of
what goes through systemd — and only when the installer turned it on; the data
you chose, the caches; and the installer's own directory last of all. A
`settings.json` the installer wrote and
nobody touched since goes back byte for byte; one changed since loses the
panel's lines and keeps the rest. The data stays unless you choose it, and data
goes only after you type the host name:

| Flag | Data |
|---|---|
| `--purge-db` | the database volume: the map, the journal, the enrolled devices; the tailnet node's keys with it |
| `--purge-state` | the state directory: the collector's snapshot, the questions of sessions, the index of conversations |
| `--purge-env` | the clone's `.env`: the secrets — without it a volume kept does not open |
| `--purge-files` | the files a phone sent to sessions |
| `--purge-exec` | the executor's and the stream's state: the guards of the map, the checklists, the answers to permissions |
| `--purge-data` | all of the above |

`--yes` removes without the question, and with it the `--purge` flags need no
host name typed. `--dry-run` shows the plan and what `root.sh` would run, and
changes nothing. The installer's cache and the executor's go every time. What
stays is named at the end with the command that removes it: the data you kept,
apt packages, claude from the native installer, the docker group, linger that
was on before, the tailnet node in its admin console, docker's build cache; and
the clone itself is never touched. Without a manifest uninstall refuses and
lists what the machine holds of the panel: an install by hand goes away as
"Removing the panel" in part 2 says, or is taken over by the installer first.

## Access from outside

- **Own domain.** A reverse proxy: `https://<domain>` → the address the answer
  to "Where does your reverse proxy reach the panel?" gave (`AACP_BIND`), port
  8776, with the headers passed through. A sample configuration is
  `deploy/nginx-aacpanel.conf`. Mind the proxy's own `client_max_body_size`:
  nginx defaults to a megabyte, and a file from a phone is cut at the proxy
  before it ever reaches the panel.
- **Tailscale.** The node runs as the container `aacpanel-tailscale`, in
  userspace mode, visible only from inside the tailnet: check it from there, or
  with `docker exec aacpanel-tailscale tailscale status`. It signs in only with
  a one-time key — signing in by the link from its log does not get through,
  because the container waits a minute for the confirmation and starts again
  with a new node key. The installer wipes the key from `.env` once the node is
  in. Beside a domain the node is a further way in with a listener of its own
  (`AACP_TS_SERVE=./deploy/tailscale/serve-leg.json`, `AACP_TS_ADDR=:8778`), so
  the panel knows where a request came from and the address map offers it.
- **Home network over TLS.** The service holds TLS itself; nginx is not needed.
  The certificate is yours to issue and renew, and only DNS-01 gets one: a
  public CA does not issue for an address in a home network, and the name in DNS
  is there for exactly that. The installer looks for pairs `<name>.crt` and
  `<name>.key` in `/etc/ssl/aacpanel` and `~/certs`; the directory is mounted
  into the container as `/tls`, and the service rereads the files when they
  change. Plain http is an option too, without a domain: the session cookie
  then goes without `Secure` and a passkey does not work there. With the home
  network as the one way in besides this machine, the passkeys live at
  localhost, and a phone signs in with a token the installer offers to make.

## When the installer stops

| Where | What it says | What to do |
|---|---|---|
| `install.sh` | go.dev does not answer | the network, or `HTTPS_PROXY` |
| `install.sh` | go`<version>` does not match the checksum go.dev publishes | run again; a download that keeps not matching is a report worth sending |
| Check the machine | a line with `stop:` | the command in that line, then `./install.sh` again |
| Root part | sudo needs a password, and there is no terminal to type it at; or you said No | an administrator runs the `sudo bash <clone>/deploy/install/root.sh apply …` the stop prints, then `./install.sh` goes on |
| Root part | `<state>` exists and belongs to another user | `sudo chown -R <user>:<group> <state>`, or remove it |
| Root part | the collector does not write `<state>/state.json` | `journalctl -u aacpanel-agent@<user> -n 30`; most often the clone moved after the install and `AACP_REPO` in `host.env` names where it was |
| User manager | the user systemd manager did not come up after linger | `systemctl status user@<uid>` |
| Install claude | `https://claude.ai/install.sh` did not download, or gave no installer script | the network or `HTTPS_PROXY`; a page instead of the script is claude.ai not serving the region |
| Install claude | Anthropic's native installer did not install claude | the lines above are its own: `curl -fsSL https://claude.ai/install.sh \| bash` runs it by hand, then `./install.sh` goes on |
| Build the executor | no room left on the disk; modules did not download from `proxy.golang.org`; the Go is not of this machine's architecture | room; the network or `HTTPS_PROXY`; the architecture |
| Executor unit | the socket did not appear | `journalctl --user -u aacpanel-exec -n 30` |
| Executor unit | `226/NAMESPACE` | an older unit: `./install.sh` puts the unit of this tree in place |
| Executor unit | the socket directory belongs to root | docker made it before the executor ran: `sudo rmdir /run/user/<uid>/aacpanel-exec`, then `./install.sh` |
| Docker cleanup timer | the timer is not active and enabled after it was enabled | `systemctl --user status aacpanel-docker-gc.timer` |
| Panel stack | the image build stopped on a vulnerability published after this release | `./install.sh update`, or knowingly `./install.sh --skip-vulncheck` |
| Panel stack | a port the panel publishes is taken | `ss -ltnp` names who holds it |
| Panel stack | a container of a name the panel takes belongs to something else | `docker ps -a`; remove or rename it |
| Panel stack | `/healthz` did not answer with the store connected | `docker compose logs aacpanel`; a wrong `AACP_BIND` |
| App role | the panel's password was refused | `./install.sh` again: the step sets the password anew |
| Tailscale | the node did not sign in | the key expired or a one-time key was used already: a new key, then `./install.sh` |
| Tailscale | the tailnet gives the node no name with a certificate | MagicDNS and HTTPS Certificates in the tailnet admin console → DNS |
| Claude settings | a `settings.json` does not parse; nothing was written to it | `python3 -m json.tool <file>` shows where |
| Claude settings | `claude mcp add` failed | `claude --version`, and the lines above it |
| Map | a contour of that name stands for another account | `--contour <dir>=<name>`, or rename it on the Profiles screen |
| Check | a link of the chain is broken | the line under it says where to look |

Every command a run gives, with its whole output, is in its journal,
`<install>/log/<date>-<time>-<command>.log`. No secret is there: none goes on a
command line, the output of a command that prints secrets is not kept, and
whatever a command echoes back of one is cut out.

## When the panel misbehaves

| Symptom | Where to look |
|---|---|
| the panel does not open | `docker compose logs aacpanel`, `docker compose ps` |
| the screens are empty, "the collector is not writing" | `journalctl -u "aacpanel-agent@$USER" -f`; most often a wrong `AACP_REPO` |
| "the executor is unavailable" | `journalctl --user -u aacpanel-exec -f`; linger; the socket must belong to the uid from `AACP_UID` |
| the panel is down and a session on the stream is needed | `~/bin/aacpanel-exec -sessions` names the sessions on the stream, `~/bin/aacpanel-exec -console <name>` moves one into tmux under the same account, then `tmux attach -t <name>`; the holders outlive a restart of the executor |
| the token is accepted, but the sign-in screen comes back empty | a cookie with `Secure` was dropped over http: an address without a certificate needs `AACP_SECURE=0` |
| the "cookie without Secure" chip in the header | the panel is opened over https while `AACP_SECURE=0` stayed: remove the line and recreate the container |
| a session opens without a window | an empty `AACP_TERMINAL` or a machine without `DISPLAY`; the button in the conversation header opens a window where there are graphics |
| the window opened, but it is not on the screen | `DISPLAY` points at a service display; the right one is named by `systemctl --user show-environment` |
| the limits of an account are empty or old | `journalctl --user -u aacpanel-exec` names a probe that failed; no `jq` or no status line leaves them to the probe alone |
| there is a button, and the journal says "unknown action" | the executor is of an older tree: `./install.sh update` |
| a session on the stream answers that it was started before the panel could do this | its holder runs the executor it was started with: restart the session, the conversation is kept |
| the feed of a session on the stream broke after claude updated itself | `python3 deploy/claude/stream-contract.py` names the request that changed: every request the feed needs against the installed claude, a dozen short turns on haiku |
| `Failed to connect to bus` from `systemctl --user` | linger is off, or you signed in without a logind session |
| the feed is visible from the phone, but there is no switch to the terminal | that is by design: `AACP_TERM_PUBLIC=0` |
| the projects screen is empty although there are projects on disk | there is no contour on the map yet: the button is on that same screen |

Boundaries that do not get fixed: passkeys, PWA and pushes work only in a secure
context — https or localhost. That is a browser's condition, not the panel's.

---

# Part 2. By hand, or by an agent — what each installer step does

This is the installer written out: the same steps in the same order, named as
the installer names them. It is for a machine where the installer stopped and
the stop cannot be fixed, or where it does not run at all — another family of
Linux, say. Each step says how to tell it is done already, what to do, how to
check it, and what the installer would have written into its manifest.

**For an agent carrying this out:**

- One step at a time, in this order. A step whose check fails stops the work:
  report the check and what it printed, and go no further.
- **Ask the person first** before anything that cannot be taken back or that
  reaches past this machine: every command as root (show it whole), adding the
  user to the `docker` group, replacing or deleting a file you did not make,
  `docker volume rm`, changing `AACP_RP_ID` once a device is enrolled, turning
  the terminal on for other devices, the Tailscale key, and signing in to claude.
- Secrets are never printed: they are drawn straight into their file, or handed
  to a program through its environment, and no report, log or message of yours
  carries one. `docker compose config` without `-q` prints them all.
- A line of the manifest is written before the change it stands for, not after.

**The manifest** is `<install>/manifest.tsv`, a line a change, four fields
separated by a tab: the step, the kind, the target, and what uninstall needs to
know — `created`, `adopted`, `by-installer`, `data`, `sha=<sha256>`,
`orig=<copy>`. The first line is the directory itself:

```
manifest	dir	/home/dev/.local/state/aacpanel-install	created
```

A file that was there before is copied into `<install>/backups/` first, named
after its path without the leading slash and with every `/` turned into `_`:
`/home/dev/.claude/settings.json` goes to
`<install>/backups/home_dev_.claude_settings.json.orig`, and the line names it
as `orig=`. With a manifest, `./install.sh uninstall` takes the install back
and `./install.sh` treats it as its own. Without one, `./install.sh` finds the
install by hand and offers to take it over, marking each thing `adopted`, which
uninstall removes only where it knows it for the panel's beyond doubt; and
"Removing the panel" below takes it away by hand.

## What the machine needs

| What | Check | Without it |
|---|---|---|
| Linux with systemd 249 or newer as PID 1 | `systemctl --version` | there is nothing to install |
| docker, the system daemon, and compose v2 2.18.0 or newer | `docker compose version` | the service and the database will not come up |
| docker without sudo | `docker version` — the call itself, not `command -v` | half the steps will need rights |
| tmux | `tmux -V` | **no session will open**, in tmux or on the stream: a session in tmux lives there and the window is only attached to it, and a session on the stream goes into tmux when it is moved there |
| python3 3.9 or newer | `python3 -V` | the collector will not start, the panel is blind |
| jq | `jq --version` | the status line writes nothing: the limits of an account wait for the executor's probe, and a model changed in tmux shows only with its next request |
| Go of the version `go.mod` names | `go version` | the executor cannot be built |
| git, openssl | `git --version`, `openssl version` | the secrets below are drawn by `openssl` |
| claude, installed natively | `claude --version` | there is nothing to show; S5a installs it |
| a terminal | `command -v konsole` (or your own) | no windows onto sessions — the normal mode for a machine without graphics |

On Debian and Ubuntu:

```bash
sudo apt install docker.io tmux python3 jq git openssl
sudo apt install docker-compose-v2   # Ubuntu; on Debian 13 the package is docker-compose
sudo usermod -aG docker "$USER"      # then log in again
```

The `docker-compose` of Debian 12 is compose v1, which the panel does not come
up on: there docker and its compose plugin come from `download.docker.com`.

**About Go.** Distributions ship an older Go than `go.mod` names. With the
default `GOTOOLCHAIN=auto` the first build downloads the toolchain it needs;
with `GOTOOLCHAIN=local` it fails complaining about the version. `./install.sh
plan` downloads that very Go into
`${XDG_CACHE_HOME:-~/.cache}/aacpanel-install/go<version>/bin/go` and changes
nothing else, on any machine where it runs.

## S2. The answers

Every answer lands in one of three places: the machine description
`<state>/host.env`, the clone's `.env`, or claude's settings. The installer
takes the first option of each question unless told otherwise; where that
option comes from is what to do by hand as well. A key that is there with an
empty value and a key that is not there are different answers: `AACP_TERMINAL=`
means "no windows", no `AACP_TERMINAL` means the built-in default.

Over an earlier install the first question is *Keep the current settings?* —
yes, every answer as it was written; or review them, the cursor on each current
answer; or change only how the panel is reached. Its answers come from the
`host.env`, the `.env` and the settings already there.

**This machine**
- *What does the panel call this machine?* — the kernel's host name up to the
  first dot (`/proc/sys/kernel/hostname`). `AACP_HOST`. Metrics history is keyed
  by it: a new name starts a new history.
- *Name of the home session (the machine's own claude session)?* — the host name
  in lower case. `AACP_HOME_SESSION`.
- *Where do your projects live?* — the children of the home directory, `/srv`
  and `/opt` that hold a project three levels down or less — a directory with
  `.git`, a `.claude` directory or `CLAUDE.md`, or one claude has opened — the
  fullest checked; with none of them, the home directory itself.
  `AACP_PROJECT_ROOTS`, the roots joined by `:`, and `AACP_PROJECT_SCAN`, the
  same without the home directory, which is never walked whole.
- *Where does the panel keep its state?* — `/var/lib/aacpanel`.
  `AACP_STATE_DIR`, in `host.env` and in `.env`.

**Session windows** — the terminal, the start and the display only where there
is a display; the locale always.
- *How should the panel open a window to a session?* — the first of these on
  `PATH`: `konsole`, `gnome-terminal --`, `xfce4-terminal -e %s`,
  `alacritty -e`, `kitty`, `wezterm start --`, `xterm -e`; otherwise no windows.
  A template of your own takes `%s` where `tmux attach` goes. `AACP_TERMINAL`,
  written empty for no windows.
- *Open a window when a session starts?* — yes. `AACP_TERMINAL_AUTO`, `1` or `0`.
- *Which display do the windows open on?* — the `DISPLAY` of the user manager
  (`systemctl --user show-environment`), then the sockets in `/tmp/.X11-unix`.
  `DISPLAY`, empty without windows.
- *Which locale do sessions run in?* — `$LANG` when `locale -a` has it, else
  `C.UTF-8`; only UTF-8 locales, since without one non-ASCII text turns into
  question marks. `AACP_LANG`.

**Claude**
- *What starts claude?* — what `command -v claude` gives, the link kept as it
  is so that an update of claude reaches the panel; off `PATH`,
  `~/.local/bin/claude`, where the native installer puts it; a wrapper of your
  own instead, where the accounts need one. `AACP_CLAUDE`.
- *Which claude accounts does the panel serve?* — `~/.claude` and every
  `~/.claude-*` holding what claude puts there (`.credentials.json`,
  `settings.json`, `projects`, `sessions`, `statsig`), `~/.claude` checked.
  `AACP_CLAUDE_HOME`, joined by `:`; each account is wired in S12 and becomes a
  contour in S13.
- *How are sessions kept?* — on the stream: the panel speaks claude's own
  protocol, and tmux stays as the way back. In tmux, claude runs in a terminal
  and the panel types the keys. The launch parameter `transport` of the contours
  S13 makes.
- *Sign in to `<account>` now?* — for an account without a sign-in: now, at a
  terminal. `CLAUDE_CONFIG_DIR=<account> claude`, `/exit` when done; for
  `~/.claude` plain `claude`. The accounts left for later go into
  `AACP_SIGN_IN_LATER`, joined by `:`, so that a run keeping the settings does
  not open claude for them again.

**Prerequisites** — asked only for what the check found missing, yes each:
*docker is not installed. Install it with apt as part of the root step?*,
*docker compose v2 is missing…*, *tmux is missing. Install it?*, *jq is
missing. Install it?* — the packages of S4; *claude is not installed. Install
it with Anthropic's native installer?* — S5a, with curl among the packages of
S4 on a machine with neither curl nor wget, and then *What starts claude?*
suggests `~/.local/bin/claude`. A no stops the run with the command that
installs it by hand.

**Kit** — *What goes into this install?* The parts and what each puts where:

| Part | Checked | What it puts in place |
|---|---|---|
| Question relay | always | the `PreToolUse` hook on `AskUserQuestion`, `agent/ask-hook.py` |
| Limits snapshot | always | the status line, `agent/rate-snapshot.sh`, before yours |
| Panel tools | always | the MCP server `aacpanel` and four allow rules |
| Page copies | yes | the `PostToolUse` hook on `Artifact`, `deploy/claude/artifact-copy.py` |
| Brief reminder | yes | the `SessionStart` hook, `deploy/claude/brief-waiting.py` |
| Context cap | yes | the `Stop` hook, `deploy/claude/context-guard.py` |
| Checklist nudge | yes | the `Stop` hook, `deploy/claude/checklist-reminder.py` |
| Self-restart | yes | the allow rule `mcp__aacpanel__session_restart` |
| Prompt stamp | no | the `UserPromptSubmit` and `PostToolBatch` hooks, `deploy/claude/prompt-stamp.py` |
| Cost snapshot | no | the `Stop` and `SubagentStop` hooks, `deploy/claude/cost-snapshot.py` |
| Background work | no | the `Stop` hook, `deploy/claude/background-reminder.py`, and its marks in `<state>/background-reminder/` |
| Docker cleanup | no | the weekly timer `aacpanel-docker-gc.timer` and its service, user units: S8a; left out over an install that has them, they go |
| Tailscale, Home network TLS, Own domain | no | the ways in, with the questions of the next block |
| Test database | no | S10a |

An addition a script of your own already does on this machine is left out: two
hooks on one event give two stamps in every message.

**Access** — only for the ways in the kit holds.
- *Tailscale auth key (one-time, admin console → Settings → Keys)* — nothing to
  suggest. `TS_AUTHKEY`, until the node signs in.
- *Node name in the tailnet?* — `aacpanel`. `AACP_TS_HOSTNAME`.
- *Which address should the panel listen on in the home network?* — the IPv4
  addresses of `ip -4 -o addr`, loopback, docker, bridges, `veth`, Tailscale
  and WireGuard left out. `AACP_LAN_BIND`.
- *Certificate and key of the home-network address?* — the pairs `<name>.crt`
  and `<name>.key` in `/etc/ssl/aacpanel` and `~/certs`; or plain http, where
  there is no domain. See S6 for the keys it writes.
- *Domain of the panel?* — nothing to suggest. `AACP_PUBLIC_URL`, and the
  passkey keys.
- *Where does your reverse proxy reach the panel?* — `127.0.0.1` for a proxy on
  this machine, the `docker0` address for one in a container, a WireGuard
  address for one on another machine, the home addresses. `AACP_BIND`.
- *How does a phone sign in over the home network?* — asked only when the home
  network is the one way in besides this machine: the passkeys then live at
  localhost, which a phone cannot open. With a token. `AACP_TOKEN`.
- *Terminal of live sessions from other devices?* — no. Yes is raw input into a
  live session past the confirmation gate, a shell as you from wherever the
  panel is reachable. `AACP_TERM_PUBLIC`.
- Moving the passkeys — asked only when the kit moves their home: *type the new
  address to go on*, since every enrolled passkey is void after it.

**More settings** — *Tune more settings?* No, unless asked:
- *Which local services should the Machine screen probe?* — the ports `ss`
  finds listening, the panel's own left out; none checked. `AACP_PROBE_PORTS`
  in `host.env`, `name=host:port` joined by commas.
- *Contact for push services?* — `mailto:` the `git config user.email` of the
  clone, or left to the panel, which names its passkey address.
  `AACP_PUSH_CONTACT`.
- *How long does a sign-in last?* — 24 hours idle, 30 days at most, the defaults
  of the compose file; or 30 minutes and 12 hours. `AACP_SESSION_IDLE`,
  `AACP_SESSION_MAX`.

**Map**
- *Name of the contour for `<account>`?* — `personal` for `~/.claude`, the name
  of the directory without `.claude-` for the others.
- *Create a group for your projects?* — `Projects`.
- *Which projects go into the group?* — the projects under the roots, those
  claude opened in the last two weeks checked.

## S3. `hostenv` — Host description

The one file all three processes read at startup: the service in its container,
the collector and the executor.

- **Done when** `<state>/host.env` holds every key of the answers with its value.
- **Do.** Start from the commented template `deploy/host.env.example` and fill
  in the answers of S2, plus two keys the machine gives: `AACP_REPO=<clone>` —
  the collector starts from there — and `AACP_UNIX_USER=<user>`. A key is set
  on its own line, the live one or where the template has it commented out;
  never two lines for one key. Where the state directory is yours already, write
  the file in place. Where it is not there yet, keep it aside for S4:
  the installer writes it to `<install>/host.env.staged`.
- **Check.** Every key of the answers is in the file, and `AACP_TERMINAL` is
  there even when empty.
- **Manifest.** `hostenv	file	<state>/host.env	created`, or `orig=<copy>` for
  a file that was there; for a staged file,
  `hostenv	file	<install>/host.env.staged	created`.

Among the keys the template carries and no question asks is how many processes
parse transcripts for usage (`AACP_USAGE_WORKERS`). One key the executor reads is not
in the template: `AACP_LIMITS_EVERY`, how old the snapshot of an account's
limits may grow before the executor renews it with a probe — a `claude -p` on
haiku that says one word, a share of the very limit it reads. Ten minutes by
default, in Go duration form (`30m`), never under a minute; an account with a
terminal open keeps its snapshot fresh through the status line and is not
probed.

`AACP_CLAUDE_REGISTRY` points at the registry of a contour router, if the
machine has one: a line an account, `name | prefix | config dir | token file`,
where the prefix is the directory a session must start under to go into that
account and `*` takes every directory no other line does. The collector reads
the accounts from it after `AACP_CLAUDE_HOME`, and a new contour on the map is
taken from them — without a registry a contour's paths are typed by hand.

## S4. `root` — Root part

Everything as root, in one call. **Ask the person first**, with the command
whole. By the script, which prints a manifest line before every change:

```bash
sudo bash <clone>/deploy/install/root.sh apply --user <user> --state <state> \
  --staged <install>/host.env.staged --package tmux --package jq
bash <clone>/deploy/install/root.sh apply --user <user> --dry-run   # what it would do, without root
```

`--package` takes only `tmux`, `jq`, `curl`, `docker.io`, `docker-compose-v2`,
`docker-compose-plugin` and `docker-compose` — curl for claude's native
installer on a machine with neither curl nor wget; `--staged` only when S3
staged the file. Or the same by hand, as root:

| | Done when | Do | Check | Manifest |
|---|---|---|---|---|
| a. packages | `dpkg-query -W -f='${Status}' <pkg>` says `install ok installed` | `apt-get install -y --no-install-recommends <pkg>…`; on "Unable to locate", `apt-get update` first | `command -v <name>` | `root	pkg	<pkg>	by-installer` |
| b. the docker group | `id -nG <user>` holds `docker` | only when docker was installed now: `usermod -aG docker <user>`; the rest of the work reaches docker as `sg docker -c '…'` until the next login | `id -nG <user>` | `root	group	docker <user>	by-installer` |
| c. the state directory | it is there and belongs to `<uid>` | typed as you, so that the ids are yours: `sudo install -d -m 0755 -o "$(id -u)" -g "$(id -g)" <state>`. A directory that belongs to another user is left as it is — it is another install's: `chown -R` it only with the person's word | a file can be written into it as `<user>` | `root	dir	<state>	created` |
| d. the machine description | `<state>/host.env` is the file of S3 | `install -m 0644 -o <uid> -g <gid> <install>/host.env.staged <state>/host.env` | the keys are there | `root	file	<state>/host.env	created`, or `replaced` |
| e. linger | `/var/lib/systemd/linger/<user>` exists | `loginctl enable-linger <user>` | the file is there | `root	linger	<user>	by-installer` — only when it was not on |
| f. the collector's unit | `/etc/systemd/system/aacpanel-agent@.service` is the shipped one | `install -m 0644 deploy/systemd/aacpanel-agent@.service /etc/systemd/system/`, then `systemctl daemon-reload`. For a state directory other than `/var/lib/aacpanel`, change two lines of the installed file and nothing else: `EnvironmentFile=-<state>/host.env` and `ReadWritePaths=<state>` — systemd expands no variable in them | `systemctl cat aacpanel-agent@<user>` | `root	sysunit	/etc/systemd/system/aacpanel-agent@.service	created sha=<sha256 of the file>` |
| g. the collector | `systemctl is-enabled` and `is-active` | `systemctl enable --now aacpanel-agent@<user>.service` | `is-active`, and `<state>/state.json` fresh within 30 seconds | `root	enabled	system aacpanel-agent@<user>.service	by-installer` |

Linger comes before the first `systemctl --user` of the next step: it is what
keeps the user's manager, and `/run/user/<uid>`, alive without a login. If the
collector does not write — `journalctl -u aacpanel-agent@<user> -n 30`; most
often `AACP_REPO` names a place the clone is not.

## S5. `usermgr` — User manager

- **Done when** `systemctl --user is-system-running` answers `running` or
  `degraded`, with `XDG_RUNTIME_DIR=/run/user/<uid>`.
- **Do.** Over ssh without a logind session, from a runner or under `sudo -u`,
  reach the manager by its address:
  `export XDG_RUNTIME_DIR=/run/user/<uid> DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/<uid>/bus`,
  and wait for the socket `/run/user/<uid>/bus` to appear, fifteen seconds at
  most.
- **Check.** The same `systemctl --user is-system-running`. `Failed to connect to
  bus` means linger is off or `user@<uid>` did not come up:
  `systemctl status user@<uid>`.
- **Manifest.** Nothing.

## S5a. `claude-install` — Install claude

Only on a machine without claude, and only with the person's yes. As the user,
never as root: the native installer puts everything under the home directory,
and under sudo it would land in root's.

- **Done when** `<claude> --version` answers, `<claude>` being the answer to
  *What starts claude?* — `~/.local/bin/claude` for the native installer.
- **Do.** Anthropic's native installer, as its documentation gives it:

  ```bash
  curl -fsSL https://claude.ai/install.sh | bash
  ```

  The installer fetches the same script with its own HTTP client and runs
  `bash <install>/claude-install.sh`, then deletes the file. The script needs
  curl or wget for its own downloads: on a machine with neither, S4 installs
  curl first (`root	pkg	curl	by-installer`, named and left at uninstall like
  the other packages).
- **Check.** `~/.local/bin/claude --version` prints a version. Signing in is
  the question of S2, not this step.
- **Manifest.** `claude-install	pkg	claude	by-installer native`, before the
  script runs. Uninstall names it and leaves it, as it leaves the packages of
  apt: `rm -f ~/.local/bin/claude && rm -rf ~/.local/share/claude` removes it.

## S6. `envfile` — Panel settings

- **Done when** `<clone>/.env` is `0600`, holds `AACP_SECRET` and
  `AACP_DB_PASSWORD`, every key below has its value, and
  `docker compose config -q` passes.
- **Do.**

  ```bash
  cp .env.example .env && chmod 0600 .env
  sed -i "s|^#\?AACP_SECRET=.*|AACP_SECRET=$(openssl rand -hex 32)|" .env
  sed -i "s|^#\?AACP_DB_PASSWORD=.*|AACP_DB_PASSWORD=$(openssl rand -hex 24)|" .env
  ```

  The two secrets are made once and never again: a new `AACP_SECRET` signs every
  device out, a new `AACP_DB_PASSWORD` locks the database volume. Then set, on
  the template's own lines:

  | Key | Value |
  |---|---|
  | `AACP_UID`, `AACP_GID` | `<uid>` and `<gid>`: the service runs under them, and they must match the owner of the executor's socket |
  | `AACP_EXEC_DIR` | `/run/user/<uid>/aacpanel-exec` |
  | `AACP_STATE_DIR` | `<state>`: compose does not read the machine description |
  | `AACP_RP_ID`, `AACP_RP_ORIGINS` | the home of the passkeys: `localhost` and `http://localhost:8776`; with a domain, `<domain>` and `https://<domain>`; with Tailscale and no domain, set in S11 |
  | `AACP_LAN_PORT` | only when 8443 is taken: a free port of 18443–18499 |

  And the keys of the ways in the kit holds:

  | Way in | Keys |
  |---|---|
  | own domain | `AACP_PUBLIC_URL=https://<domain>`, `AACP_BIND=<where the proxy reaches the panel>` |
  | home network, TLS | `AACP_LAN_BIND=<address>`, `AACP_TLS_DIR=<directory of the certificate>`, `AACP_TLS_CERT=/tls/<name>.crt`, `AACP_TLS_KEY=/tls/<name>.key`, `AACP_LAN_ADDR=:8443`, `AACP_LAN_PORT=<port on the host>`, `AACP_LAN_URL=https://<name the certificate is for>:<port>` |
  | home network, plain http | `AACP_LAN_BIND=<address>`, `AACP_BIND=<the same address>`, `AACP_SECURE=0` |
  | the home network alone, with a token | `AACP_TOKEN=$(openssl rand -hex 24)` |
  | any way in | `AACP_TERM_PUBLIC=0` or `1` |
  | tuned | `AACP_PUSH_CONTACT`, `AACP_SESSION_IDLE`, `AACP_SESSION_MAX` |

  A way in that is turned off takes its keys back to the template: the line
  returns to its commented form.
- **Check.** `stat -c %a .env` says `600`; `docker compose config --format json`
  runs the service as `<uid>:<gid>` and mounts `/run/user/<uid>/aacpanel-exec`
  as `/run/aacpanel-exec`. Its output carries the secrets: read the two fields,
  keep nothing of it.
- **Manifest.** `envfile	file	<clone>/.env	created data`, then a line a
  secret made: `envfile	envkey	AACP_SECRET	generated`,
  `envfile	envkey	AACP_DB_PASSWORD	generated`, and `AACP_TOKEN` when it was.

`AACP_SECURE=0` lets the cookie travel over plain http too. It has to be set
only where the panel is opened at an address without a certificate: otherwise
the token is accepted, 200 comes back, and an empty sign-in screen returns — the
browser dropped the cookie silently. `AACP_TOKEN` is the second door next to the
passkey; empty, there is no such door, and on a domain that is how it should be.

The local listener on `127.0.0.1:8777` is on unless `.env` says
`AACP_LOCAL_ADDR=` (empty). It lets in any process of the machine without a
sign-in, and the sessions rely on it: their restart, their letters to one
another and the note about an unsent brief go through it, and so do S13 and the
check. Close it only on a machine whose other users must not drive its sessions,
and those go with it.

## S7. `exec-build` — Build the executor

The service travels as an image, the executor as a binary built on the machine.
Built from another tree than the image, it answers the panel with "unknown
action" while the button is alive.

- **Done when** a build of this tree comes out the same as `<home>/bin/aacpanel-exec`:
  with the same Go, `-trimpath` and `-buildvcs=false` the build is the same file.
- **Do.** Built beside itself and renamed into place:

  ```bash
  mkdir -p ~/bin
  GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -buildvcs=false -o ~/bin/aacpanel-exec.new ./cmd/aacpanel-exec
  mv ~/bin/aacpanel-exec.new ~/bin/aacpanel-exec
  ```

  Never over the file: the holders of sessions on the stream run from it, a
  build over a running file answers "text file busy", and a rename leaves the
  running holders on the old copy until their sessions end.
- **Check.** `~/bin/aacpanel-exec -list` names the actions of this machine, and
  every one of them is in `internal/action/action.go`. Without tmux the actions
  over sessions and windows are not in the list.
- **Manifest.** `exec-build	dir	<home>/bin	created` when it was made, and
  `exec-build	file	<home>/bin/aacpanel-exec	created sha=<sha256>`.

## S8. `exec-unit` — Executor unit

**The executor is a user unit**, because it needs the session bus and graphics.

- **Done when** `~/.config/systemd/user/aacpanel-exec.service` is the shipped
  file, the unit is enabled and active, and S7 did not replace the binary.
- **Do.**

  ```bash
  install -Dm644 deploy/systemd/aacpanel-exec.service ~/.config/systemd/user/aacpanel-exec.service
  systemctl --user daemon-reload
  systemctl --user enable aacpanel-exec.service
  systemctl --user restart aacpanel-exec.service
  ```

  For a state directory other than `/var/lib/aacpanel`, change its
  `EnvironmentFile=` line to `-<state>/host.env`. The executor starts before the
  stack on purpose: it makes the directory of its socket itself, and a directory
  compose makes first belongs to root.
- **Check.** `/run/user/<uid>/aacpanel-exec/sock` is a socket, the directory is
  `0700`, both belong to `<uid>`: `stat -c '%u %a %F' …`. No socket —
  `journalctl --user -u aacpanel-exec -n 30`. `226/NAMESPACE` there is an older
  unit without the minus in `InaccessiblePaths`: install the shipped one again.
  A directory of root — `sudo rmdir` it, restart the unit.
- **Manifest.** A line a directory made, from the top down:
  `exec-unit	dir	<home>/.config/systemd/user	created`;
  `exec-unit	userunit	<home>/.config/systemd/user/aacpanel-exec.service	created`;
  `exec-unit	enabled	user aacpanel-exec.service	by-installer`, and
  `exec-unit	dir	<home>/.config/systemd/user/default.target.wants	created` when
  `enable` made it.

## S8a. `gc-timer` — Docker cleanup timer

Only when the kit holds Docker cleanup. Once a week it removes build cache
older than two weeks and untagged images — of the whole machine, not only the
panel's. Both units are the user's: the service reaches docker through the
docker group, as you do.

- **Done when** `~/.config/systemd/user/aacpanel-docker-gc.service` and
  `aacpanel-docker-gc.timer` are the shipped files, and the timer is enabled and
  active.
- **Do.**

  ```bash
  install -Dm644 deploy/systemd/aacpanel-docker-gc.service ~/.config/systemd/user/aacpanel-docker-gc.service
  install -Dm644 deploy/systemd/aacpanel-docker-gc.timer   ~/.config/systemd/user/aacpanel-docker-gc.timer
  systemctl --user daemon-reload
  systemctl --user enable --now aacpanel-docker-gc.timer
  ```

  A timer enabled already, whose files changed, is restarted instead.
- **Check.** `systemctl --user is-enabled aacpanel-docker-gc.timer` and
  `is-active` say `enabled` and `active`; `systemctl --user list-timers` names
  its next run. S14 asks the same and warns when it is off.
- **Manifest.** A line a directory made, as in S8;
  `gc-timer	userunit	<home>/.config/systemd/user/aacpanel-docker-gc.service	created`;
  the same for `aacpanel-docker-gc.timer`;
  `gc-timer	enabled	user aacpanel-docker-gc.timer	by-installer`, and
  `gc-timer	dir	<home>/.config/systemd/user/timers.target.wants	created` when
  `enable` made it. Uninstall disables the timer, removes both files and the
  link, and the directories it made where nothing else lives.

**`gc-timer-off` — Remove the docker cleanup timer.** A later run whose kit
leaves Docker cleanup out, over units of it the manifest holds, takes them away
the way uninstall does: `systemctl --user disable --now` the timer and the
service, both files and the link removed, `systemctl --user daemon-reload`,
and `timers.target.wants` removed if nothing else lives there. Their lines
then leave the manifest; the directories the executor's unit shares stay on
record. Done when the manifest holds no line of the cleanup; units of it the
manifest does not hold are not the installer's and stay.

## S9. `compose` — Panel stack

- **Done when** there is nothing to do: an image built again from the same tree
  comes out of the cache with the same id, and `docker compose up -d` recreates
  no container whose image and configuration hold.
- **Do.**

  ```bash
  BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker compose build aacpanel   # minutes the first time
  docker compose up -d
  ```

  Without the attestation variable BuildKit writes the time of the build into
  the image, its id changes on every build, and `up` recreates the panel every
  time. The service rolls out the database schema itself on its first start.
  `--build-arg SKIP_VULNCHECK=1` builds without the check of the dependencies
  against published vulnerabilities: only knowingly, when one published after
  the release stops the build.
- **Check.** `aacpanel-db` is `healthy` within 90 seconds
  (`docker inspect -f '{{.State.Health.Status}}' aacpanel-db`); within 120
  seconds `http://<AACP_BIND or 127.0.0.1>:8776/healthz` answers 200 and
  `docker compose logs aacpanel` says `store: connected`. If not — the logs, and
  go no further.
- **Manifest.** `compose	compose	aacpanel	project`;
  `compose	volume	aacpanel_aacpanel-db	data` and
  `compose	volume	aacpanel_aacpanel-ts-state	data` for the volumes that were
  not there — `up` makes both, whether the node runs or not;
  `compose	image	aacpanel-aacpanel	local`; and a line
  `compose	image	<ref>	pulled` for every image of
  `docker compose config --images` the machine did not have.

## S10. `approle` — App role

The service works under this role: it has neither DDL nor `TRUNCATE`, so it
cannot wipe the action journal — and the immutability triggers do not hold the
table owner.

- **Done when** `.env` holds both `AACP_APP_ROLE` and `AACP_APP_PASSWORD`, the
  role is in `pg_roles`, and the panel's log since its start has no
  `authentication failed`.
- **Do.**

  ```bash
  p=$(openssl rand -hex 24)
  AACP_APP_PASSWORD=$p AACP_APP_ROLE=monitor_app ./deploy/create-app-role.sh
  sed -i "s|^#\?AACP_APP_ROLE=.*|AACP_APP_ROLE=monitor_app|" .env
  sed -i "s|^#\?AACP_APP_PASSWORD=.*|AACP_APP_PASSWORD=$p|" .env
  unset p
  docker compose up -d aacpanel        # recreated: the DSN of the service changes to the role
  ```

  The name and the password go **together**: compose builds the DSN of the two,
  and a password without its name connects as the owner with a password not the
  owner's — the service comes up, `healthz` answers 200, and it is not connected
  to the database. The script sets the password with `ALTER ROLE` when the role
  is there, so a stop between the role and `.env` heals by running it again. The
  role's rights are issued by the service at every start, not by the script.
- **Check.** The checks of S9; `docker compose logs aacpanel | grep -c
  'authentication failed'` is 0; `http://127.0.0.1:8777/api/profiles` answers
  200.
- **Manifest.** `approle	dbrole	monitor_app	created`,
  `approle	envkey	AACP_APP_ROLE	generated`,
  `approle	envkey	AACP_APP_PASSWORD	generated`.

## S10a. `testdb` — Test database

Only when the kit holds it: the database `make check` gives the tests that need
one. Without it they are skipped silently, and the run reads green.

- **Done when** `.env` holds `AACP_TEST_DSN` and the container
  `aacpanel-test-db` runs and takes connections (`docker exec aacpanel-test-db
  pg_isready -q`).
- **Do.** Port `127.0.0.1:55432` has to be free.

  ```bash
  pw=$(openssl rand -hex 24)
  POSTGRES_PASSWORD=$pw docker run -d --restart unless-stopped --name aacpanel-test-db \
    -p 127.0.0.1:55432:5432 -e POSTGRES_USER=aacpanel -e POSTGRES_PASSWORD -e POSTGRES_DB=aacpanel \
    postgres:18-alpine
  echo "AACP_TEST_DSN=postgres://aacpanel:$pw@127.0.0.1:55432/aacpanel?sslmode=disable" >> .env   # the template has no such line
  unset pw
  ```

  `--restart unless-stopped`: a test database gone after a reboot turns
  `make check` green without its tests. A container of that name made earlier
  is kept, and the DSN is written from its own environment. `make check` reads
  the DSN from `.env`.
- **Check.** `pg_isready` as above.
- **Manifest.** `testdb	image	postgres:18-alpine	pulled` when the image was not
  there, `testdb	testdb	aacpanel-test-db	created`,
  `testdb	envkey	AACP_TEST_DSN	generated`.

## S11. `tailscale` — Tailscale

Only when the kit holds it. **The key is the person's**: ask for it, keep it off
the screen and off the command line.

- **Done when** `docker exec aacpanel-tailscale tailscale status --json` says
  `"BackendState": "Running"` with a `DNSName`, `.env` has no `TS_AUTHKEY`, and
  the keys below hold the node's name.
- **Do.** In `.env`: `AACP_TAILSCALE=1`, `AACP_TS_HOSTNAME=<node name>`,
  `TS_AUTHKEY=<the key>`; beside a domain also
  `AACP_TS_SERVE=./deploy/tailscale/serve-leg.json` and `AACP_TS_ADDR=:8778`.
  Then `docker compose up -d tailscale` and wait for `Running`, ninety seconds
  at most. The node's name, `DNSName` without its last dot, goes into `.env`:
  without a domain as the home of the passkeys, `AACP_RP_ID=<name>` and
  `AACP_RP_ORIGINS=https://<name>`; beside a domain as
  `AACP_TS_URL=https://<name>`. `TS_AUTHKEY` comes out of `.env`, and
  `docker compose up -d` brings the panel up with the new keys.
- **Check.** The node is `Running`, and `docker exec aacpanel-tailscale
  tailscale serve status` proxies to `http://aacpanel:8776` — to
  `http://aacpanel:8778` beside a domain. A node that did not sign in: the key
  expired or a one-time key was used already. A node without a name with a
  certificate: MagicDNS and HTTPS Certificates in the tailnet admin console → DNS.
- **Manifest.** `tailscale	volume	aacpanel_aacpanel-ts-state	data` unless S9
  wrote it, and `tailscale	tsnode	<node name>	` — uninstall reminds that the
  node is removed in the admin console.

## S12. `claude` — Claude settings

For every account of S2: `<account>/settings.json`, that is
`~/.claude/settings.json` and the same file in every other account's directory.
It is edited **as JSON**, not as text: your own hooks are in that file and they
stay.

**Keep a copy first** into `<install>/backups/` (see the manifest above), and
**stop at a file that does not parse** — `python3 -m json.tool <file>` shows
where — without writing to it.

The question hook and the status line go into every account. A session in tmux
reports a question to the panel only through the hook: the record of the call
reaches the transcript after the answer, and without the hook the card never
arrives. A session on the stream hands its question to its holder as a request,
hook or not. The status line script writes the 5-hour and weekly percentages
claude tells the status line of a terminal, at most every twenty seconds,
together with the model and the effort a session in tmux runs; without it the
limits come only from the executor's probe (S3, `AACP_LIMITS_EVERY`). It goes
first in the chain and passes the payload on to the command that was there:

```json
{
  "hooks": {
    "PreToolUse": [
      {"matcher": "AskUserQuestion",
       "hooks": [{"type": "command", "command": "test -f <clone>/agent/ask-hook.py && python3 <clone>/agent/ask-hook.py", "timeout": 5}]}
    ]
  },
  "statusLine": {"type": "command",
    "command": "AACP_STATUSLINE_NEXT='<the command that was there>' bash <clone>/agent/rate-snapshot.sh"},
  "permissions": {"allow": [
    "mcp__aacpanel__checklist", "mcp__aacpanel__brief_publish",
    "mcp__aacpanel__brief_delete", "mcp__aacpanel__notify",
    "mcp__aacpanel__session_restart"]}
}
```

Without a status line of your own it is simply
`"bash <clone>/agent/rate-snapshot.sh"`. The last allow rule is Self-restart:
the restart past the context cap is done with nobody at the screen; the price is
that the model restarts its own session without asking whenever it decides to,
while its agents and background commands still hold the restart off.

The hooks of the kit, each an entry in a group of its own under its event, with
the matcher where there is one. A hook that runs the same script already is
changed where it stands, never added a second time:

| Part | Event | Matcher | Command | Timeout |
|---|---|---|---|---|
| Page copies | `PostToolUse` | `Artifact` | `python3 <clone>/deploy/claude/artifact-copy.py` | 10 |
| Brief reminder | `SessionStart` | — | `python3 <clone>/deploy/claude/brief-waiting.py` | 5 |
| Context cap | `Stop` | — | `python3 <clone>/deploy/claude/context-guard.py` | 5 |
| Checklist nudge | `Stop` | — | `python3 <clone>/deploy/claude/checklist-reminder.py` | 5 |
| Prompt stamp | `UserPromptSubmit` | — | `python3 <clone>/deploy/claude/prompt-stamp.py` | 5 |
| Prompt stamp | `PostToolBatch` | `*` | `python3 <clone>/deploy/claude/prompt-stamp.py PostToolBatch` | 5 |
| Cost snapshot | `Stop`, `SubagentStop` | — | `python3 <clone>/deploy/claude/cost-snapshot.py` | — |
| Background work | `Stop` | — | `python3 <clone>/deploy/claude/background-reminder.py` | 5 |

Each command in the table runs behind a check that its script is there:
`test -f <clone>/<script> && python3 <clone>/<script>`. Python on a missing
file exits 2, which claude reads as "do not stop" for a `Stop` hook, "throw the
prompt away" for `UserPromptSubmit` and "refuse the call" for `PreToolUse`: a
clone moved or a script renamed would hold every session of the account.
Behind the check such a hook exits 1 instead, an error claude reports under
every turn and passes over until the clone is back or the installer runs again.

For a state directory other than `/var/lib/aacpanel`, the commands of the
context cap, the prompt stamp and the background reminder set
`AACP_STATE_DIR='<state>'` before `python3`: the first two read the collector's
snapshot there, the reminder keeps there when it first saw each task.

**The panel's server**, through claude's own command — claude rewrites its
`.claude.json` all the time, so the file is never edited by hand:

```bash
claude mcp add --scope user aacpanel -- <home>/bin/aacpanel-exec -mcp                                  # ~/.claude
CLAUDE_CONFIG_DIR=<account> claude mcp add --scope user aacpanel -- <home>/bin/aacpanel-exec -mcp     # every other account
```

The name is `aacpanel` and no other: claude calls the tools
`mcp__aacpanel__<tool>`, and the allow rules name them so. For a state
directory other than the default, `-e AACP_STATE_DIR=<state>` goes after the
name. A second `add` of the name is refused: a new path goes in after
`claude mcp remove aacpanel -s user`. A session the panel starts gets the same
server on its command line anyway, and claude keeps one server of a name, so
the server in the account serves the sessions started by hand. Such a session
keeps 2048 UTF-16 units of the server's word, claude's default, and cuts the
rest, the lines of the last tools first: the launcher lifts the ceiling for its
own sessions with `CLAUDE_CODE_MAX_MCP_DESCRIPTION_LENGTH=4096`, and a session
started by hand gets the same with the variable in the environment claude
starts in.

Rules of the machine's own for the text of a brief go into
`${XDG_CONFIG_HOME:-~/.config}/aacpanel/brief-guide.md`: the brief tool gives
them to a session after the shipped ones, and without the file there are none.
`deploy/claude/restart-via-panel.py` is no hook but the restart of the
`session_restart` tool for a script or a skill run inside a session; nothing
installs it.

**Sign in** to an account the person chose to sign in to now:
`CLAUDE_CONFIG_DIR=<account> claude` (plain `claude` for `~/.claude`), `/exit`
once done. An account without a sign-in stops nothing; its sessions start once
it is signed in.

- **Done when** every account's settings carry the kit as it is, its
  `.claude.json` (`~/.claude.json` for `~/.claude`) holds the server `aacpanel`
  with this command, and no account chosen for a sign-in now is left without one.
- **Check.** In every account the question hook runs once:

  ```bash
  jq '[.hooks.PreToolUse[]?.hooks[]? | select((.command // "") | contains("agent/ask-hook.py"))] | length' ~/.claude/settings.json   # 1
  jq '.statusLine.command | contains("agent/rate-snapshot.sh")' ~/.claude/settings.json                                     # true
  claude mcp get aacpanel                                                                                                  # Scope: User config
  bash <clone>/agent/rate-snapshot.sh </dev/null                                                                           # exits 0
  ```

- **Manifest.** `claude	json	<account>/settings.json	orig=<copy> sha=<sha256 of the file as written>`,
  or `created sha=…` for a file that was not there; and
  `claude	mcp	<account>	claude=<what starts claude> file=<its .claude.json>`.
  The sum lets uninstall tell whether the file is still as it was written: then
  the copy goes back byte for byte.

## S13. `map` — Map

The first contours, a group and its projects, through the panel's local
listener, as the Profiles screen does: the map keeps its own checks and its
journal. Only with the local listener on.

- **Done when** `curl -s http://127.0.0.1:8777/api/profiles` has a contour of
  every account (by `configDir`), the group in the first one, and every project
  anywhere on the map.
- **Do.** A request a thing, without an `Origin` header:

  ```bash
  curl -s -X POST http://127.0.0.1:8777/api/profiles -H 'Content-Type: application/json' \
    -d '{"name":"personal","configDir":"/home/dev/.claude","launch":{"transport":"stream"}}'
  curl -s -X POST http://127.0.0.1:8777/api/profiles/<contour id>/groups -H 'Content-Type: application/json' \
    -d '{"name":"Projects"}'
  curl -s -X POST http://127.0.0.1:8777/api/groups/<group id>/projects -H 'Content-Type: application/json' \
    -d '{"name":"shop","path":"/home/dev/code/shop"}'
  ```

  A contour, a group or a project that is there already is left where it is. A
  contour name answered with 409 stands for another account: pick another
  name.
- **Check.** `GET /api/profiles` again.
- **Manifest.** `map	profile	contour <name>	created`,
  `map	profile	group <name>	created`, `map	profile	project <path>	created`.
  They live in the database volume and go with it.

## S13a. `collector-restart` — Restart the collector

The collector runs from the tree, so its restart is its rollout.

- **Done when** the collector started after the tree was last installed, and
  neither `host.env` nor its unit changed since.
- **Do.** First the check that every module imports — one that does not keeps
  the collector from starting at all: `(cd agent && python3 importcheck.py)`.
  Then `sudo systemctl restart aacpanel-agent@<user>.service` — **root, so ask**
  — or `sudo bash <clone>/deploy/install/root.sh restart-agent --user <user>`.
  A collector the root part started in this run is on this tree already.
- **Check.** `<state>/state.json` is newer than the restart within 30 seconds.
- **Manifest.** `collector-restart	rev	<git rev-parse --short HEAD>	`.

## S14. `check` — Check

Every link, and a different thing fixes each:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8776/healthz   # the service: 200
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8777/healthz   # the local listener: 200
echo $(( $(date +%s) - $(stat -c %Y /var/lib/aacpanel/state.json) ))      # the collector writes: seconds < 60
~/bin/aacpanel-exec -list | head -3                                      # the binary knows the actions
stat -c '%u %F' "/run/user/$(id -u)/aacpanel-exec/sock"                  # the socket: <uid>, "socket"
curl -s http://127.0.0.1:8777/api/exec                                   # "available": true
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8777/api/profiles                 # the database: 200
docker compose logs aacpanel | grep -c 'authentication failed'                              # 0
docker inspect -f '{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' \
  aacpanel aacpanel-db aacpanel-socket-proxy                             # running, and healthy where it has a healthcheck
systemctl is-active "aacpanel-agent@$USER"; systemctl --user is-active aacpanel-exec          # active, active
stat -c %F /run/aacpanel-agent/ask.sock                                  # the questions of sessions: socket
systemctl --user is-active aacpanel-docker-gc.timer                      # with the docker cleanup: active
```

| Silent | Where to look |
|---|---|
| the service | `docker compose ps`, `docker compose logs aacpanel`; a wrong `AACP_BIND` |
| the collector | `systemctl status "aacpanel-agent@$USER"`, `journalctl -u "aacpanel-agent@$USER" -n 50`; `AACP_REPO` |
| the binary | S7 |
| the socket | none — `systemctl --user status aacpanel-exec`; another uid — `AACP_UID` in `.env` does not match the owner, and the service will not open a `0600` socket |
| the database | `AACP_APP_ROLE` and `AACP_APP_PASSWORD` — both or neither; `docker compose ps aacpanel-db` healthy |
| `ask.sock` | `journalctl -u "aacpanel-agent@$USER" -n 30`: the collector opens it |
| the cleanup's timer | `systemctl --user status aacpanel-docker-gc.timer`; the panel works without it, so it is a warning |

**The test session** — *Open a test session to check the whole chain?*, yes
unless claude is not signed in to the first account, whose session would stop
at the sign-in. When the person agrees: open a session named
`aacpanel-check` through the executor's socket (`session.open`), see it in
`<state>/state.json`, close it (`session.close`) — only it; a live session of
that name already there is not the check's and is left alone.
`./install.sh check --session` does exactly this.

Nothing for the manifest.

## S15. `enroll` — First device

- **Done when** a device is enrolled: `curl -s http://127.0.0.1:8777/api/devices`
  lists one.
- **Do.** `docker compose exec -T aacpanel /aacpanel -enroll` prints a code that
  lives five minutes and works once; show it to the person with the address,
  `http://localhost:8776` — or the domain, or the tailnet name, where the
  passkeys live — and on a machine without a browser
  `ssh -L 8776:127.0.0.1:8776 <user>@<host>` from their computer.
- **Manifest.** Nothing.

## S16. The report

Say what was changed, what was in place and left, and what is left to the
person: an account to sign in to, a phone that signs in with the token, a
docker group that reaches new terminals only after a new login.

## Updating by hand

The whole stack, in this order, every time — also when a change seems to live
in one part:

```bash
git pull
BUILDX_NO_DEFAULT_ATTESTATIONS=1 docker compose build aacpanel
docker compose pull socket-proxy aacpanel-db     # and tailscale where the node runs
docker compose up -d
go build -trimpath -buildvcs=false -o ~/bin/aacpanel-exec.new ./cmd/aacpanel-exec
mv ~/bin/aacpanel-exec.new ~/bin/aacpanel-exec
systemctl --user restart aacpanel-exec.service
sudo systemctl restart "aacpanel-agent@$USER.service"   # the collector runs from the tree
```

The executor restarted after the rename is the new one, while a session on the
stream takes what the new build brings to it only after its own restart, which
keeps the conversation; until then an operation its holder does not know is
refused with those words. Keys of the template missing from `.env` are worth
carrying over:

```bash
diff <(grep -oE '^#?[A-Z_]+=' .env.example | tr -d '#') <(grep -oE '^[A-Z_]+=' .env) | grep '^<'
```

The units are installed again when the shipped ones changed their directives,
with the same edits for a state directory of your own. Then the checks of S14.

## Removing the panel

For an install by hand, without the installer's manifest. The order keeps the
machine working to the end: claude's wiring goes first, while the clone its
hooks run from is there — a hook calling a file that is gone breaks every
question of every session.

1. In every account's `settings.json`: the question hook, the status line
   (put back the command after `AACP_STATUSLINE_NEXT=`, or remove the status
   line), the hooks of `deploy/claude`, the rules allowing `mcp__aacpanel__*`.
   Then `claude mcp remove aacpanel -s user` in every account that has the
   server, or each session there starts with a server that fails to connect.
2. The units and the executor:

   ```bash
   systemctl --user disable --now aacpanel-exec.service aacpanel-docker-gc.timer
   rm -f ~/.config/systemd/user/aacpanel-*.{service,timer} ~/bin/aacpanel-exec
   systemctl --user daemon-reload
   ```

3. The stack: `docker compose down --rmi local`.
4. As root — **ask first**:

   ```bash
   sudo systemctl disable --now "aacpanel-agent@$USER.service"
   sudo rm -f /etc/systemd/system/aacpanel-agent@.service && sudo systemctl daemon-reload
   ```

The rest is data, one thing at a time and with the person's word:

- `docker volume rm aacpanel_aacpanel-db` — the profile map, the journal, the
  history, the device passkeys;
- `docker volume rm aacpanel_aacpanel-ts-state` — the tailnet node's keys;
- `<state>` — the snapshot, the session questions, the conversation index;
- `.env` of the clone — the secrets, without which a volume kept does not open;
- `<home>/.local/state/aacpanel` and `<home>/.local/state/aacpanel-stream` —
  the guards of the map, the checklists of sessions, the answers to permissions
  kept for the feed; `<home>/.local/share/aacpanel-exec` — the files a phone
  sent; `<home>/.cache/aacpanel` — where the executor's probe of the limits
  starts claude;
- `docker rm -f -v aacpanel-test-db` — the test database;
- `sudo loginctl disable-linger "$USER"`, unless something else of yours needs
  user units without a login;
- claude, where the native installer put it for the panel and nothing else of
  yours uses it: `rm -f ~/.local/bin/claude && rm -rf ~/.local/share/claude`.

Delete the clone only after step 1.
