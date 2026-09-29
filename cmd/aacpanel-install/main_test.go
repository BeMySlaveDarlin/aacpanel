package main

import (
	"bytes"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/install"
)

func TestTheCommandLine(t *testing.T) {
	never := func() bool {
		t.Fatal("the command reached the terminal on a command line that should stop before it")
		return false
	}
	noTerminal := func() bool { return false }
	for _, c := range []struct {
		args     []string
		terminal func() bool
		status   int
		says     string
	}{
		{[]string{"-h"}, never, 0, "aacpanel-install demo [--speed N]"},
		{[]string{"--help"}, never, 0, "usage: aacpanel-install [install]"},
		{[]string{"upgrade"}, never, 2, `unknown command "upgrade"`},
		{[]string{"update", "-h"}, never, 0, "-to"},
		{[]string{"check", "extra"}, never, 2, `unexpected "extra"`},
		{[]string{"uninstall", "-h"}, never, 0, "-purge-data"},
		{[]string{"enroll", "--nope"}, never, 2, "flag provided but not defined"},
		{[]string{"install", "-h"}, never, 0, "-yes"},
		{[]string{"install", "extra"}, never, 2, `aacpanel-install install: unexpected "extra"`},
		{[]string{"plan", "-h"}, never, 0, "-plain"},
		{[]string{"plan", "extra"}, never, 2, `unexpected "extra"`},
		{[]string{"demo", "-h"}, never, 0, "-speed"},
		{[]string{"demo", "--speed", "0"}, never, 2, "--speed takes a number above zero"},
		{[]string{"demo", "--fail", "tailscale"}, never, 2, `--fail takes compose, not "tailscale"`},
		{[]string{"demo", "extra"}, never, 2, `unexpected "extra"`},
		{[]string{"demo", "--speed", "3", "--fail", "compose"}, noTerminal, 1, "needs a terminal"},
	} {
		var stdout, stderr bytes.Buffer
		status := run(c.args, &stdout, &stderr, c.terminal)
		said := stdout.String() + stderr.String()
		if status != c.status || !strings.Contains(said, c.says) {
			t.Errorf("%q: status %d, said:\n%s\nwant %d and %q", c.args, status, said, c.status, c.says)
		}
	}
}

// TestTheFlagsAnswerTheQuestions: every flag of a question reaches the run,
// a flag given twice gathers, a flag about an account keeps the account,
// and a switch stands for its answer.
func TestTheFlagsAnswerTheQuestions(t *testing.T) {
	home, _ := os.UserHomeDir()
	var got *install.Run
	var stdout, stderr bytes.Buffer
	status := runWith([]string{"plan", "--plain", "--yes", "--host", "studio", "--no-windows",
		"--project", "/srv/a", "--project", "/srv/b", "--contour", "~/.claude-work=job", "--claude-login", "later"}, env{
		stdout: &stdout, stderr: &stderr,
		terminal: func() bool { return false },
		inspect: func(clone string) install.Inspection {
			return install.Inspection{Facts: install.Facts{Account: install.Account{Name: "u", UID: 1000, Home: "/home/u"},
				Clone: clone, Claude: "/usr/bin/claude", Mode: install.Fresh}}
		},
		survey: func(in install.Inspection, r *install.Run) *install.Survey {
			got = r
			return install.NewSurvey(&install.Table{Acct: in.Account}, in, r, time.Now())
		},
	})
	if status != 0 || got == nil {
		t.Fatalf("status %d:\n%s%s", status, stdout.String(), stderr.String())
	}
	want := map[string]string{"--host": "studio", "--terminal": "", "--project": "/srv/a,/srv/b",
		"--contour " + home + "/.claude-work": "job", "--claude-login": "later"}
	if !got.Yes || !maps.Equal(got.Answers, want) {
		t.Errorf("the run got %v (yes %v), want %v", got.Answers, got.Yes, want)
	}
	if !strings.Contains(stdout.String(), "host studio (flag)") {
		t.Errorf("the plan does not carry the flag's answer:\n%s", stdout.String())
	}
	stdout.Reset()
	if status := run([]string{"plan", "-h"}, &stdout, &stdout, func() bool { return false }); status != 0 {
		t.Errorf("plan -h ended with %d", status)
	}
	for _, f := range append(install.Flags, install.Flag{Name: "--no-windows"}) {
		name := "  -" + strings.TrimPrefix(f.Name, "--")
		if !strings.Contains(stdout.String(), name+" ") && !strings.Contains(stdout.String(), name+"\n") {
			t.Errorf("plan -h does not list %s", f.Name)
		}
	}
}

