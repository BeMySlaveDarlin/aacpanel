package install

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/contours"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// desktop is healthy() with a display, a terminal, a locale of its own, one
// account claude is signed in to and two repositories under ~/code, one of
// them opened by claude two days ago.
func desktop() *fake {
	m := healthy()
	m.Vars["LANG"] = "en_US.UTF-8"
	m.Files["/proc/sys/kernel/hostname"] = "lab.example.org\n"
	m.Files[home+"/.claude/.credentials.json"] = "{}"
	m.Files[home+"/code/shop/.git/HEAD"] = "ref: refs/heads/main\n"
	m.Files[home+"/code/landing/.git/HEAD"] = "ref: refs/heads/main\n"
	m.Stats["/tmp/.X11-unix/X0"] = Stat{Mode: fs.ModeSocket | 0o777}
	m.Stats[home+"/.claude/projects/-home-u-code-shop"] = Stat{Mode: fs.ModeDir | 0o700, Mod: now.Add(-50 * time.Hour)}
	m.Path["konsole"] = "/usr/bin/konsole"
	m.Cmds[key("systemctl", "--user", "show-environment")] = ok("DISPLAY=:0\nLANG=en_US.UTF-8\n")
	m.Cmds[key("locale", "-a")] = ok("C\nC.utf8\nen_US.utf8\nPOSIX\n")
	m.Cmds[key("ip", "-4", "-o", "addr")] = ok("1: lo    inet 127.0.0.1/8 scope host lo\n" +
		"2: enp3s0    inet 192.168.1.20/24 brd 192.168.1.255 scope global enp3s0\n" +
		"3: docker0    inet 172.17.0.1/16 brd 172.17.255.255 scope global docker0\n")
	m.Cmds[key("git", "-C", clone, "config", "user.email")] = ok("u@example.org\n")
	return m
}

func survey(m *fake, r *Run) *Survey {
	return NewSurvey(m, Inspect(m, clone), r, now)
}

// ids are the questions of a block, the moot ones marked.
func ids(qs []Question) []string {
	var out []string
	for _, q := range qs {
		if q.Moot {
			out = append(out, "("+q.ID+")")
			continue
		}
		out = append(out, q.ID)
	}
	return out
}

func find(t *testing.T, s *Survey, b BlockID, id string) Question {
	t.Helper()
	for _, q := range s.Questions(b) {
		if q.ID == id {
			return q
		}
	}
	t.Fatalf("block %s has no question %q: %v", b, id, ids(s.Questions(b)))
	return Question{}
}

// answerAll takes every suggested answer, block by block, and fails on a
// question left to ask.
func answerAll(t *testing.T, s *Survey) {
	t.Helper()
	for _, b := range Blocks {
		open, err := s.Settle(b)
		if err != nil || len(open) > 0 {
			t.Fatalf("block %s: %v, left to ask %v", b, err, ids(open))
		}
	}
}

func TestTheQuestionsOfAFreshDesktop(t *testing.T) {
	s := survey(desktop(), &Run{Yes: true})
	answerAll(t, s)
	for _, c := range []struct{ id, value, source string }{
		{"host", "lab", "hostname"},
		{"home", "lab", "host name"},
		{"roots", home + "/code", "found"},
		{"state", DefaultStateDir, "recommended"},
		{"terminal", "konsole", "found on PATH"},
		{"auto", yes, "default"},
		{"display", ":0", "from the user manager"},
		{"locale", "en_US.UTF-8", "$LANG, and in locale -a"},
		{"claude", home + "/.local/bin/claude", "command -v claude"},
		{"accounts", home + "/.claude", "found"},
		{"transport", "stream", "recommended"},
		{"kit", "relay,limits,tools,copies,brief,cap,nudge,restart", "suggested"},
		{"tune", no, "default"},
		{"push", "mailto:u@example.org", "git config user.email"},
		{"life", "24h/720h", "the panel's default"},
		{"contour:" + home + "/.claude", contours.Personal, "the account's directory"},
		{"group", "Projects", "suggested"},
		{"projects", home + "/code/shop", "claude opened them lately"},
	} {
		g, ok := s.Value(c.id)
		if !ok || g.Value != c.value || g.Source != c.source {
			t.Errorf("%s: %+v (answered %v), want %q from %q", c.id, g, ok, c.value, c.source)
		}
	}
	for _, id := range []string{"tskey", "lanaddr", "domain", "term", "passkey", "login:" + home + "/.claude"} {
		if g, ok := s.Value(id); ok {
			t.Errorf("%s was answered on a machine that does not ask it: %+v", id, g)
		}
	}
	if got := ids(s.Questions(BlockB)); !slices.Equal(got, []string{"terminal", "auto", "display", "locale"}) {
		t.Errorf("block B asks %v", got)
	}
}

