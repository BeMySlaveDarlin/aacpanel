package install

import "io/fs"

// fake is the machine of the tests: a Table the tests fill in.
type fake = Table

func ok(out string) Reply       { return Says(out) }
func fails(stderr string) Reply { return FailsWith(stderr) }
func key(name string, args ...string) string {
	return Command(name, args...)
}

const (
	home  = "/home/u"
	clone = home + "/aacpanel"
)

// healthy is an Ubuntu machine that takes the panel as it is: every check
// passes, and nothing of the panel is on it yet.
func healthy() *fake {
	return &fake{
		EUID:  1000,
		Acct:  Account{Name: "u", UID: 1000, GID: 1000, Home: home},
		Uname: "x86_64",
		Vars:  map[string]string{"HOME": home},
		Files: map[string]string{
			"/etc/os-release":             "PRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\nNAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n",
			"/proc/sys/kernel/osrelease":  "6.8.0-45-generic\n",
			"/proc/1/comm":                "systemd\n",
			"/proc/meminfo":               "MemTotal:       16303412 kB\nMemFree:         1000000 kB\n",
			"/proc/net/tcp":               "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n",
			"/proc/net/tcp6":              "  sl  local_address                         remote_address                        st\n",
			"/etc/passwd":                 "root:x:0:0:root:/root:/bin/bash\nu:x:1000:1000::/home/u:/bin/bash\n",
			clone + "/install.sh":         "#!/usr/bin/env bash\n",
			clone + "/docker-compose.yml": "name: aacpanel\n",
			home + "/.local/bin/claude":   "\x7fELF\x02\x01\x01",
		},
		Stats: map[string]Stat{
			clone:                  {Mode: fs.ModeDir | 0o755, UID: 1000},
			"/var/run/docker.sock": {Mode: fs.ModeSocket | 0o660, UID: 0},
			"/var/lib":             {Mode: fs.ModeDir | 0o755, UID: 0},
			"/usr/bin/node":        {},
		},
		Path: map[string]string{
			"apt-get": "/usr/bin/apt-get", "dpkg": "/usr/bin/dpkg",
			"docker": "/usr/bin/docker", "tmux": "/usr/bin/tmux", "jq": "/usr/bin/jq",
			"claude": home + "/.local/bin/claude", "python3": "/usr/bin/python3", "sudo": "/usr/bin/sudo",
			"curl": "/usr/bin/curl",
		},
		Cmds: map[string]Reply{
			key("git", "-C", clone, "rev-parse", "--short", "HEAD"):                                    ok("3f1c2ab\n"),
			key("systemctl", "--version"):                                                              ok("systemd 255 (255.4-1ubuntu8)\n+PAM +AUDIT\n"),
			key("docker", "version", "--format", "{{.Server.Version}}"):                                ok("27.5.1\n"),
			key("docker", "info", "--format", "{{json .}}"):                                            ok(`{"SecurityOptions":["name=apparmor","name=seccomp,profile=builtin"],"OperatingSystem":"Ubuntu 24.04.3 LTS","DockerRootDir":"/var/lib/docker"}`),
			key("docker", "compose", "version", "--short"):                                             ok("2.33.1\n"),
			key("docker", "ps", "-a", "--format", psFormat):                                            ok(""),
			key("docker", "volume", "ls", "--format", "{{.Name}}"):                                     ok(""),
			key(home+"/.local/bin/claude", "--version"):                                                ok("2.1.283 (Claude Code)\n"),
			key("python3", "-c", pythonVersion):                                                        ok("3.12.3\n"),
			key("sudo", "-n", "true"):                                                                  ok(""),
			key("ss", "-Hltnp"):                                                                        ok(""),
			key("apt-cache", "policy", "docker-compose-v2", "docker-compose-plugin", "docker-compose"): ok(""),
		},
		Spaces: map[string]Space{
			clone:             {Free: 100 * gb},
			"/var/lib/docker": {Free: 212 * gb},
			"/var/lib":        {Free: 212 * gb},
		},
		Web: map[string]int{
			"https://registry-1.docker.io/v2/": 401,
			"https://proxy.golang.org/":        302,
		},
	}
}
