package executor

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"aacpanel/internal/launcher"
	"aacpanel/internal/observer"
)

// Every unit the executor starts runs on the cores of the machine
// description — a session in tmux and on the stream, its window, a terminal
// of the panel, a codex daemon, an observer — and a description that names
// none leaves every one of them to the system, with no affinity at all: one
// rule, held by the arguments systemd-run is given.
func TestEveryUnitRunsOnTheCoresOfTheDescription(t *testing.T) {
	ctx := context.Background()
	units := map[string]func(e *Executor) error{
		"a session in tmux": func(e *Executor) error {
			_, err := e.runLauncher(ctx, project{Path: "/srv/proj", Session: "proj"}, "")
			return err
		},
		"a session on the stream": func(e *Executor) error {
			_, err := e.runLauncher(ctx, project{Path: "/srv/proj", Session: "proj",
				Launch: []byte(`{"transport":"stream"}`)}, "")
			return err
		},
		"its window": func(e *Executor) error {
			_, err := e.runWindowOpener(ctx, tmuxServer(""), "/srv/proj", "proj")
			return err
		},
		"a terminal of the panel": func(e *Executor) error {
			_, err := e.runTermStarter(ctx, "/srv/proj", "t-0123abcd")
			return err
		},
		"codex in tmux": func(e *Executor) error {
			t.Setenv(codexEnv, "/bin/true")
			_, err := e.runCodexTerminal(ctx, project{Path: "/srv/proj", Session: "proj"}, "/home/u/.codex", "thread")
			return err
		},
		"a codex daemon": func(e *Executor) error {
			return startDaemon(ctx, "/bin/true", "/home/u/.codex", t.TempDir(), e.cores)
		},
		"an observer": func(e *Executor) error {
			return observerUnits{cores: e.cores}.Start(ctx, "aacpanel-observer-0123456789abcdef.service",
				t.TempDir(), observer.Spec{Name: "ci"})
		},
	}
	for what, start := range units {
		for _, cores := range []string{"8-23", ""} {
			fakeLauncher(t, launcher.Report{Session: "proj"})
			log := fakeSystemdRun(t)
			e, _ := newTest(t, "")
			if err := start(e.HoldTo(cores)); err != nil {
				t.Fatalf("%s did not start: %v", what, err)
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var props []string
			for _, a := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if strings.HasPrefix(a, "--property=") {
					props = append(props, a)
				}
			}
			held := slices.ContainsFunc(props, func(a string) bool { return strings.HasPrefix(a, "--property=CPUAffinity") })
			switch {
			case cores != "" && !slices.Contains(props, "--property=CPUAffinity="+cores):
				t.Errorf("%s is started off the cores %s of the description: %q", what, cores, props)
			case cores == "" && held:
				t.Errorf("%s is held to cores the description does not name: %q", what, props)
			}
		}
	}
}
