#!/usr/bin/env bash
# The one command that installs the panel: ./install.sh [command] [flags].
# Without a command it installs: it looks the machine over, asks, and
# changes nothing before the plan is approved.
#
# The installer is a Go program, and nothing is built in advance, so this
# script gets it onto the screen: it downloads the Go that go.mod names from
# go.dev into ~/.cache/aacpanel-install, checks the archive against the
# checksum go.dev publishes, builds the installer with it and hands over. The
# system is not touched: no package, no root, nothing outside the cache. A
# second run takes the Go it already has; a new version in go.mod downloads
# the new one and removes the old.
#
# The script also comes on its own, with no clone around it:
#
#   curl -fsSL https://raw.githubusercontent.com/BeMySlaveDarlin/aacpanel/<tag>/install.sh | bash
#
# It then clones the release it belongs to into ${AACP_DIR:-~/aacpanel} and
# runs the install.sh of the clone with the same arguments and the terminal
# as its input. The clone stays for good: the collector and the hooks run
# from it. Run again, the same line goes on in the clone it made.
#
# AACP_VERSION names another release to clone, vP.M.m. AACP_ORIGIN is the
# repository to clone from, AACP_GO_DOWNLOADS a mirror of go.dev/dl with the
# same layout.
#
# Everything is inside main, called on the last line, and the call is in
# braces: bash does not start a braced command it has not read to the end,
# so a download cut short anywhere, even halfway through that line, runs
# nothing.

set -euo pipefail

# The release this script belongs to. make release writes it into the commit
# it tags, so the script taken from a tag clones that very tag.
RELEASE=v1.1.0

stop() {
	printf 'stop: %s\n' "$*" >&2
	exit 1
}

# fetch URL FILE downloads with whatever the machine has: curl, wget, or the
# python3 the collector needs anyway. curl tries again on any error: the
# archive comes from a CDN that now and then drops a stream halfway, which
# curl alone does not count as worth a retry.
fetch() {
	local url=$1 out=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 --retry-all-errors -o "$out" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$out" "$url"
	elif command -v python3 >/dev/null 2>&1; then
		python3 -c 'import shutil, sys, urllib.request
with urllib.request.urlopen(sys.argv[1], timeout=60) as r, open(sys.argv[2], "wb") as f:
    shutil.copyfileobj(r, f)' "$url" "$out"
	else
		stop "there is no curl, wget or python3 here to download Go with. sudo apt install curl, then run ./install.sh again."
	fi
}

# go_version reads the Go of go.mod: the toolchain line when there is one,
# the go line otherwise.
go_version() {
	local v
	v=$(sed -n 's/^toolchain go\([0-9][0-9a-z.]*\)$/\1/p' "$1/go.mod")
	if [ -z "$v" ]; then
		v=$(sed -n 's/^go \([0-9][0-9a-z.]*\)$/\1/p' "$1/go.mod")
	fi
	[ -n "$v" ] || stop "$1/go.mod names no Go version: the clone is damaged. Clone it again."
	printf '%s\n' "$v"
}

# checksum FILE JSON finds the sha256 go.dev publishes for FILE. Every file
# of the listing is an object of its own with no braces inside, so a line
# from one opening brace to the next holds one file and nothing of another,
# and the file is found without a JSON parser.
checksum() {
	tr -d '\n' <"$2" | tr '{' '\n' | grep -F "\"$1\"" |
		sed -n 's/.*"sha256": *"\([0-9a-f]\{64\}\)".*/\1/p' | head -n 1
}

