package executor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
	"aacpanel/internal/observer"
)

var testBudget = hostcfg.Memory{
	Agents:  hostcfg.Ceilings{High: "28G", Max: "34G", Swap: "4G"},
	Session: hostcfg.Ceilings{High: "6G", Max: "10G", Swap: "1G"},
}

// memoryProps are the memory properties among the arguments of systemd-run.
func memoryProps(args []string) []string {
	var props []string
	for _, a := range args {
		if strings.HasPrefix(a, "--property=Memory") {
			props = append(props, strings.TrimPrefix(a, "--property="))
		}
	}
	return props
}

// Every unit the executor starts for the agents lives in their slice, and
// goes on when the kernel kills a process in it. A session on the stream is
// the one thing its unit keeps, and it alone is held to the ceilings of a
// session; a unit that may keep a server of many — the tmux server of the
// sessions or of the terminals, a codex daemon — gets the slice alone, and an
// observer keeps ceilings of its own.
func TestEveryUnitForTheAgentsLivesInTheirSlice(t *testing.T) {
	ctx := context.Background()
	session := []string{"MemoryHigh=6G", "MemoryMax=10G", "MemorySwapMax=1G"}
	units := []struct {
		what  string
		start func(e *Executor) error
		held  []string
	}{
		{"a session in tmux", func(e *Executor) error {
			_, err := e.runLauncher(ctx, project{Path: "/srv/proj", Session: "proj"}, "")
			return err
		}, nil},
		{"a session on the stream", func(e *Executor) error {
			_, err := e.runLauncher(ctx, project{Path: "/srv/proj", Session: "proj",
				Launch: []byte(`{"transport":"stream"}`)}, "")
			return err
		}, session},
		{"its window", func(e *Executor) error {
			_, err := e.runWindowOpener(ctx, tmuxServer(""), "/srv/proj", "proj")
			return err
		}, nil},
		{"a terminal of the panel", func(e *Executor) error {
			_, err := e.runTermStarter(ctx, "/srv/proj", "t-0123abcd")
			return err
		}, nil},
		{"codex in tmux", func(e *Executor) error {
			t.Setenv(codexEnv, "/bin/true")
			_, err := e.runCodexTerminal(ctx, project{Path: "/srv/proj", Session: "proj",
				Launch: []byte(`{"transport":"stream"}`)}, "/home/u/.codex", "thread")
			return err
		}, nil},
		{"a codex daemon", func(e *Executor) error {
			return startDaemon(ctx, "/bin/true", "/home/u/.codex", t.TempDir(), e.cores)
		}, nil},
		{"an observer", func(e *Executor) error {
			return observerUnits{cores: e.cores}.Start(ctx, "aacpanel-observer-0123456789abcdef.service",
				t.TempDir(), observer.Spec{Name: "ci"})
		}, []string{"MemoryMax=" + observer.MemoryMax, "MemorySwapMax=0"}},
	}
	for _, u := range units {
		fakeLauncher(t, launcher.Report{Session: "proj"})
		log := fakeSystemdRun(t)
		e, _ := newTest(t, "")
		if err := u.start(e.HoldMemory(testBudget)); err != nil {
			t.Fatalf("%s did not start: %v", u.what, err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if !slices.Contains(args, "--slice="+AgentsSlice) {
			t.Errorf("%s is started outside the slice of the agents: %q", u.what, args)
		}
		if !slices.Contains(args, "--property=OOMPolicy=continue") {
			t.Errorf("%s is stopped whole when the kernel kills a process in it: %q", u.what, args)
		}
		if got := memoryProps(args); !slices.Equal(got, u.held) {
			t.Errorf("%s is held to %q, want %q", u.what, got, u.held)
		}
	}
}

// The feed check of claude runs one claude, and is held as a session is,
// with the bounds of a check after them.
func TestTheFeedCheckIsHeldAsASession(t *testing.T) {
	args := checkArgs([]string{"PATH=/usr/bin"}, "/opt/bin/aacpanel-exec", "8-23", testBudget.Session)
	if !slices.Contains(args, "--slice="+AgentsSlice) || !slices.Contains(args, "--property=OOMPolicy=continue") {
		t.Errorf("the check is started outside the slice of the agents: %q", args)
	}
	if got := memoryProps(args); !slices.Equal(got, []string{"MemoryHigh=6G", "MemoryMax=10G", "MemorySwapMax=1G"}) {
		t.Errorf("the check is held to %q", got)
	}
	tail := args[len(args)-5:]
	if !slices.Equal(tail, []string{"--property=KillMode=control-group", "--property=RuntimeMaxSec=12min",
		"--", "/opt/bin/aacpanel-exec", ClaudeContractFlag}) {
		t.Errorf("the check ends with %q", tail)
	}
}

// A ceiling the description turns off is not passed to the unit at all:
// the unit is held by the slice there, and the other ceilings stay.
func TestACeilingTurnedOffIsNotPassed(t *testing.T) {
	held := hostcfg.Ceilings{High: "6G", Max: "10G"}
	args := launchArgs([]string{"PATH=/usr/bin"}, "/opt/bin/aacpanel-exec", launchFlag, "", held)
	if !slices.Contains(args, "--slice="+AgentsSlice) {
		t.Errorf("the session is started outside the slice of the agents: %q", args)
	}
	if got := memoryProps(args); !slices.Equal(got, []string{"MemoryHigh=6G", "MemoryMax=10G"}) {
		t.Errorf("a session with no swap ceiling is held to %q", got)
	}
	if got := memoryProps(launchArgs(nil, "/b", launchFlag, "", hostcfg.Ceilings{})); len(got) != 0 {
		t.Errorf("a session with no ceilings is held to %q", got)
	}
}

// fakeSystemctl stands in for systemctl, writes down its arguments and
// answers with what it is given.
func fakeSystemctl(t *testing.T, answer string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+log+"\n"+answer+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(systemctlEnv, script)
	return log
}

// The executor sets the ceilings of the slice at its start, for the run of
// the manager only, and lifts one the description turned off.
func TestTheSliceIsSetUpFromTheDescription(t *testing.T) {
	log := fakeSystemctl(t, "exit 0")
	if err := SetUpAgentsSlice(context.Background(), hostcfg.Ceilings{High: "28G", Swap: "4G"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--user", "--runtime", "set-property", AgentsSlice,
		"MemoryHigh=28G", "MemoryMax=infinity", "MemorySwapMax=4G"}
	if got := strings.Split(strings.TrimSpace(string(raw)), "\n"); !slices.Equal(got, want) {
		t.Errorf("systemctl is called with %q, want %q", got, want)
	}
}

// A manager that does not answer is told in the error, with what systemctl
// said: the executor logs it and starts all the same.
func TestASliceNotSetUpSaysWhy(t *testing.T) {
	fakeSystemctl(t, "echo 'Failed to connect to bus: No medium found' >&2; exit 1")
	err := SetUpAgentsSlice(context.Background(), testBudget.Agents)
	if err == nil || !strings.Contains(err.Error(), AgentsSlice) || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Errorf("the failure says %v", err)
	}
}
