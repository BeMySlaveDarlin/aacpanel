package claudecfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("the config does not parse: %v", err)
	}
	return cfg
}

func trustedIn(t *testing.T, cfg map[string]any, dir string) bool {
	t.Helper()
	projects, _ := cfg["projects"].(map[string]any)
	entry, _ := projects[dir].(map[string]any)
	if entry == nil {
		return false
	}
	ok, _ := entry[TrustKey].(bool)
	return ok
}

func TestGrantCreatesConfigOnAFreshMachine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg", FileName)
	project := t.TempDir()

	added, err := Grant(path, project)
	if err != nil {
		t.Fatalf("on a fresh machine the trust was not issued: %v", err)
	}
	if added != 1 {
		t.Errorf("%d trusts issued instead of one", added)
	}
	if !trustedIn(t, read(t, path), Dir(project)) {
		t.Error("the trust is written somewhere other than where claude looks for it")
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("the new config is created with the mode %o: it holds the history of every conversation of the account", perm)
	}
}

func TestGrantKeepsEverythingElse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	const cost = "169.03352100000018"
	body := `{
  "projects": {
    "/srv/proj/other": {"hasTrustDialogAccepted": true, "allowedTools": ["Bash"]}
  },
  "oauthAccount": {"emailAddress": "u@example.com"},
  "lastCost": ` + cost + `
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	if _, err := Grant(path, project); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), cost) {
		t.Errorf("a number from somebody else's json was rewritten: %s is not in the file", cost)
	}
	cfg := read(t, path)
	if _, ok := cfg["oauthAccount"]; !ok {
		t.Error("the edit lost the neighbouring keys")
	}
	if !trustedIn(t, cfg, "/srv/proj/other") {
		t.Error("the edit lost the trust of the neighbouring directory")
	}
	projects, _ := cfg["projects"].(map[string]any)
	other, _ := projects["/srv/proj/other"].(map[string]any)
	if tools, _ := other["allowedTools"].([]any); len(tools) != 1 {
		t.Error("the edit lost the fields of the neighbouring record")
	}
}

func TestGrantCountsOnlyWhatItChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	project := t.TempDir()

	if added, err := Grant(path, project); err != nil || added != 1 {
		t.Fatalf("the first issue: added=%d err=%v", added, err)
	}
	added, err := Grant(path, project)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("a repeated issue reported %d directories while there was nothing to do", added)
	}
}

func TestGrantWritesThePhysicalPath(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	path := filepath.Join(base, FileName)

	if _, err := Grant(path, link+"/"); err != nil {
		t.Fatal(err)
	}
	cfg := read(t, path)
	if !trustedIn(t, cfg, Dir(real)) {
		t.Error("the trust is written by the link rather than by the physical path — claude will not find it")
	}
	if trustedIn(t, cfg, link) {
		t.Error("a record by the link appeared in the config: claude writes none such")
	}
	ok, err := Trusted(path, link)
	if err != nil || !ok {
		t.Errorf("the trust is not read through the link: ok=%v err=%v", ok, err)
	}
}

func TestGrantRefusesBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Grant(path, t.TempDir()); err == nil {
		t.Error("a broken config was accepted silently")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("the refusal touched the file all the same")
	}
}

func TestGrantLeavesNoLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if _, err := Grant(path, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != FileName {
			t.Errorf("%s is left behind after the write", e.Name())
		}
	}
}

func TestAsksForTrustFollowsTheRepository(t *testing.T) {
	empty := t.TempDir()
	if AsksForTrust(empty) {
		t.Error("an empty directory is declared as asking — the panel will refuse where everything would have opened")
	}

	withFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(withFile, "index.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withFile, "CLAUDE.md"), []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(withFile, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if AsksForTrust(withFile) {
		t.Error("files and settings are taken for the reason of the dialog: the measurement says they are silent")
	}

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !AsksForTrust(repo) {
		t.Error("the repository is not recognised — the panel will bring a session up straight into the dialog")
	}

	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /srv/proj/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !AsksForTrust(worktree) {
		t.Error("a worktree is not recognised: .git is a file there, and the dialog is the same")
	}
}
