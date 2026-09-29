package install

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/contours"
	"aacpanel/internal/hostcfg"
)

// Survey is the questions of a run and their answers. It knows the machine
// through what the check found and what Discover found, the earlier install
// through the host.env and the .env it left, and the command line through a
// Run. It asks nothing itself: Settle answers what needs nobody, and the
// screen asks the rest and hands the answers back with Set.
type Survey struct {
	Facts Facts
	Found Found

	m       Machine
	run     *Run
	now     time.Time
	missing []Missing
	was     earlier

	keep  string // the answer to "Keep the current settings?"; empty on a fresh install
	given map[string]Given
	found map[string][]ProjectDir // the projects under a list of roots, by the list
}

// Given is an answer and where it came from: the flag, the person, the
// machine, the earlier install.
type Given struct {
	Value, Source string
	Secret        bool
}

// earlier is what an earlier install wrote: the keys of host.env and of the
// .env of the clone, and the parts of the kit its claude settings carry. A
// key that is there with an empty value is an answer — "no windows" — and a
// key that is not there is none, so the maps keep that difference.
type earlier struct {
	host, env map[string]string
	kit       []string // nil: no part of the kit is wired
}

// CertDirs are where the certificates of the home-network address are
// looked for, besides the directory an earlier install named.
var CertDirs = []string{"/etc/ssl/aacpanel", "~/certs"}

// NewSurvey reads what the questions start from: the machine, the earlier
// install, and the programs the check found missing.
func NewSurvey(m Machine, in Inspection, r *Run, now time.Time) *Survey {
	if r == nil {
		r = &Run{}
	}
	s := &Survey{Facts: in.Facts, m: m, run: r, now: now, given: map[string]Given{}, found: map[string][]ProjectDir{}}
	for _, f := range in.Findings {
		if f.Missing != nil {
			s.missing = append(s.missing, *f.Missing)
		}
	}
	s.was = readEarlier(m, in.Facts)
	home := in.Facts.Account.Home
	dirs := []string{}
	if d, ok := s.before(DotEnvFile, "AACP_TLS_DIR"); ok && d != "" {
		dirs = append(dirs, d)
	}
	for _, d := range CertDirs {
		dirs = append(dirs, Expand(d, home))
	}
	s.Found = Discover(m, in.Facts, dirs)
	return s
}

func readEarlier(m Machine, f Facts) earlier {
	var e earlier
	if raw, err := m.ReadFile(filepath.Join(f.StateDir, "host.env")); err == nil {
		e.host = hostcfg.Parse(raw)
	}
	if raw, err := m.ReadFile(filepath.Join(f.Clone, ".env")); err == nil {
		e.env = hostcfg.Parse(raw)
	}
	e.kit = wiredKit(m, f, e)
	return e
}

// wiredKit is the kit an install by hand put in: a hook part is in when its
// script is in the claude settings of an account, a way in when the .env
// turns it on. Nothing wired — no kit to keep.
func wiredKit(m Machine, f Facts, e earlier) []string {
	home := f.Account.Home
	dirs := []string{filepath.Join(home, ".claude")}
	if list, ok := e.host[contours.HomeEnv]; ok && list != "" {
		dirs = nil
		for _, d := range strings.Split(list, ":") {
			dirs = append(dirs, Expand(d, home))
		}
	}
	var settings strings.Builder
	for _, d := range dirs {
		if raw, err := m.ReadFile(filepath.Join(d, "settings.json")); err == nil && json.Valid(raw) {
			settings.Write(raw)
		}
	}
	text := settings.String()
	on := map[string]bool{}
	for _, p := range Kit {
		if p.Trace != "" && strings.Contains(text, p.Trace) {
			on[p.ID] = true
		}
	}
	if _, err := m.Stat(filepath.Join(home, ".config", "systemd", "user", "aacpanel-docker-gc.timer")); err == nil {
		on["gc"] = true
	}
	on["tailscale"] = e.env["AACP_TAILSCALE"] == "1"
	on["lan"] = e.env["AACP_LAN_ADDR"] != ""
	on["testdb"] = e.env["AACP_TEST_DSN"] != ""
	if u := e.env["AACP_PUBLIC_URL"]; u != "" {
		on["domain"] = true
	} else if rp := e.env["AACP_RP_ID"]; rp != "" && rp != "localhost" && !strings.HasSuffix(rp, ".ts.net") {
		on["domain"] = true
	}
	if !on["relay"] && !on["tailscale"] && !on["lan"] && !on["domain"] {
		return nil
	}
	// A session the panel starts is allowed the panel's tools, the restart
	// among them, by its launch: settings with no rule of those tools leave
	// the restart to the launch, and a rule of the whole server allows it as
	// the restart's own rule would.
	if !strings.Contains(text, "mcp__aacpanel__") || strings.Contains(text, `"mcp__aacpanel"`) ||
		strings.Contains(text, `"mcp__aacpanel__*"`) {
		on["restart"] = true
	}
	var out []string
	for _, p := range Kit {
		if on[p.ID] || p.Group == GroupAlways {
			out = append(out, p.ID)
		}
	}
	return out
}

