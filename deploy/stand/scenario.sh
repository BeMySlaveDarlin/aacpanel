#!/usr/bin/env bash
# Plays a scenario against an interactive program in tmux, the way a person
# would: a key goes only once the screen shows what it answers. A digit picks
# an option at once and Enter takes whatever the cursor is on, so a key sent
# blind answers a question nobody has seen; a scenario that puts a key without
# a wait before it is refused before anything starts.
#
#   scenario.sh [-o DIR] [-x COLS] [-y ROWS] SCENARIO -- COMMAND [ARG...]
#
# The program runs in a tmux server of its own, so no other tmux on the
# machine is touched, and the server goes when the scenario ends. DIR gets the
# transcript — every screen a wait matched, every key sent — and the shots.
#
# A scenario is a file of lines, one directive each; # starts a comment:
#
#   timeout SECONDS    how long each wait below may take (30 at first)
#   wait TEXT          until TEXT is on the screen
#   gone TEXT          until TEXT is off the screen
#   key KEY            one key, by its tmux name: Enter, Escape, Up, Down,
#                      Tab, Space, BSpace, C-c, 1 ...
#   type TEXT          TEXT into a field, as one paste
#   resize COLS [ROWS] the terminal changes its size
#   shot NAME          the screen now, into DIR/NAME.txt
#   exit CODE          until the program ends, and it has to end with CODE

set -euo pipefail

usage() {
	sed -n 's/^#   scenario.sh/scenario.sh/p' "${BASH_SOURCE[0]}" >&2
	exit 2
}

out='' cols=80 rows=30
while getopts o:x:y: opt; do
	case $opt in
	o) out=$OPTARG ;;
	x) cols=$OPTARG ;;
	y) rows=$OPTARG ;;
	*) usage ;;
	esac
done
shift $((OPTIND - 1))
if [ $# -lt 3 ] || [ "$2" != -- ]; then
	usage
fi
file=$1
shift 2
[ -r "$file" ] || { echo "scenario.sh: cannot read $file" >&2; exit 2; }

# The scenario is read whole first: a mistake in it stops the run before the
# program starts, rather than halfway through an install.
steps=() lines=() seen=0 n=0
while IFS= read -r line || [ -n "$line" ]; do
	n=$((n + 1))
	line=${line#"${line%%[![:space:]]*}"}
	line=${line%"${line##*[![:space:]]}"}
	case $line in '' | '#'*) continue ;; esac
	verb=${line%% *}
	arg=''
	[ "$verb" = "$line" ] || arg=${line#* }
	case $verb in
	wait) seen=1 ;;
	key | type)
		if [ "$seen" = 0 ]; then
			echo "scenario.sh: $file:$n: $verb without a wait before it — a key goes only after the screen it answers is seen" >&2
			exit 2
		fi
		seen=0
		;;
	gone | shot | resize | timeout | exit) ;;
	*)
		echo "scenario.sh: $file:$n: no directive $verb" >&2
		exit 2
		;;
	esac
	case $verb in
	key) [[ $arg =~ ^[^[:space:]]+$ ]] || { echo "scenario.sh: $file:$n: key takes one key" >&2; exit 2; } ;;
	timeout | exit) [[ $arg =~ ^[0-9]+$ ]] || { echo "scenario.sh: $file:$n: $verb takes a number" >&2; exit 2; } ;;
	resize) [[ $arg =~ ^[0-9]+( [0-9]+)?$ ]] || { echo "scenario.sh: $file:$n: resize takes COLS [ROWS]" >&2; exit 2; } ;;
	wait | gone | type | shot) [ -n "$arg" ] || { echo "scenario.sh: $file:$n: $verb takes a text" >&2; exit 2; } ;;
	esac
	steps+=("$verb $arg")
	lines+=("$n")
done <"$file"

if [ -z "$out" ]; then
	out=$(mktemp -d "${TMPDIR:-/tmp}/scenario.XXXXXX")
