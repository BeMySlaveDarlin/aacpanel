#!/usr/bin/env bash
# What an install leaves on a machine, one sorted line a trace:
# KIND<TAB>NAME<TAB>WHAT. Files come with their mode, owner and sha256, links
# with their target, directories with their mode and owner, so a directory
# left empty is a trace as well. Taken before an install and after its
# uninstall, the two lists have to agree on everything uninstall promises to
# put back.
#
#   traces.sh                  the traces of this machine
#   traces.sh starts           when the panel's units and every container
#                              started: a run that changes nothing restarts
#                              nothing, so two of these around it agree
#   traces.sh compare OLD NEW  the lines that differ; status 1 when one of
#                              them is a trace uninstall promises to take back
#
# What is looked at: the system and user units, ~/bin, the settings.json,
# rate-limits.json and session-models of every claude account (~/.claude and
# ~/.claude-*), the names of mcpServers in each .claude.json with a sha256 of
# their entry, linger, the state directory (AACP_STATE_DIR, /var/lib/aacpanel
# by default), the directories of the executor and of the installer with
# their caches, the .env of this clone, docker containers, volumes, networks
# and images, the installed apt packages and the docker group. A network is
# listed with its driver, not its id: docker makes its bridge anew every
# time the daemon starts.
#
# AACP_TRACES_ROOT puts every path under another root and prints it as it is
# on the machine; docker, systemctl and getent still answer for this one.
# Docker that does not let the user in is asked through sudo -n.

set -euo pipefail

root=${AACP_TRACES_ROOT:-}
clone=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)

usage() {
	sed -n 's/^#   //p' "${BASH_SOURCE[0]}" >&2
	exit 2
}

# tree KIND PATH prints PATH and everything under it; nothing when it is not
# there.
tree() {
	local kind=$1 real=$root$2 type path meta link sum
	[ -e "$real" ] || [ -L "$real" ] || return 0
	while IFS=$'\t' read -r type path meta link; do
		path=${path#"$root"}
		case $type in
		f)
			sum=$(sha256sum -- "$root$path" 2>/dev/null) || sum=unreadable
			printf '%s\t%s\tfile %s %s\n' "$kind" "$path" "$meta" "${sum%% *}"
			;;
		l) printf '%s\t%s\tlink %s\n' "$kind" "$path" "$link" ;;
		d) printf '%s\t%s\tdir %s\n' "$kind" "$path" "$meta" ;;
		*) printf '%s\t%s\t%s %s\n' "$kind" "$path" "$type" "$meta" ;;
		esac
	done < <(find "$real" -printf '%y\t%p\t%m %u\t%l\n' 2>/dev/null)
}

# mcp FILE prints the MCP servers of a .claude.json: their names and a
# sha256 of each entry. The rest of the file claude rewrites all the time.
mcp() {
	local file=$1
	[ -f "$root$file" ] || return 0
	python3 - "$root$file" "$file" <<'PY'
import hashlib, json, sys

path, shown = sys.argv[1], sys.argv[2]
try:
    with open(path, encoding="utf-8") as f:
        servers = json.load(f).get("mcpServers") or {}
except (OSError, ValueError) as e:
    print("mcp\t%s\tunreadable: %s" % (shown, e.__class__.__name__))
    sys.exit(0)
for name, entry in servers.items():
    blob = json.dumps(entry, sort_keys=True, separators=(",", ":")).encode()
    print("mcp\t%s %s\t%s" % (shown, name, hashlib.sha256(blob).hexdigest()))
PY
}

