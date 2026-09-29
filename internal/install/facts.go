package install

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/hostcfg"
)

// MinCompose is the oldest docker compose the stack of the panel comes up on,
// by the releases of docker/compose on GitHub: the image is built with
// BuildKit cache mounts (2.0.0), up leaves a service of replicas 0 alone
// (2.2.0), the project is named at the top of the file (2.3.0, compose-go
// 1.1.0), and the installer asks up what it would do with --dry-run — the
// newest of the four, which reached up in 2.18.0. Compose 5.5.1 does all
// four: replicas from ${VAR:-0} come out as 0, a dry run creates nothing,
// and the cache mount builds.
const MinCompose = "2.18.0"

// Repository is where the panel is cloned from.
const Repository = "https://github.com/BeMySlaveDarlin/aacpanel"

// The fixed ports of the panel, and the range the home-network listener
// moves into when 8443 is somebody else's.
const (
	PortPanel = 8776
	PortLocal = 8777
	PortLAN   = 8443
	lanFirst  = 18443
	lanLast   = 18499
)

// PanelContainers are the names the compose file gives its containers.
var PanelContainers = []string{"aacpanel", "aacpanel-db", "aacpanel-socket-proxy", "aacpanel-tailscale"}

// Project is the compose project of the panel, and DBVolume the volume of
// its database: the map, the journal, the enrolled devices.
const (
	Project  = "aacpanel"
	DBVolume = "aacpanel_aacpanel-db"
)

// DefaultStateDir is where the panel keeps its state unless the .env of the
// clone names another place.
const DefaultStateDir = "/var/lib/aacpanel"

// Mode is what the run finds of an earlier install.
type Mode string

const (
	Fresh   Mode = "fresh"   // no trace of the panel
	Upgrade Mode = "upgrade" // the installer's manifest is here
	Adopt   Mode = "adopt"   // traces without a manifest: an install by hand
)

// Facts are what the check learned of the machine; the questions take their
// suggested answers from them.
type Facts struct {
	Account    Account
	Clone      string // the clone with its symlinks resolved
	Version    string // the commit the clone is on
	InstallDir string // the installer's own: manifest, journal, backups
	StateDir   string
	OS         string // PRETTY_NAME
	Arch       string
	Docker     string
	Compose    string
	Claude     string // the path, as PATH gives it
	LANPort    int
	Mode       Mode
	Traces     []string // what an earlier install left, when there was one
	// ViaGroup is docker reached through sg: the user is in the docker group
	// and this terminal began before the group was added.
	ViaGroup bool
}

// Finding is a line of the check.
type Finding struct {
	Mark Mark
	Text string
	// Missing is set for a program the installer can put on the machine
	// itself. The check leaves the decision to the run: now it stops with
	// the command, and a run that asks turns it into a question.
	Missing *Missing
}

// Missing is a program the panel needs and the machine lacks.
type Missing struct {
	Name    string
	Why     string // what the panel does with it
	Command string // what puts it there by hand
}

// Stop is the stop of a run that does not install what is missing.
func (m Missing) Stop() string {
	return fmt.Sprintf("stop: %s is needed: %s. %s, then run again.", m.Name, m.Why, m.Command)
}

// Inspection is the whole check: the facts and the lines, in the order the
// feed shows them.
type Inspection struct {
	Facts
	Findings []Finding
}

// Stops counts the lines that stop the install.
func (in Inspection) Stops() int {
	n := 0
	for _, f := range in.Findings {
		if f.Mark == Stop {
			n++
		}
	}
	return n
}

// Inspect looks the machine over for the clone at clone. It reads and never
// writes: the check runs before anything is approved.
func Inspect(m Machine, clone string) Inspection {
	c := &checker{m: m}
	c.account(clone)
	c.system()
	c.docker()
	c.claude()
	c.tools()
	c.ports()
	c.python()
	c.sudo()
	c.clone()
	c.names()
	c.room()
	c.network()
	c.previous()
	return Inspection{Facts: c.f, Findings: c.out}
}

