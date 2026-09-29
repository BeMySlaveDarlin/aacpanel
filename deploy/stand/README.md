# The stand: someone else's machine in a container

Tooling, not part of the delivery. This is where it is checked that the panel
installs **from scratch** by `./install.sh`, or by hand by
[`INSTALL.md`](../../INSTALL.md), part 2, onto a foreign machine: Ubuntu 24.04 with no desktop, no domain, no konsole and no KDE, with a
user of **uid 1001**.

## Bring it up

```sh
deploy/stand/up.sh          # build the image, start the container, load the tree
deploy/stand/sh.sh          # get inside as the dev user
deploy/stand/sync-repo.sh   # load the tree again (a git bundle of HEAD)
deploy/stand/down.sh        # take it down; --purge removes the image volumes too
```

The stand goes on `/`, where there is room, and not on a flash drive or in `/tmp`
(tmpfs, that is, RAM).

## Second level: a virtual machine with a desktop

A container brings up no graphics, and so it does not check the main thing a
person does by hand — the window to a session. It is opened by a real terminal of
the desktop, and that can only be seen where a desktop exists.

```sh
deploy/stand/vm.sh up         # bring it up; downloads the cloud image if missing
deploy/stand/vm.sh status     # whether the machine is alive and the install finished
deploy/stand/vm.sh ssh        # get inside as the installing user, or dev
deploy/stand/vm.sh down       # shut down; purge removes the disk as well
deploy/stand/vm.sh user-data  # what cloud-init would get, without a machine
```

Inside is Ubuntu 24.04 with GNOME on Wayland, brought up from a cloud image: it
installs without a single keypress, and the environment comes out the same as for
a person who installed the system from a disk. The first run takes some twenty
minutes — a desktop is being installed.

The screen is watched over VNC on `127.0.0.1:5911`, ssh is forwarded to port 2223
and the panel inside to 8778 of the host. All of that is changed by the variables
listed at the top of the script.

### Variants

The variant is chosen by variables, and a disk keeps the variant it first came up
with: cloud-init reads its seed once. Another variant is another directory.

| Variable | What it changes |
|---|---|
| `AACP_STAND_VM_DIR`, `AACP_STAND_SSH_PORT`, `AACP_STAND_PANEL_PORT`, `AACP_STAND_VNC` | where the disk lives and which ports it takes; the machine is found by its disk, so stands in different directories run side by side |
| `AACP_STAND_OS=debian13` | Debian 13 with `gnome-core` instead of Ubuntu 24.04: its compose package is `docker-compose`, its GDM reads `daemon.conf` |
| `AACP_STAND_BARE=1` | no docker, tmux, jq, curl or Go — the cloud images bring curl, jq and tmux along, and they are purged: the machine where the installer has to name each one |
| `AACP_STAND_USER=<name>` | the installing user, uid 1001, gets the desktop, the ssh key and the docker group (unless bare) |
| `AACP_STAND_SUDO=password`, `AACP_STAND_PASSWORD` | that user gets root by password (`stand` unless set) instead of `NOPASSWD` |
| `AACP_STAND_FOREGROUND=1` | `up` keeps qemu in the foreground rather than detaching it, for a caller that holds the machine as a job of its own and wants it gone with that job |

The installer's stand, apart from the one of the landing's shots:

```sh
export AACP_STAND_VM_DIR=~/vm/stand-install AACP_STAND_SSH_PORT=2224 \
       AACP_STAND_PANEL_PORT=8779 AACP_STAND_VNC=12 AACP_STAND_USER=inst
deploy/stand/vm.sh up
```

KVM needs access to `/dev/kvm`: `sudo usermod -aG kvm "$USER"` and a re-login.
Without it the machine comes up on emulation and installs for hours, so the script
refuses right away and says what to do.

### Whoever has the desktop does the installing

The window to a session opens on the desktop of the user the installation runs
as — for anyone else there is nothing to check it with, and the window is why the
virtual machine exists at all. The cloud image creates `dev` (uid 1000), while the
installation is done by a separate user with uid 1001. With `AACP_STAND_USER` that
user is made at the first boot and the GDM autologin is theirs; on a disk that came
up without one, the desktop is handed over by hand:

```sh
sudo sed -i 's/^AutomaticLogin=dev/AutomaticLogin=<user>/' /etc/gdm3/custom.conf
sudo systemctl reboot
```

Get inside as that user (`vm.sh ssh` does, when `AACP_STAND_USER` names them):
without a logind session of its own `systemctl --user` answers "Failed to connect
to bus", and half of the installation would look like a refusal. `dev` stays for
the administration of the machine: `ssh -p <port> dev@127.0.0.1`.

### The tree

Into the container it is put by `sync-repo.sh`; into the virtual machine it goes
as the same bundle of HEAD, only over ssh, and it is cloned by the installing
user — a directory created by somebody else cannot be written to afterwards:

```sh
git bundle create ~/.cache/repo.bundle HEAD
scp -P 2223 ~/.cache/repo.bundle <user>@127.0.0.1:
vm.sh ssh 'git clone ~/repo.bundle ~/Projects/aacpanel'
```

### The screen

GNOME locks the screen after five minutes, and the autologin user has no password
— there is nothing to unlock it with from the keyboard. It is turned off from
inside that session:

```sh
gsettings set org.gnome.desktop.screensaver lock-enabled false
gsettings set org.gnome.desktop.session idle-delay 0
loginctl unlock-session <id>
```

The screen is watched from the host over VNC with any client that can take a shot
(`vncdo -s 127.0.0.1::5911 capture screen.png`). There is no other way to see the
window: on Wayland `xwd` captures XWayland only, and GNOME windows do not go
there.

## How to run the instructions