// TestWithoutADisplayOnlyTheLocaleIsAsked: the windows need a display, the
// locale is asked all the same.
func TestWithoutADisplayOnlyTheLocaleIsAsked(t *testing.T) {
	m := desktop()
	delete(m.Stats, "/tmp/.X11-unix/X0")
	delete(m.Cmds, key("systemctl", "--user", "show-environment"))
	s := survey(m, &Run{Yes: true})
	if got := ids(s.Questions(BlockB)); !slices.Equal(got, []string{"locale"}) {
		t.Errorf("block B without a display asks %v", got)
	}
	answerAll(t, s)
	if plan := planText(s); !strings.Contains(plan, "no windows: sessions live in tmux only (no display found)") {
		t.Errorf("the plan does not say why there are no windows:\n%s", plan)
	}
}

func planText(s *Survey) string {
	var b strings.Builder
	for _, r := range s.Plan() {
		b.WriteString(r.Text + " " + r.Tag + "\n")
	}
	return b.String()
}

// TestNoWindowsMakesTheWindowQuestionsMoot: the question of windows on start
// and of the display stay in the block, hidden and unanswered.
func TestNoWindowsMakesTheWindowQuestionsMoot(t *testing.T) {
	s := survey(desktop(), &Run{Yes: true, Answers: map[string]string{"--terminal": ""}})
	if _, err := s.Settle(BlockB); err != nil {
		t.Fatal(err)
	}
	if got := ids(s.Questions(BlockB)); !slices.Equal(got, []string{"terminal", "(auto)", "(display)", "locale"}) {
		t.Errorf("block B with no windows: %v", got)
	}
	if g, ok := s.Value("terminal"); !ok || g.Value != "" || g.Source != "flag" {
		t.Errorf("no windows is an answer: %+v %v", g, ok)
	}
	for _, id := range []string{"auto", "display"} {
		if _, ok := s.Value(id); ok {
			t.Errorf("the moot %s was answered", id)
		}
	}
}

// TestTheCursorStandsOnTheEarlierAnswerOnlyWhenItsKeyIsThere: a key an
// earlier install wrote is its answer, empty or not; a key it did not write
// leaves the question to what the machine suggests.
func TestTheCursorStandsOnTheEarlierAnswerOnlyWhenItsKeyIsThere(t *testing.T) {
	for _, c := range []struct {
		name, hostEnv    string
		terminal, source string
		host             string
	}{
		{"no key", "AACP_REPO=" + clone + "\n#AACP_TERMINAL=konsole\n", "konsole", "found on PATH", "lab"},
		{"an empty key", "AACP_REPO=" + clone + "\nAACP_TERMINAL=\n", "", "current setting", "lab"},
		{"a key", "AACP_REPO=" + clone + "\nAACP_TERMINAL=xterm -e\nAACP_HOST=LAB\n", "xterm -e", "current setting", "LAB"},
	} {
		m := desktop()
		m.Files[DefaultStateDir+"/host.env"] = c.hostEnv
		s := survey(m, &Run{})
		if !s.Earlier() {
			t.Fatalf("%s: host.env is there, and the survey sees no earlier install", c.name)
		}
		q := find(t, s, BlockB, "terminal")
		if q.Default != c.terminal || s.defaultSource(q) != c.source {
			t.Errorf("%s: the cursor of the terminal is on %q from %q, want %q from %q", c.name, q.Default, s.defaultSource(q), c.terminal, c.source)
		}
		if q := find(t, s, BlockA, "host"); q.Default != c.host {
			t.Errorf("%s: the cursor of the host is on %q, want %q", c.name, q.Default, c.host)
		}
	}
}