// Kept is the survey of a run that asks nothing and keeps every answer the
// install on the machine wrote: check and enroll read the install through
// it as an update that keeps the settings would.
func Kept(m Machine, in Inspection, now time.Time) *Survey {
	s := NewSurvey(m, in, &Run{Yes: true}, now)
	s.Keep(Given{Value: yes, Source: "the earlier install"})
	for _, b := range Blocks {
		// A question with no answer to take for granted stays unanswered:
		// what reads the answers falls back as a fresh install would.
		_, _ = s.Settle(b)
	}
	return s
}

// before is a key an earlier install wrote, and whether it wrote it.
func (s *Survey) before(file, key string) (string, bool) {
	m := s.was.host
	if file == DotEnvFile {
		m = s.was.env
	}
	v, ok := m[key]
	return v, ok
}

// Earlier tells whether an earlier install left answers to keep.
func (s *Survey) Earlier() bool {
	return s.Facts.Mode != Fresh && (s.was.host != nil || s.was.env != nil)
}

// KeepQuestion is the one question of a run over an earlier install.
func (s *Survey) KeepQuestion() Question {
	return Question{
		ID: "keep", Prompt: "Keep the current settings?", Form: One,
		Options: []Option{
			{Value: "yes", Label: "Yes — update and check", Source: "the earlier install",
				Detail: "Every answer stays as the earlier install wrote it; what it left unanswered takes the suggested value."},
			{Value: "review", Label: "Review the answers", Detail: "The same questions, with the cursor on the current answers."},
			{Value: "access", Label: "Change how the panel is reached", Detail: "The kit and its ways in; the rest stays."},
		},
		Flag: "--keep", Values: []string{"yes", "review", "access"}, Default: "yes",
	}
}

// Answer gives the answer to a question outside the blocks — whether to
// keep the current settings — from its flag or --yes, and stops a run with
// nobody to ask it.
func (s *Survey) Answer(q Question) (Given, error) {
	if raw, ok := s.run.Flagged(q); ok {
		v, err := s.fromFlag(q, raw)
		return Given{Value: v, Source: "flag"}, err
	}
	if s.run.Yes && !q.Required {
		return Given{Value: q.Default, Source: "--yes"}, nil
	}
	return Given{}, Unasked(q)
}

// Review is the person asking to see the answers the run took without
// asking: --yes no longer answers for them, and the settings of an earlier
// install are reviewed rather than kept. The flags still answer theirs.
func (s *Survey) Review() {
	s.run.Yes = false
	if s.keep != "" {
		s.Keep(Given{Value: "review", Source: "you"})
	}
}

// Keep takes the answer to "Keep the current settings?".
func (s *Survey) Keep(g Given) {
	s.keep = g.Value
	s.given["keep"] = g
}

// quiet tells whether block b is answered without asking: everything the
// person kept, and the rare settings nobody asked to tune.
func (s *Survey) quiet(b BlockID) bool {
	switch {
	case b == BlockP:
		return false
	case s.keep == "yes":
		return true
	case s.keep == "access":
		return b != BlockK && b != BlockD
	case b == BlockF:
		return s.valueOr("tune", no) != yes
	}
	return false
}

// Questions are the questions of block b that apply now, each with its
// suggested answer settled.
func (s *Survey) Questions(b BlockID) []Question {
	qs := s.questions(b)
	for i := range qs {
		settleDefault(&qs[i])
	}
	return qs
}

