package view

import (
	"bytes"
	"os"
	"os/signal"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

// installing drives install on the screen with --yes up to the plan, whose
// Enter begins the steps.
func installing(t *testing.T, steps ...*install.Step) *screen {
	t.Helper()
	s := &screen{}
	s.m = newPlanModel(PlanOptions{Theme: ui.NewTheme(false), Inspect: fresh,
		Survey: surveyOn(desktop(), &install.Run{Yes: true}), Yes: true,
		Begin: func(*install.Survey) (*install.Run, []*install.Step, error) {
			m, err := install.OpenManifest(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return &install.Run{Manifest: m, Place: &install.Place{Clone: "/srv/clone", User: "u"}}, steps, nil
		}})
	s.m.out = s.collect
	s.m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	s.send(inspectedMsg{fresh()})
	if s.m.stage != proceeding {
		t.Fatalf("with --yes install stopped at stage %d", s.m.stage)
	}
	s.send(press("enter"))
	if s.m.stage != running {
		t.Fatalf("the approved plan did not begin the steps: stage %d", s.m.stage)
	}
	return s
}

// pump hands the screen what the run says until until holds.
func (s *screen) pump(t *testing.T, until func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !until() {
		msg := s.m.queue.pop()
		if msg == nil {
			if time.Now().After(deadline) {
				t.Fatalf("the run went quiet at stage %d, tasks %+v; the feed:\n%s", s.m.stage, s.m.tasks, ui.Strip(strings.Join(s.feed, "\n")))
			}
			time.Sleep(time.Millisecond)
			continue
		}
		s.send(msg)
	}
}

func (s *screen) text() string { return ui.Strip(strings.Join(s.feed, "\n")) }

func TestTheStepsRunOnTheScreenAndSayWhatIsInPlace(t *testing.T) {
	s := installing(t,
		&install.Step{ID: "a", Title: "Host description", Done: func(*install.Run) (bool, error) { return true, nil }},
		&install.Step{ID: "b", Title: "Build the executor", Apply: func(r *install.Run) error {
			r.Say(install.Pass, "~/bin/aacpanel-exec · -list names 31 of 31 actions")
			return nil
		}})
	s.pump(t, func() bool { return s.m.stage == over })
	feed := s.text()
	for _, want := range []string{"⏺ Host description\n  ⎿  ✓ in place already: nothing to do",
		"⏺ Build the executor\n  ⎿  ✓ ~/bin/aacpanel-exec · -list names 31 of 31 actions", "✻ Installed in", "Left as it was"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed does not say %q:\n%s", want, feed)
		}
	}
	if s.m.status != 0 || !s.quit {
		t.Errorf("the run ended with %d, quit %v", s.m.status, s.quit)
	}
}

// TestNoToTheRootFrameStopsWithTheAdministratorsCommand: the root command
// is framed and asked about, and a no is a stop with the command to hand
// to an administrator.
func TestNoToTheRootFrameStopsWithTheAdministratorsCommand(t *testing.T) {
	s := installing(t, &install.Step{ID: "root", Title: "Root part", Undo: install.UndoKind,
		Apply: func(r *install.Run) error {
			return r.AsRoot("Root command", "Installs tmux with apt. Nothing else runs as root.", "apply", "--user", "u")
		}})
	s.pump(t, func() bool { return s.m.stage == handing })
	view := ui.Strip(s.m.View().Content)
	if !strings.Contains(s.text(), "sudo bash /srv/clone/deploy/install/root.sh apply --user u") ||
		!strings.Contains(view, "Do you want to run it?") || !strings.Contains(view, "Show the script first") {
		t.Fatalf("the root frame is not asked; the feed:\n%s\nthe live part:\n%s", s.text(), view)
	}
	s.send(press("3"))
	s.pump(t, func() bool { return s.m.stage == over })
	feed := s.text()
	for _, want := range []string{"✗ the root part did not run: you said no", "An administrator runs, as root:",
		"sudo bash /srv/clone/deploy/install/root.sh apply --user u", "Fix it and run ./install.sh again"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed does not say %q:\n%s", want, feed)
		}
	}
	if s.m.status != 1 {
		t.Errorf("the run ended with %d", s.m.status)
	}
}