// TestEveryQuestionHasAFlag walks a machine that asks every question there
// is and holds that each has a flag the command line knows, and that no
// flag stands for a question that is gone.
func TestEveryQuestionHasAFlag(t *testing.T) {
	m := desktop()
	for _, name := range []string{"tmux", "jq", "claude", "docker"} {
		delete(m.Path, name)
	}
	m.Files[home+"/.claude-work/settings.json"] = "{}"
	m.Files[DefaultStateDir+"/host.env"] = "AACP_REPO=" + clone + "\n"
	m.Files[clone+"/.env"] = "AACP_RP_ID=old.example.org\nAACP_DB_PASSWORD=x\n"
	r := &Run{Answers: map[string]string{
		"--kit": "+tailscale,+lan,+domain", "--more": yes, "--domain": "panel.example.org",
		"--account": home + "/.claude," + home + "/.claude-work",
	}}
	s := survey(m, r)
	s.Keep(Given{Value: "review"})
	used := map[string]bool{"--keep": true}
	check := func(q Question) {
		if q.Flag == "" {
			t.Errorf("%s (%q) has no flag: a run without a terminal cannot answer it", q.ID, q.Prompt)
			return
		}
		if _, ok := FlagNamed(q.Flag); !ok {
			t.Errorf("%s answers to %s, which the command line does not know", q.ID, q.Flag)
		}
		used[q.Flag] = true
	}
	check(s.KeepQuestion())
	for _, b := range Blocks {
		qs, _ := s.Settle(b)
		for _, q := range s.Questions(b) {
			check(q)
		}
		for _, q := range qs {
			s.Set(q, q.Default, "test")
		}
	}
	for _, id := range []string{"pkg:tmux", "pkg:jq", "pkg:claude", "pkg:docker", "login:" + home + "/.claude-work",
		"tskey", "lancert", "domain", "bind", "passkey", "term", "probes", "contour:" + home + "/.claude-work", "projects"} {
		if _, ok := s.given[id]; !ok {
			t.Errorf("the walk never met %s: the test no longer covers every question", id)
		}
	}
	// The token of a phone is asked only where the home network is the one
	// way in besides this machine.
	lan := survey(desktop(), &Run{Yes: true, Answers: map[string]string{"--kit": "+lan"}})
	answerAll(t, lan)
	check(find(t, lan, BlockD, "token"))
	for _, f := range Flags {
		if !used[f.Name] && f.Name != "--install-compose" {
			t.Errorf("%s answers no question", f.Name)
		}
	}
}

// TestYesAsksNothing: with --yes a fresh machine and an earlier install
// alike go through without one question.
func TestYesAsksNothing(t *testing.T) {
	m := desktop()
	delete(m.Path, "tmux")
	answerAll(t, survey(m, &Run{Yes: true}))

	m = desktop()
	m.Files[DefaultStateDir+"/host.env"] = "AACP_REPO=" + clone + "\nAACP_HOST=LAB\n"
	s := survey(m, &Run{Yes: true})
	g, err := s.Answer(s.KeepQuestion())
	if err != nil || g.Value != "yes" {
		t.Fatalf("keep under --yes: %+v %v", g, err)
	}
	s.Keep(g)
	answerAll(t, s)
	if v, _ := s.Value("host"); v.Value != "LAB" {
		t.Errorf("keeping the settings changed the host to %+v", v)
	}
}

// TestAPlainRunStopsAtTheFirstQuestion names the flag that answers it.
func TestAPlainRunStopsAtTheFirstQuestion(t *testing.T) {
	s := survey(desktop(), &Run{})
	open, err := s.Settle(BlockA)
	if err != nil || len(open) != 4 {
		t.Fatalf("block A left %v to ask, error %v", ids(open), err)
	}
	if got := Unasked(open[0]).Error(); got != `stop: no terminal to ask "What does the panel call this machine?"; pass --host <value> or --yes` {
		t.Errorf("the stop says %q", got)
	}
}

