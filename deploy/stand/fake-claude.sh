#!/bin/sh
name=stand
while [ $# -gt 0 ]; do case "$1" in -n|--name) name=$2; shift ;; esac; shift; done
d="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/sessions"; mkdir -p "$d"
start=$(awk '{print $22}' /proc/$$/stat)
cat > "$d/$$.json" <<J
{"pid":$$,"sessionId":"11111111-2222-3333-4444-555555555555","cwd":"$PWD","name":"$name","procStart":"$start","status":"idle","waitingFor":"","kind":"interactive"}
J
printf 'the stand-in claude: pid %s, cwd %s\n' "$$" "$PWD"
while read -r line; do printf 'received: %s\n' "$line"; done
