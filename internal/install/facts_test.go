package install

import (
	"slices"
	"strings"
	"testing"
)

func marked(in Inspection) []string {
	var out []string
	for _, f := range in.Findings {
		mark := map[Mark]string{Note: "·", Pass: "✓", Warn: "⚠", Stop: "✗"}[f.Mark]
		out = append(out, mark+" "+f.Text)
	}
	return out
}

func stopsOf(in Inspection) []string {
	var out []string
	for _, f := range in.Findings {
		if f.Mark == Stop {
			out = append(out, f.Text)
		}
	}
	return out
}

// says fails the test unless a line of the check is exactly want.
func says(t *testing.T, in Inspection, want string) {
	t.Helper()
	for _, l := range marked(in) {
		if l == want {
			return
		}
	}
	t.Errorf("the check never said %q; it said:\n%s", want, strings.Join(marked(in), "\n"))
}

func TestAMachineOfTheProfileGoesThrough(t *testing.T) {
	in := Inspect(healthy(), clone)
	want := []string{
		"✓ Ubuntu 24.04.3 LTS · x86_64 · systemd 255",
		"✓ docker 27.5.1 · compose 2.33.1 · system daemon",
		"✓ claude 2.1.283 · ~/.local/bin/claude",
		"✓ tmux and jq are here",
		"✓ ports 8776, 8777, 8443 are free",
		"✓ python3 3.12.3 for the collector",
		"✓ sudo goes through without a password",
		"✓ the clone ~/aacpanel belongs to u and is not on tmpfs",
		"✓ no container is named aacpanel, aacpanel-db, aacpanel-socket-proxy, aacpanel-tailscale",
		"✓ 212.0 GB free under /var/lib/docker · 16.7 GB of memory",
		"✓ registry-1.docker.io and proxy.golang.org answer",
		"✓ no trace of the panel: a fresh install",
	}
	if got := marked(in); !slices.Equal(got, want) {
		t.Errorf("the check said:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if in.Mode != Fresh || in.Version != "3f1c2ab" || in.StateDir != DefaultStateDir || in.LANPort != PortLAN {
		t.Errorf("facts: mode %s, version %q, state %s, lan port %d", in.Mode, in.Version, in.StateDir, in.LANPort)
	}
	if in.InstallDir != home+"/.local/state/aacpanel-install" {
		t.Errorf("the installer's directory is %s", in.InstallDir)
	}
}

func TestRootIsRefused(t *testing.T) {
	m := healthy()
	m.EUID = 0
	says(t, Inspect(m, clone), "✗ stop: run the installer as the user whose claude sessions the panel will manage, "+
		"not as root: the executor refuses to run as root. As that user: ./install.sh")
}

func TestTheFamilyIsToldByIDAndApt(t *testing.T) {
	for _, c := range []struct {
		name, osRelease string
		noApt           bool
		stop            string
	}{
		{name: "ubuntu", osRelease: "PRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\nID=ubuntu\nID_LIKE=debian\n"},
		{name: "debian", osRelease: "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n"},
		{name: "mint", osRelease: "PRETTY_NAME=\"Linux Mint 22\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\n"},
		{name: "fedora", osRelease: "PRETTY_NAME=\"Fedora Linux 41\"\nID=fedora\n",
			stop: "stop: Fedora Linux 41 is not of the Debian or Ubuntu family: the installer installs with apt. The manual path is INSTALL.md, part 2."},
		{name: "debian without apt", osRelease: "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n", noApt: true,
			stop: "stop: Debian GNU/Linux 13 (trixie) is not of the Debian or Ubuntu family: the installer installs with apt. The manual path is INSTALL.md, part 2."},
		{name: "no os-release", osRelease: "",
			stop: "stop: this system is not of the Debian or Ubuntu family: the installer installs with apt. The manual path is INSTALL.md, part 2."},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			if c.osRelease == "" {
				delete(m.Files, "/etc/os-release")
			} else {
				m.Files["/etc/os-release"] = c.osRelease
			}
			if c.noApt {
				delete(m.Path, "apt-get")
			}
			stops := stopsOf(Inspect(m, clone))
			if c.stop == "" && len(stops) > 0 {
				t.Errorf("the family stopped: %q", stops)
			}
			if c.stop != "" && !slices.Contains(stops, c.stop) {
				t.Errorf("stops %q, want %q", stops, c.stop)
			}
		})
	}
}

