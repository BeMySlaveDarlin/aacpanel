#!/usr/bin/env bash
# Everything the installer does as root, in one file to read before saying yes:
# apt packages, the docker group, the state directory, the host description,
# linger and the collector's unit. It reads nothing from the tree but that unit,
# and before every change it prints a line MANIFEST<TAB>kind<TAB>target<TAB>meta:
# the installer writes it into its manifest, and uninstall takes the change back
# by it. With --dry-run it changes nothing, needs no root, and prints the same
# lines and the commands a real run would give.

set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
unit_src=$here/../systemd/aacpanel-agent@.service
unit=/etc/systemd/system/aacpanel-agent@.service
lingers=/var/lib/systemd/linger
default_state=/var/lib/aacpanel
# The packages the installer offers: nothing else is installed as root.
known_packages=" tmux jq docker.io docker-compose-v2 docker-compose-plugin docker-compose "
clean_path='^/[A-Za-z0-9._/-]+$'

usage() {
	cat <<'EOF'
usage: root.sh apply --user NAME [--state DIR] [--staged FILE] [--package NAME]... [--dry-run]
       root.sh restart-agent --user NAME [--dry-run]
       root.sh remove --user NAME [--state DIR] [--linger] [--purge-state] [--dry-run]

  apply          installs the packages named, adds NAME to the docker group when
                 docker.io is installed now, creates the state directory for NAME
                 (default /var/lib/aacpanel), puts the staged host description in
                 it, enables linger for NAME, installs the collector's unit and
                 enables aacpanel-agent@NAME.
  restart-agent  restarts aacpanel-agent@NAME.
  remove         disables aacpanel-agent@NAME and removes its unit; --linger turns
                 linger off, --purge-state deletes the state directory.
  --dry-run      changes nothing and needs no root: prints what a real run would do.
EOF
	exit "$1"
}

stop() {
	printf 'stop: %s\n' "$*" >&2
	exit 1
}

# bad is an argument root.sh does not take: status 2, as a usage error.
bad() {
	printf 'root.sh: %s; root.sh --help lists what it takes.\n' "$*" >&2
	exit 2
}

# manifest KIND TARGET META comes before the change it stands for.
manifest() { printf 'MANIFEST\t%s\t%s\t%s\n' "$1" "$2" "$3"; }

# run CMD... is a change: a dry run only names it.
run() {
	if [ "$dry" = 1 ]; then
		printf 'would run: %s\n' "$*"
		return 0
	fi
	printf 'root.sh: %s\n' "$*"
	"$@"
}

# render writes the collector's unit for the state directory: byte for byte
# the shipped one at the default place, else with the two lines that name the
# directory, since systemd expands no variable in either.
render() {
	sed -e "s|^EnvironmentFile=-$default_state/host.env\$|EnvironmentFile=-$state/host.env|" \
		-e "s|^ReadWritePaths=$default_state\$|ReadWritePaths=$state|" "$unit_src"
}

installed() { dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q 'install ok installed'; }

