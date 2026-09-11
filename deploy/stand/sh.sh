#!/bin/sh
set -eu
stand=${STAND_CONTAINER:-stand-u2404}
exec docker exec -it -u dev \
    -e HOME=/home/dev \
    -e USER=dev \
    -e XDG_RUNTIME_DIR=/run/user/1001 \
    -e DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus \
    -e TERM=xterm-256color \
    -e LANG=en_US.UTF-8 \
    -w /home/dev/aacpanel \
    "$stand" "$@"
