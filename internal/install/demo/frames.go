package demo

import (
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/install/ui"
)

const (
	stagedHostEnv = "~/.local/state/aacpanel-install/host.env.staged"
	installLog    = "~/.local/state/aacpanel-install/log/"
)

func welcome(t ui.Theme, mc machine, width int) string {
	return t.Frame(ui.Frame{Edge: t.Edge, Rows: []ui.Row{
		{Text: t.Spin.Render("✻") + " " + t.Strong.Render("aacpanel installer")},
		{},
		{Text: "  clone    " + mc.clone + "  (" + mc.version + ")"},
		{Text: fmt.Sprintf("  user     %s (uid %d)", mc.user, mc.uid)},
		{Text: "  mode     fresh install — no trace of the panel on this machine"},
		{Text: "  demo     a made-up machine: nothing here is changed or sent"},
	}}, width)
}

// hooks is how many hooks the kit wires into claude: the question relay and
// every hook part that is checked.
func (a answers) hooks() int {
	n := 1
	for _, part := range []string{"copies", "brief", "cap", "nudge", "stamp", "cost", "background"} {
		if a.has(part) {
			n++
		}
	}
	return n
}

// allows is how many allow rules go into permissions: the panel's four tools
// and the restart when Self-restart is checked.
func (a answers) allows() int {
	if a.has("restart") {
		return 5
	}
	return 4
}

func (a answers) services() string {
	s := []string{"aacpanel", "aacpanel-db", "socket-proxy"}
	if a.has("tailscale") {
		s = append(s, "tailscale")
	}
	if a.has("testdb") {
		s = append(s, "test database")
	}
	return strings.Join(s, ", ")
}

