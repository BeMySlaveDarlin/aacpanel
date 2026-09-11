# The stand: someone else's machine in a container

Tooling, not part of the delivery. This is where it is checked that the panel
installs **from scratch** by [`INSTALL.md`](../../INSTALL.md) onto a foreign
machine: Ubuntu 24.04 with no desktop, no domain, no konsole and no KDE, with a
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
deploy/stand/vm.sh up       # bring it up; downloads the cloud image if missing
deploy/stand/vm.sh status   # whether the machine is alive and the install finished
deploy/stand/vm.sh ssh      # get inside as the dev user
deploy/stand/vm.sh down     # shut down; purge removes the disk as well
```

Inside is Ubuntu 24.04 with GNOME on Wayland, brought up from a cloud image: it
installs without a single keypress, and the environment comes out the same as for
a person who installed the system from a disk. The first run takes some twenty
minutes — a desktop is being installed.

The screen is watched over VNC on `127.0.0.1:5911`, ssh is forwarded to port 2223
and the panel inside to 8778 of the host. All of that is changed by the variables
listed at the top of the script.

KVM needs access to `/dev/kvm`: `sudo usermod -aG kvm "$USER"` and a re-login.
Without it the machine comes up on emulation and installs for hours, so the script
refuses right away and says what to do.

### Whoever has the desktop does the installing

The window to a session opens on the desktop of the user the installation runs
as — for anyone else there is nothing to check it with, and the window is why the
virtual machine exists at all. The cloud image creates `dev` (uid 1000) and puts
that user into the GDM autologin, while the installation is done by a separate
user with uid 1001, so the desktop is handed over:

```sh
sudo sed -i 's/^AutomaticLogin=dev/AutomaticLogin=<user>/' /etc/gdm3/custom.conf
sudo systemctl reboot
```

Get inside as that user from then on, putting the key into their
`~/.ssh/authorized_keys`: without a logind session of its own `systemctl --user`
answers "Failed to connect to bus", and half of the installation would look like a
refusal. They need the whole `sudo`: the instructions install a system unit and
enable linger.

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
with its TUI, dialogs and questions, tailscale and the domain.
