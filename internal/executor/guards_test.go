package executor

import (
	"os"
	"path/filepath"
	"testing"

	"aacpanel/internal/action"
)

// The host reads a line a place, and the file is replaced whole: a place the
// map dropped is gone after the next handing, not left over from the last.
func TestKeepGuardsWritesALineAPlace(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	e := &Executor{}
	if err := e.KeepGuards(t.Context(), []action.Guard{
		{Path: "/srv/proj/Pets/service/aacpanel", Cap: 80, Restart: true},
		{Path: "/srv/proj/Algo", Cap: 70},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(GuardsPath())
	if err != nil {
		t.Fatal(err)
	}
	if want := "/srv/proj/Algo\t70\t0\n/srv/proj/Pets/service/aacpanel\t80\t1\n"; string(got) != want {
		t.Errorf("the guards file reads %q, meant %q", got, want)
	}
	if info, _ := os.Stat(GuardsPath()); info.Mode().Perm() != 0o600 {
		t.Errorf("the guards file is %v — the map of another user's projects is not for others", info.Mode().Perm())
	}

	if err := e.KeepGuards(t.Context(), []action.Guard{{Path: "/srv/proj/Algo", Cap: 60}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(GuardsPath()); string(got) != "/srv/proj/Algo\t60\t0\n" {
		t.Errorf("after a smaller map the file reads %q", got)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(GuardsPath()), ".guards-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left beside the guards: %v", left)
	}
}