type checker struct {
	m   Machine
	f   Facts
	out []Finding

	osID      []string // ID and ID_LIKE
	dockerUp  bool     // the daemon answers this user
	dockerDir string
	listed    []container
}

func (c *checker) add(m Mark, format string, args ...any) {
	c.out = append(c.out, Finding{Mark: m, Text: fmt.Sprintf(format, args...)})
}

func (c *checker) missing(m Missing) {
	c.out = append(c.out, Finding{Mark: Stop, Text: m.Stop(), Missing: &m})
}

// Short writes a path under the home directory with ~, the way a person
// reads it.
func (f Facts) Short(path string) string {
	home := f.Account.Home
	if home != "" && home != "/" && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (c *checker) short(path string) string { return c.f.Short(path) }

func (c *checker) account(clone string) {
	a, err := c.m.User()
	if err != nil {
		a = Account{Name: c.m.Env("USER"), UID: c.m.Euid(), GID: -1, Home: c.m.Env("HOME")}
	}
	c.f.Account = a
	c.f.InstallDir = StateDir(c.m.Env)
	c.f.Clone = clone
	if real, err := c.m.Real(clone); err == nil {
		c.f.Clone = real
	}
	if out, err := c.m.Run("git", "-C", c.f.Clone, "rev-parse", "--short", "HEAD"); err == nil {
		c.f.Version = strings.TrimSpace(out)
	}
	c.f.StateDir = DefaultStateDir
	if env, err := c.m.ReadFile(filepath.Join(c.f.Clone, ".env")); err == nil {
		if dir := hostcfg.Parse(env)[hostcfg.StateDirEnv]; dir != "" {
			c.f.StateDir = dir
		}
	}
	if c.m.Euid() == 0 {
		c.add(Stop, "stop: run the installer as the user whose claude sessions the panel will manage, not as root: "+
			"the executor refuses to run as root. As that user: ./install.sh")
	}
}

// system checks the kind of machine: the family, WSL, systemd, the
// architecture. The version of the system is not checked: what the panel
// needs of it is checked by what it can do.
func (c *checker) system() {
	stops := len(c.out)
	osr := map[string]string{}
	if raw, err := c.m.ReadFile("/etc/os-release"); err == nil {
		osr = hostcfg.Parse(raw)
	}
	c.f.OS = osr["PRETTY_NAME"]
	if c.f.OS == "" {
		c.f.OS = osr["NAME"]
	}
	if c.f.OS == "" {
		c.f.OS = "this system"
	}
	c.osID = append(strings.Fields(osr["ID"]), strings.Fields(osr["ID_LIKE"])...)
	family := slices.Contains(c.osID, "debian") || slices.Contains(c.osID, "ubuntu")
	_, apt := c.m.LookPath("apt-get")
	_, dpkg := c.m.LookPath("dpkg")
	if !family || apt != nil || dpkg != nil {
		c.add(Stop, "stop: %s is not of the Debian or Ubuntu family: the installer installs with apt. "+
			"The manual path is INSTALL.md, part 2.", c.f.OS)
	}

	if raw, err := c.m.ReadFile("/proc/sys/kernel/osrelease"); err == nil &&
		strings.Contains(strings.ToLower(string(raw)), "microsoft") {
		c.add(Stop, "stop: WSL is not supported: the panel needs a systemd machine with the system docker daemon.")
	}

	systemd := ""
	init, _ := c.m.ReadFile("/proc/1/comm")
	if comm := strings.TrimSpace(string(init)); comm != "systemd" {
		if comm == "" {
			comm = "unknown"
		}
		c.add(Stop, "stop: systemd is not the init of this machine (PID 1 is %s): "+
			"the collector and the executor are systemd units.", comm)
	} else if out, err := c.m.Run("systemctl", "--version"); err == nil {
		f := strings.Fields(out)
		if len(f) >= 2 {
			systemd = f[1]
		}
		if v, err := strconv.Atoi(systemd); err == nil && v < 249 {
			c.add(Stop, "stop: systemd %d is too old: the units of the panel need 249 or newer.", v)
		}
	}

	c.f.Arch = c.m.Arch()
	switch c.f.Arch {
	case "x86_64":
	case "aarch64":
		c.add(Warn, "warn: aarch64 is not tried on the stand: a step may fail here that goes through on x86_64.")
	default:
		c.add(Stop, "stop: %s is not supported.", c.f.Arch)
	}

	if !c.stoppedSince(stops) {
		line := c.f.OS + " · " + c.f.Arch
		if systemd != "" {
			line += " · systemd " + systemd
		}
		c.insert(stops, Finding{Mark: Pass, Text: line})
	}
}

// stoppedSince tells whether a stop was added after the first n lines.
func (c *checker) stoppedSince(n int) bool {
	for _, f := range c.out[n:] {
		if f.Mark == Stop {
			return true
		}
	}
	return false
}

// insert puts a line at i: the line that sums a group up goes above the
// warnings the group raised.
func (c *checker) insert(i int, f Finding) {
	c.out = slices.Insert(c.out, i, f)
}

// docker checks the daemon, that it is the system one, and compose.
func (c *checker) docker() {
	path, err := c.m.LookPath("docker")
	if err != nil {
		c.dockerMissing()
		return
	}
	out, err := c.dockerRun("version", "--format", "{{.Server.Version}}")
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "permission denied") && c.inDockerGroup() {
		// The group came after this terminal did: the run that added it
		// stopped later, and this one began in the same terminal. sg opens
		// the group without a new login, as the run that added it did.
		c.f.ViaGroup = true
		out, err = c.dockerRun("version", "--format", "{{.Server.Version}}")
	}
	if err != nil {
		why := strings.ToLower(err.Error())
		switch {
		case strings.Contains(why, "permission denied"):
			u := c.f.Account.Name
			c.add(Stop, `stop: docker answers "permission denied": %s is not in the docker group. `+
				"sudo usermod -aG docker %s, log out and in, run ./install.sh again.", u, u)
		case strings.Contains(why, "cannot connect") || strings.Contains(why, "daemon running"):
			c.add(Stop, "stop: the docker daemon does not answer: sudo systemctl enable --now docker")
		default:
			c.add(Stop, "stop: docker version fails: %s", firstLine(err.Error()))
		}
		return
	}
	c.dockerUp = true
	c.f.Docker = strings.TrimSpace(out)
	stops := len(c.out)

	var info struct {
		SecurityOptions []string
		OperatingSystem string
		DockerRootDir   string
	}
	if raw, err := c.dockerRun("info", "--format", "{{json .}}"); err == nil {
		_ = json.Unmarshal([]byte(raw), &info)
	}
	c.dockerDir = info.DockerRootDir
	kind := ""
	real, _ := c.m.Real(path)
	sock, sockErr := c.m.Stat("/var/run/docker.sock")
	switch {
	case strings.HasPrefix(real, "/snap/") || strings.HasPrefix(path, "/snap/"):
		kind = "snap"
	case slices.ContainsFunc(info.SecurityOptions, func(s string) bool { return strings.Contains(s, "rootless") }):
		kind = "rootless"
	case strings.Contains(info.OperatingSystem, "Docker Desktop"):
		kind = "Docker Desktop"
	case sockErr != nil || sock.Mode&fs.ModeSocket == 0:
		kind = "this"
	}
	if kind != "" {
		c.add(Stop, "stop: %s docker is not supported: the panel reads /var/run/docker.sock of the system daemon "+
			"and mounts the state directory and /run/user/%d into its container.", kind, c.f.Account.UID)
	}

	out, err = c.dockerRun("compose", "version", "--short")
	if err != nil {
		if pkg, ok := c.composePackage(); ok {
			c.missing(Missing{Name: "docker compose", Why: "the panel is a stack of compose services",
				Command: "sudo apt install " + pkg})
		} else {
			c.add(Stop, "stop: apt has no docker compose v2 of %s or newer here. "+
				"Install docker from download.docker.com and run again.", MinCompose)
		}
		return
	}
	c.f.Compose = strings.TrimPrefix(strings.TrimSpace(out), "v")
	if older(c.f.Compose, MinCompose) {
		c.add(Stop, "stop: docker compose %s is older than %s.", c.f.Compose, MinCompose)
	}
	if !c.stoppedSince(stops) {
		line := "docker " + c.f.Docker + " · compose " + c.f.Compose + " · system daemon"
		if c.f.ViaGroup {
			line += " · through sg docker: this terminal began before the group was added"
		}
		c.insert(stops, Finding{Mark: Pass, Text: line})
	}
}