func TestTheSystemStopsOnWhatThePanelCannotLiveOn(t *testing.T) {
	for _, c := range []struct {
		name  string
		edit  func(*fake)
		stops string
		warns string
	}{
		{"wsl", func(m *fake) { m.Files["/proc/sys/kernel/osrelease"] = "5.15.153.1-microsoft-standard-WSL2\n" },
			"stop: WSL is not supported: the panel needs a systemd machine with the system docker daemon.", ""},
		{"no systemd", func(m *fake) { m.Files["/proc/1/comm"] = "docker-init\n" },
			"stop: systemd is not the init of this machine (PID 1 is docker-init): the collector and the executor are systemd units.", ""},
		{"old systemd", func(m *fake) { m.Cmds[key("systemctl", "--version")] = ok("systemd 245 (245.4-4ubuntu3)\n") },
			"stop: systemd 245 is too old: the units of the panel need 249 or newer.", ""},
		{"riscv", func(m *fake) { m.Uname = "riscv64" }, "stop: riscv64 is not supported.", ""},
		{"arm", func(m *fake) { m.Uname = "aarch64" }, "",
			"warn: aarch64 is not tried on the stand: a step may fail here that goes through on x86_64."},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			c.edit(m)
			in := Inspect(m, clone)
			if c.stops != "" {
				says(t, in, "✗ "+c.stops)
			}
			if c.warns != "" {
				says(t, in, "⚠ "+c.warns)
				says(t, in, "✓ Ubuntu 24.04.3 LTS · aarch64 · systemd 255")
			}
			if strings.Contains(strings.Join(marked(in), "\n"), "✓ Ubuntu") && c.stops != "" {
				t.Error("the line of the system passed along with a stop of the system")
			}
		})
	}
}

func TestDocker(t *testing.T) {
	const (
		version = "docker version --format {{.Server.Version}}"
		info    = "docker info --format {{json .}}"
		compose = "docker compose version --short"
		policy  = "apt-cache policy docker-compose-v2 docker-compose-plugin docker-compose"
	)
	for _, c := range []struct {
		name string
		edit func(*fake)
		want string
	}{
		{"missing on ubuntu, apt lists never fetched", func(m *fake) { delete(m.Path, "docker") },
			"✗ stop: docker is needed: the panel, its database and the socket proxy run as containers. " +
				"sudo apt install docker.io docker-compose-v2, then run again."},
		{"missing on debian 13", func(m *fake) {
			delete(m.Path, "docker")
			m.Files["/etc/os-release"] = "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n"
			m.Cmds[policy] = ok("docker-compose:\n  Installed: (none)\n  Candidate: 2.26.1-4\n  Version table:\n")
		}, "✗ stop: docker is needed: the panel, its database and the socket proxy run as containers. " +
			"sudo apt install docker.io docker-compose, then run again."},
		{"missing on debian 12, compose v1 only", func(m *fake) {
			delete(m.Path, "docker")
			m.Files["/etc/os-release"] = "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\n"
			m.Cmds[policy] = ok("docker-compose:\n  Installed: (none)\n  Candidate: 1.29.2-3\n  Version table:\n")
		}, "✗ stop: apt has no docker compose v2 of " + MinCompose + " or newer here. Install docker from download.docker.com and run again."},
		{"not in the group", func(m *fake) {
			m.Cmds[version] = fails("permission denied while trying to connect to the docker API at unix:///var/run/docker.sock")
		}, `✗ stop: docker answers "permission denied": u is not in the docker group. sudo usermod -aG docker u, log out and in, run ./install.sh again.`},
		{"daemon down", func(m *fake) {
			m.Cmds[version] = fails("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
		}, "✗ stop: the docker daemon does not answer: sudo systemctl enable --now docker"},
		{"rootless", func(m *fake) {
			m.Cmds[info] = ok(`{"SecurityOptions":["name=seccomp,profile=builtin","name=rootless"],"OperatingSystem":"Ubuntu","DockerRootDir":"/home/u/.local/share/docker"}`)
		}, "✗ stop: rootless docker is not supported: the panel reads /var/run/docker.sock of the system daemon and mounts the state directory and /run/user/1000 into its container."},
		{"desktop", func(m *fake) {
			m.Cmds[info] = ok(`{"OperatingSystem":"Docker Desktop","DockerRootDir":"/var/lib/docker"}`)
		}, "✗ stop: Docker Desktop docker is not supported: the panel reads /var/run/docker.sock of the system daemon and mounts the state directory and /run/user/1000 into its container."},
		{"snap", func(m *fake) { m.Path["docker"] = "/snap/bin/docker" },
			"✗ stop: snap docker is not supported: the panel reads /var/run/docker.sock of the system daemon and mounts the state directory and /run/user/1000 into its container."},
		{"no system socket", func(m *fake) { delete(m.Stats, "/var/run/docker.sock") },
			"✗ stop: this docker is not supported: the panel reads /var/run/docker.sock of the system daemon and mounts the state directory and /run/user/1000 into its container."},
		{"old compose", func(m *fake) { m.Cmds[compose] = ok("v2.17.3\n") },
			"✗ stop: docker compose 2.17.3 is older than " + MinCompose + "."},
		{"compose five", func(m *fake) { m.Cmds[compose] = ok("5.5.1\n") },
			"✓ docker 27.5.1 · compose 5.5.1 · system daemon"},
		{"no compose plugin", func(m *fake) { m.Cmds[compose] = fails("docker: 'compose' is not a docker command.") },
			"✗ stop: docker compose is needed: the panel is a stack of compose services. sudo apt install docker-compose-v2, then run again."},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			c.edit(m)
			in := Inspect(m, clone)
			says(t, in, c.want)
		})
	}
}