apply() {
	if [ -e "$state" ] && [ ! -d "$state" ]; then
		stop "$state is there and is not a directory. Move it away, then run again."
	fi
	if [ -d "$state" ]; then
		local owner
		owner=$(stat -c %u "$state")
		if [ "$owner" != "$uid" ]; then
			stop "$state exists and belongs to $(id -nu "$owner" 2>/dev/null || echo "uid $owner") — left from another install. sudo chown -R $user:$(id -gn "$user") $state (or remove it), then run again."
		fi
	fi
	if [ -n "$staged" ] && [ ! -f "$staged" ]; then
		stop "$staged is not there: ./install.sh writes it before it asks for root. Run ./install.sh again."
	fi

	local missing=() pkg
	for pkg in "${packages[@]}"; do
		installed "$pkg" || missing+=("$pkg")
	done
	if [ ${#missing[@]} -gt 0 ]; then
		for pkg in "${missing[@]}"; do manifest pkg "$pkg" by-installer; done
		# A machine whose apt lists were never fetched knows no package yet.
		run apt-get install -y --no-install-recommends "${missing[@]}" ||
			{ run apt-get update && run apt-get install -y --no-install-recommends "${missing[@]}"; }
		if [[ " ${missing[*]} " == *" docker.io "* ]] && ! id -nG "$user" | grep -qw docker; then
			manifest group "docker $user" by-installer
			run usermod -aG docker "$user"
		fi
	fi

	if [ ! -d "$state" ]; then
		manifest dir "$state" created
		run install -d -m 0755 -o "$uid" -g "$gid" "$state"
	fi
	if [ -n "$staged" ] && ! cmp -s "$staged" "$state/host.env"; then
		if [ -e "$state/host.env" ]; then
			manifest file "$state/host.env" replaced
		else
			manifest file "$state/host.env" created
		fi
		run install -m 0644 -o "$uid" -g "$gid" "$staged" "$state/host.env"
	fi

	if [ ! -e "$lingers/$user" ]; then
		manifest linger "$user" by-installer
		run loginctl enable-linger "$user"
	fi

	if ! render | cmp -s - "$unit"; then
		local how=created sum
		[ -e "$unit" ] && how=replaced
		sum=$(render | sha256sum | cut -d' ' -f1)
		manifest sysunit "$unit" "$how sha=$sum"
		if [ "$dry" = 1 ]; then
			printf 'would write: %s\n' "$unit"
		else
			render >"$unit.new"
			chmod 0644 "$unit.new"
			mv -f "$unit.new" "$unit"
		fi
		run systemctl daemon-reload
	fi

	local agent=aacpanel-agent@$user.service
	if ! systemctl is-enabled --quiet "$agent" 2>/dev/null; then
		manifest enabled "system $agent" by-installer
		run systemctl enable --now "$agent"
	elif ! systemctl is-active --quiet "$agent"; then
		run systemctl start "$agent"
	fi
}

remove() {
	local agent=aacpanel-agent@$user.service
	if systemctl is-enabled --quiet "$agent" 2>/dev/null || systemctl is-active --quiet "$agent" 2>/dev/null; then
		run systemctl disable --now "$agent"
	fi
	if [ -e "$unit" ]; then
		run rm -f "$unit"
		run systemctl daemon-reload
	fi
	if [ "$linger" = 1 ] && [ -e "$lingers/$user" ]; then
		run loginctl disable-linger "$user"
	fi
	if [ "$purge" = 1 ] && [ -d "$state" ]; then
		run rm -rf "$state"
	fi
}

main() {
	local cmd=${1-} given="$*"
	case $cmd in
	apply | restart-agent | remove) shift ;;
	-h | --help | help) usage 0 ;;
	'') bad "a command is missing" ;;
	*) bad "no command $cmd" ;;
	esac
	user='' state=$default_state staged='' dry=0 linger=0 purge=0 packages=()
	local takes=" --user --state "
	[ "$cmd" = apply ] && takes+="--staged --package "
	while [ $# -gt 0 ]; do
		case "$cmd $1" in
		*" -h" | *" --help") usage 0 ;;
		*" --dry-run") dry=1 && shift && continue ;;
		"remove --linger") linger=1 && shift && continue ;;
		"remove --purge-state") purge=1 && shift && continue ;;
		esac
		[[ $takes == *" $1 "* ]] || bad "$cmd takes no $1"
		[ $# -ge 2 ] || bad "$1 takes a value"
		case $1 in
		--user) user=$2 ;;
		--state) state=$2 ;;
		--staged) staged=$2 ;;
		--package)
			[[ $known_packages == *" $2 "* ]] || bad "--package takes${known_packages% }; $2 is not one of them"
			packages+=("$2")
			;;
		esac
		shift 2
	done
	[ -n "$user" ] || bad "$cmd takes --user NAME, the user whose sessions the panel manages"
	for path in "$state" ${staged:+"$staged"}; do
		[[ $path =~ $clean_path && $path != *..* && $path != / ]] ||
			bad "$path is not a plain absolute path: unit files and hook commands cannot carry it"
	done

	uid=$(id -u "$user" 2>/dev/null) || stop "there is no user $user on this machine."
	gid=$(id -g "$user")
	[ "$uid" != 0 ] || stop "the panel runs as a user, not as root: name that user with --user."
	if [ "$dry" = 0 ] && [ "$(id -u)" != 0 ]; then
		stop "root.sh $cmd changes the system and runs as root: sudo bash $0 $given, or --dry-run to see what it would do."
	fi

	case $cmd in
	apply) apply ;;
	restart-agent) run systemctl restart "aacpanel-agent@$user.service" ;;
	remove) remove ;;
	esac
}

main "$@"