There is no live claude on the stand, so the part of the installing agent is
played by whoever runs the stand: they read `INSTALL.md` and carry out the steps
as commands through `sh.sh` (it gives a login with a logind session,
`XDG_RUNTIME_DIR` and a bus — without them `systemctl --user` answers "Failed to
connect to bus", and that would look like a failed installation). Every step comes
with its check; a disagreement between the instructions and the machine is a
finding, and it is the instructions that get fixed, not the stand.

The tree arrives as a `git bundle` of HEAD rather than as the working directory:
an uncommitted change would travel to the stand silently. Your own is put on top
through `STAND_PATCH`:

```sh
STAND_PATCH="INSTALL.md deploy/systemd/aacpanel-exec.service" deploy/stand/sync-repo.sh
```

There is one stand: two runs write into the same units, the same `.env` and the
same database. Before starting — `docker exec stand-u2404 docker ps` and
`deploy/stand/sh.sh systemctl --user status`, so as not to land on top of someone
else's installation.

## What is inside and why exactly this

**systemd as PID 1.** The installation puts down two units — a system one (the
agent) and a user one (the executor), enables linger and expects `systemctl
--user` to exist. Without systemd the stand would check half of the installation
and say nothing about the other half.

**Its own docker daemon (dind) rather than the host socket.** The panel comes up
through `docker compose up -d` and creates containers with fixed names
(`aacpanel`, `aacpanel-db`, `aacpanel-socket-proxy`). With the socket passed in,
the first run of the stand would collide with the production ones by name. Its own
namespace removes that possibility entirely: `docker exec stand-u2404 docker ps`
does not see the production containers at all.

**uid 1001, and that is no accident.** On a clean machine the first user gets
1000, and a stand running under 1000 would not catch any default of "assume 1000"
in the delivery: uid 1000 is taken by the `ubuntu` user of the base image, and our
`dev` is created next and explicitly.

**Of the dependencies only docker, python3, git and Go are preinstalled.** Neither
tmux, nor jq, nor claude — their absence is exactly what is being checked: the
step that looks the machine over has to name each one and explain its price. They
are installed later, by the very command the instructions name.

**Locales are `en_US.UTF-8` and `C.UTF-8` only.** A colleague may have no Russian
one at all; the panel has to work on such a machine too.

## The stand-in claude

There is no live claude on the stand: an account for a test run is unnecessary,
and a live one would spend real subscription limits on the stand. That does not
cost us half of the road — `fake-claude.sh` plays a live session, and on it the
launcher, tmux, session recognition by the agent and the card in the panel are
checked, the test session of the installation check included. It writes a session
file with the pid, the process start time and the kind, that is, the fields the
panel recognises a session by.

```sh
docker cp deploy/stand/fake-claude.sh stand-u2404:/home/dev/bin/claude
docker exec stand-u2404 chown dev:dev /home/dev/bin/claude
```

The executor unit does not keep `~/bin` in PATH, so in the `host.env` of the stand
the stand-in is named explicitly: `AACP_CLAUDE=/home/dev/bin/claude`. The account
directory that a first run of claude leaves on a live machine is created here by
hand: `mkdir -p ~/.claude/sessions && echo '{}' > ~/.claude/settings.json`.

What the installer asks of claude outside a session the stand-in answers the way
claude does: `--version`, and `claude mcp add|get|remove|list` in the user scope
with the same messages, streams and exit codes, keeping the server in
`$CLAUDE_CONFIG_DIR/.claude.json`, or `~/.claude.json` without it. A call into
the local or project scope, or of a command it does not play (`auth`, `doctor`
and the rest), stops with status 2 instead of waiting for input like a session.

## Traces: what an install leaves

`traces.sh` lists what an install leaves on the machine — units, `~/bin`, the
`settings.json` and MCP servers of each claude account, linger, the state
directory, the directories of the executor and the installer, the `.env`, docker
containers, volumes, networks and images, apt packages and the docker group —
one sorted line each, files with their sha256. Taken before the install and
after the uninstall, the two lists are compared:

```sh
deploy/stand/traces.sh > before
./install.sh install && ./install.sh uninstall --purge-data
deploy/stand/traces.sh > after
deploy/stand/traces.sh compare before after
```

`compare` fails on every line uninstall promises to put back, and only names
the packages, the units they enable and the docker group, which uninstall never
takes back, and the docker lists of a machine where docker did not answer before. `traces.sh starts`
prints the invocation of every `aacpanel*` unit and the start of every container:
the same before and after a second run means the run restarted nothing. The same
steps run in the CI job `install.yml` on a runner, which is a whole virtual
machine with systemd, docker and sudo.

## Questions answered through tmux

The installer is a TUI in which a digit picks an option at once and Enter takes
whatever the cursor is on, so a key sent blind answers a question nobody saw.
`scenario.sh` plays a scenario against it in a tmux server of its own: before
each key it waits for the text of the screen the key answers, and a scenario
with a key that no wait comes before is refused before the program starts.

```sh
cat > enter.txt <<'EOF'
timeout 60
wait What does the panel call this machine?
key Enter
wait Name of the home session
key Enter
wait Where do your projects live?
shot projects
key C-c
exit 130
EOF
deploy/stand/scenario.sh -x 60 -o out enter.txt -- ./install.sh install
```

`out/transcript.txt` keeps every screen a wait matched and every key sent; the
directives are listed at the top of the script.

## Traps of the stand (not of the panel)

**`/var/lib/containerd` has to be on a volume.** The Ubuntu daemon keeps images
with the containerd snapshotter, that is, not in `/var/lib/docker`. Left on the
container overlayfs it gives a nested overlay over overlay, and the build of the
panel image fails on the very first `WORKDIR`:

```
failed to solve: mount source: "overlay", … err: invalid argument
```

That reads as trouble with the panel build. Both directories are moved onto
volumes (`stand-docker`, `stand-containerd`).

**Disks in the agent snapshot look odd.** Inside a container `/proc/mounts` shows
the bind mounts of docker, and the panel shows `/etc/hostname` instead of `/`.
That is a property of the stand, not of the agent.

## What the stand does not check

The terminal window, GNOME and Wayland, logging in from a phone, a live claude
with its TUI, dialogs and questions, tailscale and the domain. Nor a session on
the stream: the stand-in plays a console and nothing of `claude -p` with
stream-json, so the holder, the feed and the panel's tools in a session are
checked only where a real claude runs.
