#!/bin/sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
cd "$here"

if [ "${1:-}" = "--purge" ]; then
    docker compose down -v
else
    docker compose down
fi