accounts() {
	local dir
	for dir in "$root$HOME"/.claude "$root$HOME"/.claude-*; do
		[ -d "$dir" ] || continue
		dir=${dir#"$root"}
		tree account "$dir/settings.json"
		tree account "$dir/rate-limits.json"
		tree account "$dir/session-models"
		mcp "$dir/.claude.json"
	done
	mcp "$HOME/.claude.json"
}

# docker_cmd finds how docker answers this user: directly, through sudo -n,
# or not at all.
docker=()
docker_cmd() {
	if ! command -v docker >/dev/null 2>&1; then
		return 1
	fi
	if docker info >/dev/null 2>&1; then
		docker=(docker)
	elif sudo -n docker info >/dev/null 2>&1; then
		docker=(sudo -n docker)
	else
		return 1
	fi
}

dockers() {
	if ! command -v docker >/dev/null 2>&1; then
		printf 'docker\t-\tabsent\n'
		return
	fi
	if ! docker_cmd; then
		printf 'docker\t-\tunreachable\n'
		return
	fi
	"${docker[@]}" ps -a --no-trunc --format '{{.Names}}{{"\t"}}{{.Image}} {{.ID}}' | sed 's/^/docker-container\t/'
	"${docker[@]}" volume ls --format '{{.Name}}{{"\t"}}{{.Driver}}' | sed 's/^/docker-volume\t/'
	"${docker[@]}" network ls --format '{{.Name}}{{"\t"}}{{.Driver}}' | sed 's/^/docker-network\t/'
	"${docker[@]}" image ls --no-trunc --format '{{.Repository}}:{{.Tag}}{{"\t"}}{{.ID}}' | sed 's/^/docker-image\t/'
}

packages() {
	command -v dpkg-query >/dev/null 2>&1 || return 0
	dpkg-query --admindir="$root/var/lib/dpkg" -W -f='${db:Status-Abbrev}\t${binary:Package}\t${Version}\n' |
		awk -F'\t' '$1 ~ /^ii/ { print "pkg\t" $2 "\t" $3 }'
}

# group prints the docker group a member a line, so that a user added to it
# is a line added rather than a line changed.
group() {
	local line gid members member
	line=$(getent group docker) || return 0
	IFS=: read -r _ _ gid members <<<"$line"
	printf 'group\tdocker\tgid %s\n' "$gid"
	IFS=, read -ra members <<<"$members"
	for member in "${members[@]}"; do
		printf 'group\tdocker\tmember %s\n' "$member"
	done
}

traces() {
	local state=${XDG_STATE_HOME:-$HOME/.local/state}
	{
		tree sysunit /etc/systemd/system
		tree userunit "$HOME/.config/systemd/user"
		tree bin "$HOME/bin"
		accounts
		tree linger /var/lib/systemd/linger
		tree state "${AACP_STATE_DIR:-/var/lib/aacpanel}"
		tree userdata "$state/aacpanel"
		tree userdata "$state/aacpanel-stream"
		tree userdata "$state/aacpanel-install"
		tree userdata "${XDG_CACHE_HOME:-$HOME/.cache}/aacpanel-install"
		tree userdata "${XDG_CACHE_HOME:-$HOME/.cache}/aacpanel"
		tree userdata "${XDG_DATA_HOME:-$HOME/.local/share}/aacpanel-exec"
		tree userdata "$HOME/.config/aacpanel"
		tree clone "$clone/.env"
		dockers
		packages
		group
	} | LC_ALL=C sort
}

# starts prints the invocation of every unit named aacpanel*, of the system
# and of the user, and the start time of every container.
starts() {
	local unit units
	# A shell without a login session has no XDG_RUNTIME_DIR, and systemctl
	# --user finds the manager of linger by it.
	export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}
	{
		units=$(systemctl list-units --all --plain --no-legend 'aacpanel*')
		while read -r unit _; do
			[ -n "$unit" ] || continue
			printf 'unit\tsystem %s\t%s\n' "$unit" "$(systemctl show -p InvocationID --value "$unit")"
		done <<<"$units"
		if units=$(systemctl --user list-units --all --plain --no-legend 'aacpanel*' 2>/dev/null); then
			while read -r unit _; do
				[ -n "$unit" ] || continue
				printf 'unit\tuser %s\t%s\n' "$unit" "$(systemctl --user show -p InvocationID --value "$unit")"
			done <<<"$units"
		else
			printf 'unit\tuser\tno user manager\n'
		fi
		if docker_cmd; then
			"${docker[@]}" ps -aq --no-trunc | while read -r id; do
				"${docker[@]}" inspect --format '{{.Name}}{{"\t"}}{{.State.StartedAt}}' "$id" | sed 's/^/container\t/'
			done
		else
			printf 'container\t-\tdocker does not answer\n'
		fi
	} | LC_ALL=C sort
}

# compare OLD NEW prints the lines that differ, - for gone and + for new.
# Packages and the docker group are never taken back by uninstall, and
# docker that did not answer before has lists nothing can be compared with:
# those lines are named, and only the rest makes the status 1.
compare() {
	local old=$1 new=$2
	if [ ! -r "$old" ] || [ ! -r "$new" ]; then
		usage
	fi
	local docker_before=1
	if grep -qP '^docker\t-\t(absent|unreachable)$' "$old"; then
		docker_before=0
	fi
	# diff says 1 for "they differ"; what differs is judged below.
	{
		diff --old-line-format='- %L' --new-line-format='+ %L' --unchanged-line-format='' \
			<(LC_ALL=C sort "$old") <(LC_ALL=C sort "$new") || [ $? -eq 1 ]
	} |
		awk -F'\t' -v docker_before="$docker_before" '
			{
				kind = substr($1, 3)
				if (kind == "pkg" || kind == "group") { kept[++k] = $0; next }
				if (docker_before == 0 && (kind == "docker" || kind ~ /^docker-/)) { unasked[++u] = $0; next }
				bad[++b] = $0
			}
			END {
				for (i = 1; i <= b; i++) print bad[i]
				if (b) printf "traces: %d line(s) differ that uninstall promises to put back\n", b
				if (k) {
					print "left on purpose — packages and the docker group are never taken back:"
					for (i = 1; i <= k; i++) print "  " kept[i]
				}
				if (u) {
					print "not compared — docker did not answer before the install:"
					for (i = 1; i <= u; i++) print "  " unasked[i]
				}
				if (!b) print "traces: uninstall put back everything it promises to"
				exit b ? 1 : 0
			}'
}

case "${1-}" in
'') traces ;;
starts) starts ;;
compare)
	[ $# -eq 3 ] || usage
	compare "$2" "$3"
	;;
*) usage ;;
esac