// TestTheDockerGroupOfAnEarlierRunGoesThroughSg: a run added the group and
// stopped later, and the next one began in the same terminal — the group is
// the user's and not the terminal's. The check reaches docker through sg,
// as the run that added the group did, and so do the steps after it.
func TestTheDockerGroupOfAnEarlierRunGoesThroughSg(t *testing.T) {
	m := healthy()
	denied := fails("permission denied while trying to connect to the docker API at unix:///var/run/docker.sock")
	for _, args := range [][]string{
		{"version", "--format", "{{.Server.Version}}"}, {"info", "--format", "{{json .}}"},
		{"compose", "version", "--short"}, {"ps", "-a", "--format", psFormat}, {"volume", "ls", "--format", "{{.Name}}"},
	} {
		plain := key("docker", args...)
		m.Cmds[key("sg", "docker", "-c", shellLine(append([]string{"docker"}, args...)))] = m.Cmds[plain]
		m.Cmds[plain] = denied
	}
	m.Cmds[key("id", "-nG", "u")] = ok("u adm docker\n")
	in := Inspect(m, clone)
	says(t, in, "✓ docker 27.5.1 · compose 2.33.1 · system daemon · through sg docker: this terminal began before the group was added")
	if in.Stops() > 0 || !in.ViaGroup {
		t.Errorf("stops %d, through sg %v:\n%s", in.Stops(), in.ViaGroup, strings.Join(marked(in), "\n"))
	}
	in2 := &Install{S: &Survey{Facts: in.Facts}}
	if got := in2.docker("compose", "up", "-d"); !slices.Equal(got, []string{"sg", "docker", "-c", "docker compose up -d"}) {
		t.Errorf("a step calls docker as %q", got)
	}

	// Not in the group at all: the stop stays.
	m.Cmds[key("id", "-nG", "u")] = ok("u adm\n")
	says(t, Inspect(m, clone), `✗ stop: docker answers "permission denied": u is not in the docker group. sudo usermod -aG docker u, log out and in, run ./install.sh again.`)
}

// noClaude takes claude off the machine: off PATH, and from where the
// native installer puts it.
func noClaude(m *fake) {
	delete(m.Path, "claude")
	delete(m.Files, NativeClaude(home))
}

