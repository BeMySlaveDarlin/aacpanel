package executor

import (
	"os"
	"path/filepath"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/codex/codextest"
	"aacpanel/internal/launcher"
)

func TestResumeUsesRelocatedDirectoryOnHost(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		for _, exists := range []bool{true, false} {
			t.Run(agent+map[bool]string{true: "/subdirectory", false: "/missing subdirectory"}[exists], func(t *testing.T) {
				t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				sessionFiles(t)
				_, root := launchDir(t, "current")
				dir := filepath.Join(root, ".claude", "worktrees", "fix")
				want := root
				if exists {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					want = dir
				}
				const conversation = "019a2000-0000-7000-8000-00000000a1b2"
				r := action.Request{Kind: action.SessionResume, Target: "service", Resume: conversation,
					ResumeAt: &action.ResumeDirectory{Path: dir, Root: root}}
				if agent == "codex" {
					srv, e, _ := onCodexContour(t)
					t.Setenv(projectRootsEnv, root)
					srv.Add(codextest.Thread{ID: conversation, CWD: "/srv/proj/former/.claude/worktrees/fix", Model: "gpt-test", Status: "notLoaded"})
					r.Agent, r.Contour = action.ResumeCodex, "acme"
					if _, _, err := e.Open(t.Context(), r); err != nil {
						t.Fatal(err)
					}
					calls := srv.Calls("thread/resume")
					if len(calls) == 0 || paramsOf(t, calls[0])["cwd"] != want {
						t.Fatalf("thread/resume directory = %s, want %s", calls, want)
					}
				} else {
					trustFile(t, map[string]bool{root: true, dir: true})
					log := fakeLauncher(t, launcher.Report{Session: "service", Konsole: 1, Agent: 2})
					e, _ := newTest(t, "")
					r.Project = &action.Project{Path: dir, Session: "service"}
					if _, err := e.Execute(t.Context(), r); err != nil {
						t.Fatal(err)
					}
					ranIn := ""
					if !exists {
						ranIn = dir
					}
					if got := launchedSpec(t, log); got.Dir != want || got.Resume != conversation || got.RanIn != ranIn {
						t.Fatalf("Claude launch = %v, want directory %s carried from %q", got, want, ranIn)
					}
				}
			})
		}
	}
}

// A worktree beside the project removed while its session slept: the
// conversation goes on in the project's own directory.
func TestResumeInARemovedWorktreeGoesOnInTheProject(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sessionFiles(t)
	_, root := launchDir(t, "current")
	gone := filepath.Join(root, "..", "worktrees", "fix-gone")
	trustFile(t, map[string]bool{root: true})
	log := fakeLauncher(t, launcher.Report{Session: "service", Konsole: 1, Agent: 2})
	e, _ := newTest(t, "")
	const conversation = "019a2000-0000-7000-8000-00000000a1b3"
	r := action.Request{Kind: action.SessionResume, Target: "service", Resume: conversation,
		ResumeAt: &action.ResumeDirectory{Path: gone, Root: root}, Project: &action.Project{Path: root, Session: "service"}}
	if _, err := e.Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if got := launchedSpec(t, log); got.Dir != root || got.Resume != conversation || got.RanIn != filepath.Clean(gone) {
		t.Fatalf("Claude launch = %v, want directory %s carried from %s", got, root, gone)
	}
}

func TestRelocatedResumeKeepsHostPathChecks(t *testing.T) {
	_, root := launchDir(t, "current")
	sibling := filepath.Join(root, "..", "sibling")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{sibling, "relative"} {
		if _, err := resumeDirectory(&action.ResumeDirectory{Path: dir, Root: root}); err == nil {
			t.Fatalf("relocated resume accepted directory %q outside its project", dir)
		}
	}
	link := filepath.Join(root, "outside")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, err := resumeDirectory(&action.ResumeDirectory{Path: link, Root: root}); err == nil {
		t.Fatal("relocated resume followed a symlink outside the allowed roots")
	}
}