fi
mkdir -p "$out"
log=$out/transcript.txt
: >"$log"

sock=aacpanel-scenario-$$
t() { tmux -L "$sock" "$@"; }
# kill-server leaves its socket file behind, so the file goes by hand.
cleanup() {
	local path
	path=$(t display-message -p -t scenario '#{socket_path}' 2>/dev/null) || true
	t kill-server >/dev/null 2>&1 || true
	[ -z "$path" ] || rm -f "$path"
}
trap cleanup EXIT

# remain-on-exit keeps the pane of a program that ended, with its status, and
# it has to be set before the program starts: the server reads it from its
# configuration.
printf 'set -g remain-on-exit on\nset -g status off\nset -g history-limit 10000\n' >"$out/tmux.conf"
t -f "$out/tmux.conf" new-session -d -s scenario -x "$cols" -y "$rows" "$@"

screen() { t capture-pane -p -J -t scenario; }

# dead prints how the program ended once it has: its exit status, or the
# signal that killed it; nothing otherwise. tmux marks the pane dead when its
# terminal closes and learns the status a moment later, when it reaps the
# program: a dead pane without either is not over yet.
dead() {
	local d s g
	IFS=: read -r d s g <<<"$(t display-message -p -t scenario '#{pane_dead}:#{pane_dead_status}:#{pane_dead_signal}')"
	[ "$d" = 1 ] || return 0
	if [ -n "$s" ]; then
		printf '%s\n' "$s"
	elif [ -n "$g" ]; then
		printf 'signal %s\n' "$g"
	fi
}

note() { printf '%s\n' "$*" >>"$log"; }

fail() {
	local s
	s=$(screen 2>/dev/null || true)
	{
		printf 'scenario.sh: %s:%s: %s\n' "$file" "$at" "$*"
		printf -- '--- the screen:\n%s\n---\n' "$s"
	} | tee -a "$log" >&2
	echo "the transcript: $log" >&2
	exit 1
}

# await on|off TEXT waits until TEXT is on the screen, or off it.
await() {
	local how=$1 text=$2 deadline=$((SECONDS + limit)) s status
	while :; do
		s=$(screen) || fail "tmux is gone"
		if grep -qF -- "$text" <<<"$s"; then
			[ "$how" = off ] || { note "=== seen: $text"; note "$s"; return 0; }
		elif [ "$how" = off ]; then
			note "=== gone: $text"
			return 0
		fi
		status=$(dead)
		[ -z "$status" ] || fail "the program ended with $status while waiting for \"$text\" to be $how the screen"
		[ "$SECONDS" -lt "$deadline" ] || fail "\"$text\" was not $how the screen in ${limit}s"
		sleep 0.2
	done
}

limit=30
for i in "${!steps[@]}"; do
	at=${lines[$i]}
	step=${steps[$i]}
	verb=${step%% *}
	arg=${step#* }
	case $verb in
	timeout) limit=$arg ;;
	wait) await on "$arg" ;;
	gone) await off "$arg" ;;
	key)
		note "=== key: $arg"
		t send-keys -t scenario -- "$arg"
		;;
	type)
		note "=== type: $arg"
		t send-keys -t scenario -l -- "$arg"
		;;
	resize)
		note "=== resize: $arg"
		read -r c r <<<"$arg"
		t resize-window -t scenario -x "$c" -y "${r:-$rows}"
		;;
	shot)
		screen >"$out/$arg.txt"
		note "=== shot: $arg.txt"
		;;
	exit)
		deadline=$((SECONDS + limit))
		until status=$(dead) && [ -n "$status" ]; do
			[ "$SECONDS" -lt "$deadline" ] || fail "the program did not end in ${limit}s"
			sleep 0.2
		done
		[ "$status" = "$arg" ] || fail "the program ended with $status, not $arg"
		note "=== exit: $status"
		;;
	esac
done

echo "scenario.sh: $file played, ${#steps[@]} steps; the transcript: $log"