// TestPlanWithoutATerminalIsPlain runs plan on a made-up inspection: with no
// terminal the plain view takes it, whole and without escapes, and a stop
// sets the status.
func TestPlanWithoutATerminalIsPlain(t *testing.T) {
	t.Setenv(cloneEnv, "/srv/aacpanel")
	var asked string
	for _, c := range []struct {
		mark   install.Mark
		status int
	}{{install.Pass, 0}, {install.Stop, 1}} {
		var stdout, stderr bytes.Buffer
		status := runWith([]string{"plan"}, env{
			stdout: &stdout, stderr: &stderr,
			terminal: func() bool { return false },
			inspect: func(clone string) install.Inspection {
				asked = clone
				return install.Inspection{
					Facts:    install.Facts{Account: install.Account{Name: "u", UID: 1000, Home: "/home/u"}, Clone: clone, Mode: install.Fresh},
					Findings: []install.Finding{{Mark: c.mark, Text: "the line"}},
				}
			},
		})
		out := stdout.String()
		if status != c.status || !strings.Contains(out, "⏺ Check the machine") || strings.Contains(out, "\x1b") {
			t.Errorf("mark %d: status %d, output:\n%q\n%s", c.mark, status, out, stderr.String())
		}
	}
	if asked != "/srv/aacpanel" {
		t.Errorf("the clone looked at is %q, not the one install.sh named", asked)
	}
}