// TestTheWaysInAskOnlyWhatIsChecked: block D has the questions of the legs
// the kit checked, and none of the others.
func TestTheWaysInAskOnlyWhatIsChecked(t *testing.T) {
	for _, c := range []struct {
		kit  string
		want []string
	}{
		{"", nil},
		{"+lan", []string{"lanaddr", "lancert", "token", "term"}},
		{"+tailscale", []string{"tskey", "tsname", "term"}},
		{"+domain", []string{"domain", "bind", "term"}},
		{"+tailscale,+lan,+domain", []string{"tskey", "tsname", "lanaddr", "lancert", "domain", "bind", "term"}},
	} {
		r := &Run{Yes: true, Answers: map[string]string{}}
		if c.kit != "" {
			r.Answers["--kit"] = c.kit
		}
		s := survey(desktop(), r)
		if _, err := s.Settle(BlockK); err != nil {
			t.Fatal(err)
		}
		if got := ids(s.Questions(BlockD)); !slices.Equal(got, c.want) {
			t.Errorf("kit %q: block D asks %v, want %v", c.kit, got, c.want)
		}
	}
}

func TestTheKitFlag(t *testing.T) {
	suggested := "relay,limits,tools,copies,brief,cap,nudge,restart"
	for _, c := range []struct{ raw, want, err string }{
		{"+stamp,-cap", "relay,limits,tools,copies,brief,nudge,restart,stamp", ""},
		{"gc,lan", "relay,limits,tools,gc,lan", ""},
		{"-relay", suggested, ""},
		{"+wings", "", `--kit knows`},
	} {
		got, err := kitFlag(c.raw, suggested)
		if got != c.want || (err == nil) != (c.err == "") || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("--kit %s: %q, %v", c.raw, got, err)
		}
	}
}

// TestNoToAPrerequisiteStops with the command that installs it by hand; yes
// puts it into the root step.
func TestNoToAPrerequisiteStops(t *testing.T) {
	m := desktop()
	delete(m.Path, "tmux")
	s := survey(m, &Run{Answers: map[string]string{"--install-tmux": no}})
	_, err := s.Settle(BlockP)
	if err == nil || err.Error() != "stop: tmux is needed: sessions live in it, and without it the executor turns off every action on sessions. sudo apt install tmux, then run again." {
		t.Errorf("no to tmux: %v", err)
	}
	s = survey(m, &Run{Yes: true})
	answerAll(t, s)
	if plan := planText(s); !strings.Contains(plan, "apt install tmux new") {
		t.Errorf("yes to tmux leaves it out of the root step:\n%s", plan)
	}
}

// TestPasskeysMoveOnlyByConsent: a new domain moves the passkeys off the
// address an earlier install gave them, which voids them all, so the move
// takes the domain typed again — or --passkey-home under --yes.
func TestPasskeysMoveOnlyByConsent(t *testing.T) {
	m := desktop()
	m.Files[DefaultStateDir+"/host.env"] = "AACP_REPO=" + clone + "\n"
	m.Files[clone+"/.env"] = "AACP_RP_ID=old.example.org\nAACP_DB_PASSWORD=x\n"
	base := map[string]string{"--keep": "access", "--kit": "+domain", "--domain": "new.example.org"}
	with := func(extra map[string]string) *Survey {
		a := map[string]string{}
		for k, v := range base {
			a[k] = v
		}
		for k, v := range extra {
			a[k] = v
		}
		s := survey(m, &Run{Yes: true, Answers: a})
		g, _ := s.Answer(s.KeepQuestion())
		s.Keep(g)
		return s
	}
	s := with(nil)
	s.Settle(BlockK)
	open, err := s.Settle(BlockD)
	if err != nil || !slices.Equal(ids(open), []string{"passkey"}) || !strings.Contains(Unasked(open[0]).Error(), "pass --passkey-home") {
		t.Errorf("--yes moved the passkeys without --passkey-home: left %v, %v", ids(open), err)
	}
	s = with(map[string]string{"--passkey-home": "elsewhere.org"})
	s.Settle(BlockK)
	if _, err := s.Settle(BlockD); err == nil {
		t.Error("--passkey-home with another domain moved the passkeys")
	}
	s = with(map[string]string{"--passkey-home": "new.example.org"})
	s.Settle(BlockK)
	if _, err := s.Settle(BlockD); err != nil {
		t.Errorf("--passkey-home with the new domain: %v", err)
	}
	if host, _ := s.PasskeyHome(); host != "new.example.org" {
		t.Errorf("the passkeys went to %s", host)
	}
}

