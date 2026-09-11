#!/bin/sh
set -eu

repo=$(cd "$(dirname "$0")/../.." && pwd)
stand=${STAND_CONTAINER:-stand-u2404}
dest=${STAND_REPO:-/home/dev/aacpanel}

rev=$(git -C "$repo" rev-parse --short HEAD)
echo "stand $stand <- $repo @ $rev -> $dest"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
git -C "$repo" bundle create "$tmp/repo.bundle" HEAD >/dev/null 2>&1

docker exec "$stand" rm -rf "$dest.bundle" "$dest.new"
docker cp "$tmp/repo.bundle" "$stand:$dest.bundle"
docker exec "$stand" chown dev:dev "$dest.bundle"
docker exec -u dev "$stand" git -c advice.detachedHead=false clone -q "$dest.bundle" "$dest.new"
docker exec -u dev "$stand" sh -c "cd '$dest.new' && git remote remove origin"

docker exec -u dev "$stand" sh -c "[ -f '$dest/.env' ] && cp '$dest/.env' '$dest.new/.env' || true"
docker exec "$stand" sh -c "rm -rf '$dest.old' && { [ -d '$dest' ] && mv '$dest' '$dest.old' || true; } && mv '$dest.new' '$dest' && rm -rf '$dest.old' '$dest.bundle'"
for file in ${STAND_PATCH:-}; do
    echo "  on top of HEAD: $file"
    docker exec "$stand" install -d -o dev -g dev "$dest/$(dirname "$file")"
    docker cp "$repo/$file" "$stand:$dest/$file"
    docker exec "$stand" chown dev:dev "$dest/$file"
done

docker exec "$stand" sh -c "echo $rev > $dest/.stand-rev && chown dev:dev $dest/.stand-rev"
echo "done"
