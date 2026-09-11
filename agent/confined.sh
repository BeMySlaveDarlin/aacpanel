#!/usr/bin/env bash
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
unit="$root/deploy/systemd/aacpanel-agent@.service"

if [ ! -f "$unit" ]; then
	echo "!! the unit is not in place, there is nowhere to take the restrictions from: $unit"
	exit 1
fi

mapfile -t rules < <(grep -E '^(NoNewPrivileges|Protect[A-Za-z]+|Restrict[A-Za-z]+|Private[A-Za-z]+|LockPersonality|MemoryDenyWriteExecute|SystemCallFilter|CapabilityBoundingSet|ReadOnlyPaths|InaccessiblePaths|UMask)=' "$unit")

if [ "${#rules[@]}" -eq 0 ]; then
	echo "!! not a single restriction found in the unit - the run would be checking emptiness: $unit"
	exit 1
fi

bus="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus"

available() {
	command -v systemd-run >/dev/null 2>&1 || return 1
	[ -S "$bus" ] || return 1
	DBUS_SESSION_BUS_ADDRESS="unix:path=$bus" \
		systemd-run --user --wait --pipe --collect --quiet /bin/true >/dev/null 2>&1
}

if ! available; then
	echo "!! the systemd user manager is unavailable: the collector's tests inside the unit's restrictions were not run"
	exit 0
fi

state=$(mktemp -d "${TMPDIR:-/var/tmp}/aacpanel-confined-state.XXXXXX") || exit 1
tmp=$(mktemp -d "${TMPDIR:-/var/tmp}/aacpanel-confined-tmp.XXXXXX") || exit 1
trap 'chmod -R u+w "$state" "$tmp" 2>/dev/null; rm -rf "$state" "$tmp"' EXIT

args=()
for rule in "${rules[@]}"; do
	args+=(-p "$rule")
done

DBUS_SESSION_BUS_ADDRESS="unix:path=$bus" \
	systemd-run --user --wait --pipe --collect --quiet \
	"${args[@]}" \
	-p "ReadWritePaths=$state $tmp" \
	-p "WorkingDirectory=$root" \
	--setenv=AACP_STATE_DIR="$state" \
	--setenv=AACP_TEST_TMPDIR="$tmp" \
	--setenv=AACP_TEST_CONFINED=1 \
	--setenv=PYTHONDONTWRITEBYTECODE=1 \
	python3 -m unittest discover -s agent -t agent -p 'test_*.py'
