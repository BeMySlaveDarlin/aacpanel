#!/usr/bin/env bash
set -euo pipefail

# Every stand lives in a directory of its own with ports of its own, so two
# of them run side by side: a second one takes another AACP_STAND_VM_DIR and
# other ports. The machine is told apart by the disk in that directory.
DIR=$(realpath -m "${AACP_STAND_VM_DIR:-$HOME/vm/stand}")
OS=${AACP_STAND_OS:-ubuntu24.04}
SSH_PORT=${AACP_STAND_SSH_PORT:-2223}
PANEL_PORT=${AACP_STAND_PANEL_PORT:-8778}
VNC_DISPLAY=${AACP_STAND_VNC:-11}
MEM=${AACP_STAND_MEM:-8G}
CPUS=${AACP_STAND_CPUS:-4}
DISK=${AACP_STAND_DISK:-60G}
KEY=${AACP_STAND_KEY:-$HOME/.ssh/id_rsa.pub}
# AACP_STAND_BARE leaves out docker, tmux, jq, curl and Go: the machine the
# installer has to name them on.
BARE=${AACP_STAND_BARE:-0}
# AACP_STAND_USER is the installing user, uid 1001, with the desktop handed
# to them; AACP_STAND_SUDO says how they get root: nopasswd, or password
# with AACP_STAND_PASSWORD.
INSTALLER=${AACP_STAND_USER:-}
SUDO=${AACP_STAND_SUDO:-nopasswd}
PASSWORD=${AACP_STAND_PASSWORD:-stand}
# AACP_STAND_FOREGROUND=1 keeps qemu in the foreground instead of detaching
# it: whoever runs up holds the machine as a job of its own, and the machine
# ends with that job.
FOREGROUND=${AACP_STAND_FOREGROUND:-0}

case $OS in
ubuntu24.04)
    IMG_URL=${AACP_STAND_IMG:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}
    IMG=noble-cloud.img
    ;;
debian13)
    IMG_URL=${AACP_STAND_IMG:-https://cloud.debian.org/images/cloud/trixie/latest/debian-13-generic-amd64.qcow2}
    IMG=trixie-cloud.qcow2
    ;;
*)
    echo "vm.sh: AACP_STAND_OS is ubuntu24.04 or debian13, not $OS" >&2
    exit 2
    ;;
esac
case $SUDO in
nopasswd | password) ;;
*)
    echo "vm.sh: AACP_STAND_SUDO is nopasswd or password, not $SUDO" >&2
    exit 2
    ;;
esac

usage() {
    cat >&2 <<'TXT'
usage: vm.sh <up|ssh|status|down|purge|user-data>

  up         bring the stand up (fetch the image if missing and wait until it is ready)
  ssh        get inside as the installing user, or as dev when there is none
  status     whether the machine is alive and ssh answers
  down       shut it down
  purge      shut it down and remove the disk holding the cloud image snapshot
  user-data  print the cloud-init user-data up would give a fresh disk
TXT
    exit 2
}

need() {
    command -v "$1" >/dev/null 2>&1 || {
        echo "vm.sh: no $1 - install it: $2" >&2
        exit 1
    }
}

# vm_pid finds the qemu that runs the disk of this directory.
vm_pid() {
    local pid
    for pid in $(pgrep -x qemu-system-x86 || true); do
        if tr '\0' '\n' <"/proc/$pid/cmdline" 2>/dev/null | grep -qF -- "file=$DIR/stand.qcow2,"; then
            echo "$pid"
            return
        fi
    done
}

# user_data prints the cloud-config of the variant asked for. dev (uid 1000)
# is always there and has the whole sudo; the installing user comes next with
# uid 1001 and, when there is one, gets the desktop.
user_data() {
    local key desktop gdm compose login=dev
    key=$(cat "$KEY")
    case $OS in
    ubuntu24.04)
        desktop=ubuntu-desktop-minimal
        compose=docker-compose-v2
        gdm=/etc/gdm3/custom.conf
        ;;
    debian13)
        desktop=gnome-core
        compose=docker-compose
        gdm=/etc/gdm3/daemon.conf
        ;;
    esac
    [ -z "$INSTALLER" ] || login=$INSTALLER

    cat <<EOF
#cloud-config
hostname: stand
users:
  - name: dev
    groups: [sudo]
    shell: /bin/bash
    sudo: ["ALL=(ALL) NOPASSWD:ALL"]
    ssh_authorized_keys:
      - $key
EOF
    if [ -n "$INSTALLER" ]; then
        cat <<EOF
  - name: $INSTALLER
    uid: 1001
    groups: [sudo]
    shell: /bin/bash
EOF
        if [ "$SUDO" = nopasswd ]; then
            echo '    sudo: ["ALL=(ALL) NOPASSWD:ALL"]'
        else
            echo '    lock_passwd: false'
        fi
        cat <<EOF
    ssh_authorized_keys:
      - $key
EOF
        if [ "$SUDO" = password ]; then
            cat <<EOF
chpasswd:
  expire: false
  users:
    - {name: $INSTALLER, password: $PASSWORD, type: text}
EOF
        fi
    fi

    cat <<EOF

