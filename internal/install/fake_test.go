package install

import (
	"errors"
	"io/fs"
	"os/exec"
	"strings"
)

// fake is a machine made of tables: the files it has, the programs on its
// PATH and what each command answers. Anything not in a table is not there,
// so a check that reaches past the tables finds nothing rather than this
// machine.
type fake struct {
	euid  int
	acct  Account
	arch  string
	env   map[string]string
	files map[string]string
	stats map[string]Stat
	links map[string]string
	path  map[string]string
	cmds  map[string]answer
	space map[string]Space
	reach map[string]int
	tty   bool
}

// answer is what a command gives: its output, or a failure with what it
// said on standard error.
type answer struct {
	out  string
	fail string
	code int
}

func ok(out string) answer       { return answer{out: out} }
func fails(stderr string) answer { return answer{fail: stderr, code: 1} }
func (a answer) failed() bool    { return a.code != 0 }
func key(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

func (f *fake) Euid() int              { return f.euid }
func (f *fake) User() (Account, error) { return f.acct, nil }
func (f *fake) Arch() string           { return f.arch }
func (f *fake) Env(k string) string    { return f.env[k] }
func (f *fake) Terminal() bool         { return f.tty }

func (f *fake) ReadFile(path string) ([]byte, error) {
	if s, ok := f.files[path]; ok {
		return []byte(s), nil
	}
	return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
}

func (f *fake) Head(path string, n int) ([]byte, error) {
	raw, err := f.ReadFile(path)
	if len(raw) > n {
		raw = raw[:n]
	}
	return raw, err
}

func (f *fake) Stat(path string) (Stat, error) {
	if st, ok := f.stats[path]; ok {
		return st, nil
	}
	if _, ok := f.files[path]; ok {
		return Stat{Mode: 0o644, UID: f.acct.UID}, nil
	}
	return Stat{}, &fs.PathError{Op: "stat", Path: path, Err: fs.ErrNotExist}
}

func (f *fake) Real(path string) (string, error) {
	if to, ok := f.links[path]; ok {
		return to, nil
	}
	return path, nil
}

func (f *fake) LookPath(name string) (string, error) {
	if p, ok := f.path[name]; ok {
		return p, nil
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func (f *fake) Run(name string, args ...string) (string, error) {
	a, ok := f.cmds[key(name, args...)]
	if !ok {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	if a.failed() {
		return a.out, &Failure{Code: a.code, Stderr: a.fail}
	}
	return a.out, nil
}

func (f *fake) Space(path string) (Space, error) {
	if sp, ok := f.space[path]; ok {
		return sp, nil
	}
	return Space{}, errors.New("no such file system")
}

func (f *fake) Reach(url string) (int, error) {
	if s, ok := f.reach[url]; ok {
		return s, nil
	}
	return 0, errors.New("dial tcp: i/o timeout")
}

const (
	home  = "/home/u"
	clone = home + "/aacpanel"
)

// healthy is an Ubuntu machine that takes the panel as it is: every check
// passes, and nothing of the panel is on it yet.
func healthy() *fake {
	return &fake{
		euid: 1000,
		acct: Account{Name: "u", UID: 1000, GID: 1000, Home: home},
		arch: "x86_64",
		env:  map[string]string{"HOME": home},
		files: map[string]string{
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
		stats: map[string]Stat{
			clone:                  {Mode: fs.ModeDir | 0o755, UID: 1000},
			"/var/run/docker.sock": {Mode: fs.ModeSocket | 0o660, UID: 0},
			"/var/lib":             {Mode: fs.ModeDir | 0o755, UID: 0},
			"/usr/bin/node":        {},
		},
		path: map[string]string{
			"apt-get": "/usr/bin/apt-get", "dpkg": "/usr/bin/dpkg",
			"docker": "/usr/bin/docker", "tmux": "/usr/bin/tmux", "jq": "/usr/bin/jq",
			"claude": home + "/.local/bin/claude", "python3": "/usr/bin/python3", "sudo": "/usr/bin/sudo",
		},
		cmds: map[string]answer{
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
		space: map[string]Space{
			clone:             {Free: 100 * gb},
			"/var/lib/docker": {Free: 212 * gb},
			"/var/lib":        {Free: 212 * gb},
		},
		reach: map[string]int{
			"https://registry-1.docker.io/v2/": 401,
			"https://proxy.golang.org/":        302,
		},
	}
}
