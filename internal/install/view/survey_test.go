package view

import (
	"bytes"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

const home = "/home/u"

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// desktop is a fresh machine with a display, a terminal and one account
// claude is signed in to: the path the count of keys in the plan is for.
func desktop() *install.Table {
	return &install.Table{
		EUID: 1000, Acct: install.Account{Name: "u", UID: 1000, GID: 1000, Home: home}, Uname: "x86_64",
		Vars: map[string]string{"HOME": home, "LANG": "en_US.UTF-8"},
		Files: map[string]string{
			"/proc/sys/kernel/hostname":         "lab\n",
			home + "/.claude/.credentials.json": "{}",
			home + "/code/shop/.git/HEAD":       "ref: refs/heads/main\n",
			home + "/code/landing/.git/HEAD":    "ref: refs/heads/main\n",
		},
		Stats: map[string]install.Stat{
			"/tmp/.X11-unix/X0":                          {Mode: fs.ModeSocket | 0o777},
			home + "/.claude/projects/-home-u-code-shop": {Mode: fs.ModeDir | 0o700, Mod: now.Add(-50 * time.Hour)},
		},
		Path: map[string]string{"konsole": "/usr/bin/konsole"},
		Cmds: map[string]install.Reply{
			install.Command("systemctl", "--user", "show-environment"): install.Says("DISPLAY=:0\nLANG=en_US.UTF-8\n"),
			install.Command("locale", "-a"):                            install.Says("C\nC.utf8\nen_US.utf8\nPOSIX\n"),
		},
		Tty: true,
	}
}

func fresh() install.Inspection {
	return install.Inspection{
		Facts: install.Facts{Account: install.Account{Name: "u", UID: 1000, GID: 1000, Home: home},
			Clone: home + "/aacpanel", Version: "3f1c2ab", StateDir: install.DefaultStateDir,
			InstallDir: home + "/.local/state/aacpanel-install", Mode: install.Fresh, LANPort: install.PortLAN,
			Claude: home + "/.local/bin/claude"},
		Findings: []install.Finding{{Mark: install.Pass, Text: "Ubuntu 24.04.3 LTS · x86_64 · systemd 255"}},
	}
}

func surveyOn(m install.Machine, r *install.Run) func(install.Inspection) *install.Survey {
	return func(in install.Inspection) *install.Survey { return install.NewSurvey(m, in, r, now) }
}

// asked drives plan on the screen on a machine and gives the screen after
// the check.
func asked(m install.Machine, r *install.Run, in install.Inspection, width int) *screen {
	s := &screen{}
	s.m = newPlanModel(PlanOptions{Theme: ui.NewTheme(false), Inspect: func() install.Inspection { return in },
		Survey: surveyOn(m, r), Save: func(string) (string, error) { return "/tmp/plan.txt", nil }})
	s.m.out = s.collect
	s.m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	s.send(inspectedMsg{in})
	return s
}

// collect takes the feed instead of the terminal, and the quit with it.
func (s *screen) collect(text string, after ...tea.Cmd) tea.Cmd {
	if text != "" {
		s.feed = append(s.feed, text)
	}
	for _, a := range after {
		switch msg := a().(type) {
		case tea.QuitMsg:
			s.quit = true
		case codeMsg:
			s.later = append(s.later, msg)
		}
	}
	return nil
}

// TestEnterAllTheWay counts the keys of the default path of a fresh machine
// with graphics and one account: Enter on every question, on the Submit of
// every block of more than one question, and on the plan. The blocks are
// This machine (4 and Submit), Session windows (4 and Submit), Claude (3 and
// Submit), the kit, More settings, the Map (3 and Submit) and the plan: 21.
// The plan of the installer counts 19 for the same path with the root step
// and the test session, which plan does not reach, and without the four
// Submits. A change of the number changes the plan with it.
func TestEnterAllTheWay(t *testing.T) {
	s := asked(desktop(), &install.Run{}, fresh(), 80)
	keys := 0
	for s.m.stage != over && keys < 60 {
		s.send(press("enter"))
		keys++
	}
	if keys != 21 || s.m.status != 0 {
		t.Errorf("the default path took %d keys and ended with %d, want 21 and 0; the feed:\n%s", keys, s.m.status, ui.Strip(strings.Join(s.feed, "\n")))
	}
	feed := ui.Strip(strings.Join(s.feed, "\n"))
	for _, want := range []string{"What does the panel call this machine? → lab", "Which display do the windows open on? → :0",
		"Install plan", "host lab (hostname)", "Would you like to proceed? → Yes"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed never says %q:\n%s", want, feed)
		}
	}
}

func TestYesAsksNothingOnTheScreen(t *testing.T) {
	s := asked(desktop(), &install.Run{Yes: true}, fresh(), 80)
	if s.m.stage != proceeding {
		t.Fatalf("with --yes the screen stopped at stage %d asking:\n%s", s.m.stage, ui.Strip(s.m.View().Content))
	}
}

// TestTheKitOnTheScreen holds the kit as the person sees it, at the widths
// the frames are laid out for.
func TestTheKitOnTheScreen(t *testing.T) {
	for _, width := range []int{80, 60} {
		s := asked(desktop(), &install.Run{Answers: map[string]string{}, Yes: false}, fresh(), width)
		for s.m.stage == questioning && s.m.ask.block != install.BlockK {
			s.send(press("enter"))
		}
		if s.m.ask == nil || s.m.ask.block != install.BlockK {
			t.Fatal("the run never reached the kit")
		}
		golden(t, "kit_"+strconv.Itoa(width), s.m.ask.ui.View(s.m.t, width, 0))
	}
}