// TestCtrlCStopsAfterTheStepAtWork: the step goes on to its end, its safe
// point, no step follows, and the run names what it changed.
func TestCtrlCStopsAfterTheStepAtWork(t *testing.T) {
	release := make(chan struct{})
	followed := false
	s := installing(t,
		&install.Step{ID: "slow", Title: "Panel stack", Undo: install.UndoKind, Apply: func(r *install.Run) error {
			if err := r.Record(install.Dir, "/srv/made", "created"); err != nil {
				return err
			}
			<-release
			return nil
		}},
		&install.Step{ID: "next", Title: "App role", Apply: func(*install.Run) error { followed = true; return nil }})
	s.pump(t, func() bool { return s.m.at == 0 && len(s.m.tasks) > 0 && s.m.tasks[0].State == ui.Running })
	s.send(press("ctrl+c"))
	if !strings.Contains(ui.Strip(s.m.View().Content), "stopping when this step is done") {
		t.Errorf("the live part does not say the run stops:\n%s", ui.Strip(s.m.View().Content))
	}
	close(release)
	s.pump(t, func() bool { return s.m.stage == over })
	feed := s.text()
	if followed || s.m.status != 130 || !strings.Contains(feed, `Interrupted after "Panel stack"`) ||
		!strings.Contains(feed, "Changed so far in this run: /srv/made") {
		t.Errorf("status %d, the next step ran: %v; the feed:\n%s", s.m.status, followed, feed)
	}
}

// TestASignInGoesToClaudeWithoutAFrame: a command the person agreed to among
// the answers gets the terminal at once, and what a step leaves the person
// is said at the end.
func TestASignInGoesToClaudeWithoutAFrame(t *testing.T) {
	var back error
	s := installing(t, &install.Step{ID: "claude", Title: "Claude settings", Apply: func(r *install.Run) error {
		back = r.Hand(install.Handover{Title: "Sign in", Argv: []string{"claude"}, Direct: true,
			Says: "claude opens here to sign in to ~/.claude-work · /exit brings you back"})
		r.Remind("claude is not signed in to ~/.claude-work: CLAUDE_CONFIG_DIR=~/.claude-work claude signs in")
		return nil
	}})
	s.pump(t, func() bool { return s.m.hand != nil })
	if s.m.stage != running || s.m.handAsk != nil {
		t.Fatalf("a sign-in was framed and asked about: stage %d", s.m.stage)
	}
	s.send(tickMsg{})
	if !strings.Contains(s.text(), "Sign in → claude opens here to sign in to ~/.claude-work") {
		t.Errorf("the feed does not say where the terminal went:\n%s", s.text())
	}
	s.send(handedMsg{})
	s.pump(t, func() bool { return s.m.stage == over })
	if back != nil || s.m.status != 0 {
		t.Errorf("the hand-over came back with %v, the run ended with %d", back, s.m.status)
	}
	if !strings.Contains(s.text(), "⚠ claude is not signed in to ~/.claude-work") {
		t.Errorf("the end does not say what is left:\n%s", s.text())
	}
}

// TestACtrlCWhileSudoHoldsTheTerminalStopsAfterTheStep: the terminal is the
// command's, and a Ctrl+C there reaches the installer as a signal rather
// than a key; the command goes on — root.sh lets it pass — and the run stops
// after the step, as after a Ctrl+C on the screen.
func TestACtrlCWhileSudoHoldsTheTerminalStopsAfterTheStep(t *testing.T) {
	// The test holds the signal itself too: a handCmd that did not would
	// otherwise leave the test binary to die of it.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, os.Interrupt)
	defer signal.Stop(guard)
	var out bytes.Buffer
	c := &handCmd{h: install.Handover{Argv: []string{"sh", "-c", "kill -INT $PPID; sleep 0.2"}, Line: func(string) {}}}
	c.SetStdout(&out)
	c.SetStderr(&out)
	c.SetStdin(strings.NewReader(""))
	if err := c.Run(); err != nil || !c.interrupted {
		t.Fatalf("a Ctrl+C while the command held the terminal: err %v, interrupted %v", err, c.interrupted)
	}

	followed := false
	s := installing(t,
		&install.Step{ID: "root", Title: "Root part", Undo: install.UndoKind, Apply: func(r *install.Run) error {
			return r.AsRoot("Root command", "Creates /var/lib/aacpanel. Nothing else runs as root.", "apply", "--user", "u")
		}},
		&install.Step{ID: "next", Title: "User manager", Apply: func(*install.Run) error { followed = true; return nil }})
	s.pump(t, func() bool { return s.m.stage == handing })
	s.send(press("1"))
	s.send(handedMsg{interrupted: true})
	s.pump(t, func() bool { return s.m.stage == over })
	if followed || s.m.status != 130 || !strings.Contains(s.text(), `Interrupted after "Root part"`) {
		t.Errorf("status %d, the next step ran: %v; the feed:\n%s", s.m.status, followed, s.text())
	}
}

