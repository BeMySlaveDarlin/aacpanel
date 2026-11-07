package rundir

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func vars(kv map[string]string) func(string) string {
	return func(name string) string { return kv[name] }
}

func TestPathTakesTheVariableFirst(t *testing.T) {
	runUser := t.TempDir()
	uid := os.Getuid()
	if err := os.Mkdir(filepath.Join(runUser, strconv.Itoa(uid)), 0o700); err != nil {
		t.Fatal(err)
	}
	got := pathIn(vars(map[string]string{"XDG_RUNTIME_DIR": "/run/elsewhere"}), runUser, uid, "aacpanel-observers", "sock")
	if want := "/run/elsewhere/aacpanel-observers/sock"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// A process whose environment lost XDG_RUNTIME_DIR, as codex's MCP servers
// do, still finds the directory the executor listens in.
func TestPathWithoutTheVariableTakesTheUsersRunDirectory(t *testing.T) {
	runUser := t.TempDir()
	uid := os.Getuid()
	own := filepath.Join(runUser, strconv.Itoa(uid))
	if err := os.Mkdir(own, 0o700); err != nil {
		t.Fatal(err)
	}
	got := pathIn(vars(nil), runUser, uid, "aacpanel-observers", "sock")
	if want := filepath.Join(own, "aacpanel-observers", "sock"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestPathFallsBackToTheTemporaryDirectory(t *testing.T) {
	uid := os.Getuid()
	want := filepath.Join(os.TempDir(), fmt.Sprintf("aacpanel-stream-%d", uid))
	cases := map[string]func(t *testing.T, runUser string){
		"no directory": func(*testing.T, string) {},
		"a file in its place": func(t *testing.T, runUser string) {
			if err := os.WriteFile(filepath.Join(runUser, strconv.Itoa(uid)), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			runUser := t.TempDir()
			prepare(t, runUser)
			if got := pathIn(vars(nil), runUser, uid, "aacpanel-stream"); got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
}

// The directory of another uid is not taken for this one's: its sockets are
// not this user's to dial.
func TestPathSkipsAnotherUsersDirectory(t *testing.T) {
	runUser := t.TempDir()
	other := os.Getuid() + 1
	if err := os.Mkdir(filepath.Join(runUser, strconv.Itoa(other)), 0o700); err != nil {
		t.Fatal(err)
	}
	got := pathIn(vars(nil), runUser, other, "aacpanel-exec", "sock")
	if want := filepath.Join(os.TempDir(), fmt.Sprintf("aacpanel-exec-%d", other), "sock"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
