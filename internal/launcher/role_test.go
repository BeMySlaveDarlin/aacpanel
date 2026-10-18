package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
)

// The role the opener gives is kept under the name the session comes up
// under — a free one after a name a live session answers to — in the
// account and the directory it runs in, as set by its opener. A start
// without a role leaves the role the name has as it was.
func TestTheRoleIsKeptUnderTheNameTheSessionComesUpUnder(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	config, dir := t.TempDir(), t.TempDir()
	place := mcp.Place{ConfigDir: config, Dir: dir}
	start := func(spec Spec, comesUp string) Report {
		t.Helper()
		proc := fakeProc(t, fproc{pid: 300, comm: "claude", args: []string{"claude", "-n", "person"}})
		fakeTmuxLauncher(t, proc, comesUp, 200, 4242)
		machineClaude(t)
		t.Setenv(terminalAutoEnv, "0")
		spec.Dir, spec.Session, spec.ConfigDir = dir, "person", config
		rep, err := Run(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}

	rep := start(Spec{Role: &action.OpenRole{Text: " the db split ", Opener: "lead"}}, "person-2")
	if rep.Session != "person-2" {
		t.Fatalf("the session came up as %q", rep.Session)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "role") {
			t.Errorf("the start warned: %s", w)
		}
	}
	got := role.Read(role.Dir(), place, "person-2")
	if got == nil || got.Role != "the db split" || got.By != "opener lead" {
		t.Fatalf("the role of the session that came up is %+v", got)
	}
	if was := role.Read(role.Dir(), place, "person"); was != nil {
		t.Errorf("the live session of the name asked for got the role: %+v", was)
	}

	start(Spec{}, "person-2")
	if again := role.Read(role.Dir(), place, "person-2"); again == nil || *again != *got {
		t.Errorf("a start without a role changed the role of the name: %+v", again)
	}
	start(Spec{Role: &action.OpenRole{}}, "person-2")
	if cleared := role.Read(role.Dir(), place, "person-2"); cleared != nil {
		t.Errorf("an empty role left %+v", cleared)
	}
}

// A role that cannot be written is a warning, not a start refused: the session
// comes up, and its opener reads why it has no role.
func TestARoleNotKeptIsAWarning(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	// The directory of the roles is a file: nothing is written under it.
	if err := os.MkdirAll(filepath.Join(state, "aacpanel"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "aacpanel", "roles"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	proc := fakeProc(t)
	fakeTmuxLauncher(t, proc, "person", 200, 4242)
	machineClaude(t)
	t.Setenv(terminalAutoEnv, "0")
	rep, err := Run(context.Background(), Spec{Dir: t.TempDir(), Session: "person", ConfigDir: t.TempDir(),
		Role: &action.OpenRole{Text: "the db split"}, Launch: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("a role not kept refused the start: %v", err)
	}
	said := strings.Join(rep.Warnings, "; ")
	if rep.Session != "person" || !strings.Contains(said, "the role was not kept") {
		t.Errorf("the start came up as %q warning %q", rep.Session, said)
	}
}
