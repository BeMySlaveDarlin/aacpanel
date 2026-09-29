package demo

// machine is the made-up machine the demo installs on. Nothing of it is read
// from the machine the demo runs on: the demo shows the installer, not this
// computer, and a screenshot of it gives nothing of the owner away.
type machine struct {
	host, user string
	uid        int
	clone      string
	version    string

	osName, arch, systemd string
	docker, compose       string
	claude, claudePath    string
	python                string
	missing               []string // packages the root step installs, asked in block P

	terminals []found
	displays  []found
	locales   []found
	roots     []found
	accounts  []found
	projects  []project
	ports     []found
	lan       []found
	certs     []found
	proxies   []found
	email     string
	tailnet   string
}

// found is a value found on the machine and where it was found.
type found struct{ value, where string }

type project struct {
	root, path, detail string
	on                 bool
}

var helios = machine{
	host:    "helios",
	user:    "demo",
	uid:     1000,
	clone:   "~/aacpanel",
	version: "v0.9.0 · 3f1c2ab",

	osName:     "Ubuntu 24.04.3 LTS",
	arch:       "x86_64",
	systemd:    "255",
	docker:     "27.5.1",
	compose:    "2.33.1",
	claude:     "2.1.283",
	claudePath: "~/.local/bin/claude",
	python:     "3.12.3",
	missing:    []string{"tmux", "jq"},

	terminals: []found{
		{"konsole", "Found on this machine."},
		{"gnome-terminal --", "Found on this machine."},
	},
	displays: []found{
		{":0", "From the user manager (systemctl --user show-environment)."},
		{":1", "Found in /tmp/.X11-unix."},
	},
	locales: []found{
		{"en_US.UTF-8", "$LANG, and present in locale -a."},
		{"C.UTF-8", "Always there: English messages, UTF-8 text."},
		{"de_DE.UTF-8", "Found in locale -a."},
	},
	roots: []found{
		{"~/code", "4 git repositories · claude opened 2 of them"},
		{"~/src", "1 git repository"},
		{"~", "only the home directory itself, not walked deeper"},
	},
	accounts: []found{
		{"~/.claude", "signed in"},
		{"~/.claude-work", "signed in"},
	},
	projects: []project{
		{"~/code", "~/code/shop", "git · claude opened it 3 days ago", true},
		{"~/code", "~/code/landing", "git · claude opened it yesterday", true},
		{"~/code", "~/code/infra", "git", false},
		{"~/code", "~/code/dotfiles", "git", false},
		{"~/src", "~/src/tools", "git", false},
		{"~", "~/notes", "claude opened it last week", false},
	},
	ports: []found{
		{":5432", "postgres, in a container of its own"},
		{":6379", "redis-server"},
		{":9090", "prometheus"},
	},
	lan: []found{
		{"192.168.1.20", "Found on enp3s0; docker and loopback addresses are left out."},
	},
	certs: []found{
		{"~/certs/helios.lan.crt", "With helios.lan.key beside it, found in ~/certs."},
	},
	proxies: []found{
		{"127.0.0.1", "The proxy runs on this machine."},
		{"172.17.0.1", "The address of docker0: the proxy runs in a container."},
		{"10.8.0.2", "wg0: the proxy is on another machine behind WireGuard."},
	},
	email:   "demo@helios.example",
	tailnet: "tail3c9a1.ts.net",
}

// inspection is what the check of the machine finds: the lines the feed shows
// and the ones it folds away until ctrl+o.
func (mc machine) inspection() (shown, folded []string) {
	shown = []string{
		"✓ " + mc.osName + " · " + mc.arch + " · systemd " + mc.systemd,
		"✓ docker " + mc.docker + " · compose " + mc.compose + " · system daemon",
		"✓ claude " + mc.claude + " · native · signed in",
		"⚠ tmux, jq are missing — the root step installs them with apt",
		"✓ ports 8776, 8777, 8443 are free",
	}
	folded = []string{
		"✓ python3 " + mc.python + " for the collector",
		"✓ sudo asks for a password: the root step runs one sudo",
		"✓ the clone " + mc.clone + " belongs to " + mc.user + " and is not on tmpfs",
		"✓ no container is named aacpanel, aacpanel-db or aacpanel-socket-proxy",
		"✓ 212 GB free under /var/lib/docker · 31 GB of memory",
		"✓ registry-1.docker.io and proxy.golang.org answer",
		"✓ no trace of the panel: a fresh install",
	}
	return shown, folded
}
