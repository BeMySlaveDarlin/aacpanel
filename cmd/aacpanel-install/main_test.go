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
		{nil, never, 2, "usage: aacpanel-install plan"},
		{[]string{"-h"}, never, 0, "aacpanel-install demo [--speed N]"},
		{[]string{"install"}, never, 2, `unknown command "install"`},
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
