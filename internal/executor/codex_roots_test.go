package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"aacpanel/internal/action"
)

func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func newGitFixture(t *testing.T, dir string) {
	t.Helper()
	gitFixture(t, dir, "init")
	gitFixture(t, dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-m", "Fixture")
}

func cacheFixture(t *testing.T) (string, string) {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	build := filepath.Join(base, "go-build")
	mod := filepath.Join(os.Getenv("HOME"), "go", "pkg", "mod")
	for _, dir := range []string{build, mod} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return build, mod
}

func TestHelperStartRestartAndRecoveryKeepBothGitDirectoriesAndCaches(t *testing.T) {
	for _, worktree := range []bool{false, true} {
		t.Run(map[bool]string{false: "main checkout", true: "worktree checkout"}[worktree], func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			onTheStream(t, true)
			newTmuxStub(t, nil, "")
			newGitFixture(t, p.Path)
			common := filepath.Join(p.Path, ".git")
			gitdir := common
			if worktree {
				copy := filepath.Join(filepath.Dir(p.Path), "work copy")
				gitFixture(t, p.Path, "worktree", "add", "--detach", copy)
				p.Path = copy
				// Git sanitizes the worktree directory's name for its metadata.
				entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
				if err != nil || len(entries) != 1 {
					t.Fatalf("the fixture has no worktree gitdir: %v %v", entries, err)
				}
				gitdir = filepath.Join(common, "worktrees", entries[0].Name())
			}
			build, mod := cacheFixture(t)
			extra := t.TempDir()
			p.OpenedBy = streamSID
			p.Codex = &action.CodexLaunch{WritableRoots: []string{extra, extra}}
			_, name, err := open(t, e, p, `{"agent":"codex","codexApproval":"untrusted"}`)
			if err != nil {
				t.Fatal(err)
			}
			old := e.codex.Find(name)[0]
			_, ownership, err := e.Restart(t.Context(), action.Request{Kind: action.SessionRestart, Target: name, Conversation: old.ID, Project: p})
			if err != nil {
				t.Fatal(err)
			}
			want := []any{extra, common}
			if worktree {
				want = append(want, gitdir)
			}
			want = append(want, build, mod)
			starts := srv.Calls("thread/start")
			if len(starts) != 2 {
				t.Fatalf("the helper did not start and restart: %s", starts)
			}
			for _, raw := range starts {
				start := paramsOf(t, raw)
				config := start["config"].(map[string]any)
				if start["approvalPolicy"] != "on-request" || start["approvalsReviewer"] != "auto_review" ||
					!reflect.DeepEqual(config["sandbox_workspace_write.writable_roots"], want) {
					t.Fatalf("the helper lost its approvals or roots: %s, want roots %v", raw, want)
				}
			}
			if ownership == nil {
				t.Fatal("the restart returned no replacement conversation")
			}
			id := ownership.Conversation
			srv.Unload(id)
			waitFor(t, "the replacement helper to recover", func() bool { return srv.Loads(id) == 1 && srv.Subscribed(id) })
			wantResume := paramsOf(t, starts[1])
			wantResume["threadId"], wantResume["excludeTurns"] = id, true
			matched := false
			for _, raw := range srv.Calls("thread/resume") {
				if reflect.DeepEqual(paramsOf(t, raw), wantResume) {
					matched = true
				}
			}
			if !matched || !heldMark(t, id) {
				t.Fatalf("the recovered helper lost its restart parameters: %s, want %v", srv.Calls("thread/resume"), wantResume)
			}
			p.OpenedBy, p.Codex = "", nil
			if _, _, err := open(t, e, p, `{"agent":"codex","codexApproval":"untrusted","codexSandbox":"workspace-write"}`); err != nil {
				t.Fatal(err)
			}
			plain := paramsOf(t, srv.Calls("thread/start")[2])
			if plain["approvalPolicy"] != "untrusted" || plain["approvalsReviewer"] != nil ||
				plain["config"].(map[string]any)["sandbox_workspace_write.writable_roots"] != nil {
				t.Fatalf("an ordinary session got helper overrides: %v", plain)
			}
		})
	}
}

func TestHelperCacheDefaultsWorkWithoutGoAndSkipMissingDirectories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		t.Setenv(key, "")
	}
	t.Setenv("PATH", t.TempDir())
	dir, explicit := t.TempDir(), t.TempDir()
	if got := helperWritableRoots(t.Context(), dir, []string{explicit, explicit}); !reflect.DeepEqual(got, []string{explicit}) {
		t.Fatalf("missing metadata and caches added roots: %v", got)
	}
	build, mod := cacheFixture(t)
	if got := helperWritableRoots(t.Context(), dir, nil); !reflect.DeepEqual(got, []string{build, mod}) {
		t.Fatalf("cache defaults depend on Go in PATH: %v", got)
	}
	t.Setenv("GOCACHE", explicit)
	t.Setenv("GOMODCACHE", explicit)
	if got := helperWritableRoots(t.Context(), dir, []string{explicit}); !reflect.DeepEqual(got, []string{explicit}) {
		t.Fatalf("explicit cache paths were replaced or duplicated: %v", got)
	}
	t.Setenv("GOCACHE", "off")
	t.Setenv("GOMODCACHE", "")
	if err := os.Remove(mod); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := helperWritableRoots(t.Context(), dir, nil); len(got) != 0 {
		t.Fatalf("disabled build cache or a file added a root: %v", got)
	}
}