// settleDefault gives a question the answer Enter takes when nothing chose
// one: the first option, the checked ones. A question with nothing to
// suggest is Required.
func settleDefault(q *Question) {
	if q.chosen || q.Default != "" {
		return
	}
	switch q.Form {
	case One:
		if len(q.Options) > 0 {
			q.Default = q.Options[0].Value
			return
		}
		q.Required = true
	case Many:
		q.Default = picked(*q)
	default:
		q.Required = true
	}
}

// Settle answers every question of block b that needs nobody — by its flag,
// by --yes, by what the person kept — and gives back the questions left to
// ask, in order. A flag with a value the question does not take is an
// error, and so is an answer that stops the run: a program the person does
// not want installed.
func (s *Survey) Settle(b BlockID) ([]Question, error) {
	s.clear(b)
	var open []Question
	seen := map[string]bool{}
	for {
		var q *Question
		for _, c := range s.Questions(b) {
			if !seen[c.ID] {
				q = &c
				break
			}
		}
		if q == nil {
			break
		}
		seen[q.ID] = true
		if q.Moot {
			continue
		}
		v, src, ok, err := s.settle(b, *q)
		if err != nil {
			return nil, err
		}
		if !ok {
			open = append(open, *q)
			continue
		}
		s.Set(*q, v, src)
	}
	if len(open) == 0 {
		return nil, s.Refused()
	}
	return open, nil
}

func (s *Survey) settle(b BlockID, q Question) (value, source string, ok bool, err error) {
	if raw, flagged := s.run.Flagged(q); flagged {
		v, err := s.fromFlag(q, raw)
		return v, "flag", err == nil, err
	}
	if q.ID == "tune" && s.tuneFlagged() {
		return yes, "flag", true, nil
	}
	if s.quiet(b) || (s.run.Yes && !q.Required) {
		if q.Required {
			return "", "", false, Unasked(q)
		}
		return q.Default, s.defaultSource(q), true, nil
	}
	return "", "", false, nil
}

func (s *Survey) tuneFlagged() bool {
	for _, f := range tuneFlags {
		if _, ok := s.run.Answers[f]; ok {
			return true
		}
	}
	return false
}

// fromFlag checks the value a flag gave and turns it into the answer.
func (s *Survey) fromFlag(q Question, raw string) (string, error) {
	switch {
	case q.ID == "kit":
		return kitFlag(raw, q.Default)
	case q.Form == Secret:
		key, err := s.m.ReadFile(Expand(raw, s.Facts.Account.Home))
		if err != nil {
			return "", fmt.Errorf("%s %s: %w", q.Flag, raw, err)
		}
		raw = strings.TrimSpace(string(key))
	case len(q.Values) > 0 && !slices.Contains(q.Values, raw):
		return "", fmt.Errorf("%s takes %s, not %q", q.Flag, strings.Join(q.Values, "|"), raw)
	}
	if q.Check != nil && raw != "" && !(q.ID == "lancert" && raw == "plain") {
		if err := q.Check(raw); err != nil {
			return "", fmt.Errorf("%s %s: %w", q.Flag, raw, err)
		}
	}
	return raw, nil
}

func (s *Survey) expandAll(v string) string {
	parts := split(v)
	for i, p := range parts {
		if strings.HasPrefix(p, "~") {
			parts[i] = Expand(p, s.Facts.Account.Home)
		}
	}
	return strings.Join(parts, ",")
}

// Source is where an answer the person gave to q came from: the machine or
// the earlier install when it is the suggested one, else the person.
func (s *Survey) Source(q Question, v string) string {
	if v == q.Default {
		return s.defaultSource(q)
	}
	return "you"
}

// defaultSource is where the suggested answer of q came from.
func (s *Survey) defaultSource(q Question) string {
	if q.source != "" {
		return q.source
	}
	for _, o := range q.Options {
		if o.Value == q.Default && q.Form == One {
			return o.Source
		}
	}
	return "suggested"
}

// Set takes an answer to q. A path typed with ~ is written out whole: the
// files the answers go into are read by systemd and by a container, and
// neither knows whose home ~ is.
func (s *Survey) Set(q Question, value, source string) {
	if q.Form != Secret {
		value = s.expandAll(value)
	}
	s.given[q.ID] = Given{Value: value, Source: source, Secret: q.Form == Secret}
}