func TestAMissingProgramIsOfferedNotJustRefused(t *testing.T) {
	m := healthy()
	noClaude(m)
	for _, name := range []string{"docker", "tmux", "jq"} {
		delete(m.Path, name)
	}
	in := Inspect(m, clone)
	var names []string
	for _, f := range in.Findings {
		if f.Missing != nil {
			names = append(names, f.Missing.Name)
			if f.Mark != Stop || f.Text != f.Missing.Stop() {
				t.Errorf("%s: until a run can ask, a missing program stops with its command: %q", f.Missing.Name, f.Text)
			}
		}
	}
	if !slices.Equal(names, []string{"docker", "claude", "tmux", "jq"}) {
		t.Errorf("missing programs %q", names)
	}
	says(t, in, "✗ stop: tmux is needed: sessions live in it, and without it the executor turns off every action on sessions. sudo apt install tmux, then run again.")
	says(t, in, "✗ stop: jq is needed: the status line script reads the limits with it. sudo apt install jq, then run again.")
	says(t, in, "✗ stop: claude is needed: the panel starts and watches claude sessions. curl -fsSL https://claude.ai/install.sh | bash, then run again.")
}

// TestCurlComesForClaudesInstaller: the native installer downloads claude
// with curl or wget, so on a machine with neither a yes to claude puts curl
// into the root step's apt, with tmux and jq; with either, nothing more.
func TestCurlComesForClaudesInstaller(t *testing.T) {
	m := desktop()
	noClaude(m)
	delete(m.Path, "curl")
	delete(m.Path, "tmux")
	in := Inspect(m, clone)
	says(t, in, "✗ stop: claude is needed: the panel starts and watches claude sessions. "+
		"sudo apt install curl && curl -fsSL https://claude.ai/install.sh | bash, then run again.")
	packages := func(m *fake, claude string) []string {
		s := survey(m, &Run{Yes: true, Answers: map[string]string{"--install-claude": claude}})
		answerAll(t, s)
		return s.Packages()
	}
	if got := packages(m, yes); !slices.Equal(got, []string{"curl", "tmux"}) {
		t.Errorf("a yes to claude on a machine without curl or wget installs %q with apt", got)
	}
	m.Path["wget"] = "/usr/bin/wget"
	if got := packages(m, yes); !slices.Equal(got, []string{"tmux"}) {
		t.Errorf("with wget there the root step installs %q", got)
	}
}

// TestClaudeOffPathIsFoundWhereTheNativeInstallerPutsIt: after the native
// installer, ~/.local/bin reaches PATH only with the next login; the run
// before it finds claude there all the same, asks nothing about installing
// it, and suggests it as what starts claude.
func TestClaudeOffPathIsFoundWhereTheNativeInstallerPutsIt(t *testing.T) {
	m := desktop()
	delete(m.Path, "claude")
	in := Inspect(m, clone)
	says(t, in, "✓ claude 2.1.283 · ~/.local/bin/claude, where the native installer puts it: not on PATH")
	for _, f := range in.Findings {
		if f.Missing != nil {
			t.Errorf("%s is asked about with claude at ~/.local/bin", f.Missing.Name)
		}
	}
	s := survey(m, &Run{Yes: true})
	answerAll(t, s)
	if g, _ := s.Value("claude"); g.Value != NativeClaude(home) || g.Source != "the native installer" {
		t.Errorf("what starts claude: %+v", g)
	}
}

func TestClaudeOnANodeTheUnitCannotSee(t *testing.T) {
	m := healthy()
	m.Files[home+"/.local/bin/claude"] = "#!/usr/bin/env node\nrequire('./cli.js')\n"
	delete(m.Stats, "/usr/bin/node")
	m.Path["node"] = home + "/.nvm/versions/node/v22.9.0/bin/node"
	says(t, Inspect(m, clone), "✗ stop: claude runs on node from ~/.nvm/versions/node/v22.9.0/bin, which the executor's unit does not see: panel sessions would not start. Install claude natively.")

	m.Stats["/usr/bin/node"] = Stat{}
	says(t, Inspect(m, clone), "✓ claude 2.1.283 · ~/.local/bin/claude")
}