// dockerRun runs docker for the check: straight, or through sg when the
// docker group is the user's and not yet this terminal's.
func (c *checker) dockerRun(args ...string) (string, error) {
	if !c.f.ViaGroup {
		return c.m.Run("docker", args...)
	}
	return c.m.Run("sg", "docker", "-c", shellLine(append([]string{"docker"}, args...)))
}

// inDockerGroup tells whether the group database puts the user in the docker
// group, whatever the groups this process was started with.
func (c *checker) inDockerGroup() bool {
	out, err := c.m.Run("id", "-nG", c.f.Account.Name)
	return err == nil && slices.Contains(strings.Fields(out), "docker")
}

func (c *checker) dockerMissing() {
	pkg, ok := c.composePackage()
	if !ok {
		c.add(Stop, "stop: apt has no docker compose v2 of %s or newer here. "+
			"Install docker from download.docker.com and run again.", MinCompose)
		return
	}
	c.missing(Missing{Name: "docker",
		Why:     "the panel, its database and the socket proxy run as containers",
		Command: "sudo apt install docker.io " + pkg})
}

// composePackages are the names compose goes by in apt, in the order they
// are tried: Ubuntu's, docker.com's, Debian's.
var composePackages = []string{"docker-compose-v2", "docker-compose-plugin", "docker-compose"}

