package executor

import (
	"context"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

func TestLaunchJournalNamesTheIntent(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	fakeLauncher(t, launcher.Report{
		Session: "aacpanel", Konsole: 1, Agent: 2,
		Intent: "read the queue and take the first one",
	})
	e, _ := newTest(t, "")

	detail, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "read the queue and take the first one") {
		t.Errorf("the reply %q says nothing about the intent — the session started working and it is unclear on whose word", detail)
	}
}

func TestLaunchJournalShortensALongIntent(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	long := strings.Repeat("x", 400)
	fakeLauncher(t, launcher.Report{
		Session: "aacpanel", Konsole: 1, Agent: 2,
		Intent: "first line\nsecond " + long,
	})
	e, _ := newTest(t, "")

	detail, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail, "\n") {
		t.Errorf("a multi-line intent tore the action log entry apart: %q", detail)
	}
	if len([]rune(detail)) > 300 {
		t.Errorf("the action log entry grew to %d characters — the intent was not shortened: %q", len([]rune(detail)), detail)
	}
	if !strings.Contains(detail, "first line second") {
		t.Errorf("no beginning of the intent is left in the action log: %q", detail)
	}
}
