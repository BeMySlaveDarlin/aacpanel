package sessionpin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"aacpanel/internal/mcp"
)

func TestThePinBelongsToTheAccountRepositoryAndName(t *testing.T) {
	root := t.TempDir()
	repo, worktree := filepath.Join(root, "repo"), filepath.Join(root, "worktree")
	gitdir := filepath.Join(repo, ".git")
	linked := filepath.Join(gitdir, "worktrees", "topic")
	for _, dir := range []string{linked, worktree} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(gitdir, "HEAD"):      "ref: refs/heads/main\n",
		filepath.Join(linked, "commondir"): "../..\n",
		filepath.Join(worktree, ".git"):    "gitdir: " + linked + "\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "pins")
	place := mcp.Place{ConfigDir: filepath.Join(root, "account"), Dir: repo}
	at := time.Unix(100, 1).UTC()
	if err := Keep(dir, place, "lead", true, at); err != nil {
		t.Fatal(err)
	}
	// A new conversation in a worktree of the same repository finds the pin
	// by name; there is no process or conversation id in that identity.
	moved := mcp.Place{ConfigDir: place.ConfigDir, Dir: worktree}
	first, err := Read(dir, moved, "lead")
	if err != nil || first == nil || first.At != at.Format(Stamp) {
		t.Fatalf("the session in its worktree has pin %+v: %v", first, err)
	}
	for _, other := range []struct {
		place mcp.Place
		name  string
	}{
		{mcp.Place{ConfigDir: place.ConfigDir + "-other", Dir: repo}, "lead"},
		{mcp.Place{ConfigDir: place.ConfigDir, Dir: root}, "lead"},
		{place, "review"},
	} {
		if pin, err := Read(dir, other.place, other.name); err != nil || pin != nil {
			t.Errorf("another session inherited the pin %+v: %v", pin, err)
		}
	}
	if err := Keep(dir, moved, "lead", true, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	again, err := Read(dir, place, "lead")
	if err != nil || again == nil || *again != *first {
		t.Fatalf("a repeated pin changed its place: %+v, %v", again, err)
	}
	for range 2 {
		if err := Keep(dir, moved, "lead", false, at); err != nil {
			t.Fatal(err)
		}
		if pin, err := Read(dir, place, "lead"); err != nil || pin != nil {
			t.Fatalf("the removed pin remains %+v: %v", pin, err)
		}
	}
	later := at.Add(time.Nanosecond)
	if err := Keep(dir, place, "lead", true, later); err != nil {
		t.Fatal(err)
	}
	last, err := Read(dir, place, "lead")
	if err != nil || last == nil || last.At != later.Format(Stamp) || last.At <= first.At {
		t.Fatalf("the new pin has no later place: %+v, %v", last, err)
	}
}
