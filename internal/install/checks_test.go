package install

import (
	"io/fs"
	"testing"
)

// A row of /proc/net/tcp in LISTEN: the address and the port in the kernel's
// hex. 0x2248 is 8776, 0x20FB 8443, 0x480B 18443.
const tcpHead = "  sl  local_address rem_address   st\n"

func listen(addr string) string {
	return "   0: " + addr + " 00000000:0000 0A 00000000:00000000 00:00000000 00000000  0 0 1\n"
}

func TestPorts(t *testing.T) {
	ps := key("docker", "ps", "-a", "--format", psFormat)
	for _, c := range []struct {
		name string
		edit func(*fake)
		want string
		lan  int
	}{
		{"a stranger's container on 8776", func(m *fake) {
			m.cmds[ps] = ok("shop-web\tshop\t127.0.0.1:8776->80/tcp\n")
		}, "✗ stop: 127.0.0.1:8776 is taken by the container shop-web. The panel publishes there and the port is fixed. Free it and run again.", PortLAN},
		{"the panel's own", func(m *fake) {
			m.cmds[ps] = ok("aacpanel\taacpanel\t127.0.0.1:8776->8776/tcp, 127.0.0.1:8777->8777/tcp, 127.0.0.1:8443->8443/tcp\n" +
				"aacpanel-db\taacpanel\t5432/tcp\n")
		}, "✓ ports 8776, 8777, 8443 are the panel's own", PortLAN},
		{"a process of the user's own on every address", func(m *fake) {
			m.files["/proc/net/tcp"] = tcpHead + listen("00000000:2248")
			m.cmds[key("ss", "-Hltnp")] = ok(`LISTEN 0 5 0.0.0.0:8776 0.0.0.0:* users:(("python3",pid=4242,fd=3))` + "\n")
		}, "✗ stop: 127.0.0.1:8776 is taken by python3 (pid 4242). The panel publishes there and the port is fixed. Free it and run again.", PortLAN},
		{"somebody else's on every ipv6 address", func(m *fake) {
			m.files["/proc/net/tcp6"] = tcpHead + listen("00000000000000000000000000000000:2248")
		}, "✗ stop: 127.0.0.1:8776 is taken by a process of another user. The panel publishes there and the port is fixed. Free it and run again.", PortLAN},
		{"another address is no conflict", func(m *fake) {
			m.files["/proc/net/tcp"] = tcpHead + listen("0100A8C0:2248")
		}, "✓ ports 8776, 8777, 8443 are free", PortLAN},
		{"8443 taken moves the home network", func(m *fake) {
			m.files["/proc/net/tcp"] = tcpHead + listen("0100007F:20FB") + listen("0100007F:480B")
		}, "✓ ports 8776, 8777 are free · 8443 is taken, the home network gets 18444", 18444},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			c.edit(m)
			in := Inspect(m, clone)
			says(t, in, c.want)
			if in.LANPort != c.lan {
				t.Errorf("the home network got %d, want %d", in.LANPort, c.lan)
			}
		})
	}
}

func TestPython(t *testing.T) {
	m := healthy()
	m.cmds[key("python3", "-c", pythonVersion)] = ok("3.8.10\n")
	says(t, Inspect(m, clone), "✗ stop: python3 3.9 or newer is needed by the collector (found 3.8.10).")
	delete(m.path, "python3")
	says(t, Inspect(m, clone), "✗ stop: python3 3.9 or newer is needed by the collector (found none).")
}

func TestSudo(t *testing.T) {
	const admin = "Ask an administrator to run: sudo bash " + clone + "/deploy/install/root.sh apply --user u " +
		"--staged " + home + "/.local/state/aacpanel-install/host.env.staged, then run ./install.sh again."
	password := func(m *fake) { m.cmds[key("sudo", "-n", "true")] = fails("sudo: a password is required") }
	for _, c := range []struct {
		name string
		edit func(*fake)
		want string
	}{
		{"no password", func(*fake) {}, "✓ sudo goes through without a password"},
		{"a password at a terminal", func(m *fake) { password(m); m.tty = true },
			"✓ sudo asks for a password: the root step runs one sudo"},
		{"a password and no terminal", password,
			"✗ stop: one step needs root (the state directory, the collector unit, linger) and sudo cannot ask for a password here. " + admin},
		{"not a sudoer", func(m *fake) {
			password(m)
			m.tty = true
			m.cmds[key("sudo", "-n", "-l")] = fails("Sorry, user u may not run sudo on lab.")
		}, "✗ stop: one step needs root (the state directory, the collector unit, linger) and u may not run sudo here. " + admin},
		{"no sudo", func(m *fake) { delete(m.path, "sudo") },
			"✗ stop: one step needs root (the state directory, the collector unit, linger) and sudo is not installed here. " + admin},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			c.edit(m)
			says(t, Inspect(m, clone), c.want)
		})
	}
}