# get_go VERSION ARCH CACHE puts that Go into CACHE/goVERSION unless it is
# there already, and removes every other version.
get_go() {
	local ver=$1 arch=$2 cache=$3
	local dir="$cache/go$ver" file="go$ver.linux-$arch.tar.gz"
	local base=${AACP_GO_DOWNLOADS:-https://go.dev/dl}
	local offline="go.dev does not answer: the installer builds itself with Go it downloads from there. Check the network or a proxy (HTTPS_PROXY) and run again."
	if [ ! -x "$dir/bin/go" ]; then
		local tmp="$cache/download"
		rm -rf "$tmp" "$dir.part"
		mkdir -p "$tmp" "$dir.part"
		printf 'Go %s for the installer: downloading it once into %s\n' "$ver" "$cache" >&2
		fetch "$base/?mode=json&include=all" "$tmp/list.json" || stop "$offline"
		local sum
		sum=$(checksum "$file" "$tmp/list.json")
		[ -n "$sum" ] || stop "go.dev publishes no $file: this machine cannot get the Go the installer is built with."
		fetch "$base/$file" "$tmp/$file" || stop "$offline"
		printf '%s  %s\n' "$sum" "$tmp/$file" | sha256sum -c --status - ||
			{
				rm -rf "$tmp" "$dir.part"
				stop "go$ver does not match the checksum go.dev publishes: the download is damaged or replaced. Run again; if it repeats, report it."
			}
		tar -xzf "$tmp/$file" -C "$dir.part" --strip-components=1
		mv "$dir.part" "$dir"
		rm -rf "$tmp"
	fi
	local old
	for old in "$cache"/go[0-9]*; do
		if [ -e "$old" ] && [ "$old" != "$dir" ]; then
			rm -rf "$old"
		fi
	done
}

# download clones the release this script belongs to and hands over to the
# install.sh of the clone. It never returns.
download() {
	local origin=${AACP_ORIGIN:-https://github.com/BeMySlaveDarlin/aacpanel}
	local dir=${AACP_DIR:-$HOME/aacpanel}
	local tag=${AACP_VERSION:-$RELEASE}
	[ -n "$tag" ] ||
		stop "this install.sh belongs to no release, so it cannot tell which to clone. Take it from a tag (…/aacpanel/<tag>/install.sh), name one with AACP_VERSION=vP.M.m, or clone it yourself: git clone $origin ~/aacpanel && ~/aacpanel/install.sh"
	tag=v${tag#v}
	[[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
		stop "$tag is not a release: a release is vP.M.m, v1.0.0 say."
	command -v git >/dev/null 2>&1 ||
		stop "git is needed to clone the panel: sudo apt install git, then run the same command again."

	if [ -e "$dir" ] && [ -n "$(ls -A "$dir" 2>/dev/null)" ]; then
		local head at
		head=$(git -C "$dir" rev-parse -q --verify HEAD 2>/dev/null) || true
		at=$(git -C "$dir" rev-parse -q --verify "refs/tags/$tag^{commit}" 2>/dev/null) || true
		if [ ! -f "$dir/deploy/install/root.sh" ] || [ -z "$head" ] || [ "$head" != "$at" ]; then
			stop "$dir is there already and is not the clone of $tag. Run the installer of the clone that is there ($dir/install.sh, or $dir/install.sh update to move it on), or clone elsewhere: AACP_DIR=/another/place"
		fi
	else
		printf 'aacpanel %s: cloning it into %s\n' "$tag" "$dir" >&2
		GIT_TERMINAL_PROMPT=0 git -c advice.detachedHead=false clone --quiet --branch "$tag" "$origin" "$dir" ||
			stop "$tag did not clone from $origin: the output above says why. Check the network and the name of the release, then run the same command again."
	fi

	if { : </dev/tty; } 2>/dev/null; then
		exec bash "$dir/install.sh" "$@" </dev/tty
	fi
	exec bash "$dir/install.sh" "$@"
}

main() {
	if [ "$(id -u)" = 0 ]; then
		stop "run the installer as the user whose claude sessions the panel will manage, not as root: the executor refuses to run as root. As that user: ./install.sh"
	fi
	# Read from a pipe, the script has no file of its own; saved by itself,
	# it has no clone around it. Either way the clone comes first.
	local src=${BASH_SOURCE[0]:-} repo arch ver cache
	if [ ! -f "$src" ] || [ ! -f "$(dirname "$src")/deploy/install/root.sh" ]; then
		download "$@"
	fi
	repo=$(cd "$(dirname "$src")" && pwd -P)
	case "$(uname -m)" in
	x86_64) arch=amd64 ;;
	aarch64) arch=arm64 ;;
	*) stop "$(uname -m) is not supported." ;;
	esac
	ver=$(go_version "$repo")
	cache="${XDG_CACHE_HOME:-$HOME/.cache}/aacpanel-install"
	mkdir -p "$cache/bin"
	get_go "$ver" "$arch" "$cache"

	# The build keeps to the cache: its build cache, its modules, and the
	# configuration and telemetry Go keeps under the user's config directory.
	(
		cd "$repo"
		GOTOOLCHAIN=local CGO_ENABLED=0 \
			GOCACHE="$cache/gocache" GOMODCACHE="$cache/gomod" XDG_CONFIG_HOME="$cache/config" \
			"$cache/go$ver/bin/go" build -trimpath -buildvcs=false \
			-o "$cache/bin/aacpanel-install.new" ./cmd/aacpanel-install
	) || stop "the installer did not build: the output above says why. Run ./install.sh again once it is fixed."
	mv -f "$cache/bin/aacpanel-install.new" "$cache/bin/aacpanel-install"

	# The installer reads its answers from the terminal even when this
	# script came through a pipe; with no terminal at all it runs plain.
	export AACP_INSTALL_CLONE="$repo" AACP_INSTALL_GO="$cache/go$ver/bin/go" AACP_INSTALL_CACHE="$cache"
	if { : </dev/tty; } 2>/dev/null; then
		exec "$cache/bin/aacpanel-install" "$@" </dev/tty
	fi
	exec "$cache/bin/aacpanel-install" "$@"
}

{ main "$@"; }