// TestThePlanFrame holds the plan of the default path.
func TestThePlanFrame(t *testing.T) {
	for _, width := range []int{80, 60} {
		s := asked(desktop(), &install.Run{Yes: true}, fresh(), width)
		golden(t, "plan_frame_"+strconv.Itoa(width), planFrame(s.m.t, s.m.s, width))
	}
}

// TestEscGoesBackABlock: Esc on the first tab reopens the block answered
// before, and its answers are still there.
func TestEscGoesBackABlock(t *testing.T) {
	s := asked(desktop(), &install.Run{}, fresh(), 80)
	for s.m.ask.block != install.BlockB {
		s.send(press("enter"))
	}
	s.send(press("esc"))
	if s.m.ask.block != install.BlockA || !s.m.ask.ui.OnSubmit() {
		t.Fatalf("Esc on the first tab of a block went to %s", s.m.ask.block)
	}
	s.send(press("enter"))
	if s.m.ask.block != install.BlockB {
		t.Errorf("submitting the block again went to %s", s.m.ask.block)
	}
}

// TestPlainWithFlags is plan without a terminal: the flags answer, --yes
// takes the rest, and the plan follows.
func TestPlainWithFlags(t *testing.T) {
	var out bytes.Buffer
	r := &install.Run{Yes: true, Answers: map[string]string{"--host": "studio", "--terminal": "",
		"--kit": "+stamp,-cap", "--transport": "tmux"}}
	status := planPlain(PlanOptions{Theme: ui.NewTheme(false), Out: &out, Inspect: fresh, Survey: surveyOn(desktop(), r)})
	if status != 0 {
		t.Errorf("status %d:\n%s", status, out.String())
	}
	golden(t, "plan_flags_plain", strings.TrimSuffix(out.String(), "\n"))
}

// TestPlainStopsAtAQuestionNoFlagAnswers names the flag that would.
func TestPlainStopsAtAQuestionNoFlagAnswers(t *testing.T) {
	var out bytes.Buffer
	status := planPlain(PlanOptions{Theme: ui.NewTheme(false), Out: &out, Inspect: fresh, Survey: surveyOn(desktop(), &install.Run{})})
	said := strings.Join(strings.Fields(out.String()), " ")
	if status != 1 || !strings.Contains(said, `stop: no terminal to ask "What does the panel call this machine?"; pass --host <value> or --yes`) {
		t.Errorf("status %d:\n%s", status, out.String())
	}
}

// earlier is the desktop after an install by hand: its host.env names the
// host in capitals and turns the windows off.
func earlier() (*install.Table, install.Inspection) {
	m := desktop()
	m.Files[install.DefaultStateDir+"/host.env"] = "AACP_REPO=" + home + "/aacpanel\nAACP_HOST=LAB\nAACP_TERMINAL=\n"
	in := fresh()
	in.Mode = install.Adopt
	return m, in
}

// TestAnEarlierInstallIsKeptWithOneKey: over an earlier install the one
// question is whether to keep it, and Enter keeps it.
func TestAnEarlierInstallIsKeptWithOneKey(t *testing.T) {
	m, in := earlier()
	s := asked(m, &install.Run{}, in, 80)
	if s.m.stage != questioning || s.m.cur != -1 {
		t.Fatalf("an earlier install did not open with its question: stage %d, block %d", s.m.stage, s.m.cur)
	}
	s.send(press("enter"))
	if s.m.stage != proceeding {
		t.Fatalf("keeping the settings asked more:\n%s", ui.Strip(s.m.View().Content))
	}
	plan := ui.Strip(planFrame(s.m.t, s.m.s, 80))
	for _, want := range []string{"host LAB (current setting)", "no windows: sessions live in tmux only (current setting)", "map: left as it is"} {
		if !strings.Contains(plan, want) {
			t.Errorf("the plan of the kept settings does not say %q:\n%s", want, plan)
		}
	}
}

// TestReviewStandsOnTheEarlierAnswers: reviewing opens the blocks with the
// cursor on what the earlier install wrote — on no windows, which it wrote
// as an empty template.
func TestReviewStandsOnTheEarlierAnswers(t *testing.T) {
	m, in := earlier()
	s := asked(m, &install.Run{}, in, 80)
	s.send(press("2"))
	if s.m.ask == nil || s.m.ask.block != install.BlockA {
		t.Fatalf("review did not open the first block")
	}
	if v := ui.Strip(s.m.ask.ui.View(s.m.t, 80, 0)); !strings.Contains(v, "❯ 1. LAB") {
		t.Errorf("the cursor of the host is not on the earlier answer:\n%s", v)
	}
	for s.m.ask.block != install.BlockB {
		s.send(press("enter"))
	}
	if v := ui.Strip(s.m.ask.ui.View(s.m.t, 80, 0)); !strings.Contains(v, "❯ 2. No windows") {
		t.Errorf("the cursor of the terminal is not on no windows:\n%s", v)
	}
}

// TestTheProceedQuestion: back to the answers reopens the last block; save
// writes the plan and ends.
func TestTheProceedQuestion(t *testing.T) {
	s := asked(desktop(), &install.Run{Yes: true}, fresh(), 80)
	s.send(press("2"))
	if s.m.stage != questioning {
		t.Fatalf("back to the answers left the stage at %d", s.m.stage)
	}
	s = asked(desktop(), &install.Run{Yes: true}, fresh(), 80)
	var saved string
	s.m.o.Save = func(text string) (string, error) { saved = text; return "/x/plan.txt", nil }
	s.send(press("3"))
	if !s.quit || s.m.status != 0 || !strings.Contains(saved, "Install plan") ||
		!strings.Contains(ui.Strip(strings.Join(s.feed, "\n")), "in /x/plan.txt") {
		t.Errorf("save: quit %v, status %d, saved %q", s.quit, s.m.status, saved)
	}
}