func TestTheClone(t *testing.T) {
	for _, c := range []struct {
		name, at string
		edit     func(*fake)
		want     string
	}{
		{"tmpfs", clone, func(m *fake) { m.space[clone] = Space{Free: gb, Tmpfs: true} },
			"✗ stop: the clone lives on tmpfs (" + clone + "): it is gone after a reboot, and the collector and the hooks run from it. " +
				"Clone into a permanent place: git clone https://github.com/BeMySlaveDarlin/aacpanel ~/aacpanel"},
		{"a space in the path", "/home/u/my panel", func(m *fake) {
			m.files["/home/u/my panel/install.sh"] = ""
			m.files["/home/u/my panel/docker-compose.yml"] = ""
		}, "✗ stop: /home/u/my panel has characters unit files and hook commands cannot carry (spaces, quotes). Move the clone, e.g. to ~/aacpanel."},
		{"somebody else's", clone, func(m *fake) { m.stats[clone] = Stat{Mode: fs.ModeDir | 0o755, UID: 0} },
			"✗ stop: " + clone + " belongs to root; the installer writes .env there. Clone it as u."},
		{"not a clone", "/home/u", func(*fake) {},
			"✗ stop: /home/u is not a clone of aacpanel: run ./install.sh from the clone."},
		{"through a symlink", "/home/u/panel", func(m *fake) { m.links["/home/u/panel"] = clone },
			"✓ the clone ~/aacpanel belongs to u and is not on tmpfs"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			m.links = map[string]string{}
			c.edit(m)
			says(t, Inspect(m, c.at), c.want)
		})
	}
}

func TestContainerNames(t *testing.T) {
	ps := key("docker", "ps", "-a", "--format", psFormat)
	m := healthy()
	m.cmds[ps] = ok("aacpanel-db\tshop\t\n")
	says(t, Inspect(m, clone), `✗ stop: a container named aacpanel-db exists and belongs to compose project "shop", not to the panel. Remove or rename it.`)
	m.cmds[ps] = ok("aacpanel\t\t\n")
	says(t, Inspect(m, clone), "✗ stop: a container named aacpanel exists outside any compose project, not as part of the panel. Remove or rename it.")
	m.cmds[ps] = ok("aacpanel\taacpanel\t\naacpanel-db\taacpanel\t\nweb\tshop\t\n")
	says(t, Inspect(m, clone), "✓ the containers aacpanel, aacpanel-db are the panel's")
}

func TestRoom(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*fake)
		want string
	}{
		{"docker root full", func(m *fake) { m.space["/var/lib/docker"] = Space{Free: 2 * gb} },
			"✗ stop: /var/lib/docker has 2.0 GB free; the images and the build need about 3 GB."},
		{"docker root tight", func(m *fake) { m.space["/var/lib/docker"] = Space{Free: 4 * gb} },
			"⚠ warn: /var/lib/docker has 4.0 GB free: the images and the build fit, with little room left to grow."},
		{"state full", func(m *fake) { m.space["/var/lib"] = Space{Free: gb / 2} },
			"✗ stop: /var/lib has 0.5 GB free; the state of the panel needs about 1 GB."},
		{"little memory", func(m *fake) { m.files["/proc/meminfo"] = "MemTotal:        1000000 kB\n" },
			"✗ stop: 1.0 GB of memory: the image build does not fit."},
		{"some memory", func(m *fake) { m.files["/proc/meminfo"] = "MemTotal:        3000000 kB\n" },
			"⚠ warn: 3.1 GB of memory: the image build may run short of it."},
		{"a VM of 2 GB is not below the floor", func(m *fake) { m.files["/proc/meminfo"] = "MemTotal:        2014000 kB\n" },
			"⚠ warn: 2.1 GB of memory: the image build may run short of it."},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			c.edit(m)
			says(t, Inspect(m, clone), c.want)
		})
	}
}