package_update: true
packages:
EOF
    local pkg
    for pkg in "$desktop" python3 git openssh-server; do
        echo "  - $pkg"
    done
    if [ "$BARE" != 1 ]; then
        for pkg in docker.io "$compose" golang-go tmux jq curl; do
            echo "  - $pkg"
        done
    fi

    cat <<EOF

runcmd:
EOF
    if [ "$BARE" = 1 ]; then
        # The cloud images bring some of these along.
        echo '  - [apt-get, purge, -y, curl, jq, tmux]'
    else
        # Into the group the package made, as on a machine where docker came
        # first: a group named at the creation of a user would get a gid of
        # its own.
        echo '  - [systemctl, enable, --now, docker]'
        echo '  - [usermod, -aG, docker, dev]'
        [ -z "$INSTALLER" ] || echo "  - [usermod, -aG, docker, $INSTALLER]"
    fi
    cat <<EOF
  - |
    mkdir -p /etc/gdm3
    printf '[daemon]\nAutomaticLoginEnable=true\nAutomaticLogin=$login\nWaylandEnable=true\n' > $gdm
  - [systemctl, set-default, graphical.target]
  - [touch, /var/lib/cloud/stand-ready]

power_state:
  mode: reboot
  timeout: 30
  condition: True
EOF
}

do_up() {
    need qemu-system-x86_64 "apt install qemu-system-x86"
    need cloud-localds "apt install cloud-image-utils"
    [ -r "$KEY" ] || { echo "vm.sh: no key $KEY - set AACP_STAND_KEY" >&2; exit 1; }

    if [ ! -r /dev/kvm ] || [ ! -w /dev/kvm ]; then
        echo "vm.sh: no access to /dev/kvm. Add yourself to the group kvm:" >&2
        echo "  sudo usermod -aG kvm \"\$USER\"   # and log in again" >&2
        exit 1
    fi

    if [ -n "$(vm_pid)" ]; then
        echo "the stand is already up (pid $(vm_pid))"
        return
    fi

    mkdir -p "$DIR/seed"
    if [ ! -f "$DIR/$IMG" ]; then
        echo "fetching the cloud image..."
        curl -fsSL -o "$DIR/$IMG.part" "$IMG_URL"
        mv "$DIR/$IMG.part" "$DIR/$IMG"
    fi

    if [ ! -f "$DIR/stand.qcow2" ]; then
        cp "$DIR/$IMG" "$DIR/stand.qcow2"
        qemu-img resize "$DIR/stand.qcow2" "$DISK" >/dev/null
    fi

    # cloud-init reads the seed once per instance-id: a disk that has come
    # up already keeps the variant it came up with.
    user_data >"$DIR/seed/user-data"
    printf 'instance-id: stand-01\nlocal-hostname: stand\n' > "$DIR/seed/meta-data"
    cloud-localds "$DIR/seed.iso" "$DIR/seed/user-data" "$DIR/seed/meta-data"

    local qemu=(qemu-system-x86_64 -enable-kvm -m "$MEM" -smp "$CPUS" -cpu host
        -drive file="$DIR/stand.qcow2",if=virtio,format=qcow2
        -drive file="$DIR/seed.iso",if=virtio,format=raw,readonly=on
        -netdev user,id=n0,hostfwd=tcp:127.0.0.1:"$SSH_PORT"-:22,hostfwd=tcp:127.0.0.1:"$PANEL_PORT"-:8776
        -device virtio-net-pci,netdev=n0
        -vga virtio -display vnc=127.0.0.1:"$VNC_DISPLAY"
        -name "aacpanel-stand-$(basename "$DIR")")
    if [ "$FOREGROUND" = 1 ]; then
        coming_up
        exec "${qemu[@]}"
    fi
    "${qemu[@]}" -daemonize
    coming_up
}

coming_up() {
    echo "the stand is coming up: ssh on port $SSH_PORT, the panel on $PANEL_PORT, the screen over vnc://127.0.0.1:$((5900 + VNC_DISPLAY))"
    echo "the first install takes some twenty minutes - a desktop is being installed"
}

do_ssh() {
    exec ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -p "$SSH_PORT" "${INSTALLER:-dev}@127.0.0.1" "$@"
}

do_status() {
    local pid
    pid=$(vm_pid)
    if [ -z "$pid" ]; then
        echo "machine: not up"
        return
    fi
    echo "machine: alive (pid $pid)"
    if timeout 5 ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o ConnectTimeout=4 -p "$SSH_PORT" dev@127.0.0.1 \
        'test -f /var/lib/cloud/stand-ready' 2>/dev/null; then
        echo "stand:  ready"
    else
        echo "stand:  still installing (or ssh is still silent)"
    fi
}

do_down() {
    local pid
    pid=$(vm_pid)
    [ -n "$pid" ] || { echo "the machine is not up anyway"; return; }
    kill "$pid"
    echo "shut down"
}

case "${1:-}" in
    up)        do_up ;;
    ssh)       shift; do_ssh "$@" ;;
    status)    do_status ;;
    down)      do_down ;;
    purge)     do_down; rm -f "$DIR/stand.qcow2" "$DIR/seed.iso"; echo "the disk is gone" ;;
    user-data) [ -r "$KEY" ] || { echo "vm.sh: no key $KEY - set AACP_STAND_KEY" >&2; exit 1; }; user_data ;;
    *)         usage ;;
esac