// TestAProgramThatDrawsGetsTheTerminalWhole: without a line reader the
// command writes straight to the terminal, in the environment it was given.
func TestAProgramThatDrawsGetsTheTerminalWhole(t *testing.T) {
	t.Setenv("AACP_PROBE_GONE", "set")
	var out bytes.Buffer
	c := &handCmd{h: install.Handover{Argv: []string{"sh", "-c", `printf '%s|%s' "$AACP_PROBE_SET" "${AACP_PROBE_GONE-unset}"`},
		Env: []string{"AACP_PROBE_SET=given"}, Unset: []string{"AACP_PROBE_GONE"}}}
	c.SetStdout(&out)
	c.SetStderr(&out)
	c.SetStdin(strings.NewReader(""))
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasSuffix(got, "\ngiven|unset") {
		t.Errorf("the program wrote %q", got)
	}
}

func TestAPlainInstallAsksForYesBeforeTheFirstStep(t *testing.T) {
	var out bytes.Buffer
	begun := false
	status := planPlain(PlanOptions{Theme: ui.NewTheme(false), Out: &out, Inspect: fresh,
		Survey: surveyOn(desktop(), &install.Run{Yes: true}),
		Begin:  func(*install.Survey) (*install.Run, []*install.Step, error) { begun = true; return nil, nil, nil }})
	if status != 1 || begun || !strings.Contains(out.String(), `stop: no terminal to ask "Would you like to proceed?"; pass --yes`) {
		t.Errorf("a plain install without --yes: status %d, begun %v:\n%s", status, begun, out.String())
	}
}

// TestAStepAsksAndShowsTheFirstDevice: the test session's question is asked
// on the screen while the steps wait, and the code of the first device
// stands in its frame, with its countdown, until q; r asks for a new code.
func TestAStepAsksAndShowsTheFirstDevice(t *testing.T) {
	var answer string
	asking := &install.Step{ID: "ask", Title: "Test session", Apply: func(r *install.Run) error {
		v, err := r.Answer(install.Question{ID: "session", Prompt: "Open a test session to check the whole chain?", Form: install.One,
			Options: []install.Option{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}, Flag: "--check-session", Default: "yes"})
		answer = v
		return err
	}}
	again := 0
	enroll := &install.Step{ID: "enroll", Title: "First device", Apply: func(r *install.Run) error {
		return r.Enroll(install.Enrollment{Code: "7K3QM-9XW2P", Expires: time.Now().Add(5 * time.Minute),
			Open: []string{"http://localhost:8776"}, Hints: []string{"no browser here: ssh -L 8776:127.0.0.1:8776 u@lab"},
			Again: func() (string, time.Time, error) { again++; return "AAAAA-BBBBB", time.Now().Add(5 * time.Minute), nil }})
	}}
	s := installing(t, asking, enroll)
	s.pump(t, func() bool { return s.m.stage == answering })
	s.send(press("2"))
	s.pump(t, func() bool { return s.m.stage == enrollingStage })
	if answer != "no" {
		t.Errorf("the step got %q", answer)
	}
	frame := ui.Strip(s.m.bottom())
	for _, w := range []string{"First device", "Open  http://localhost:8776  and enter the code", "7K3QM-9XW2P", "valid 4:5", "r — a new code · q — finish", "ssh -L 8776"} {
		if !strings.Contains(frame, w) {
			t.Errorf("the frame does not say %q:\n%s", w, frame)
		}
	}
	s.send(press("r"))
	for _, msg := range s.later {
		s.send(msg)
	}
	if again != 1 || !strings.Contains(ui.Strip(s.m.bottom()), "AAAAA-BBBBB") {
		t.Errorf("r: %d new codes, the frame:\n%s", again, ui.Strip(s.m.bottom()))
	}
	s.send(press("q"))
	s.pump(t, func() bool { return s.m.stage == over })
	if s.m.status != 0 || !strings.Contains(s.text(), "✻ Installed in") {
		t.Errorf("the run ended with %d:\n%s", s.m.status, s.text())
	}
}
