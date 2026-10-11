package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/historymove"
	"aacpanel/internal/stream"
)

func moveFixture(t *testing.T) (action.Request, string) {
	t.Helper()
	root, project := launchDir(t, "shop")
	from, to := filepath.Join(root, "old"), filepath.Join(root, "new")
	for _, dir := range []string{from, to} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", root)
	t.Setenv(registry.HomeEnv, strings.Join([]string{from, to}, string(os.PathListSeparator)))
	t.Setenv(registry.RegistryEnv, "")
	t.Setenv(sessionsEnv, "")
	t.Setenv("XDG_RUNTIME_DIR", root)
	procFS(t)
	path := filepath.Join(from, "projects", historymove.Slug(project), "12345678-1234-4321-8765-123456789abc.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("transcript"), 0o600); err != nil {
		t.Fatal(err)
	}
	return action.Request{ID: "apply", Kind: action.ProjectMove, Target: project, Move: &action.ProjectMoveSpec{ID: "move", From: from, To: to}}, path
}

func TestProjectMoveRefusesLiveSessionsInEveryContour(t *testing.T) {
	for _, kind := range []string{"source", "destination", "codex", "worktree"} {
		t.Run(kind, func(t *testing.T) {
			r, path := moveFixture(t)
			procFS(t, fakeProc{pid: 717, comm: "claude", start: "123"})
			cwd := r.Target
			if kind == "worktree" {
				cwd = filepath.Join(filepath.Dir(r.Target), "linked")
				if err := os.Mkdir(cwd, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cwd, ".git"), []byte("gitdir: "+filepath.Join(r.Target, ".git/worktrees/linked")), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			config := r.Move.From
			if kind == "destination" {
				config = r.Move.To
			}
			if kind == "codex" {
				if err := os.MkdirAll(stream.Dir(), 0o700); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(map[string]any{"agent": "codex", "name": "busy-codex", "holder": 717, "cwd": cwd, "contour": "new"})
				if err := os.WriteFile(filepath.Join(stream.Dir(), "thread.json"), raw, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				sessionFilesIn(t, filepath.Join(config, "sessions"), fakeSession{pid: 717, name: "busy-claude", cwd: cwd, start: "123"})
			}
			_, err := (&Executor{}).Execute(t.Context(), r)
			if err == nil || !strings.Contains(err.Error(), "busy-") {
				t.Fatalf("live session was not named: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("refusal lost source", err)
			}
		})
	}
}
func TestProjectMoveKeepsUndoAcrossExecutorRestart(t *testing.T) {
	r, path := moveFixture(t)
	if _, err := (&Executor{}).Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []action.Kind{action.SessionOpen, action.SessionResume} {
		req := action.Request{Kind: kind, Target: "shop", Project: &action.Project{Session: "shop", Path: r.Target}, Resume: "12345678-1234-4321-8765-123456789abc"}
		if _, err := (&Executor{}).Execute(t.Context(), req); err == nil || !strings.Contains(err.Error(), "unfinished move") {
			t.Fatalf("launch passed pending move: %v", err)
		}
	}
	r.Move.Finish = "rollback"
	if _, err := (&Executor{}).Execute(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "transcript" {
		t.Fatalf("undo: %q %v", raw, err)
	}
}

func TestProjectMoveRefusesAContradictingHostRouter(t *testing.T) {
	r, path := moveFixture(t)
	registryFile := filepath.Join(filepath.Dir(r.Target), "registry")
	body := "old|" + r.Target + "/|" + r.Move.From + "|-\nnew|*|" + r.Move.To + "|-\n"
	if err := os.WriteFile(registryFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(registry.RegistryEnv, registryFile)
	if _, err := (&Executor{}).Execute(t.Context(), r); err == nil || !strings.Contains(err.Error(), "host router") {
		t.Fatalf("wrong account admitted: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("refusal moved the source", err)
	}
}

func TestMovedProjectTrustUsesItsDestinationAccount(t *testing.T) {
	r, _ := moveFixture(t)
	p := project{Path: r.Target, ConfigDir: r.Move.To, FromMap: true}
	if _, err := trustProjectFor(p); err != nil {
		t.Fatal(err)
	}
	trusted, err := projectTrustedFor(p)
	if err != nil || !trusted {
		t.Fatalf("destination trust: %v %v", trusted, err)
	}
	if _, err := os.Stat(filepath.Join(r.Move.From, ".claude.json")); !os.IsNotExist(err) {
		t.Fatal("source account was changed", err)
	}
}
