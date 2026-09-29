package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/contours"
	"aacpanel/internal/hostcfg"
)

// The blocks of questions, in the order a run asks them. A block that does
// not apply to the machine or to the answers so far has no questions and is
// passed by.
const (
	BlockP BlockID = "P" // prerequisites the machine lacks
	BlockA BlockID = "A" // this machine
	BlockB BlockID = "B" // session windows and the locale
	BlockC BlockID = "C" // claude
	BlockL BlockID = "L" // signing in to the accounts that are not
	BlockK BlockID = "K" // the kit
	BlockD BlockID = "D" // the ways in the kit checked
	BlockT BlockID = "T" // whether to tune the rare settings
	BlockF BlockID = "F" // the rare settings
	BlockM BlockID = "M" // the map
)

// Blocks is the order of the run.
var Blocks = []BlockID{BlockP, BlockA, BlockB, BlockC, BlockL, BlockK, BlockD, BlockT, BlockF, BlockM}

// Title heads the answers of the block in the feed.
func (b BlockID) Title() string {
	switch b {
	case BlockP:
		return "Prerequisites"
	case BlockA:
		return "This machine"
	case BlockB:
		return "Session windows"
	case BlockC:
		return "Claude"
	case BlockL:
		return "Sign in"
	case BlockK:
		return "Kit"
	case BlockD:
		return "Access"
	case BlockT, BlockF:
		return "More settings"
	case BlockM:
		return "Map"
	}
	return string(b)
}

// Part is a part of the kit: what goes into the install besides the panel.
type Part struct {
	ID, Label, Detail, Group string
	// Trace is how an install by hand is told to have the part: the script
	// its hook runs, or the allow rule it adds, as claude settings carry it.
	Trace string
}

// The groups of the kit. The first is the panel itself and does not come
// off; the second is checked for the person and stays quiet; the rest waits
// to be asked for.
const (
	GroupAlways = "Always"
	GroupQuiet  = "Quiet — checked for you"
	GroupAsk    = "On request"
	GroupLegs   = "Ways in besides this machine"
	GroupDev    = "For development"
)

// Kit is every part, in the order of the screen.
var Kit = []Part{
	{"relay", "Question relay", "a session's questions reach your phone", GroupAlways, "agent/ask-hook.py"},
	{"limits", "Limits snapshot", "5-hour and weekly limits, before your status line", GroupAlways, "agent/rate-snapshot.sh"},
	{"tools", "Panel tools", "checklist, briefs, a call to your phone, restart", GroupAlways, ""},
	{"copies", "Page copies", "a published page opens on the phone under any account", GroupQuiet, "deploy/claude/artifact-copy.py"},
	{"brief", "Brief reminder", "a session learns that an answered brief waits", GroupQuiet, "deploy/claude/brief-waiting.py"},
	{"cap", "Context cap", "stops a session at the cap, restarts it with your line", GroupQuiet, "deploy/claude/context-guard.py"},
	{"nudge", "Checklist nudge", "reminds a session that forgot its checklist", GroupQuiet, "deploy/claude/checklist-reminder.py"},
	{"restart", "Self-restart", "a session may restart itself without asking you", GroupQuiet, "mcp__aacpanel__session_restart"},
	{"stamp", "Prompt stamp", "time, context, limits and load before every prompt", GroupAsk, "deploy/claude/prompt-stamp.py"},
	{"cost", "Cost snapshot", "spend per session, written after every turn", GroupAsk, "deploy/claude/cost-snapshot.py"},
	{"gc", "Docker cleanup", "weekly prune of unused images — of the whole machine", GroupAsk, ""},
	{"tailscale", "Tailscale", "your phone from anywhere, through your tailnet", GroupLegs, ""},
	{"lan", "Home network, TLS", "straight at home, with a certificate you issue", GroupLegs, ""},
	{"domain", "Own domain", "behind your reverse proxy", GroupLegs, ""},
	{"testdb", "Test database", "for make check: forty tests skip without it", GroupDev, ""},
}

// Legs are the parts of the kit that are ways in, in the order block D asks
// them. This machine is always a way in and is not a part.
var Legs = []string{"tailscale", "lan", "domain"}

// hookParts are the parts that wire a hook into claude, besides the relay.
var hookParts = []string{"copies", "brief", "cap", "nudge", "stamp", "cost"}

const (
	typeOwn  = "Type something."
	typePath = "Type a path."
	yes      = "yes"
	no       = "no"
)

// The defaults of a sign-in, as docker-compose.yml passes them to the panel.
const (
	defaultIdle = "24h"
	defaultMax  = "720h"
)

