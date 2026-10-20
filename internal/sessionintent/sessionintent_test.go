package sessionintent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
)

func TestSessionMessagesOverrideAndClearIndependently(t *testing.T) {
	dir := t.TempDir()
	p := mcp.Place{ConfigDir: t.TempDir(), Dir: t.TempDir()}
	read := func(name string) Values {
		t.Helper()
		got, err := Read(dir, p, name)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	keep := func(change Values) {
		t.Helper()
		if err := Keep(dir, p, "lead", change); err != nil {
			t.Fatal(err)
		}
	}
	if got := read("lead").Pick(Restart, ""); got != "" {
		t.Fatalf("no message anywhere became %q", got)
	}
	keep(Values{First: " Read the queue "})
	keep(Values{Restart: " Continue "})
	got := read("lead")
	if got.Pick(Restart, "Summary") != "Continue" || got.Pick(First, "Project") != "Read the queue" {
		t.Fatalf("the session did not override the project: %v", got)
	}
	if got := read("neighbour").Pick(Restart, "Summary"); got != "Summary" {
		t.Fatalf("the neighbour borrowed the override: %q", got)
	}
	keep(Values{Restart: " \n "})
	if got := read("lead"); got.Pick(Restart, "Summary") != "Summary" || got[First] != "Read the queue" {
		t.Fatalf("clearing a restart message changed the first or lost inheritance: %v", got)
	}
	if err := Move(dir, p, "lead", "pilot"); err != nil {
		t.Fatal(err)
	}
	if len(read("lead")) != 0 || read("pilot")[First] != "Read the queue" {
		t.Fatal("the messages did not follow the rename")
	}
	info, err := os.Stat(filepath.Join(dir, role.File(p, "pilot")))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the message file permissions: %v, %v", info, err)
	}
}

func TestSessionMessagesUseTheRoleKeyAcrossWorktrees(t *testing.T) {
	root, store := t.TempDir(), t.TempDir()
	repo, wt := filepath.Join(root, "repo"), filepath.Join(root, "worktree")
	git := filepath.Join(repo, ".git")
	metadata := filepath.Join(git, "worktrees", "branch")
	for _, d := range []string{metadata, wt} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, text := range map[string]string{
		filepath.Join(git, "HEAD"):           "ref: refs/heads/main",
		filepath.Join(wt, ".git"):            "gitdir: " + metadata,
		filepath.Join(metadata, "commondir"): "../..",
	} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := mcp.Place{ConfigDir: t.TempDir(), Dir: repo}
	want := Values{First: "Queue", Restart: "Continue"}
	if err := Keep(store, p, "lead", want); err != nil {
		t.Fatal(err)
	}
	p.Dir = wt
	if got, err := Read(store, p, "lead"); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("the worktree reads %v, %v", got, err)
	}
	p.ConfigDir = t.TempDir()
	if got, err := Read(store, p, "lead"); err != nil || len(got) != 0 {
		t.Fatalf("another account reads %v, %v", got, err)
	}
	p.Dir = t.TempDir()
	if got, err := Read(store, p, "lead"); err != nil || len(got) != 0 {
		t.Fatalf("another repository reads %v, %v", got, err)
	}
}
