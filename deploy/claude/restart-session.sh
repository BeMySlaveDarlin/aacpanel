#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat >&2 <<'TXT'
usage: restart-session.sh [--continue] [--dry-run]

  --continue   carry the current conversation into the new session (claude --continue)
  --dry-run    show what would be done and exit
TXT
    exit 2
}

cont=0
dry=0
for arg in "$@"; do
    case "$arg" in
        --continue) cont=1 ;;
        --dry-run)  dry=1 ;;
        -h|--help)  usage ;;
        *) echo "restart-session: unknown argument $arg" >&2; usage ;;
    esac
done

command -v tmux >/dev/null 2>&1 || {
    echo "restart-session: tmux not found - the session lives inside it, there is nothing to restart with" >&2
    exit 1
}

claude_pid=""
pid=$PPID
for _ in 1 2 3 4 5 6 7 8; do
    [ -r "/proc/$pid/comm" ] || break
    read -r comm < "/proc/$pid/comm" || break
    if [ "$comm" = "claude" ]; then
        claude_pid=$pid
        break
    fi
    read -r _ _ _ ppid _ < "/proc/$pid/stat" || break
    [ "$ppid" -gt 1 ] || break
    pid=$ppid
done
[ -n "$claude_pid" ] || {
    echo "restart-session: no claude process among the parents - the script has to be run from inside a session" >&2
    exit 1
}

target=""
while read -r pane pane_pid; do
    p=$pane_pid
    for _ in 1 2 3 4 5 6 7 8; do
        if [ "$p" = "$claude_pid" ]; then
            target=$pane
            break
        fi
        [ -r "/proc/$p/stat" ] || break
        read -r _ _ _ pp _ < "/proc/$p/stat" || break
        [ "$pp" -gt 1 ] || break
        p=$pp
    done
    [ -n "$target" ] && break
done < <(tmux list-panes -a -F '#{pane_id} #{pane_pid}' 2>/dev/null || true)

[ -n "$target" ] || {
    echo "restart-session: the tmux pane with this session was not found." >&2
    echo "  The session was not brought up through tmux - restart it the same way you started it." >&2
    exit 1
}

env_args=()
while IFS= read -r -d '' kv; do
    case "$kv" in
        HOME=*|PATH=*|TERM=*|LANG=*|LC_*=*|SHELL=*|USER=*|CLAUDE_CONFIG_DIR=*|CLAUDE_CODE_TMPDIR=*|AACP_*=*)
            env_args+=(-e "$kv") ;;
    esac
done < "/proc/$claude_pid/environ"

workdir=$(readlink -f "/proc/$claude_pid/cwd" 2>/dev/null || pwd)

bin=${AACP_CLAUDE:-claude}
cmd=("$bin")
[ "$cont" = "1" ] && cmd+=(--continue)

if [ "$dry" = "1" ]; then
    printf 'pane:     %s\n' "$target"
    printf 'directory: %s\n' "$workdir"
    printf 'command:  %s\n' "${cmd[*]}"
    printf 'environment: %s variables\n' "${#env_args[@]}"
    exit 0
fi

exec tmux respawn-pane -k -t "$target" -c "$workdir" "${env_args[@]}" -- "${cmd[@]}"
