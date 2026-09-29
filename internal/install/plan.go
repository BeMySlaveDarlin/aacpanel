package install

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SecretShown is what stands for a secret anywhere an answer is shown.
const SecretShown = "set · not shown"

// PlanRow is a line of the plan: a heading, or a thing the install does to
// the machine with a tag that says whether it is new or there already.
type PlanRow struct {
	Text string
	Tag  string
	Head bool
}

// The tags of the plan.
const (
	TagNew   = "new"
	TagThere = "there"
)

// Plan is what the answers make of the machine, grouped by whose hands do
// it — root in one sudo, the person, claude's settings — and then every
// value with where it came from. It reads and changes nothing.
func (s *Survey) Plan() []PlanRow {
	f := s.Facts
	u := f.Account.Name
	state := s.valueOr("state", DefaultStateDir)
	tag := func(path string) string {
		if _, err := s.m.Stat(path); err == nil {
			return TagThere
		}
		return TagNew
	}
	row := func(text, t string) PlanRow { return PlanRow{Text: "  · " + text, Tag: t} }

	rows := []PlanRow{{Text: "As root — one sudo", Head: true}}
	if pkgs := s.packages(); len(pkgs) > 0 {
		rows = append(rows, row("apt install "+strings.Join(pkgs, " "), TagNew))
	}
	if s.valueOr("pkg:docker", "") == yes {
		rows = append(rows, row("the docker group for "+u, TagNew))
	}
	rows = append(rows,
		row(state+", owner "+u, tag(state)),
		row(filepath.Join(state, "host.env"), tag(filepath.Join(state, "host.env"))),
		row("aacpanel-agent@.service, enabled for "+u, tag("/etc/systemd/system/aacpanel-agent@.service")),
		row("linger for "+u, tag("/var/lib/systemd/linger/"+u)),
		PlanRow{Text: "As you", Head: true},
	)
	if s.valueOr("pkg:claude", "") == yes {
		rows = append(rows, row("claude, with Anthropic's native installer", TagNew))
	}
	home := f.Account.Home
	rows = append(rows,
		row("~/bin/aacpanel-exec, built with the installer's Go", tag(filepath.Join(home, "bin", "aacpanel-exec"))),
		row("aacpanel-exec.service, user unit", tag(filepath.Join(home, ".config", "systemd", "user", "aacpanel-exec.service"))),
		row(f.Short(filepath.Join(f.Clone, ".env")), tag(filepath.Join(f.Clone, ".env"))),
		row("compose: "+s.services(), s.composeTag()),
	)
	if s.Has("testdb") {
		rows = append(rows, row("a test database for make check", TagNew))
	}
	gc := filepath.Join(home, ".config", "systemd", "user", "aacpanel-docker-gc.timer")
	if s.Has("gc") {
		rows = append(rows, row("aacpanel-docker-gc.timer, weekly", tag(gc)))
	} else if tag(gc) == TagThere {
		rows = append(rows, row("aacpanel-docker-gc.timer taken away: the kit leaves it out", ""))
	}
	rows = append(rows, s.mapRows()...)

	var settings []string
	for _, dir := range split(s.valueOr("accounts", "")) {
		settings = append(settings, f.Short(filepath.Join(dir, "settings.json")))
	}
	rows = append(rows,
		PlanRow{Text: "In claude settings — " + strings.Join(settings, ", "), Head: true},
		PlanRow{Text: "  · question hook · status line before yours · MCP server"},
		PlanRow{Text: fmt.Sprintf("  · %s · %s", count(s.allows(), "allow rule"), count(s.hooks(), "hook"))},
	)
	for _, dir := range split(s.valueOr("accounts", "")) {
		if s.valueOr("login:"+dir, "") == "now" {
			rows = append(rows, PlanRow{Text: "  · sign in to " + f.Short(dir) + ": claude opens here"})
		}
	}

	rows = append(rows, PlanRow{Text: "Values", Head: true})
	for _, line := range s.values() {
		rows = append(rows, PlanRow{Text: "  " + line})
	}
	if mind := s.Mind(); len(mind) > 0 {
		rows = append(rows, PlanRow{Text: "Mind", Head: true})
		for _, line := range mind {
			rows = append(rows, PlanRow{Text: "  · " + line})
		}
	}
	return rows
}