func TestThePasskeyHomeIsTheMainListener(t *testing.T) {
	for _, c := range []struct{ kit, want string }{
		{"", "localhost"},
		{"+lan", "localhost"},
		{"+tailscale,+lan", "aacpanel.<tailnet>.ts.net"},
		{"+tailscale,+domain", "panel.example.org"},
	} {
		m := desktop()
		m.Files["/k"] = "tskey-auth-k\n"
		s := survey(m, &Run{Yes: true, Answers: map[string]string{"--kit": c.kit, "--domain": "panel.example.org", "--ts-authkey-file": "/k"}})
		if c.kit == "" {
			delete(s.run.Answers, "--kit")
		}
		s.Settle(BlockK)
		s.Settle(BlockD)
		if host, _ := s.PasskeyHome(); host != c.want {
			t.Errorf("kit %q: passkeys at %s, want %s", c.kit, host, c.want)
		}
	}
}

func TestContoursAreNamedByTheirAccount(t *testing.T) {
	for dir, want := range map[string]string{
		home + "/.claude":      contours.Personal,
		"~/.claude":            contours.Personal,
		home + "/.claude-work": "work",
		home + "/.claude-Ops":  "ops",
	} {
		if got := contourName(dir, home); got != want {
			t.Errorf("%s: %q, want %q", dir, got, want)
		}
	}
}

// TestAnAccountFlagNamesItsAccount: --contour dir=name answers the question
// of that account only, a bare --claude-login every account alike.
func TestAnAccountFlagNamesItsAccount(t *testing.T) {
	m := desktop()
	m.Files[home+"/.claude-work/settings.json"] = "{}"
	s := survey(m, &Run{Yes: true, Answers: map[string]string{
		"--account":                           home + "/.claude," + home + "/.claude-work",
		"--contour " + home + "/.claude-work": "job",
		"--claude-login":                      "later",
	}})
	answerAll(t, s)
	for id, want := range map[string]string{
		"contour:" + home + "/.claude":      contours.Personal,
		"contour:" + home + "/.claude-work": "job",
		"login:" + home + "/.claude-work":   "later",
	} {
		if g, _ := s.Value(id); g.Value != want {
			t.Errorf("%s: %+v, want %q", id, g, want)
		}
	}
}

// TestAnInstallByHandKeepsItsKit: the parts of the kit an install by hand
// wired into claude are the kit it keeps.
func TestAnInstallByHandKeepsItsKit(t *testing.T) {
	m := desktop()
	m.Files[DefaultStateDir+"/host.env"] = "AACP_REPO=" + clone + "\n"
	m.Files[clone+"/.env"] = "AACP_TAILSCALE=1\nAACP_DB_PASSWORD=x\n"
	m.Files[home+"/.claude/settings.json"] = `{"hooks":{"PreToolUse":[{"hooks":[{"command":"python3 ` + clone +
		`/agent/ask-hook.py"}]}],"Stop":[{"hooks":[{"command":"python3 ` + clone + `/deploy/claude/context-guard.py"}]}]}}`
	s := survey(m, &Run{Yes: true})
	if q := find(t, s, BlockK, "kit"); q.Default != "relay,limits,tools,cap,tailscale" {
		t.Errorf("the kit kept is %q", q.Default)
	}
}