// Clear forgets the answers of a block before it is answered again: an
// answer to a question the block no longer asks must not linger.
func (s *Survey) Clear(b BlockID) { s.clear(b) }

func (s *Survey) clear(b BlockID) {
	for id := range s.given {
		if blockOf(id) == b {
			delete(s.given, id)
		}
	}
}

func blockOf(id string) BlockID {
	name, _, _ := strings.Cut(id, ":")
	switch name {
	case "pkg":
		return BlockP
	case "host", "home", "roots", "state":
		return BlockA
	case "terminal", "auto", "display", "locale":
		return BlockB
	case "claude", "accounts", "transport":
		return BlockC
	case "login":
		return BlockL
	case "kit":
		return BlockK
	case "tskey", "tsname", "lanaddr", "lancert", "domain", "bind", "passkey", "token", "term":
		return BlockD
	case "tune":
		return BlockT
	case "probes", "push", "life":
		return BlockF
	case "contour", "group", "projects":
		return BlockM
	}
	return ""
}

// Refused is the stop of a run whose person declined a program the panel
// cannot do without: the command that installs it by hand.
func (s *Survey) Refused() error {
	for _, m := range s.missing {
		if s.valueOr("pkg:"+m.Name, yes) == no {
			return fmt.Errorf("%s", m.Stop())
		}
	}
	return nil
}

// Value is the answer to the question id, and whether it has one.
func (s *Survey) Value(id string) (Given, bool) {
	g, ok := s.given[id]
	return g, ok
}

func (s *Survey) valueOr(id, fallback string) string {
	if g, ok := s.given[id]; ok {
		return g.Value
	}
	return fallback
}

// prefer puts the cursor on the answer an earlier install wrote for key —
// only when it wrote the key: an empty value is an answer, a missing key is
// not, and the question then suggests what the machine gives.
func (s *Survey) prefer(q *Question, file, key, detail string) {
	if v, ok := s.before(file, key); ok {
		s.preferValue(q, v, detail)
	}
}

// preferValue puts the cursor on v, adding it as an option of its own when
// the machine did not offer it.
func (s *Survey) preferValue(q *Question, v, detail string) {
	q.chosen, q.Default, q.source = true, v, "current setting"
	for i := range q.Options {
		if q.Options[i].Value == v {
			if detail != "" {
				q.Options[i].Detail = detail
			}
			q.Options[i].Source = "current setting"
			return
		}
	}
	q.Options = append([]Option{{Value: v, Detail: detail, Source: "current setting"}}, q.Options...)
}

// preferList checks the values an earlier install listed under key, and
// only them.
func (s *Survey) preferList(q *Question, file, key, sep string) {
	v, ok := s.before(file, key)
	if !ok {
		return
	}
	var want []string
	for _, p := range strings.Split(v, sep) {
		if p = strings.TrimSpace(p); p != "" {
			want = append(want, Expand(p, s.Facts.Account.Home))
		}
	}
	s.preferParts(q, want)
}

func (s *Survey) preferParts(q *Question, want []string) {
	for i := range q.Options {
		q.Options[i].On = q.Options[i].Locked || slices.Contains(want, q.Options[i].Value)
	}
	for _, w := range want {
		if !slices.ContainsFunc(q.Options, func(o Option) bool { return o.Value == w }) {
			q.Options = append(q.Options, Option{Value: w, Label: s.Facts.Short(w), Detail: "the current setting", On: true})
		}
	}
	q.chosen, q.Default, q.source = true, picked(*q), "current setting"
}

// projects are the projects under the roots answered in block A.
func (s *Survey) projects() []ProjectDir {
	roots := s.valueOr("roots", "")
	if p, ok := s.found[roots]; ok {
		return p
	}
	p := Scan(s.m, split(roots), s.Facts.Account.Home, s.Found.Known)
	s.found[roots] = p
	return p
}

// Offered is the check as a run that asks sees it: a program the installer
// can put on the machine is no stop there but a question of block P.
func Offered(in Inspection) Inspection {
	out := in
	out.Findings = make([]Finding, len(in.Findings))
	for i, f := range in.Findings {
		if f.Missing != nil {
			f.Mark = Warn
			f.Text = fmt.Sprintf("%s is missing: %s. The installer offers to install it.", f.Missing.Name, f.Missing.Why)
		}
		out.Findings[i] = f
	}
	return out
}