// packages are what apt installs in the root step.
func (s *Survey) packages() []string {
	var out []string
	for _, m := range s.missing {
		if s.valueOr("pkg:"+m.Name, "") != yes {
			continue
		}
		// claude installs as the person; what its installer lacks comes
		// with apt all the same.
		if m.Name != "claude" {
			out = append(out, strings.Fields(strings.TrimPrefix(m.Command, "sudo apt install "))...)
		}
		out = append(out, m.Needs...)
	}
	return out
}

func (s *Survey) services() string {
	out := []string{"aacpanel", "aacpanel-db", "socket-proxy"}
	if s.Has("tailscale") {
		out = append(out, "tailscale")
	}
	return strings.Join(out, ", ")
}

func (s *Survey) composeTag() string {
	for _, t := range s.Facts.Traces {
		if strings.HasPrefix(t, "compose project ") {
			return TagThere
		}
	}
	return TagNew
}

// mapRows are the map as the plan reads it: the contours of the accounts,
// then the group with its projects, each tagged by what the panel's map
// holds already — a machine with its contours in place gets none of them
// made again.
func (s *Survey) mapRows() []PlanRow {
	if s.keep == "yes" || s.keep == "access" {
		return []PlanRow{{Text: "  · map: left as it is"}}
	}
	var names []string
	for _, dir := range split(s.valueOr("accounts", "")) {
		names = append(names, s.valueOr("contour:"+dir, "")+" ("+s.Facts.Short(dir)+")")
	}
	line := "map: contour " + strings.Join(names, ", ")
	if len(names) > 1 {
		line = "map: contours " + strings.Join(names, ", ")
	}
	contours, group := TagNew, TagNew
	if v, ok := s.mapNow(); ok {
		c, g := s.mapMissing(v)
		if len(c) == 0 {
			contours = TagThere
		}
		if len(g) == 0 {
			group = TagThere
		}
	}
	g := s.valueOr("group", "")
	if g == "" {
		return []PlanRow{{Text: "  · " + line + ", no group", Tag: contours}}
	}
	return []PlanRow{
		{Text: "  · " + line, Tag: contours},
		{Text: fmt.Sprintf("  · map: group %s, %s", g, count(len(split(s.valueOr("projects", ""))), "project")), Tag: group},
	}
}

// mapNow is the map of the panel on this machine, when its local listener
// answers: a listener turned off in .env is not asked.
func (s *Survey) mapNow() (mapView, bool) {
	if v, ok := s.before(DotEnvFile, "AACP_LOCAL_ADDR"); ok && strings.TrimSpace(v) == "" {
		return mapView{}, false
	}
	return mapNow(s.m, fmt.Sprintf("http://127.0.0.1:%d", PortLocal))
}

// hooks is how many hooks the kit wires into claude: the question relay and
// every hook part that is checked.
func (s *Survey) hooks() int {
	n := 1
	for _, p := range hookParts {
		if s.Has(p) {
			n++
		}
	}
	return n
}

// allows is how many allow rules go into permissions: the panel's four
// tools, and the restart with Self-restart.
func (s *Survey) allows() int {
	if s.Has("restart") {
		return 5
	}
	return 4
}