// composePackage picks the package of compose apt would install: the first
// whose candidate is MinCompose or newer. When apt knows none of them — its
// lists were never fetched, as in a fresh container — the family's usual
// name stands in, and the root step fetches the lists before it installs.
// Only when apt knows them all to be older is there no package: Debian 12
// has compose v1 alone.
func (c *checker) composePackage() (string, bool) {
	out, _ := c.m.Run("apt-cache", "policy", composePackages[0], composePackages[1], composePackages[2])
	candidate := map[string]string{}
	name := ""
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			name = strings.TrimSuffix(line, ":")
			continue
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Candidate:"); ok && name != "" {
			if v = strings.TrimSpace(v); v != "(none)" {
				candidate[name] = v
			}
		}
	}
	for _, pkg := range composePackages {
		if v, ok := candidate[pkg]; ok && !older(v, MinCompose) {
			return pkg, true
		}
	}
	if len(candidate) > 0 {
		return "", false
	}
	if slices.Contains(c.osID, "ubuntu") {
		return "docker-compose-v2", true
	}
	return "docker-compose", true
}

// unitPath is the PATH a user unit runs with: systemd's own, not the one of
// the person's shell.
var unitPath = []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}

func (c *checker) claude() {
	path, err := c.m.LookPath("claude")
	if err != nil {
		c.missing(Missing{Name: "claude", Why: "the panel starts and watches claude sessions",
			Command: "curl -fsSL https://claude.ai/install.sh | bash"})
		return
	}
	c.f.Claude = path
	if head, err := c.m.Head(path, 256); err == nil && strings.HasPrefix(string(head), "#!") {
		f := strings.Fields(strings.TrimPrefix(firstLine(string(head)), "#!"))
		if len(f) >= 2 && filepath.Base(f[0]) == "env" && f[len(f)-1] == "node" && !c.onUnitPath("node") {
			where := "a directory of your shell's PATH"
			if node, err := c.m.LookPath("node"); err == nil {
				where = filepath.Dir(node)
			}
			c.add(Stop, "stop: claude runs on node from %s, which the executor's unit does not see: "+
				"panel sessions would not start. Install claude natively.", c.short(where))
			return
		}
	}
	line := "claude"
	if out, err := c.m.Run(path, "--version"); err == nil {
		if f := strings.Fields(out); len(f) > 0 {
			line += " " + f[0]
		}
	}
	c.add(Pass, "%s · %s", line, c.short(path))
}