var (
	hostName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,62}$`)
	sessionName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	cleanPath   = regexp.MustCompile(`^(~|/)[A-Za-z0-9._/-]*$`)
	duration    = regexp.MustCompile(`^[0-9]+(m|h)$`)
	probe       = regexp.MustCompile(`^[A-Za-z0-9_-]+=[A-Za-z0-9.:-]+:[0-9]+$`)
)

func checkHost(s string) error {
	if !hostName.MatchString(s) {
		return errors.New("a host name takes letters, digits, dots and dashes")
	}
	return nil
}

func checkName(s string) error {
	if !sessionName.MatchString(s) {
		return errors.New("a name takes lower-case letters, digits, dashes and underscores")
	}
	return nil
}

// checkPaths takes a path or a list of them joined by commas: a path goes
// into unit files and hook commands, which cannot carry spaces or quotes.
func checkPaths(s string) error {
	for _, p := range strings.Split(s, ",") {
		if !cleanPath.MatchString(p) {
			return errors.New("a path starts with / or ~ and carries no spaces or quotes: unit files cannot hold them")
		}
	}
	return nil
}

func checkLife(s string) error {
	idle, max, ok := strings.Cut(s, "/")
	if !ok || !duration.MatchString(idle) || !duration.MatchString(max) {
		return errors.New("idle/max, each a number of minutes or hours: 30m/12h")
	}
	return nil
}

func checkProbes(s string) error {
	for _, p := range strings.Split(s, ",") {
		if !probe.MatchString(p) {
			return errors.New("a probe is name=host:port, like pg=127.0.0.1:5432")
		}
	}
	return nil
}

// questions are the questions of block b that apply to the machine and to
// the answers given so far, with their options and suggested answers.
func (s *Survey) questions(b BlockID) []Question {
	var qs []Question
	switch b {
	case "":
		if s.Earlier() {
			qs = []Question{s.KeepQuestion()}
		}
	case BlockP:
		qs = s.prerequisites()
	case BlockA:
		qs = s.thisMachine()
	case BlockB:
		qs = s.windows()
	case BlockC:
		qs = s.claude()
	case BlockL:
		qs = s.signIn()
	case BlockK:
		qs = s.kit()
	case BlockD:
		qs = s.access()
	case BlockT:
		qs = s.tune()
	case BlockF:
		qs = s.more()
	case BlockM:
		qs = s.mapQuestions()
	}
	for i := range qs {
		qs[i].Block = b
	}
	return qs
}

// prerequisites turns each program the check found missing into a question:
// the root step installs it, or the run stops with the command that does.
func (s *Survey) prerequisites() []Question {
	why := map[string]string{
		"docker":         "The panel, its database and the socket proxy run as containers. The root step installs docker with apt and adds you to its group; the rest of the run reaches docker through that group without logging out.",
		"docker compose": "The panel is a stack of compose services, and this docker has no compose v2. The root step installs it with apt.",
		"claude":         "The panel starts and watches claude sessions. Anthropic's native installer puts claude into ~/.local/bin as you, without root; signing in is asked with the accounts.",
		"tmux":           "Sessions without the stream live in tmux, and a session on the stream moves into it when the panel is down (tmux attach). Windows to a session and the panel's live terminal go through it too. Without tmux the executor turns off every action on sessions: their buttons go grey. Choosing the stream still keeps tmux as the way back.",
		"jq":             "The status line script reads the limits with it: without jq the 5-hour and weekly percentages are silently not written.",
	}
	prompt := map[string]string{
		"docker":         "docker is not installed. Install it with apt as part of the root step?",
		"docker compose": "docker compose v2 is missing. Install it with apt as part of the root step?",
		"claude":         "claude is not installed. Install it with Anthropic's native installer?",
		"tmux":           "tmux is missing. Install it?",
		"jq":             "jq is missing. Install it?",
	}
	var qs []Question
	for _, m := range s.missing {
		yesLabel := "Yes — with apt, in the root step"
		note := why[m.Name]
		if m.Name == "claude" {
			yesLabel = "Yes — as you, no root needed"
			if len(m.Needs) > 0 {
				yesLabel = "Yes — as you; " + strings.Join(m.Needs, " ") + " with apt, in the root step"
				note += " Its installer downloads claude with curl or wget, and the machine has neither: the root step installs curl."
			}
		}
		qs = append(qs, Question{
			ID: "pkg:" + m.Name, Tab: m.Name, Prompt: prompt[m.Name], Note: note, Form: One,
			Options: []Option{
				{Value: yes, Label: yesLabel, Source: "recommended"},
				{Value: no, Label: "No — show me the command", Detail: "The run stops here with the command that installs it by hand."},
			},
			Flag: "--install-" + strings.ReplaceAll(strings.TrimPrefix(m.Name, "docker "), " ", "-"), Values: []string{yes, no},
			Default: yes, Writes: []Target{{File: "root step", Key: m.Name}},
		})
	}
	return qs
}

func (s *Survey) thisMachine() []Question {
	f := s.Facts
	host := Question{
		ID: "host", Tab: "Host", Prompt: "What does the panel call this machine?", Form: One,
		Own: typeOwn, Flag: "--host", Check: checkHost,
		Writes: []Target{{HostEnvFile, hostcfg.HostEnv}},
	}
	if s.Found.Host != "" {
		host.Options = []Option{{Value: s.Found.Host, Source: "hostname",
			Detail: "From hostname. Metrics history is keyed by this name: changing it later starts a new history."}}
	}
	s.prefer(&host, HostEnvFile, hostcfg.HostEnv, "The current name. Metrics history is keyed by it: a new name starts a new history.")
	settleDefault(&host)

	home := Question{
		ID: "home", Tab: "Session", Prompt: "Name of the home session (the machine's own claude session)?", Form: One,
		Own: typeOwn, Flag: "--home-session", Check: checkName,
		Writes: []Target{{HostEnvFile, hostcfg.HomeSessionEnv}},
	}
	if name := strings.ToLower(s.valueOr("host", host.Default)); name != "" {
		home.Options = []Option{{Value: name, Source: "host name", Detail: "The host name in lower case."}}
	}
	s.prefer(&home, HostEnvFile, hostcfg.HomeSessionEnv, "The current name.")

	roots := Question{
		ID: "roots", Tab: "Projects", Prompt: "Where do your projects live?", Form: Many,
		Note: "The panel opens sessions under these and looks for projects three levels down; the home directory is never walked whole.",
		Own:  typePath, Flag: "--projects-root", Check: checkPaths,
		Writes: []Target{{HostEnvFile, hostcfg.ProjectRootsEnv}, {HostEnvFile, "AACP_PROJECT_SCAN"}},
	}
	for i, r := range s.Found.Roots {
		detail := count(r.Projects, "project")
		if r.Opened > 0 {
			detail += fmt.Sprintf(" · claude opened %d of them", r.Opened)
		}
		roots.Options = append(roots.Options, Option{Value: r.Path, Label: f.Short(r.Path), Detail: detail, Source: "found", On: i == 0})
	}
	roots.Options = append(roots.Options, Option{Value: f.Account.Home, Label: "~",
		Detail: "only the home directory itself, not walked deeper", Source: "the home directory", On: len(s.Found.Roots) == 0})
	roots.source = "found"
	if len(s.Found.Roots) == 0 {
		roots.source = "no projects found elsewhere"
	}
	s.preferList(&roots, HostEnvFile, hostcfg.ProjectRootsEnv, ":")

	state := Question{
		ID: "state", Tab: "State", Prompt: "Where does the panel keep its state?", Form: One,
		Options: []Option{{Value: DefaultStateDir, Source: "recommended",
			Detail: "Recommended. The root step creates it for " + f.Account.Name + ", and the collector's unit points there as written."}},
		Own: typeOwn, Flag: "--state-dir", Check: checkPaths,
		Writes: []Target{{HostEnvFile, hostcfg.StateDirEnv}, {DotEnvFile, hostcfg.StateDirEnv}},
	}
	s.prefer(&state, DotEnvFile, hostcfg.StateDirEnv, "The current directory: the panel's state is there.")
	return []Question{host, home, roots, state}
}

// Graphics tells whether there is a display to open windows on. A terminal
// without a display opens nothing, so the display decides.
func (s *Survey) Graphics() bool { return len(s.Found.Displays) > 0 }

func (s *Survey) windows() []Question {
	locale := Question{
		ID: "locale", Tab: "Locale", Prompt: "Which locale do sessions run in?", Form: One,
		Note: "Sessions start in a clean environment: without a UTF-8 locale non-ASCII text turns into question marks.",
		Flag: "--locale", Own: typeOwn, Writes: []Target{{HostEnvFile, hostcfg.LangEnv}},
	}
	for _, l := range s.Found.Locales {
		locale.Options = append(locale.Options, Option{Value: l.Value, Detail: l.Source, Source: l.Source})
	}
	if v, ok := s.before(HostEnvFile, hostcfg.LangEnv); ok && v == "" {
		s.preferValue(&locale, hostcfg.DefaultLang, "The current setting: empty means "+hostcfg.DefaultLang+".")
	} else {
		s.prefer(&locale, HostEnvFile, hostcfg.LangEnv, "The current setting.")
	}
	if !s.Graphics() {
		return []Question{locale}
	}

	terminal := Question{
		ID: "terminal", Tab: "Terminal", Prompt: "How should the panel open a window to a session?", Form: One,
		Note: "A template of your own takes %s where tmux attach goes.",
		Own:  "Type a template.", Flag: "--terminal", Writes: []Target{{HostEnvFile, "AACP_TERMINAL"}},
	}
	for _, t := range s.Found.Terminals {
		terminal.Options = append(terminal.Options, Option{Value: t.Value, Detail: t.Source, Source: "found on PATH"})
	}
	terminal.Options = append(terminal.Options, Option{
		Value: "", Label: "No windows — sessions live in tmux only", Source: "no terminal found",
		Detail: "The panel still shows every session and its terminal; a window opens by hand, with tmux attach.",
	})
	s.prefer(&terminal, HostEnvFile, "AACP_TERMINAL", "The current template.")
	settleDefault(&terminal)
	// Without windows there is nothing to open on start and no display to
	// open it on; the locale is asked all the same.
	none := s.valueOr("terminal", terminal.Default) == ""

	auto := Question{
		ID: "auto", Tab: "On start", Prompt: "Open a window when a session starts?", Form: One,
		Options: []Option{
			{Value: yes, Label: "Yes", Source: "default", Detail: "A window opens on the display the moment a session starts."},
			{Value: no, Label: "No, only on request", Detail: "A window opens when you ask for one on the panel."},
		},
		Flag: "--window-on-start", Values: []string{yes, no}, Writes: []Target{{HostEnvFile, "AACP_TERMINAL_AUTO"}},
		Moot: none,
	}
	if v, ok := s.before(HostEnvFile, "AACP_TERMINAL_AUTO"); ok {
		was := yes
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "no", "off", "false":
			was = no
		}
		s.preferValue(&auto, was, "")
	}

	display := Question{
		ID: "display", Tab: "Display", Prompt: "Which display do the windows open on?", Form: One,
		Own: typeOwn, Flag: "--display", Writes: []Target{{HostEnvFile, hostcfg.DisplayEnv}},
		Moot: none,
	}
	for _, d := range s.Found.Displays {
		display.Options = append(display.Options, Option{Value: d.Value, Detail: d.Source, Source: d.Source})
	}
	s.prefer(&display, HostEnvFile, hostcfg.DisplayEnv, "The current display.")
	return []Question{terminal, auto, display, locale}
}

func (s *Survey) claude() []Question {
	f := s.Facts
	cmd := Question{
		ID: "claude", Tab: "claude", Prompt: "What starts claude?", Form: One,
		Own: typeOwn, Flag: "--claude", Check: checkPaths, Writes: []Target{{HostEnvFile, "AACP_CLAUDE"}},
	}
	switch {
	case f.ClaudeOffPath:
		cmd.Options = []Option{{Value: f.Claude, Label: f.Short(f.Claude), Source: "the native installer",
			Detail: "Where Anthropic's native installer puts it; not on PATH until your next login."}}
	case f.Claude != "":
		cmd.Options = []Option{{Value: f.Claude, Label: f.Short(f.Claude), Source: "command -v claude",
			Detail: "Found with command -v claude. The link is kept as it is, so an update of claude reaches the panel."}}
	case s.valueOr("pkg:claude", "") == yes:
		native := NativeClaude(f.Account.Home)
		cmd.Options = []Option{{Value: native, Label: f.Short(native), Source: "the native installer",
			Detail: "Where Anthropic's native installer puts it."}}
	}
	s.prefer(&cmd, HostEnvFile, "AACP_CLAUDE", "The current command: a wrapper of your own stays.")

	accounts := Question{
		ID: "accounts", Tab: "Accounts", Prompt: "Which claude accounts does the panel serve?", Form: Many,
		Note: "Each account gets the panel's hooks and tools and becomes a contour on the map.",
		Own:  typePath, Flag: "--account", Check: checkPaths,
		Writes: []Target{{HostEnvFile, contours.HomeEnv}, {File: "claude settings"}},
	}
	personal := filepath.Join(f.Account.Home, ".claude")
	for i, a := range s.Found.Accounts {
		accounts.Options = append(accounts.Options, Option{Value: a.Dir, Label: f.Short(a.Dir),
			Source: "found", On: a.Dir == personal || i == 0})
	}
	accounts.source = "found"
	if len(accounts.Options) == 0 {
		accounts.Options = []Option{{Value: personal, Label: f.Short(personal), On: true, Source: "claude's own",
			Detail: "not there yet: claude makes it on its first start"}}
		accounts.source = "claude's own"
	}
	if _, ok := s.before(HostEnvFile, contours.HomeEnv); ok {
		s.preferList(&accounts, HostEnvFile, contours.HomeEnv, ":")
	} else if dirs := s.registered(); len(dirs) > 0 {
		s.preferParts(&accounts, dirs)
	}
	for i, o := range accounts.Options {
		if o.Source != "claude's own" {
			_, accounts.Options[i].Detail = s.SignedIn(o.Value)
		}
	}

	transport := Question{
		ID: "transport", Tab: "Sessions", Prompt: "How are sessions kept?", Form: One,
		Options: []Option{
			{Value: "stream", Label: "On the stream", Source: "recommended",
				Detail: "The panel speaks claude's own protocol: the feed, questions and permissions as in the native client. tmux stays as the way back."},
			{Value: "tmux", Label: "In tmux",
				Detail: "claude runs in a terminal and the panel types the keys; tmux attach reaches a session whatever happens to the panel."},
		},
		Flag: "--transport", Values: []string{"stream", "tmux"}, Writes: []Target{{File: "map", Key: "launch.transport of the first contour"}},
	}
	transport.Default = transport.Options[0].Value
	return []Question{cmd, accounts, transport}
}

// registered are the accounts of the wrapper registry an earlier install
// names instead of a list of accounts.
func (s *Survey) registered() []string {
	var out []string
	for _, c := range s.registry() {
		if c.Config != "" && !slices.Contains(out, c.Config) {
			out = append(out, c.Config)
		}
	}
	return out
}

// registry is the wrapper registry an earlier install names in host.env.
func (s *Survey) registry() []contours.Contour {
	reg, ok := s.before(HostEnvFile, contours.RegistryEnv)
	if !ok || reg == "" {
		return nil
	}
	home := s.Facts.Account.Home
	raw, err := s.m.ReadFile(Expand(reg, home))
	if err != nil {
		return nil
	}
	return contours.ParseRegistry(raw, home)
}

// SignedIn tells whether claude can work under an account, and how that is
// known. claude keeps its own sign-in in .credentials.json; an account the
// wrapper of the registry signs in with a token of its file, or whose
// settings hand claude a key, has none there and is signed in all the same.
func (s *Survey) SignedIn(dir string) (bool, string) {
	dir = Expand(dir, s.Facts.Account.Home)
	if exists(s.m, filepath.Join(dir, ".credentials.json")) {
		return true, "signed in"
	}
	for _, c := range s.registry() {
		if filepath.Clean(c.Config) == filepath.Clean(dir) && c.Token != "" && exists(s.m, c.Token) {
			return true, "signed in by the wrapper, with " + s.Facts.Short(c.Token)
		}
	}
	if raw, err := s.m.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		var set struct {
			APIKeyHelper string            `json:"apiKeyHelper"`
			Env          map[string]string `json:"env"`
		}
		if json.Unmarshal(raw, &set) == nil &&
			(set.APIKeyHelper != "" || set.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "" || set.Env["ANTHROPIC_API_KEY"] != "") {
			return true, "signed in by its settings"
		}
	}
	return false, "not signed in"
}

// signIn asks, for every chosen account claude is not signed in to, whether
// to sign in now. The sign-in itself hands the terminal to claude.
func (s *Survey) signIn() []Question {
	var qs []Question
	for _, dir := range split(s.valueOr("accounts", "")) {
		if in, _ := s.SignedIn(dir); in {
			continue
		}
		short := s.Facts.Short(dir)
		q := Question{
			ID: "login:" + dir, Tab: short, Prompt: "Sign in to " + short + " now?", Form: One, For: dir,
			Options: []Option{
				{Value: "now", Label: "Yes — claude opens here, /exit brings you back", Source: "a person at the terminal"},
				{Value: "later", Label: "Later", Source: "no terminal to sign in at",
					Detail: "The account's sessions start once you sign in: CLAUDE_CONFIG_DIR=" + short + " claude"},
			},
			Flag: "--claude-login", Values: []string{"now", "later"}, Writes: []Target{{File: "claude settings", Key: dir}},
		}
		q.Default = "later"
		if s.m.Terminal() {
			q.Default = "now"
		}
		qs = append(qs, q)
	}
	return qs
}

func (s *Survey) kit() []Question {
	q := Question{
		ID: "kit", Tab: "Kit", Prompt: "What goes into this install?", Form: Many,
		Note: "Space checks a part, Enter takes the list as it stands.",
		Flag: "--kit", Writes: []Target{{File: "kit"}},
	}
	for _, p := range Kit {
		q.Options = append(q.Options, Option{Value: p.ID, Label: p.Label, Detail: p.Detail, Group: p.Group,
			Locked: p.Group == GroupAlways, On: p.Group == GroupAlways || p.Group == GroupQuiet, Source: "suggested"})
	}
	if s.was.kit != nil {
		s.preferParts(&q, s.was.kit)
	}
	q.Default = picked(q)
	return []Question{q}
}

// kitFlag reads --kit: a list names the whole kit, and +part,-part changes
// the suggested one. The parts that are always in stay in either way.
func kitFlag(raw, suggested string) (string, error) {
	on := map[string]bool{}
	relative := raw != "" && (raw[0] == '+' || raw[0] == '-')
	if relative {
		for _, p := range split(suggested) {
			on[p] = true
		}
	}
	for _, item := range split(raw) {
		name, drop := strings.TrimPrefix(item, "+"), strings.HasPrefix(item, "-")
		name = strings.TrimPrefix(name, "-")
		if !isPart(name) {
			var ids []string
			for _, p := range Kit {
				ids = append(ids, p.ID)
			}
			return "", fmt.Errorf("--kit knows %s, not %q", strings.Join(ids, ", "), name)
		}
		on[name] = !drop
	}
	var out []string
	for _, p := range Kit {
		if on[p.ID] || p.Group == GroupAlways {
			out = append(out, p.ID)
		}
	}
	return strings.Join(out, ","), nil
}

func isPart(id string) bool {
	for _, p := range Kit {
		if p.ID == id {
			return true
		}
	}
	return false
}

// Has tells whether the kit answered so far holds the part.
func (s *Survey) Has(part string) bool {
	for _, p := range split(s.valueOr("kit", "")) {
		if p == part {
			return true
		}
	}
	return false
}

// PasskeyHome is where passkeys live: the address of the main listener. The
// other ways in have listeners of their own that hand the sign-in over to
// it, so there is one passkey address, and the kit decides it: a domain
// always reaches the main listener through its proxy; without one, a tailnet
// node proxies to it; with neither, it is this machine.
func (s *Survey) PasskeyHome() (host, why string) {
	switch {
	case s.Has("domain"):
		d := s.valueOr("domain", s.domainWas())
		if d == "" {
			d = "your domain"
		}
		return d, "the domain reaches the main listener"
	case s.Has("tailscale"):
		return s.valueOr("tsname", "aacpanel") + ".<tailnet>.ts.net", "the tailnet node proxies to the main listener"
	}
	return "localhost", "only this machine signs in with a passkey"
}

// passkeyWas is the passkey address an earlier install set, and whether it
// set one.
func (s *Survey) passkeyWas() (string, bool) {
	v, ok := s.before(DotEnvFile, "AACP_RP_ID")
	return strings.TrimSpace(v), ok && strings.TrimSpace(v) != ""
}

// passkeyMoves tells whether this run moves the passkeys to another address,
// which voids every one of them. A tailnet name is known only once the node
// signs in, so a tailnet address stays when the node keeps its name.
func (s *Survey) passkeyMoves() (from, to string, moves bool) {
	from, ok := s.passkeyWas()
	if !ok {
		return "", "", false
	}
	to, _ = s.PasskeyHome()
	if strings.HasSuffix(to, ".<tailnet>.ts.net") && strings.HasSuffix(from, ".ts.net") {
		node := strings.TrimSuffix(to, ".<tailnet>.ts.net")
		return from, to, !strings.HasPrefix(from, node+".")
	}
	return from, to, from != to
}

func (s *Survey) access() []Question {
	f := s.Facts
	var qs []Question
	if s.Has("tailscale") {
		// The key is asked for until the node signs in: the installer's step
		// wipes it from .env then, so a key it left there is one that did not
		// take. An install by hand keeps its key after the node is in.
		v, _ := s.before(DotEnvFile, "AACP_TAILSCALE")
		left, _ := s.before(DotEnvFile, "TS_AUTHKEY")
		if v != "1" || (s.Facts.Mode == Upgrade && strings.TrimSpace(left) != "") {
			qs = append(qs, Question{
				ID: "tskey", Tab: "Tailscale key", Form: Secret, Required: true,
				Prompt: "Tailscale auth key (one-time, admin console → Settings → Keys)",
				Note:   "Turn on MagicDNS and HTTPS Certificates in the tailnet's DNS page first. The key is used once and wiped after the node signs in.",
				Flag:   "--ts-authkey-file", Writes: []Target{{DotEnvFile, "TS_AUTHKEY"}},
				Check: func(v string) error {
					if !strings.HasPrefix(v, "tskey-") {
						return errors.New("a Tailscale auth key starts with tskey-")
					}
					return nil
				},
			})
		}
		name := Question{
			ID: "tsname", Tab: "Node", Prompt: "Node name in the tailnet?", Form: One,
			Options: []Option{{Value: "aacpanel", Source: "the compose file",
				Detail: "The phone opens https://<name>.<your tailnet>.ts.net."}},
			Own: typeOwn, Flag: "--ts-hostname", Check: checkHost, Writes: []Target{{DotEnvFile, "AACP_TS_HOSTNAME"}},
		}
		s.prefer(&name, DotEnvFile, "AACP_TS_HOSTNAME", "The current node name.")
		qs = append(qs, name)
	}
	if s.Has("lan") {
		addr := Question{
			ID: "lanaddr", Tab: "LAN address", Prompt: "Which address should the panel listen on in the home network?", Form: One,
			Own: typeOwn, Flag: "--lan-addr", Writes: []Target{{DotEnvFile, "AACP_LAN_BIND"}},
		}
		for _, a := range s.Found.LAN {
			addr.Options = append(addr.Options, Option{Value: a.Value, Detail: a.Source + "; docker and loopback addresses are left out.", Source: a.Source})
		}
		s.prefer(&addr, DotEnvFile, "AACP_LAN_BIND", "The current address.")
		cert := Question{
			ID: "lancert", Tab: "Certificate", Prompt: "Certificate and key of the home-network address?", Form: One,
			Note: "The panel serves TLS on this address itself; the certificate stays yours to issue and renew.",
			Own:  "Type the path of the .crt; its .key lies beside it.", Flag: "--lan-cert", Check: checkPaths,
			Writes: []Target{{DotEnvFile, "AACP_TLS_DIR"}, {DotEnvFile, "AACP_TLS_CERT"}, {DotEnvFile, "AACP_TLS_KEY"},
				{DotEnvFile, "AACP_LAN_ADDR"}, {DotEnvFile, "AACP_LAN_PORT"}, {DotEnvFile, "AACP_LAN_URL"}},
		}
		for _, c := range s.Found.Certs {
			cert.Options = append(cert.Options, Option{Value: c.Cert, Label: f.Short(c.Cert), Source: "found",
				Detail: "For " + c.Name + ", with " + filepath.Base(c.Key) + " beside it."})
		}
		// Plain http needs the main listener on the home address, and a
		// domain's proxy needs that listener where the proxy reaches it:
		// the two do not go together.
		if !s.Has("domain") {
			cert.Options = append(cert.Options, Option{Value: "plain", Label: "No TLS — plain http",
				Detail: "The main listener goes on the home address and the session cookie goes without Secure: whoever sees the traffic sees the cookie, and a passkey does not work there."})
		}
		if dir, ok := s.before(DotEnvFile, "AACP_TLS_DIR"); ok && dir != "" {
			if c, ok := s.before(DotEnvFile, "AACP_TLS_CERT"); ok && c != "" {
				s.preferValue(&cert, filepath.Join(dir, filepath.Base(c)), "The current certificate.")
			}
		}
		qs = append(qs, addr, cert)
	}
	if s.Has("domain") {
		domain := Question{
			ID: "domain", Tab: "Domain", Prompt: "Domain of the panel?", Form: One,
			Note: "The reverse proxy, the DNS record and the certificate stay yours; the panel only needs its address.",
			Own:  "Type the domain, like panel.example.org.", Flag: "--domain", Check: checkHost,
			Writes: []Target{{DotEnvFile, "AACP_PUBLIC_URL"}, {DotEnvFile, "AACP_RP_ID"}, {DotEnvFile, "AACP_RP_ORIGINS"}},
		}
		if d := s.domainWas(); d != "" {
			s.preferValue(&domain, d, "The current domain.")
		}
		bind := Question{
			ID: "bind", Tab: "Proxy", Prompt: "Where does your reverse proxy reach the panel?", Form: One,
			Own: typeOwn, Flag: "--bind", Writes: []Target{{DotEnvFile, "AACP_BIND"}},
		}
		for _, p := range s.Found.Proxies {
			bind.Options = append(bind.Options, Option{Value: p.Value, Detail: p.Source + ".", Source: p.Source})
		}
		s.prefer(&bind, DotEnvFile, "AACP_BIND", "The current address.")
		qs = append(qs, domain, bind)
	}
	if len(qs) == 0 {
		return nil
	}
	// The move of the passkeys is asked in the block that makes it, so it
	// stands there from the start and shows once an answer above moves them.
	if _, was := s.passkeyWas(); was {
		from, to, moves := s.passkeyMoves()
		qs = append(qs, Question{
			ID: "passkey", Tab: "Passkeys", Form: Line, Required: true, Moot: !moves,
			Prompt: "Changing the passkey domain from " + from + " to " + to + " voids every enrolled passkey; devices enroll again with a code. Type " + to + " to go on.",
			Note:   "Esc goes back: leaving the kit as it was keeps the passkeys.",
			Flag:   "--passkey-home", Writes: []Target{{DotEnvFile, "AACP_RP_ID"}, {DotEnvFile, "AACP_RP_ORIGINS"}},
			Check: func(v string) error {
				if v != to {
					return fmt.Errorf("type %s to move the passkeys there", to)
				}
				return nil
			},
		})
	}
	if s.LANOnly() {
		token := Question{
			ID: "token", Tab: "Phone sign-in", Prompt: "How does a phone sign in over the home network?", Form: One,
			Note: "Without a domain or Tailscale, passkeys live at localhost: the home-network address hands a sign-in over to this machine, which a phone cannot open.",
			Options: []Option{
				{Value: yes, Label: "With a token (Recommended)", Source: "recommended",
					Detail: "A token made once on this machine goes into .env as AACP_TOKEN, and the sign-in screen takes it. Keep it like a password: it lets its holder in as a passkey does."},
				{Value: no, Label: "No token — only this machine signs in",
					Detail: "A phone reaches the address and stops at the sign-in until a domain or Tailscale is added."},
			},
			Flag: "--lan-token", Values: []string{yes, no}, Default: yes, Writes: []Target{{DotEnvFile, "AACP_TOKEN"}},
		}
		if v, ok := s.before(DotEnvFile, "AACP_TOKEN"); ok {
			was := no
			if strings.TrimSpace(v) != "" {
				was = yes
			}
			s.preferValue(&token, was, "")
		}
		qs = append(qs, token)
	}
	term := Question{
		ID: "term", Tab: "Terminal", Prompt: "Terminal of live sessions from other devices?", Form: One,
		Options: []Option{
			{Value: no, Label: "No — local panel only (Recommended)", Source: "recommended",
				Detail: "The live terminal opens only on the panel of this machine."},
			{Value: yes, Label: "Yes — behind a passkey",
				Detail: "Raw input into a live session, past the confirmation gate: whoever holds a passkey of the panel gets a shell as " + f.Account.Name + " here, from wherever the panel is reachable. The journal keeps the connection, not the keys typed."},
		},
		Flag: "--term-public", Values: []string{yes, no}, Writes: []Target{{DotEnvFile, "AACP_TERM_PUBLIC"}},
	}
	term.Default = no
	if v, ok := s.before(DotEnvFile, "AACP_TERM_PUBLIC"); ok {
		was := no
		if strings.TrimSpace(v) == "1" {
			was = yes
		}
		s.preferValue(&term, was, "")
	}
	return append(qs, term)
}

// LANOnly tells whether the home network is the one way in besides this
// machine: its listener then hands a sign-in over to localhost, where the
// passkeys live, and a phone has no way to sign in but a token.
func (s *Survey) LANOnly() bool {
	return s.Has("lan") && !s.Has("tailscale") && !s.Has("domain")
}

// domainWas is the domain an earlier install serves the panel on: its
// public address, or else a passkey domain that is not this machine's and
// not a tailnet's.
func (s *Survey) domainWas() string {
	if u, ok := s.before(DotEnvFile, "AACP_PUBLIC_URL"); ok && u != "" {
		host := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
		host, _, _ = strings.Cut(host, "/")
		host, _, _ = strings.Cut(host, ":")
		return host
	}
	if rp, ok := s.passkeyWas(); ok && rp != "localhost" && !strings.HasSuffix(rp, ".ts.net") {
		return rp
	}
	return ""
}

func (s *Survey) tune() []Question {
	return []Question{{
		ID: "tune", Prompt: "Tune more settings?", Form: One,
		Options: []Option{
			{Value: no, Label: "No — defaults", Source: "default",
				Detail: "Probes of local services, the contact for pushes and the length of a sign-in keep their defaults."},
			{Value: yes, Label: "Yes", Detail: "Three more questions."},
		},
		Flag: "--more", Values: []string{yes, no}, Default: no,
	}}
}

// tuneFlags are the flags of block F: any of them answers "Tune more
// settings?" with yes.
var tuneFlags = []string{"--probe", "--push-contact", "--login-life"}

func (s *Survey) more() []Question {
	probes := Question{
		ID: "probes", Tab: "Probes", Prompt: "Which local services should the Machine screen probe?", Form: Many,
		Note: "Listening ports found with ss; the panel's own are left out.",
		Own:  "Type name=host:port.", Flag: "--probe", Check: checkProbes,
		Writes: []Target{{HostEnvFile, "AACP_PROBE_PORTS"}},
	}
	for _, p := range s.Found.Ports {
		probes.Options = append(probes.Options, Option{Value: p.Value, Detail: p.Source, Source: "found with ss"})
	}
	probes.source = "default"
	s.preferList(&probes, HostEnvFile, "AACP_PROBE_PORTS", ",")

	push := Question{
		ID: "push", Tab: "Push contact", Prompt: "Contact for push services?", Form: One,
		Own: typeOwn, Flag: "--push-contact", Writes: []Target{{DotEnvFile, "AACP_PUSH_CONTACT"}},
	}
	if s.Found.Email != "" {
		push.Options = append(push.Options, Option{Value: "mailto:" + s.Found.Email, Source: "git config user.email",
			Detail: "From git config user.email. A push service writes here when pushes misbehave."})
	}
	push.Options = append(push.Options, Option{Value: "", Label: "Leave it to the panel", Source: "the panel's default",
		Detail: "The panel names its passkey address: https://<passkey domain>."})
	s.prefer(&push, DotEnvFile, "AACP_PUSH_CONTACT", "The current contact.")

	life := Question{
		ID: "life", Tab: "Sign-in", Prompt: "How long does a sign-in last?", Form: One,
		Options: []Option{
			{Value: defaultIdle + "/" + defaultMax, Label: defaultIdle + " idle · 30 days at most", Source: "the panel's default",
				Detail: "The defaults of the compose file."},
			{Value: "30m/12h", Label: "30m idle · 12h at most", Detail: "For a panel reached from outside the home."},
		},
		Own: "Type idle/max, like 1h/24h.", Flag: "--login-life", Check: checkLife,
		Writes: []Target{{DotEnvFile, "AACP_SESSION_IDLE"}, {DotEnvFile, "AACP_SESSION_MAX"}},
	}
	idle, iok := s.before(DotEnvFile, "AACP_SESSION_IDLE")
	max, mok := s.before(DotEnvFile, "AACP_SESSION_MAX")
	if iok || mok {
		if idle == "" {
			idle = defaultIdle
		}
		if max == "" {
			max = defaultMax
		}
		s.preferValue(&life, idle+"/"+max, "The current setting.")
	}
	return []Question{probes, push, life}
}

func (s *Survey) mapQuestions() []Question {
	if s.keep == "yes" || s.keep == "access" {
		return nil
	}
	var qs []Question
	accounts := split(s.valueOr("accounts", ""))
	for n, dir := range accounts {
		tab := "Contour"
		if len(accounts) > 1 {
			tab = fmt.Sprintf("Contour %d", n+1)
		}
		qs = append(qs, Question{
			ID: "contour:" + dir, Tab: tab, Prompt: "Name of the contour for " + s.Facts.Short(dir) + "?", Form: One, For: dir,
			Options: []Option{{Value: contourName(dir, s.Facts.Account.Home), Source: "the account's directory",
				Detail: "A contour gathers the sessions of one account on the Profiles screen."}},
			Own: typeOwn, Flag: "--contour", Check: checkName, Writes: []Target{{File: "map", Key: "contour"}},
		})
	}
	group := Question{
		ID: "group", Tab: "Group", Prompt: "Create a group for your projects?", Form: One,
		Options: []Option{
			{Value: "Projects", Source: "suggested", Detail: "The first group of the contour; more come on the Profiles screen."},
			{Value: "", Label: "Skip", Detail: "No group now, and no projects in it."},
		},
		Own: typeOwn, Flag: "--group", Writes: []Target{{File: "map", Key: "group"}},
	}
	group.Default = "Projects"
	qs = append(qs, group)
	name := s.valueOr("group", group.Default)
	projects := Question{
		ID: "projects", Tab: "Projects", Prompt: fmt.Sprintf("Which projects go into the group %q?", name), Form: Many,
		Own: typePath, Skip: "Skip — add projects on the Profiles screen later", Flag: "--project", Check: checkPaths,
		Writes: []Target{{File: "map", Key: "projects"}},
		Moot:   name == "",
	}
	for _, p := range s.projects() {
		projects.Options = append(projects.Options, Option{Value: p.Path, Label: s.Facts.Short(p.Path),
			Detail: s.projectDetail(p), Source: "found", On: !p.Opened.IsZero() && s.now.Sub(p.Opened) < recent})
	}
	projects.source = "claude opened them lately"
	return append(qs, projects)
}

// recent is how lately claude has to have opened a project for the map to
// check it for the person.
const recent = 14 * 24 * time.Hour

// contourName is the suggested name of the contour of an account: the
// personal one for ~/.claude, the rest by their directory.
func contourName(dir, home string) string {
	if Expand(dir, home) == filepath.Join(home, ".claude") {
		return contours.Personal
	}
	name := strings.TrimPrefix(filepath.Base(dir), ".claude-")
	name = strings.ToLower(strings.TrimPrefix(name, "."))
	if checkName(name) != nil {
		return "contour"
	}
	return name
}

func (s *Survey) projectDetail(p ProjectDir) string {
	var parts []string
	if p.Git {
		parts = append(parts, "git")
	}
	if !p.Opened.IsZero() {
		parts = append(parts, "claude opened it "+ago(s.now.Sub(p.Opened)))
	}
	return strings.Join(parts, " · ")
}

func ago(d time.Duration) string {
	days := int(d / (24 * time.Hour))
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", days)
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// picked is the checked values of a Many question, joined by commas.
func picked(q Question) string {
	var out []string
	for _, o := range q.Options {
		if o.On || o.Locked {
			out = append(out, o.Value)
		}
	}
	return strings.Join(out, ",")
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