// values are the answers as the plan reads them back, each with where it
// came from, and after them what the install derives without asking.
func (s *Survey) values() []string {
	f := s.Facts
	var out []string
	add := func(parts ...string) {
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		if len(kept) > 0 {
			out = append(out, strings.Join(kept, " · "))
		}
	}
	add(s.shown("host", "host "), s.shown("home", "home session "))
	add(s.shown("state", "state "), s.shown("roots", "projects under "))
	if g, ok := s.Value("terminal"); ok && g.Value != "" {
		line := "windows " + g.Value + s.src(g) + " on " + s.shown("display", "")
		if s.valueOr("auto", yes) == yes {
			line += ", opened on start"
		}
		add(line, s.shown("locale", "locale "))
	} else {
		why := "you"
		if g, ok := s.Value("terminal"); ok {
			why = g.Source
		} else if !s.Graphics() {
			why = "no display found"
		}
		add("no windows: sessions live in tmux only ("+why+")", s.shown("locale", "locale "))
	}
	sessions := ""
	if g, ok := s.Value("transport"); ok {
		sessions = map[string]string{"stream": "sessions on the stream", "tmux": "sessions in tmux"}[g.Value] + s.src(g)
	}
	add(s.shown("claude", "claude "), sessions)
	add(s.shown("accounts", "accounts "))
	host, why := s.PasskeyHome()
	add("passkeys " + host + " (" + why + ")")
	if s.Has("tailscale") {
		key := ""
		if _, ok := s.Value("tskey"); ok {
			key = "auth key " + SecretShown
		}
		add(s.shown("tsname", "tailscale node "), key)
	}
	if s.Has("lan") {
		cert := s.valueOr("lancert", "")
		if cert == "plain" {
			add(s.shown("lanaddr", "home network "), "plain http, cookie without Secure")
		} else {
			add(s.shown("lanaddr", "home network "), fmt.Sprintf("port %d", f.LANPort), s.shown("lancert", "certificate "))
		}
	}
	if s.Has("domain") {
		add(s.shown("domain", "domain "), s.shown("bind", "the proxy reaches "))
	}
	if len(s.legs()) > 0 {
		add(s.shown("token", "phone sign-in "), s.shown("term", "terminal from other devices "))
	}
	if probes := s.valueOr("probes", ""); probes != "" {
		add("probes " + strings.ReplaceAll(probes, ",", ", "))
	}
	add(s.shown("push", "push contact "), s.shown("life", "sign-in "))
	add("repo "+f.Short(f.Clone)+" (the clone)", fmt.Sprintf("uid %d gid %d (id)", f.Account.UID, f.Account.GID))
	add("secrets: made once on this machine, never shown")
	return out
}

func (s *Survey) legs() []string {
	var out []string
	for _, l := range Legs {
		if s.Has(l) {
			out = append(out, l)
		}
	}
	return out
}

// shown is an answer the way the plan reads it, with its source: empty when
// the question was not answered.
func (s *Survey) shown(id, lead string) string {
	g, ok := s.Value(id)
	if !ok {
		return ""
	}
	return lead + s.show(id, g) + s.src(g)
}

func (s *Survey) src(g Given) string {
	if g.Source == "" {
		return ""
	}
	return " (" + g.Source + ")"
}

// show is an answer in words: the label of the option it is, the value
// with ~ for a path under home, a secret never.
func (s *Survey) show(id string, g Given) string {
	if g.Secret {
		if g.Value == "" {
			return "not set"
		}
		return SecretShown
	}
	var q *Question
	for _, c := range s.Questions(blockOf(id)) {
		if c.ID == id {
			q = &c
			break
		}
	}
	name := func(v string) string {
		if q != nil {
			for _, o := range q.Options {
				if o.Value == v && o.Label != "" {
					return o.Label
				}
			}
		}
		if strings.HasPrefix(v, "/") {
			return s.Facts.Short(v)
		}
		return v
	}
	if q != nil && q.Form == Many {
		vals := split(g.Value)
		if len(vals) == 0 {
			return "none"
		}
		for i, v := range vals {
			vals[i] = name(v)
		}
		return strings.Join(vals, ", ")
	}
	if n := name(g.Value); n != "" {
		return n
	}
	return "none"
}

// Lines are the answers of block b as the feed keeps them: a line a
// question, with where the answer came from.
func (s *Survey) Lines(b BlockID) []string {
	var out []string
	for _, q := range s.Questions(b) {
		g, ok := s.Value(q.ID)
		if !ok || q.Moot {
			continue
		}
		out = append(out, "· "+q.Prompt+" → "+s.show(q.ID, g)+s.src(g))
	}
	return out
}