func (c *checker) onUnitPath(name string) bool {
	for _, dir := range unitPath {
		if st, err := c.m.Stat(filepath.Join(dir, name)); err == nil && st.Mode.IsRegular() {
			return true
		}
	}
	return false
}

func (c *checker) tools() {
	why := map[string]string{
		"tmux": "sessions live in it, and without it the executor turns off every action on sessions",
		"jq":   "the status line script reads the limits with it",
	}
	all := true
	for _, name := range []string{"tmux", "jq"} {
		if _, err := c.m.LookPath(name); err != nil {
			all = false
			c.missing(Missing{Name: name, Why: why[name], Command: "sudo apt install " + name})
		}
	}
	if all {
		c.add(Pass, "tmux and jq are here")
	}
}

// psFormat is what docker ps says of a container: its name, its compose
// project and its published ports.
const psFormat = `{{.Names}}\t{{.Label "com.docker.compose.project"}}\t{{.Ports}}`

// container is a line of docker ps.
type container struct {
	name, project string
	ports         []int // the host ports it publishes
}

func (c *checker) dockerPS() []container {
	if !c.dockerUp || c.listed != nil {
		return c.listed
	}
	c.listed = []container{}
	out, err := c.dockerRun("ps", "-a", "--format", psFormat)
	if err != nil {
		return c.listed
	}
	// A container without ports ends its line in a tab, so the lines are
	// not trimmed.
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || f[0] == "" {
			continue
		}
		ct := container{name: f[0], project: f[1]}
		for _, p := range strings.Split(f[2], ",") {
			host, _, ok := strings.Cut(strings.TrimSpace(p), "->")
			if !ok {
				continue
			}
			if i := strings.LastIndex(host, ":"); i >= 0 {
				ct.ports = append(ct.ports, portRange(host[i+1:])...)
			}
		}
		c.listed = append(c.listed, ct)
	}
	return c.listed
}

// portRange reads a host port of docker ps: one port, or the range docker
// makes of ports published one after another (8776-8777).
func portRange(s string) []int {
	from, to, isRange := strings.Cut(s, "-")
	lo, err := strconv.Atoi(from)
	if err != nil {
		return nil
	}
	hi := lo
	if isRange {
		if hi, err = strconv.Atoi(to); err != nil || hi < lo || hi > 65535 {
			return nil
		}
	}
	var out []int
	for p := lo; p <= hi; p++ {
		out = append(out, p)
	}
	return out
}

func (ct container) panels() bool {
	return ct.project == Project && slices.Contains(PanelContainers, ct.name)
}