func TestTheNetworkOnlyWarns(t *testing.T) {
	m := healthy()
	m.reach = map[string]int{"https://registry-1.docker.io/v2/": 503}
	in := Inspect(m, clone)
	says(t, in, "⚠ warn: registry-1.docker.io does not answer; pulling the images may fail.")
	says(t, in, "⚠ warn: proxy.golang.org does not answer; building the executor may fail.")
	if in.Stops() != 0 {
		t.Errorf("the network stopped the install: %q", stopsOf(in))
	}
}

func TestAnEarlierInstallSetsTheMode(t *testing.T) {
	ps := key("docker", "ps", "-a", "--format", psFormat)
	volumes := key("docker", "volume", "ls", "--format", "{{.Name}}")
	manifest := home + "/.local/state/aacpanel-install/manifest.tsv"
	for _, c := range []struct {
		name string
		edit func(*fake)
		mode Mode
		want string
	}{
		{"nothing", func(*fake) {}, Fresh, "✓ no trace of the panel: a fresh install"},
		{"the manifest", func(m *fake) { m.files[manifest] = "manifest\tdir\t/x\tcreated\n" }, Upgrade,
			"✓ the manifest of an earlier install is here: this run updates it"},
		{"an install by hand", func(m *fake) {
			m.files["/var/lib/aacpanel/host.env"] = "AACP_REPO=" + clone + "\n"
			m.files[clone+"/.env"] = "AACP_DB_PASSWORD=0123abcd\n"
			m.files[home+"/bin/aacpanel-exec"] = ""
			m.cmds[ps] = ok("aacpanel\taacpanel\t\naacpanel-db\taacpanel\t\n")
			m.cmds[volumes] = ok("aacpanel_aacpanel-db\nshop_data\n")
		}, Adopt, "✓ the panel is here, installed without the installer: this run takes it over"},
		{"a volume without its password", func(m *fake) { m.cmds[volumes] = ok("aacpanel_aacpanel-db\n") }, Adopt,
			"✗ stop: the database volume aacpanel_aacpanel-db exists, but the .env with its password is missing. " +
				"Put back the .env of that install, or delete the volume with everything in it " +
				"(docker volume rm aacpanel_aacpanel-db: the map, the journal, the enrolled devices) and run again."},
		{"a description of another clone", func(m *fake) {
			m.files["/var/lib/aacpanel/host.env"] = "AACP_REPO=/srv/aacpanel\n"
		}, Adopt, "✗ stop: /var/lib/aacpanel/host.env names another clone (/srv/aacpanel): run the installer from there or change AACP_REPO."},
		{"a state directory of the .env's choosing", func(m *fake) {
			m.files[clone+"/.env"] = "AACP_STATE_DIR=/srv/state\n"
			m.files["/srv/state/host.env"] = "AACP_REPO=/srv/other\n"
		}, Adopt, "✗ stop: /srv/state/host.env names another clone (/srv/other): run the installer from there or change AACP_REPO."},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := healthy()
			c.edit(m)
			in := Inspect(m, clone)
			says(t, in, c.want)
			if in.Mode != c.mode {
				t.Errorf("mode %s, want %s; traces %q", in.Mode, c.mode, in.Traces)
			}
		})
	}
}

func TestVersionsCompareByTheirNumbers(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		older bool
	}{
		{"2.17.3", "2.18.0", true},
		{"2.18.0", "2.18.0", false},
		{"5.5.1", "2.18.0", false},
		{"2.24.6+ds1-0ubuntu2", "2.18.0", false},
		{"1.29.2-3", "2.18.0", true},
		{"1:2.20.2-1", "2.18.0", false},
		{"v2.9", "2.18.0", true},
		{"3.8.10", "3.9", true},
		{"3.13.5", "3.9", false},
	} {
		if got := older(c.a, c.b); got != c.older {
			t.Errorf("older(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
