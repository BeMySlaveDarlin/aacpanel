#!/usr/bin/env bash
set -euo pipefail

DIR=${AACP_STAND_VM_DIR:-$HOME/vm/stand}
IMG_URL=${AACP_STAND_IMG:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}
SSH_PORT=${AACP_STAND_SSH_PORT:-2223}
PANEL_PORT=${AACP_STAND_PANEL_PORT:-8778}
VNC_DISPLAY=${AACP_STAND_VNC:-11}
MEM=${AACP_STAND_MEM:-8G}
CPUS=${AACP_STAND_CPUS:-4}
DISK=${AACP_STAND_DISK:-60G}
KEY=${AACP_STAND_KEY:-$HOME/.ssh/id_rsa.pub}

usage() {
    cat >&2 <<'TXT'
usage: vm.sh <up|ssh|status|down|purge>

  up      bring the stand up (fetch the image if missing and wait until it is ready)
  ssh     get inside as the user dev
  status  whether the machine is alive and ssh answers
  down    shut it down
  purge   shut it down and remove the disk holding the cloud image snapshot
TXT
    exit 2
}

need() {
    command -v "$1" >/dev/null 2>&1 || {
        echo "vm.sh: no $1 - install it: $2" >&2
        exit 1
    }
}

vm_pid() { pgrep -f 'qemu-system-x86_64.*aacpanel-stand' | head -1; }

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
    if [ ! -f "$DIR/noble-cloud.img" ]; then
        echo "fetching the cloud image..."
        curl -fsSL -o "$DIR/noble-cloud.img" "$IMG_URL"
    fi

    if [ ! -f "$DIR/stand.qcow2" ]; then
        cp "$DIR/noble-cloud.img" "$DIR/stand.qcow2"
        qemu-img resize "$DIR/stand.qcow2" "$DISK" >/dev/null
    fi

    cat > "$DIR/seed/user-data" <<EOF
#cloud-config
hostname: stand
users:
  - name: dev
    groups: [sudo, docker]
    shell: /bin/bash
    sudo: ["ALL=(ALL) NOPASSWD:ALL"]
    ssh_authorized_keys:
      - $(cat "$KEY")

package_update: true
packages:
  - ubuntu-desktop-minimal
  - docker.io
  - docker-compose-v2
  - golang-go
  - tmux
  - python3
  - jq
  - curl
  - git
  - openssh-server

runcmd:
  - [systemctl, enable, --now, docker]
  - [usermod, -aG, docker, dev]
  - |
    mkdir -p /etc/gdm3
    printf '[daemon]\nAutomaticLoginEnable=true\nAutomaticLogin=dev\nWaylandEnable=true\n' > /etc/gdm3/custom.conf
  - [systemctl, set-default, graphical.target]
  - [touch, /var/lib/cloud/stand-ready]

power_state:
  mode: reboot
  timeout: 30
  condition: True
EOF
    printf 'instance-id: stand-01\nlocal-hostname: stand\n' > "$DIR/seed/meta-data"
    cloud-localds "$DIR/seed.iso" "$DIR/seed/user-data" "$DIR/seed/meta-data"

    qemu-system-x86_64 -enable-kvm -m "$MEM" -smp "$CPUS" -cpu host \
        -drive file="$DIR/stand.qcow2",if=virtio,format=qcow2 \
        -drive file="$DIR/seed.iso",if=virtio,format=raw,readonly=on \
        -netdev user,id=n0,hostfwd=tcp:127.0.0.1:"$SSH_PORT"-:22,hostfwd=tcp:127.0.0.1:"$PANEL_PORT"-:8776 \
        -device virtio-net-pci,netdev=n0 \
        -vga virtio -display vnc=127.0.0.1:"$VNC_DISPLAY" \
        -name aacpanel-stand -daemonize

    echo "the stand is coming up: ssh on port $SSH_PORT, the panel on $PANEL_PORT, the screen over vnc://127.0.0.1:59$VNC_DISPLAY"
    echo "the first install takes some twenty minutes - a desktop is being installed"
}

do_ssh() {
    exec ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -p "$SSH_PORT" dev@127.0.0.1 "$@"
}

do_status() {
    local pid
    pid=$(vm_pid) || true
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
    pid=$(vm_pid) || true
    [ -n "$pid" ] || { echo "the machine is not up anyway"; return; }
    kill "$pid"
    echo "shut down"
}

case "${1:-}" in
    up)     do_up ;;
    ssh)    shift; do_ssh "$@" ;;
    status) do_status ;;
    down)   do_down ;;
    purge)  do_down; rm -f "$DIR/stand.qcow2" "$DIR/seed.iso"; echo "the disk is gone" ;;
    *)      usage ;;
esac
