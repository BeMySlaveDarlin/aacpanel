package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in dir for a test, as a person of its own.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.org",
		"-c", "advice.detachedHead=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// releases is an origin with a commit a release, v1.0.0 to v2.0.0, and a
// clone of it on v1.0.0.
func releases(t *testing.T) (origin, clone string) {
	t.Helper()
	root := t.TempDir()
	origin, clone = filepath.Join(root, "origin"), filepath.Join(root, "clone")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "init", "--quiet")
	for _, tag := range []string{"v1.0.0", "v1.1.0", "v2.0.0"} {
		if err := os.WriteFile(filepath.Join(origin, "file"), []byte(tag), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, origin, "add", "file")
		gitIn(t, origin, "commit", "--quiet", "-m", tag)
		gitIn(t, origin, "tag", "-a", tag, "-m", tag)
	}
	gitIn(t, root, "clone", "--quiet", "--branch", "v1.0.0", origin, clone)
	return origin, clone
}

func localRun() *Run { return &Run{Shell: Local{}} }

// TestAnUpdateGoesToTheNewestReleaseOfItsParadigm: a clone on a release
// moves within its paradigm and names the later one it does not take; --to
// takes it; a paradigm behind is refused.
func TestAnUpdateGoesToTheNewestReleaseOfItsParadigm(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git here")
	}
	_, clone := releases(t)
	from := gitIn(t, clone, "rev-parse", "HEAD")
	m, err := MoveClone(localRun(), clone, -1)
	if err != nil {
		t.Fatal(err)
	}
	if m.From != from || m.To == from || m.Says != "v1.0.0 → v1.1.0" || m.Ahead != "v2.0.0" {
		t.Errorf("moved %+v", m)
	}
	if at := gitIn(t, clone, "describe", "--tags", "--exact-match"); at != "v1.1.0" {
		t.Errorf("the clone is on %s", at)
	}
	// Nothing newer: it stays.
	if m, err := MoveClone(localRun(), clone, -1); err != nil || m.To != m.From || !strings.Contains(m.Says, "the newest release of paradigm 1") {
		t.Errorf("again: %+v, %v", m, err)
	}
	if m, err := MoveClone(localRun(), clone, 2); err != nil || m.Says != "v1.1.0 → v2.0.0" {
		t.Errorf("--to 2: %+v, %v", m, err)
	}
	if _, err := MoveClone(localRun(), clone, 1); err == nil || !strings.Contains(err.Error(), "an update goes forward") {
		t.Errorf("back to paradigm 1: %v", err)
	}
}

// TestAnUpdateOfABranchPullsWithoutAMerge, and changes of one's own stop it
// before anything moves.
func TestAnUpdateOfABranchPullsWithoutAMerge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git here")
	}
	origin, clone := releases(t)
	gitIn(t, clone, "checkout", "--quiet", "-B", "main", "--track", "origin/main")
	gitIn(t, clone, "reset", "--quiet", "--hard", "v1.1.0")
	m, err := MoveClone(localRun(), clone, -1)
	if err != nil || m.Says != "the branch main, pulled" || m.To != gitIn(t, origin, "rev-parse", "HEAD") {
		t.Errorf("a pull: %+v, %v", m, err)
	}
	if _, err := MoveClone(localRun(), clone, 2); err == nil || !strings.Contains(err.Error(), "--to moves a clone that is on a release") {
		t.Errorf("--to on a branch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clone, "file"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := gitIn(t, clone, "rev-parse", "HEAD")
	_, err = MoveClone(localRun(), clone, -1)
	if err == nil || !strings.Contains(err.Error(), "the clone has changes of its own") || gitIn(t, clone, "rev-parse", "HEAD") != head {
		t.Errorf("a clone with changes: %v", err)
	}
}