func (a answers) mapLine() string {
	var contours []string
	for _, dir := range a.accounts {
		contours = append(contours, a.contours[dir])
	}
	line := "contour " + strings.Join(contours, ", ")
	if len(contours) > 1 {
		line = "contours " + strings.Join(contours, ", ")
	}
	if a.group == "-" || a.group == "" {
		return line + ", no group"
	}
	return fmt.Sprintf("%s, group %s, %s", line, a.group, count(len(a.projects), "project"))
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (a answers) src(id string) string {
	if s := a.source[id]; s != "" {
		return " (" + s + ")"
	}
	return ""
}

func (a answers) windowsLine() string {
	if a.terminal == "none" {
		return "no windows: sessions live in tmux only" + a.src("terminal")
	}
	line := "windows " + a.terminal + a.src("terminal") + " on " + a.display + a.src("display")
	if a.auto == "yes" {
		line += ", opened on start"
	}
	return line
}

func (a answers) transportLine() string {
	if a.transport == "tmux" {
		return "sessions in tmux" + a.src("transport")
	}
	return "sessions on the stream" + a.src("transport")
}

func planFrame(t ui.Theme, mc machine, a answers, width int) string {
	settings := strings.Join(func() []string {
		var s []string
		for _, dir := range a.accounts {
			s = append(s, dir+"/settings.json")
		}
		return s
	}(), ", ")

	rows := []ui.Row{
		{Text: "As root — one sudo", Head: true},
		{Text: "  · apt install " + strings.Join(mc.missing, " "), Tag: "new"},
		{Text: "  · " + a.state + ", owner " + mc.user, Tag: "new"},
		{Text: "  · aacpanel-agent@.service, enabled for " + mc.user, Tag: "new"},
		{Text: "  · linger for " + mc.user, Tag: "new"},
		{Text: "As you", Head: true},
		{Text: "  · ~/bin/aacpanel-exec, built with the installer's Go", Tag: "new"},
		{Text: "  · aacpanel-exec.service, user unit", Tag: "new"},
		{Text: "  · compose: " + a.services(), Tag: "new"},
	}
	if a.has("gc") {
		rows = append(rows, ui.Row{Text: "  · aacpanel-docker-gc.timer, weekly", Tag: "new"})
	}
	rows = append(rows,
		ui.Row{Text: "  · map: " + a.mapLine(), Tag: "new"},
		ui.Row{Text: "In claude settings — " + settings, Head: true},
		ui.Row{Text: "  · question hook · status line before yours · MCP server"},
		ui.Row{Text: fmt.Sprintf("  · %d allow rules · %d hooks", a.allows(), a.hooks())},
		ui.Row{Text: "Values", Head: true},
		ui.Row{Text: "  host " + a.host + a.src("host") + " · home session " + a.home + a.src("home")},
		ui.Row{Text: "  state " + a.state + a.src("state") + " · projects under " + strings.Join(a.roots, ", ")},
		ui.Row{Text: "  " + a.windowsLine() + " · locale " + a.locale + a.src("locale")},
		ui.Row{Text: "  claude " + a.claude + a.src("claude") + " · " + a.transportLine()},
	)
	if a.has("tailscale") {
		key := "not set"
		if a.tskeySet {
			key = ui.SecretShown
		}
		rows = append(rows, ui.Row{Text: "  tailscale node " + a.tsnode + " · auth key " + key})
	}
	if a.domain != "" {
		rows = append(rows, ui.Row{Text: "  domain " + a.domain})
	}
	if a.lan != "" {
		rows = append(rows, ui.Row{Text: "  home network " + a.lan + ":8443"})
	}
	return t.Frame(ui.Frame{Title: "Install plan", Edge: t.PlanEdge, Rows: rows}, width)
}

func rootCommand(mc machine) string {
	return "sudo bash deploy/install/root.sh apply --user " + mc.user + " --staged " + stagedHostEnv
}

func rootFrame(t ui.Theme, mc machine, a answers, width int) string {
	return t.Frame(ui.Frame{Title: "Root command", Edge: t.RootEdge, Rows: []ui.Row{
		{Text: "sudo bash deploy/install/root.sh apply --user " + mc.user + " \\"},
		{Text: "  --staged " + stagedHostEnv},
		{},
		{Text: "Installs " + strings.Join(mc.missing, " ") + " with apt, creates " + a.state +
			", installs the collector unit and enables linger. Nothing else runs as root."},
	}}, width)
}

// rootScript is what the terminal runs while the installer has handed it
// over: in a real run, sudo and root.sh; in the demo, a shell that asks for
// the password the way sudo does, reads it without echo and forgets it, then
// prints what root.sh would. It needs bash for read -s. The values it prints
// arrive as arguments, never spliced into the script.
const rootScript = `cmd=$1 user=$2 state=$3 pkgs=$4
printf '$ %s\n' "$cmd"
tries=0
while :; do
	printf '[sudo] password for %s: ' "$user"
	IFS= read -rs pw
	printf '\n'
	[ -n "$pw" ] && break
	tries=$((tries + 1))
	if [ "$tries" -ge 3 ]; then
		echo 'sudo: 3 incorrect password attempts'
		exit 1
	fi
	echo 'Sorry, try again.'
done
unset pw
echo "root.sh: apt-get install -y --no-install-recommends $pkgs"
echo 'Reading package lists... Done'
echo 'Building dependency tree... Done'
echo 'The following NEW packages will be installed:'
echo '  jq libjq1 libonig5 libutempter0 tmux'
echo 'Setting up libonig5:amd64 (6.9.9-1build1) ...'
echo 'Setting up libjq1:amd64 (1.7.1-3build1) ...'
echo 'Setting up jq (1.7.1-3build1) ...'
echo 'Setting up tmux (3.4-1ubuntu0.1) ...'
echo "root.sh: install -d -m 0755 -o 1000 -g 1000 $state"
echo "root.sh: install -m 0644 host.env.staged $state/host.env"
echo "root.sh: loginctl enable-linger $user"
echo "root.sh: systemctl enable --now aacpanel-agent@$user.service"
echo "Created symlink /etc/systemd/system/multi-user.target.wants/aacpanel-agent@$user.service → /etc/systemd/system/aacpanel-agent@.service."
echo 'root.sh: done'
`

// rootScriptText is the file "Show the script first" opens: the part that
// runs as root, readable in one screen.
func rootScriptText(mc machine, a answers) []string {
	text := fmt.Sprintf(`#!/usr/bin/env bash
# Everything the installer does as root, in one file: packages, the state
# directory, the collector's unit and linger. It reads nothing from the tree
# but its arguments, and prints a MANIFEST line for every change, so that
# ./install.sh uninstall can take each one back.
set -euo pipefail

usage() { echo "usage: root.sh apply|restart-agent|remove --user NAME [--staged FILE]" >&2; exit 2; }

apply() {
	local user=$1 staged=$2
	local uid gid state
	uid=$(id -u "$user") gid=$(id -g "$user")
	state=$(sed -n 's/^AACP_STATE_DIR=//p' "$staged")

	missing=()
	for pkg in %s; do
		dpkg -s "$pkg" >/dev/null 2>&1 || missing+=("$pkg")
	done
	if ((${#missing[@]})); then
		apt-get install -y --no-install-recommends "${missing[@]}"
		for pkg in "${missing[@]}"; do printf 'MANIFEST\troot\tpkg\t%%s\tby-installer\n' "$pkg"; done
	fi

	if [[ ! -d $state ]]; then
		install -d -m 0755 -o "$uid" -g "$gid" "$state"
		printf 'MANIFEST\troot\tdir\t%%s\tcreated\n' "$state"
	fi
	install -m 0644 -o "$uid" -g "$gid" "$staged" "$state/host.env"

	if [[ ! -e /var/lib/systemd/linger/$user ]]; then
		loginctl enable-linger "$user"
		printf 'MANIFEST\troot\tlinger\t%%s\tby-installer\n' "$user"
	fi

	install -m 0644 deploy/systemd/aacpanel-agent@.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable --now "aacpanel-agent@$user.service"
	printf 'MANIFEST\troot\tenabled\tsystem\taacpanel-agent@%%s.service\n' "$user"
}

case ${1:-} in
apply) shift; apply "$2" "$4" ;;
*) usage ;;
esac

# In this demo the file is shown and never run: %s is a made-up machine.`,
		strings.Join(mc.missing, " "), a.host)
	return strings.Split(strings.ReplaceAll(text, "\t", "    "), "\n")
}

func center(s string, width int) string {
	gap := (width - ui.Width(s)) / 2
	if gap < 1 {
		return s
	}
	return strings.Repeat(" ", gap) + s
}

func enrollFrame(t ui.Theme, mc machine, a answers, code string, left time.Duration, width int) string {
	inner := min(width, ui.MaxFrame) - 4
	valid := "valid " + clock(left) + " · once"
	if left <= 0 {
		valid = t.Warn.Render("expired")
	}
	rows := []ui.Row{
		{Text: "Open  " + t.Accent.Render("http://localhost:8776") + "  and enter the code"},
	}
	if a.has("tailscale") {
		rows = append(rows, ui.Row{Text: "or, on the phone,  " + t.Accent.Render("https://"+a.tsnode+"."+mc.tailnet)})
	}
	rows = append(rows,
		ui.Row{},
		ui.Row{Text: center(t.Strong.Render(code), inner)},
		ui.Row{},
		ui.Row{Text: valid + " · r — a new code · q — finish"},
	)
	return t.Frame(ui.Frame{Title: "First device", Edge: t.Edge, Rows: rows}, width)
}

func clock(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func reportFrame(t ui.Theme, mc machine, a answers, took time.Duration, width int) string {
	rows := []ui.Row{
		{Text: t.Spin.Render("✻") + " " + t.Strong.Render("Installed in "+ui.Elapsed(took))},
		{},
		{Text: "Changed", Head: true},
		{Text: "  · " + strings.Join(mc.missing, " ") + " with apt · " + a.state + " · aacpanel-agent@" + mc.user + " · linger"},
		{Text: "  · ~/aacpanel/.env · ~/bin/aacpanel-exec · aacpanel-exec.service"},
		{Text: "  · compose: " + a.services()},
	}
	for _, dir := range a.accounts {
		rows = append(rows, ui.Row{Text: "  · " + dir + "/settings.json, its original kept among the installer's backups"})
	}
	rows = append(rows,
		ui.Row{Text: "  · map: " + a.mapLine()},
		ui.Row{Text: "Left as it was", Head: true},
		ui.Row{Text: "  · docker " + mc.docker + " · claude " + mc.claude},
		ui.Row{Text: "  · your status line, now behind the limits snapshot"},
	)
	var warnings []string
	if a.terminal == "none" {
		warnings = append(warnings, "no windows: a session opens in tmux, and tmux attach reaches it")
	}
	if a.has("tailscale") {
		warnings = append(warnings, "the auth key is spent: a node made again needs a new key")
	}
	if a.term == "yes" {
		warnings = append(warnings, "the live terminal answers any device with a passkey")
	}
	if len(warnings) > 0 {
		rows = append(rows, ui.Row{Text: "Warnings", Head: true})
		for _, w := range warnings {
			rows = append(rows, ui.Row{Text: "  · " + t.Warn.Render(w)})
		}
	}
	rows = append(rows, ui.Row{Text: "Next", Head: true})
	if a.has("tailscale") {
		rows = append(rows, ui.Row{Text: "  · on the phone: https://" + a.tsnode + "." + mc.tailnet})
	}
	rows = append(rows,
		ui.Row{Text: "  · another device: ./install.sh enroll · a check: ./install.sh check"},
		ui.Row{Text: "  · update: git pull && ./install.sh · remove: ./install.sh uninstall"},
	)
	return t.Frame(ui.Frame{Edge: t.Edge, Rows: rows}, width)
}
