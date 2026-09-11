package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

func configPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".claude.json")
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSessionOpenTrustsProjectFromMap(t *testing.T) {
	for name, before := range map[string]map[string]bool{
		"the directory is not in the file at all": {},
		"there is a record but no trust":          {"": false},
	} {
		t.Run(name, func(t *testing.T) {
			root, dir := launchDir(t, "aacpanel")
			trusted := map[string]bool{}
			for path, ok := range before {
				if path == "" {
					path = dir
				}
				trusted[path] = ok
			}
			trustFile(t, trusted)
			t.Setenv(projectRootsEnv, root)
			log := fakeLauncher(t, launcher.Report{Session: "aacpanel", Konsole: 1, Agent: 2})
			e, _ := newTest(t, "")

			detail, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
				Path: dir, Session: "aacpanel",
			}, ""))
			if err != nil {
				t.Fatalf("a project from the map did not open: %v", err)
			}
			if _, err := os.ReadFile(log); err != nil {
				t.Errorf("the launcher was not called: %v", err)
			}
			if ok, err := projectTrusted(dir); err != nil || !ok {
				t.Errorf("trust was not written (%v, %v) — the session freezes on the question while the panel says it opened", ok, err)
			}
			if !strings.Contains(detail, "trust") {
				t.Errorf("the reply %q says nothing about the panel having granted the trust", detail)
			}
		})
	}
}

func TestSessionOpenNeverTrustsPathOutsideMap(t *testing.T) {
	home := t.TempDir()
	body, err := json.Marshal(map[string]any{
		"projects": map[string]any{home: map[string]any{"hasTrustDialogAccepted": false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	log := fakeLauncher(t, launcher.Report{Session: "home", Konsole: 1, Agent: 2})
	e, _ := newTest(t, "")

	_, err = e.Execute(context.Background(), req(action.SessionOpen, homeSessionName()))
	if err == nil {
		t.Fatal("a session came up in an untrusted repository — it will freeze on the question")
	}
	if strings.Contains(err.Error(), "rc-console.sh") {
		t.Errorf("the refusal %q calls a script of its own — on another machine there is none", err)
	}
	if _, err := os.ReadFile(log); err == nil {
		t.Error("the launcher was called after all: an empty window then has to be closed as well")
	}
	if got := readFile(t, path); !bytes.Equal(got, body) {
		t.Errorf("the trust file was edited on the fallback road:\n%s", got)
	}
}

func TestTrustGrantIsSeenByTheSameReader(t *testing.T) {
	_, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{})

	granted, err := trustProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Error("trust was granted for the first time, yet the executor thinks it was already there")
	}
	ok, err := projectTrusted(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("the trust that was written is not read back by the same code — the key drifted apart")
	}
}

func TestTrustGrantKeepsTheRestOfTheFile(t *testing.T) {
	_, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{})
	path := configPath(t)

	const cost = "169.03352100000018"
	const id = "12345678901234567890"
	before := `{
  "numStartups": 1690,
  "cachedGates": {"id": ` + id + `},
  "projects": {
    "/srv/proj/other": {
      "allowedTools": ["Bash(git status)"],
      "hasTrustDialogAccepted": true,
      "lastCost": ` + cost + `
    },
    "` + dir + `": {
      "allowedTools": ["Read"],
      "hasTrustDialogAccepted": false
    }
  },
  "userID": "u1"
}
`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := trustProject(dir); err != nil {
		t.Fatal(err)
	}

	after := readFile(t, path)
	for _, want := range []string{cost, id} {
		if !strings.Contains(string(after), want) {
			t.Errorf("a number moved along the way — looked for %s:\n%s", want, after)
		}
	}
	var cfg struct {
		Startups int `json:"numStartups"`
		UserID   string
		Projects map[string]struct {
			Tools   []string `json:"allowedTools"`
			Trusted bool     `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(after, &cfg); err != nil {
		t.Fatalf("the file does not parse after the edit: %v\n%s", err, after)
	}
	if cfg.Startups != 1690 || cfg.UserID != "u1" {
		t.Errorf("the top-level keys are lost: %+v", cfg)
	}
	if other := cfg.Projects["/srv/proj/other"]; !other.Trusted || len(other.Tools) != 1 {
		t.Errorf("a project of another is lost or changed: %+v", other)
	}
	mine := cfg.Projects[dir]
	if !mine.Trusted {
		t.Error("trust was not set")
	}
	if len(mine.Tools) != 1 || mine.Tools[0] != "Read" {
		t.Errorf("our own record was rewritten from scratch instead of one key being edited: %+v", mine)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Errorf("the file mode became %v instead of 0640", st.Mode().Perm())
	}
}

func TestTrustGrantRefusesBrokenConfig(t *testing.T) {
	_, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{})
	path := configPath(t)

	broken := []byte("{\"projects\": {oops\n")
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := trustProject(dir); err == nil {
		t.Fatal("a broken file was parsed and rewritten")
	}
	if got := readFile(t, path); !bytes.Equal(got, broken) {
		t.Errorf("the broken file was rewritten after all:\n%s", got)
	}
}

func TestSessionOpenLeavesTrustedConfigAlone(t *testing.T) {
	root, dir := launchDir(t, "aacpanel")
	trustFile(t, map[string]bool{dir: true})
	t.Setenv(projectRootsEnv, root)
	path := configPath(t)
	before := readFile(t, path)

	fakeLauncher(t, launcher.Report{Session: "aacpanel", Konsole: 1, Agent: 2})
	e, _ := newTest(t, "")

	detail, err := e.Execute(context.Background(), openWith(action.SessionOpen, &action.Project{
		Path: dir, Session: "aacpanel",
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !bytes.Equal(got, before) {
		t.Errorf("a trusted directory was rewritten from scratch:\n%s", got)
	}
	if strings.Contains(detail, "trust") {
		t.Errorf("the reply %q credits the panel with a dialog it never went through", detail)
	}
}

func TestTrustIsGrantedWhenConfigIsNotThereYet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	real, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if real != home {
		t.Fatalf("the test is running in the real home %s — stop, the history of every conversation is there", real)
	}

	_, dir := launchDir(t, "aacpanel")
	path := configPath(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the temporary home already has %s — the fresh-machine scenario is not reproduced", path)
	}

	if _, err := projectTrusted(dir); !os.IsNotExist(err) {
		t.Fatalf("a missing config reads as %v, while it has to be «no such file»", err)
	}

	if _, err := trustProject(dir); err != nil {
		t.Fatalf("trust was not granted on a fresh machine: %v", err)
	}
	ok, err := projectTrusted(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("trust was granted but is not read back — the session freezes on the dialog all the same")
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("a new .claude.json was created with mode %o: it holds the history of every conversation of the account", perm)
	}
}

func TestFallbackOpensWhereNothingWillAsk(t *testing.T) {
	home := t.TempDir()
	body, err := json.Marshal(map[string]any{
		"projects": map[string]any{home: map[string]any{"hasTrustDialogAccepted": false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	log := fakeLauncher(t, launcher.Report{Session: "home", Konsole: 1, Agent: 2})
	e, _ := newTest(t, "")

	if _, err := e.Execute(context.Background(), req(action.SessionOpen, homeSessionName())); err != nil {
		t.Fatalf("a directory without a repository was rejected: %v", err)
	}
	if _, err := os.ReadFile(log); err != nil {
		t.Errorf("the launcher was not called: %v", err)
	}
	if got := readFile(t, path); !bytes.Equal(got, body) {
		t.Errorf("trust was granted to the fallback road after all:\n%s", got)
	}
}