// ports checks the fixed ports of the panel. A port the panel itself
// publishes is its own and fine. 8443 taken by somebody else is not a stop:
// the home-network listener moves to a free port of its range.
func (c *checker) ports() {
	listening := c.listeners()
	var free, own []string
	c.f.LANPort = PortLAN
	for _, port := range []int{PortPanel, PortLocal, PortLAN} {
		holder, taken := listening[port]
		switch {
		case !taken:
			free = append(free, strconv.Itoa(port))
		case holder == "":
			own = append(own, strconv.Itoa(port))
		case port == PortLAN:
			c.f.LANPort = 0
			for p := lanFirst; p <= lanLast; p++ {
				if _, taken := listening[p]; !taken {
					c.f.LANPort = p
					break
				}
			}
		default:
			c.add(Stop, "stop: 127.0.0.1:%d is taken by %s. The panel publishes there and the port is fixed. "+
				"Free it and run again.", port, holder)
		}
	}
	var parts []string
	if len(free) > 0 {
		parts = append(parts, strings.Join(free, ", ")+" are free")
	}
	if len(own) > 0 {
		parts = append(parts, strings.Join(own, ", ")+" are the panel's own")
	}
	if len(parts) > 0 {
		parts[0] = "ports " + parts[0]
	}
	switch {
	case c.f.LANPort == 0:
		c.add(Stop, "stop: 127.0.0.1:%d and every port from %d to %d are taken; the home-network listener has "+
			"nowhere to go. Free one and run again.", PortLAN, lanFirst, lanLast)
	case c.f.LANPort != PortLAN:
		parts = append(parts, fmt.Sprintf("8443 is taken, the home network gets %d", c.f.LANPort))
	}
	if len(parts) > 0 {
		c.add(Pass, "%s", strings.Join(parts, " · "))
	}
}

// listeners are the ports something listens on where the panel publishes —
// loopback or every address — with who holds each: empty for the panel,
// else a container or a process. A port docker publishes without a proxy
// process listens nowhere the kernel shows, so the ports of containers
// count as well.
func (c *checker) listeners() map[int]string {
	out := map[int]string{}
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := c.m.ReadFile(file)
		if err != nil {
			continue
		}
		for _, port := range listening(string(raw)) {
			out[port] = "a process of another user"
		}
	}
	if ss, err := c.m.Run("ss", "-Hltnp"); err == nil {
		for port, proc := range ssProcesses(ss) {
			if _, ok := out[port]; ok {
				out[port] = proc
			}
		}
	}
	for _, ct := range c.dockerPS() {
		for _, port := range ct.ports {
			if ct.panels() {
				out[port] = ""
			} else {
				out[port] = "the container " + ct.name
			}
		}
	}
	return out
}

// listening reads /proc/net/tcp or tcp6 for the ports in LISTEN on an
// address that stands in the way of 127.0.0.1: loopback itself and the
// address of every interface.
func listening(table string) []int {
	var out []int
	for _, line := range strings.Split(table, "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != "0A" {
			continue
		}
		addr, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		port, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil {
			continue
		}
		switch addr {
		case "0100007F", "00000000", // 127.0.0.1, 0.0.0.0
			"00000000000000000000000000000000", // ::
			"0000000000000000FFFF00000100007F": // ::ffff:127.0.0.1
			out = append(out, int(port))
		}
	}
	return out
}

var ssUser = regexp.MustCompile(`users:\(\("([^"]+)",pid=(\d+)`)

// ssProcesses names the processes ss can see behind listening ports: only
// the user's own, since the rest are hidden from an unprivileged ss.
func ssProcesses(out string) map[int]string {
	procs := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		local := f[3]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		if m := ssUser.FindStringSubmatch(line); m != nil {
			procs[port] = fmt.Sprintf("%s (pid %s)", m[1], m[2])
		}
	}
	return procs
}

const pythonVersion = `import sys; print("%d.%d.%d" % sys.version_info[:3])`

func (c *checker) python() {
	const found = "stop: python3 3.9 or newer is needed by the collector (found %s)."
	if _, err := c.m.LookPath("python3"); err != nil {
		c.add(Stop, found, "none")
		return
	}
	out, err := c.m.Run("python3", "-c", pythonVersion)
	v := strings.TrimSpace(out)
	if err != nil || v == "" {
		c.add(Stop, found, "one that does not run")
		return
	}
	if older(v, "3.9") {
		c.add(Stop, found, v)
		return
	}
	c.add(Pass, "python3 %s for the collector", v)
}

