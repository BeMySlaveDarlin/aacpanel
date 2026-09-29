#!/bin/sh
# The stand-in claude. With no command it plays a live session: it writes the
# session file the panel recognises a session by and echoes what it is sent.
# It also answers what the installer asks of claude on its own — --version and
# claude mcp add|get|remove|list with --scope user — the way claude does: the
# same messages on the same streams, the same exit codes, and the server kept
# in .claude.json of the account, $CLAUDE_CONFIG_DIR/.claude.json or
# ~/.claude.json. The local and project scopes are not played: a call that
# would land there stops with status 2 instead of passing on the stand and
# failing on a real machine.

version=${FAKE_CLAUDE_VERSION:-2.1.284}

# mcp runs claude mcp on .claude.json; python3 keeps the rest of the file
# the way it was.
mcp() {
	config=${CLAUDE_CONFIG_DIR:-$HOME}/.claude.json
	exec python3 - "$config" "$@" <<'PY'
import json, os, sys

config, args = sys.argv[1], sys.argv[2:]
cmd, rest = (args[0], args[1:]) if args else ("", [])

def stop(msg, code=1):
    print(msg, file=sys.stderr)
    sys.exit(code)

def load():
    try:
        with open(config, encoding="utf-8") as f:
            return json.load(f)
    except FileNotFoundError:
        return {}

def save(data):
    os.makedirs(os.path.dirname(config), exist_ok=True)
    tmp = config + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
    os.replace(tmp, config)
    print("File modified: " + config)

# Options come before the name, as claude parses them: -s|--scope, and
# -t|--transport stdio. Anything else the stand-in does not play.
scope, pos = None, []
while rest:
    a = rest.pop(0)
    if a == "--":
        pos += rest
        break
    if a in ("-s", "--scope") and rest:
        scope = rest.pop(0)
    elif a.startswith("--scope="):
        scope = a.split("=", 1)[1]
    elif a in ("-t", "--transport") and rest:
        if rest.pop(0) != "stdio":
            stop("the stand-in claude plays stdio servers only", 2)
    elif a.startswith("-") and not pos:
        stop("the stand-in claude does not play claude mcp %s %s" % (cmd, a), 2)
    else:
        pos.append(a)

if scope not in (None, "user"):
    stop("the stand-in claude keeps MCP servers in the user scope only, not %s" % scope, 2)

data = load()
servers = data.get("mcpServers") or {}

if cmd == "add":
    if scope != "user":
        stop("the stand-in claude adds to the user scope only: claude mcp add --scope user ...", 2)
    if len(pos) < 2:
        stop("claude mcp add takes <name> <command> [args...]", 2)
    name, command, argv = pos[0], pos[1], pos[2:]
    if name in servers:
        stop("MCP server %s already exists in user config" % name)
    servers[name] = {"type": "stdio", "command": command, "args": argv, "env": {}}
    data["mcpServers"] = servers
    print("Added stdio MCP server %s with command: %s %s to user config" % (name, command, " ".join(argv)))
    save(data)
elif cmd == "remove":
    if len(pos) != 1:
        stop("claude mcp remove takes <name>", 2)
    name = pos[0]
    if name not in servers:
        if scope:
            stop('No MCP server named "%s" in user scope' % name)
        stop('No MCP server named "%s". Run `claude mcp add` to add one.' % name)
    del servers[name]
    data["mcpServers"] = servers
    print(("Removed MCP server %s from user config" if scope else 'Removed MCP server "%s" from user config') % name)
    save(data)
elif cmd == "get":
    if len(pos) != 1:
        stop("claude mcp get takes <name>", 2)
    name = pos[0]
    if name not in servers:
        stop('No MCP server named "%s". Run `claude mcp add` to add one.' % name)
    s = servers[name]
    ok = os.access(s.get("command", ""), os.X_OK)
    print("%s:" % name)
    print("  Scope: User config (available in all your projects)")
    print("  Status: " + ("✔ Connected" if ok else "✘ Failed to connect"))
    print("  Type: " + s.get("type", "stdio"))
    print("  Command: " + s.get("command", ""))
    print(("  Args: " + " ".join(s.get("args", []))).rstrip())
    print("  Environment:")
    for k, v in (s.get("env") or {}).items():
        print("    %s=%s" % (k, v))
    print()
    print("To remove this server, run: claude mcp remove %s -s user" % name)
elif cmd == "list":
    if not servers:
        print("No MCP servers configured. Use `claude mcp add` to add a server.")
    else:
        print("Checking MCP server health…")
        print()
        for name, s in servers.items():
            ok = os.access(s.get("command", ""), os.X_OK)
            print("%s: %s %s - %s" % (name, s.get("command", ""), " ".join(s.get("args", [])),
                                     "✔ Connected" if ok else "✘ Failed to connect"))
else:
    stop("the stand-in claude does not play claude mcp %s" % cmd, 2)
PY
}

case "${1-}" in
-v | --version)
	printf '%s (Claude Code)\n' "$version"
	exit 0
	;;
mcp)
	shift
	mcp "$@"
	;;
# The commands of claude the stand-in does not play: without this a call of
# one would wait for input like a session and hang whoever called it.
agents | attach | auth | auto-mode | doctor | gateway | import | install | logs | plugin | plugins | project | respawn | rm | setup-token | stop | kill | ultrareview | update | upgrade)
	printf 'the stand-in claude does not play claude %s\n' "$1" >&2
	exit 2
	;;
esac

# claude -p is not played either — the probe of the limits and a session on
# the stream run it — and it stops at once rather than wait like a session.
for arg in "$@"; do
	case $arg in
	-p | --print)
		printf 'the stand-in claude does not play claude %s\n' "$arg" >&2
		exit 2
		;;
	esac
done

name=stand
while [ $# -gt 0 ]; do
	case "$1" in -n | --name) name=$2; shift ;; esac
	shift
done
d="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/sessions"
mkdir -p "$d"
start=$(awk '{print $22}' /proc/$$/stat)
cat >"$d/$$.json" <<J
{"pid":$$,"sessionId":"11111111-2222-3333-4444-555555555555","cwd":"$PWD","name":"$name","procStart":"$start","status":"idle","waitingFor":"","kind":"interactive"}
J
printf 'the stand-in claude: pid %s, cwd %s\n' "$$" "$PWD"
while read -r line; do printf 'received: %s\n' "$line"; done
