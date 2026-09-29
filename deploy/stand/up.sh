#!/bin/sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
cd "$here"

docker compose build
docker compose up -d

printf 'waiting for systemd and docker inside the stand'
i=0
until docker exec stand-u2404 docker info >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
        echo
        echo "did not come up in 60 s; look at: docker exec stand-u2404 systemctl --failed" >&2
        exit 1
    fi
    printf .
    sleep 1
done
echo " done"

"$here/sync-repo.sh"

cat <<'TXT'

Next comes the install from the inside, as dev:
  deploy/stand/sh.sh                  # get inside the stand
  cd ~/aacpanel && ./install.sh       # or by hand, by INSTALL.md, part 2
TXT