// TestNoCommandInstalls: ./install.sh alone, as curl | bash runs it, and
// ./install.sh with flags alone are install — and install asks before it
// changes anything, so without a terminal a question no flag answers stops
// the run with the flag's name before the first step.
func TestNoCommandInstalls(t *testing.T) {
	for _, c := range []struct {
		args   []string
		status int
		says   string
		begins bool
	}{
		{nil, 1, "pass --host <value>", false},
		{[]string{"--plain", "--host", "lab", "--locale", "C.UTF-8"}, 1, "or --yes", false},
		{[]string{"--plain", "--host", "lab", "--locale", "C.UTF-8", "--yes"}, 0, "Would you like to proceed? → Yes (--yes)", true},
	} {
		began := false
		var stdout, stderr bytes.Buffer
		status := runWith(c.args, env{
			stdout: &stdout, stderr: &stderr,
			terminal: func() bool { return false },
			inspect: func(clone string) install.Inspection {
				return install.Inspection{Facts: install.Facts{Account: install.Account{Name: "u", UID: 1000, Home: "/home/u"},
					Clone: clone, Claude: "/usr/bin/claude", Mode: install.Fresh}}
			},
			survey: func(in install.Inspection, run *install.Run) *install.Survey {
				return install.NewSurvey(&install.Table{Acct: in.Account}, in, run, time.Now())
			},
			begin: func(*install.Survey, install.Install) (*install.Run, []*install.Step, error) {
				began = true
				m, err := install.OpenManifest(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				return &install.Run{Manifest: m}, nil, nil
			},
		})
		said := stdout.String() + stderr.String()
		if status != c.status || began != c.begins || !strings.Contains(said, c.says) {
			t.Errorf("%q: status %d, began %v, said:\n%s\nwant %d, began %v and %q", c.args, status, began, said, c.status, c.begins, c.says)
		}
	}
}

// madeUp is a run of install on a machine of the test's own: a clean check,
// no question left to the terminal, and the steps begin gives.
func madeUp(t *testing.T, args []string, steps ...*install.Step) (int, string, *install.Run) {
	t.Helper()
	var r *install.Run
	var stdout, stderr bytes.Buffer
	status := runWith(append([]string{"install", "--plain", "--host", "lab", "--locale", "C.UTF-8"}, args...), env{
		stdout: &stdout, stderr: &stderr,
		terminal: func() bool { return false },
		inspect: func(clone string) install.Inspection {
			return install.Inspection{Facts: install.Facts{Account: install.Account{Name: "u", UID: 1000, Home: "/home/u"},
				Clone: clone, Claude: "/usr/bin/claude", Mode: install.Fresh}}
		},
		survey: func(in install.Inspection, run *install.Run) *install.Survey {
			return install.NewSurvey(&install.Table{Acct: in.Account}, in, run, time.Now())
		},
		begin: func(*install.Survey, install.Install) (*install.Run, []*install.Step, error) {
			m, err := install.OpenManifest(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r = &install.Run{Manifest: m}
			return r, steps, nil
		},
	})
	return status, stdout.String() + stderr.String(), r
}

// TestInstallGoesOnPastThePlan: install is plan that takes the steps of
// the approved plan.
func TestInstallGoesOnPastThePlan(t *testing.T) {
	ran := false
	step := &install.Step{ID: "s", Title: "A step", Apply: func(*install.Run) error { ran = true; return nil }}
	status, out, _ := madeUp(t, []string{"--yes"}, step)
	if status != 0 || !ran || !strings.Contains(out, "Would you like to proceed? → Yes (--yes)") || !strings.Contains(out, "Installed in") {
		t.Errorf("install --yes: status %d, ran %v:\n%s", status, ran, out)
	}
}

// TestAStopNamesTheStepAndWhatChanged: a failing step ends the run there
// with its diagnosis and what the run changed so far, and the steps after
// it do not run.
func TestAStopNamesTheStepAndWhatChanged(t *testing.T) {
	made := &install.Step{ID: "made", Title: "Make a directory", Undo: install.UndoKind,
		Apply: func(r *install.Run) error { return r.Record(install.Dir, "/srv/made", "created") }}
	failing := &install.Step{ID: "failing", Title: "Panel stack", Apply: func(*install.Run) error {
		return &install.Failed{Diagnosis: "/healthz did not answer in 120 s", Tail: []string{"aacpanel | store: connection refused"}}
	}}
	after := false
	last := &install.Step{ID: "last", Title: "Last", Apply: func(*install.Run) error { after = true; return nil }}
	status, out, _ := madeUp(t, []string{"--yes"}, made, failing, last)
	for _, want := range []string{"✗ /healthz did not answer in 120 s", "aacpanel | store: connection refused",
		"Fix it and run ./install.sh again — finished steps are skipped.", "Changed in this run: /srv/made"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stop does not say %q:\n%s", want, out)
		}
	}
	if status != 1 || after {
		t.Errorf("status %d, the step after the stop ran: %v", status, after)
	}
}

// TestAStepOfInstallTakesTheFlagsToo: the test session is asked on the way,
// and the command line answers it as it answered the blocks.
func TestAStepOfInstallTakesTheFlagsToo(t *testing.T) {
	var got string
	step := &install.Step{ID: "s", Title: "Test session", Apply: func(r *install.Run) error {
		v, err := r.Answer(install.Question{ID: "session", Flag: "--check-session", Default: "yes"})
		got = v
		return err
	}}
	if status, out, _ := madeUp(t, []string{"--yes", "--check-session", "no"}, step); status != 0 || got != "no" {
		t.Errorf("status %d, the step got %q:\n%s", status, got, out)
	}
	if status, out, _ := madeUp(t, []string{"--yes"}, step); status != 0 || got != "yes" {
		t.Errorf("--yes: status %d, the step got %q:\n%s", status, got, out)
	}
}
