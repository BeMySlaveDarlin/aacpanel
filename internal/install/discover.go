package install

import (
	"crypto/x509"
	"encoding/pem"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Choice is a value found on the machine and where it was found, for a
// person to read: an option without its source is a guess the person cannot
// check.
type Choice struct{ Value, Source string }

// Found is what the questions offer as their options: everything here was
// read off this machine, nothing is a default of the panel.
type Found struct {
	Host      string // the short host name
	Terminals []Choice
	Displays  []Choice
	Locales   []Choice
	Roots     []Root
	Accounts  []ClaudeHome
	LAN       []Choice
	Proxies   []Choice
	Certs     []Cert
	Ports     []Choice
	Email     string
	// Known is every directory claude opened, by its slug, over every
	// account found, with when it last opened it.
	Known map[string]time.Time
}

// Discover looks the machine over for the options of the questions. It reads
// and never writes, like the check before it; a source that does not answer
// leaves its list empty rather than stopping the run, since every question
// also takes a typed answer.
func Discover(m Machine, f Facts, certDirs []string) Found {
	home := f.Account.Home
	accounts := ClaudeHomes(m, home)
	dirs := make([]string, len(accounts))
	for i, a := range accounts {
		dirs[i] = a.Dir
	}
	known := KnownSlugs(m, dirs)
	lan, proxies := Addresses(m)
	return Found{
		Host:      shortHost(m),
		Terminals: Terminals(m),
		Displays:  Displays(m),
		Locales:   Locales(m),
		Roots:     FindRoots(m, home, known),
		Accounts:  accounts,
		LAN:       lan,
		Proxies:   proxies,
		Certs:     Certs(m, certDirs),
		Ports:     ListeningPorts(m, []int{PortPanel, PortLocal, f.LANPort}),
		Email:     gitEmail(m, f.Clone),
		Known:     known,
	}
}

// gitEmail is the address git commits in the clone are signed with.
func gitEmail(m Machine, clone string) string {
	if clone == "" {
		return ""
	}
	out, err := m.Run("git", "-C", clone, "config", "user.email")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// shortHost is the name of the machine up to the first dot: the kernel's
// own, which an installer started from a script without a login shell still
// sees, and $HOSTNAME only when the kernel's cannot be read.
func shortHost(m Machine) string {
	name := ""
	if raw, err := m.ReadFile("/proc/sys/kernel/hostname"); err == nil {
		name = strings.TrimSpace(string(raw))
	}
	if name == "" {
		name = strings.TrimSpace(m.Env("HOSTNAME"))
	}
	name, _, _ = strings.Cut(name, ".")
	return name
}

// terminal is a terminal the launcher knows how to hand a command to. The
// way differs for each, and a wrong template does not fail loudly: the
// window just never opens, while the session lives on in tmux.
type terminal struct{ bin, spec, how string }

// knownTerminals is a closed list in the order it is offered: only
// terminals whose way of taking a command was tried. Anything else is
// typed by hand.
var knownTerminals = []terminal{
	{"konsole", "konsole", "the launcher calls it with its own flags: the directory and the tab title"},
	{"gnome-terminal", "gnome-terminal --", "the command goes after --"},
	{"xfce4-terminal", "xfce4-terminal -e %s", "the command goes as one string in -e"},
	{"alacritty", "alacritty -e", "the command goes as arguments after -e"},
	{"kitty", "kitty", "the command goes as arguments"},
	{"wezterm", "wezterm start --", "the command goes after start --"},
	{"xterm", "xterm -e", "the command goes as arguments after -e"},
}

// Terminals are the known terminals on PATH, each as the template the
// launcher takes (AACP_TERMINAL).
func Terminals(m Machine) []Choice {
	var out []Choice
	for _, t := range knownTerminals {
		path, err := m.LookPath(t.bin)
		if err != nil {
			continue
		}
		out = append(out, Choice{Value: t.spec, Source: path + " · " + t.how})
	}
	return out
}

// Displays are the X displays of this machine. The user manager's DISPLAY
// comes first: that is the desktop the units of the panel open windows on.
// The sockets follow, because the installer is often run over ssh, where
// there is no DISPLAY at all while a desktop is running on the machine.
func Displays(m Machine) []Choice {
	var out []Choice
	named := ""
	if env, err := m.Run("systemctl", "--user", "show-environment"); err == nil {
		for _, line := range strings.Split(env, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "DISPLAY="); ok && v != "" {
				named = v
				out = append(out, Choice{Value: v, Source: "from the user manager"})
				break
			}
		}
	}
	entries, _ := m.List("/tmp/.X11-unix")
	var nums []int
	for _, e := range entries {
		digits, ok := strings.CutPrefix(e.Name, "X")
		if !ok || digits == "" || strings.Trim(digits, "0123456789") != "" {
			continue
		}
		if n, err := strconv.Atoi(digits); err == nil {
			nums = append(nums, n)
		}
	}
	slices.Sort(nums)
	for _, n := range slices.Compact(nums) {
		d := ":" + strconv.Itoa(n)
		if named != "" && screenless(named) == d {
			continue
		}
		out = append(out, Choice{Value: d, Source: "socket in /tmp/.X11-unix"})
	}
	return out
}

// screenless drops the screen number: ":10.0" and ":10" are one display.
func screenless(display string) string {
	if i := strings.LastIndex(display, "."); i > strings.LastIndex(display, ":") {
		return display[:i]
	}
	return display
}

// cUTF8 is there on every glibc, whatever locale -a says: English messages
// with UTF-8 text.
const cUTF8 = "C.UTF-8"

// Locales are the UTF-8 locales of this machine. Only UTF-8: a claude
// session draws a TUI and takes any script, and a one-byte locale turns
// the text into the question marks the setting exists to prevent. Names
// are written the way LANG takes them: locale -a prints ru_RU.utf8, which
// is ru_RU.UTF-8 spelled another way.
func Locales(m Machine) []Choice {
	out, _ := m.Run("locale", "-a")
	var rest []string
	for _, line := range strings.Split(out, "\n") {
		if name, ok := canonicalLocale(strings.TrimSpace(line)); ok && !slices.Contains(rest, name) {
			rest = append(rest, name)
		}
	}
	slices.Sort(rest)

	var list []Choice
	if lang, ok := canonicalLocale(m.Env("LANG")); ok && slices.Contains(rest, lang) {
		list = append(list, Choice{Value: lang, Source: "$LANG, and in locale -a"})
	}
	if len(list) == 0 || list[0].Value != cUTF8 {
		list = append(list, Choice{Value: cUTF8, Source: "always there: English messages, UTF-8 text"})
	}
	for _, name := range rest {
		if name != cUTF8 && name != list[0].Value {
			list = append(list, Choice{Value: name, Source: "in locale -a"})
		}
	}
	return list
}

// canonicalLocale writes a UTF-8 locale as LANG takes it, and refuses the
// rest: C and POSIX without a code set, and every one-byte code set.
func canonicalLocale(name string) (string, bool) {
	lang, codeset, ok := strings.Cut(name, ".")
	if !ok || lang == "" {
		return "", false
	}
	codeset, modifier, hasModifier := strings.Cut(codeset, "@")
	if strings.ToLower(strings.ReplaceAll(codeset, "-", "")) != "utf8" {
		return "", false
	}
	out := lang + ".UTF-8"
	if hasModifier {
		out += "@" + modifier
	}
	return out, true
}

// Expand turns "~" and "~/x" into absolute paths. People type paths with a
// tilde, but what is written goes to systemd and to a container, which
// never expand one. A tilde anywhere else is part of a name.
func Expand(path, home string) string {
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return path
}

// ClaudeHome is a claude configuration directory: an account, with its own
// login, conversations and limits.
type ClaudeHome struct {
	Dir      string
	SignedIn bool // a login is stored there
}

// accountMarkers are what claude puts into its configuration directory
// itself; any one is enough. settings.json is a name many programs use, so
// only the name of the directory (.claude or .claude-*) keeps
// ~/.config/settings.json from making ~/.config an account.
var accountMarkers = []string{".credentials.json", "settings.json", "projects", "sessions", "statsig"}

// ClaudeHomes are the claude accounts in the home directory, ~/.claude first:
// the panel takes the hooks and limits of the first as the reference. Only
// the direct children are looked at; an empty directory made for a future
// account is not offered, since there is nothing in it to manage yet.
func ClaudeHomes(m Machine, home string) []ClaudeHome {
	if home == "" {
		return nil
	}
	entries, err := m.List(home)
	if err != nil {
		return nil
	}
	var out []ClaudeHome
	for _, e := range entries {
		// A link is taken here, unlike in the walk: it is one name looked
		// up, not a way into other trees, and ~/.claude is often a link to
		// a bigger disk.
		if !(e.Dir || e.Link) || (e.Name != ".claude" && !strings.HasPrefix(e.Name, ".claude-")) {
			continue
		}
		dir := filepath.Join(home, e.Name)
		if !slices.ContainsFunc(accountMarkers, func(mk string) bool { return exists(m, filepath.Join(dir, mk)) }) {
			continue
		}
		out = append(out, ClaudeHome{Dir: dir, SignedIn: exists(m, filepath.Join(dir, ".credentials.json"))})
	}
	slices.SortFunc(out, func(a, b ClaudeHome) int {
		if pa, pb := filepath.Base(a.Dir) == ".claude", filepath.Base(b.Dir) == ".claude"; pa != pb {
			if pa {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Dir, b.Dir)
	})
	return out
}

func exists(m Machine, path string) bool {
	_, err := m.Stat(path)
	return err == nil
}

// KnownSlugs are the directories claude opened, by slug, over every account:
// a work directory may have been opened only under a work account. The time
// is the newest change of its transcript directory among the accounts, which
// moves when a conversation starts there.
func KnownSlugs(m Machine, accounts []string) map[string]time.Time {
	out := map[string]time.Time{}
	for _, account := range accounts {
		dir := filepath.Join(account, "projects")
		entries, err := m.List(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.Dir {
				continue
			}
			var mod time.Time
			if st, err := m.Stat(filepath.Join(dir, e.Name)); err == nil {
				mod = st.Mod
			}
			if seen, ok := out[e.Name]; !ok || mod.After(seen) {
				out[e.Name] = mod
			}
		}
	}
	return out
}

// Slug names the transcript directory claude keeps for a working directory:
// every rune that is not an ASCII letter or digit becomes a dash. A slug
// cannot be turned back into a path (a dash in a name and a separator look
// the same), so paths are compared by their slugs, never the other way.
func Slug(path string) string {
	var b strings.Builder
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ProjectDir is a directory that looks like a project.
type ProjectDir struct {
	Path   string
	Git    bool
	Opened time.Time // when claude last opened it; zero: never
}

// Root is a directory projects live under, with how many.
type Root struct {
	Path     string
	Projects int
	Opened   int // how many of them claude opened
}

// scanDepth is how many levels under a root the walk looks. The deepest
// projects people keep sit on the third; below that a directory without a
// single marker leads the walk into other people's trees.
const scanDepth = 3

// scanSkip are dependencies and build output: a .git inside them is a
// vendored repository, not a project of the person.
var scanSkip = []string{"node_modules", "vendor", "dist", "build", "target", "__pycache__"}

// Scan finds the projects under the roots, the ones claude opened most
// recently first, then the rest, each group by path. A project is a
// directory with .git (a directory, or a file in a worktree), a .claude
// directory or CLAUDE.md, or one claude opened even without any of them:
// someone works there. The walk does not go into a project (a nested
// repository is part of it), skips hidden directories and scanSkip, and
// never follows a link: a link leads anywhere, /etc included. A root that
// is the home directory is looked at one level deep: walking all of it is
// walking everything the person ever downloaded.
//
// The answer is never nil: "nothing found" is an answer, not "not looked".
func Scan(m Machine, roots []string, home string, known map[string]time.Time) []ProjectDir {
	w := walker{m: m, home: filepath.Clean(home), known: known,
		walked: map[string]int{}, found: map[string]bool{}, out: []ProjectDir{}}
	for _, root := range roots {
		// A relative path would be walked from wherever the installer runs.
		if root = Expand(root, home); filepath.IsAbs(root) {
			w.walk(filepath.Clean(root), scanDepth, true)
		}
	}
	slices.SortFunc(w.out, func(a, b ProjectDir) int {
		if c := b.Opened.Compare(a.Opened); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return w.out
}

type walker struct {
	m      Machine
	home   string
	known  map[string]time.Time
	walked map[string]int // how many levels under a directory were already looked at
	found  map[string]bool
	out    []ProjectDir
}

// walk looks at dir and left levels under it; a root is not a project of
// its own. The home directory keeps one level wherever the walk meets it. A
// root inside another root is looked at again only where it goes deeper
// than before, and a project found twice is listed once.
func (w *walker) walk(dir string, left int, root bool) {
	entries, err := w.m.List(dir)
	if err != nil {
		return
	}
	if !root && w.project(dir, entries) {
		return
	}
	if dir == w.home {
		left = min(left, 1)
	}
	if left <= 0 || w.walked[dir] >= left {
		return
	}
	w.walked[dir] = left
	for _, e := range entries {
		if walkable(e) {
			w.walk(filepath.Join(dir, e.Name), left-1, false)
		}
	}
}

// walkable tells whether the walk goes into an entry.
func walkable(e DirEntry) bool {
	return e.Dir && !e.Link && !strings.HasPrefix(e.Name, ".") && !slices.Contains(scanSkip, e.Name)
}

// project tells whether dir is a project, and lists it the first time.
func (w *walker) project(dir string, entries []DirEntry) bool {
	marked, git := projectMarks(dir, entries, w.known)
	if marked && !w.found[dir] {
		w.found[dir] = true
		w.out = append(w.out, ProjectDir{Path: dir, Git: git, Opened: w.known[Slug(dir)]})
	}
	return marked
}

// projectMarks tells, by what dir holds, whether it is a project and
// whether it is a git repository.
func projectMarks(dir string, entries []DirEntry, known map[string]time.Time) (marked, git bool) {
	_, marked = known[Slug(dir)]
	for _, e := range entries {
		switch {
		case e.Name == ".git":
			git, marked = true, true
		case e.Name == ".claude" && e.Dir, e.Name == "CLAUDE.md":
			marked = true
		}
	}
	return marked, git
}

// FindRoots offers the directories projects live under: the children of the
// home directory, of /srv and of /opt that hold at least one project, the
// fullest first. The home directory itself is offered by the question on its
// own and is never in the list, and so a child of it that is itself a
// project is not a root: it belongs to the home directory.
func FindRoots(m Machine, home string, known map[string]time.Time) []Root {
	parents := []string{"/srv", "/opt"}
	if filepath.IsAbs(home) {
		home = filepath.Clean(home)
		parents = append([]string{home}, parents...)
	}
	var out []Root
	for i, parent := range parents {
		entries, err := m.List(parent)
		if err != nil || slices.Contains(parents[:i], parent) {
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(parent, e.Name)
			if !walkable(e) || dir == home {
				continue
			}
			if parent == home {
				inside, err := m.List(dir)
				if err != nil {
					continue
				}
				if marked, _ := projectMarks(dir, inside, known); marked {
					continue
				}
			}
			projects := Scan(m, []string{dir}, home, known)
			if len(projects) == 0 {
				continue
			}
			r := Root{Path: dir, Projects: len(projects)}
			for _, p := range projects {
				if !p.Opened.IsZero() {
					r.Opened++
				}
			}
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Root) int {
		if a.Projects != b.Projects {
			return b.Projects - a.Projects
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

// lanSkip are interfaces that are not the home network: loopback, docker's
// bridges and the tunnels.
var lanSkip = []string{"docker", "br-", "veth", "tailscale", "wg"}

// Addresses are the IPv4 addresses of this machine, as two answers. lan is
// where a phone on the home network reaches the panel. proxies is where the
// reverse proxy in front of the panel may reach it from: this machine
// itself, a container on docker0, another machine behind WireGuard, or the
// home network.
func Addresses(m Machine) (lan, proxies []Choice) {
	out, _ := m.Run("ip", "-4", "-o", "addr")
	proxies = []Choice{{Value: "127.0.0.1", Source: "the proxy runs on this machine"}}
	var docker, wg []Choice
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[2] != "inet" {
			continue
		}
		iface, _, _ := strings.Cut(f[1], "@")
		addr, _, _ := strings.Cut(f[3], "/")
		switch {
		case iface == "lo":
		case iface == "docker0":
			docker = append(docker, Choice{Value: addr, Source: "docker0: the proxy runs in a container"})
		case strings.HasPrefix(iface, "wg"):
			wg = append(wg, Choice{Value: addr, Source: "on " + iface + ": the proxy is on another machine behind WireGuard"})
		case slices.ContainsFunc(lanSkip, func(p string) bool { return strings.HasPrefix(iface, p) }):
		default:
			lan = append(lan, Choice{Value: addr, Source: "on " + iface})
		}
	}
	for _, c := range slices.Concat(docker, wg, lan) {
		if !slices.ContainsFunc(proxies, func(p Choice) bool { return p.Value == c.Value }) {
			proxies = append(proxies, c)
		}
	}
	return lan, proxies
}

// Cert is a certificate and its key lying side by side, and the name it is
// for.
type Cert struct{ Cert, Key, Name string }

// Certs are the pairs X.crt and X.key in the directories, the way lego and
// most hand-made setups name them. The name is what the certificate is
// issued for: its first DNS name, else its common name, else X. A directory
// that cannot be read is passed over.
func Certs(m Machine, dirs []string) []Cert {
	var out []Cert
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		entries, err := m.List(dir)
		if err != nil {
			continue
		}
		names := map[string]bool{}
		for _, e := range entries {
			if !e.Dir {
				names[e.Name] = true
			}
		}
		for _, e := range entries {
			base, ok := strings.CutSuffix(e.Name, ".crt")
			if e.Dir || !ok || base == "" || !names[base+".key"] {
				continue
			}
			c := Cert{Cert: filepath.Join(dir, e.Name), Key: filepath.Join(dir, base+".key"), Name: base}
			if slices.ContainsFunc(out, func(o Cert) bool { return o.Cert == c.Cert }) {
				continue
			}
			if raw, err := m.ReadFile(c.Cert); err == nil {
				if name := certName(raw); name != "" {
					c.Name = name
				}
			}
			out = append(out, c)
		}
	}
	return out
}

// certName is the first name a PEM certificate is issued for.
func certName(raw []byte) string {
	for {
		block, rest := pem.Decode(raw)
		if block == nil {
			return ""
		}
		raw = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return ""
		}
		if len(cert.DNSNames) > 0 {
			return cert.DNSNames[0]
		}
		return cert.Subject.CommonName
	}
}

// ephemeralFirst is where the ports the kernel hands out for a while begin:
// a listener there is a program's passing socket, not a service.
const ephemeralFirst = 32768

// ListeningPorts are the services listening on this machine, offered as
// port checks in the AACP_PROBE_PORTS format name=host:port. The panel's own
// ports and the local resolver are left out. A port on every address or on
// loopback is checked on 127.0.0.1; one on a single IPv4 address, there; a
// port on a single IPv6 address is left out, since the checks are IPv4.
// The name is the process when ss can see it, which it can only for the
// user's own.
func ListeningPorts(m Machine, own []int) []Choice {
	out, _ := m.Run("ss", "-Hltnp")
	type listener struct{ name, host string }
	byPort := map[int]*listener{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		i := strings.LastIndex(f[3], ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(f[3][i+1:])
		if err != nil || port <= 0 || port >= ephemeralFirst || slices.Contains(own, port) {
			continue
		}
		host, ok := listenHost(f[3][:i])
		if !ok || host == "127.0.0.53" || host == "127.0.0.54" {
			continue
		}
		l := byPort[port]
		if l == nil {
			l = &listener{host: host}
			byPort[port] = l
		}
		if host == "127.0.0.1" {
			l.host = host
		}
		if u := ssUser.FindStringSubmatch(line); u != nil && l.name == "" {
			l.name = probeName(u[1])
		}
	}
	ports := make([]int, 0, len(byPort))
	for p := range byPort {
		ports = append(ports, p)
	}
	slices.Sort(ports)
	var list []Choice
	for _, port := range ports {
		l := byPort[port]
		target := l.host + ":" + strconv.Itoa(port)
		name, who := l.name, l.name
		if name == "" {
			name, who = "port"+strconv.Itoa(port), "something"
		}
		list = append(list, Choice{Value: name + "=" + target, Source: who + " listens on " + target})
	}
	return list
}

// listenHost is where a port check reaches a listener on the local address
// ss prints, and false when the check cannot: another IPv6 address.
func listenHost(addr string) (string, bool) {
	addr, _, _ = strings.Cut(addr, "%") // 127.0.0.1%lo: bound to an interface
	addr = strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]")
	switch addr {
	case "0.0.0.0", "*", "::", "127.0.0.1", "::1":
		return "127.0.0.1", true
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil || !ip.Is4() {
		return "", false
	}
	return ip.String(), true
}

// probeName keeps a process name inside its item of AACP_PROBE_PORTS: the
// list is split on commas and each item on its first "=".
func probeName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == ',' || r == '=' || r == ' ' || r == '\t' {
			return '-'
		}
		return r
	}, name)
}