// sudo checks that the one step as root can run: at once, or with a
// password asked at the terminal.
//
// A person without sudo of their own goes on: the root command's frame takes
// a No, and the run stops there with the command an administrator runs —
// with the host description it names already written, which it is not yet
// while the machine is only looked over.
func (c *checker) sudo() {
	why := "sudo cannot ask for a password here"
	if _, err := c.m.LookPath("sudo"); err != nil {
		why = "sudo is not installed here"
	} else if _, err := c.m.Run("sudo", "-n", "true"); err == nil {
		c.add(Pass, "sudo goes through without a password")
		return
	} else if out, err := c.m.Run("sudo", "-n", "-l"); strings.Contains(out+errText(err), "may not run") {
		why = c.f.Account.Name + " may not run sudo here"
	} else if c.m.Terminal() {
		c.add(Pass, "sudo asks for a password: the root step runs one sudo")
		return
	}
	if c.m.Terminal() {
		c.add(Warn, "warn: one step needs root, and %s. At the root command answer No: the run stops there with "+
			"the command for an administrator, and ./install.sh goes on once it has run.", why)
		return
	}
	c.add(Stop, "stop: one step needs root (the state directory, the collector unit, linger) and %s. "+
		"Run ./install.sh at a terminal: sudo asks for its password there, and a No at the root command "+
		"stops the run with the command for an administrator.", why)
}

var clonePath = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

// clone checks the place of the clone: the collector and the hooks run from
// it for as long as the panel is installed.
func (c *checker) clone() {
	dir := c.f.Clone
	for _, name := range []string{"install.sh", "docker-compose.yml"} {
		if _, err := c.m.Stat(filepath.Join(dir, name)); err != nil {
			c.add(Stop, "stop: %s is not a clone of aacpanel: run ./install.sh from the clone.", dir)
			return
		}
	}
	stops := len(c.out)
	if sp, err := c.m.Space(dir); err == nil && sp.Tmpfs {
		c.add(Stop, "stop: the clone lives on tmpfs (%s): it is gone after a reboot, and the collector and the hooks "+
			"run from it. Clone into a permanent place: git clone %s ~/aacpanel", dir, Repository)
	}
	if !clonePath.MatchString(dir) {
		c.add(Stop, "stop: %s has characters unit files and hook commands cannot carry (spaces, quotes). "+
			"Move the clone, e.g. to ~/aacpanel.", dir)
	}
	if st, err := c.m.Stat(dir); err == nil && st.UID != c.f.Account.UID {
		c.add(Stop, "stop: %s belongs to %s; the installer writes .env there. Clone it as %s.",
			dir, c.owner(st.UID), c.f.Account.Name)
	}
	if !c.stoppedSince(stops) {
		c.add(Pass, "the clone %s belongs to %s and is not on tmpfs", c.short(dir), c.f.Account.Name)
	}
}

// owner names the user of a uid by /etc/passwd, or by the number.
func (c *checker) owner(uid int) string {
	raw, _ := c.m.ReadFile("/etc/passwd")
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 2 && f[2] == strconv.Itoa(uid) {
			return f[0]
		}
	}
	return "uid " + strconv.Itoa(uid)
}

// names checks that the names the compose file gives its containers are
// free, or the panel's own.
func (c *checker) names() {
	if !c.dockerUp {
		return
	}
	stops := len(c.out)
	var ours []string
	for _, ct := range c.dockerPS() {
		if !slices.Contains(PanelContainers, ct.name) {
			continue
		}
		if ct.project == Project {
			ours = append(ours, ct.name)
			continue
		}
		if ct.project == "" {
			c.add(Stop, "stop: a container named %s exists outside any compose project, not as part of the panel. "+
				"Remove or rename it.", ct.name)
			continue
		}
		c.add(Stop, `stop: a container named %s exists and belongs to compose project "%s", not to the panel. `+
			"Remove or rename it.", ct.name, ct.project)
	}
	switch {
	case c.stoppedSince(stops):
	case len(ours) > 0:
		c.add(Pass, "the containers %s are the panel's", strings.Join(ours, ", "))
	default:
		c.add(Pass, "no container is named %s", strings.Join(PanelContainers, ", "))
	}
}
