package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

// A helper's sandbox is the person's wherever they set one — the call, the
// map, config.toml of the home — and the panel's preset fills only what none
// of them says. The override of writable roots replaces codex's list, so a
// workspace helper's carries config.toml's roots, then the caller's, then the
// checkout's and the Go caches; its network is config.toml's where config.toml
// has a sandbox_workspace_write table.
func TestAHelpersSandboxIsThePersonsBeforeThePanelsPreset(t *testing.T) {
	const shared, explicit = "/srv/shared", "/srv/explicit"
	cases := []struct {
		name   string
		launch string
		call   action.CodexLaunch
		mode   string
		table  map[string]any
		refuse bool
		// sandbox and network are what thread/start gets, nil network for no
		// override; workspace says the roots are overridden, roots leading
		// the panel's own.
		sandbox   string
		network   any
		workspace bool
		roots     []string
		warn      string
	}{
		{name: "nothing anywhere takes the preset", launch: `{"agent":"codex"}`,
			sandbox: "workspace-write", network: true, workspace: true},
		{name: "the map's full access", launch: `{"agent":"codex","codexSandbox":"danger-full-access"}`,
			call: action.CodexLaunch{WritableRoots: []string{explicit}}, sandbox: "danger-full-access"},
		{name: "config.toml's full access", launch: `{"agent":"codex"}`, mode: "danger-full-access",
			sandbox: "danger-full-access"},
		{name: "config.toml's workspace without network", launch: `{"agent":"codex"}`, mode: "workspace-write",
			table:   map[string]any{"network_access": false, "writable_roots": []any{shared}},
			call:    action.CodexLaunch{WritableRoots: []string{explicit}},
			sandbox: "workspace-write", workspace: true, roots: []string{shared, explicit}},
		{name: "a table of roots that says nothing of the network", launch: `{"agent":"codex"}`,
			table:   map[string]any{"writable_roots": []any{shared, explicit}},
			call:    action.CodexLaunch{WritableRoots: []string{explicit}},
			sandbox: "workspace-write", workspace: true, roots: []string{shared, explicit}},
		{name: "config.toml's roots with the network on", launch: `{"agent":"codex","codexSandbox":"workspace-write"}`,
			mode: "workspace-write", table: map[string]any{"network_access": true, "writable_roots": []any{shared}},
			sandbox: "workspace-write", workspace: true, roots: []string{shared}},
		{name: "the call's read-only before the map's full access",
			launch: `{"agent":"codex","codexSandbox":"danger-full-access"}`, call: action.CodexLaunch{Sandbox: "read-only"},
			sandbox: "read-only"},
		{name: "the map's workspace before config.toml's full access",
			launch: `{"agent":"codex","codexSandbox":"workspace-write"}`, mode: "danger-full-access",
			sandbox: "workspace-write", network: true, workspace: true},
		{name: "the map's read-only before config.toml's full access",
			launch: `{"agent":"codex","codexSandbox":"read-only"}`, mode: "danger-full-access",
			call: action.CodexLaunch{WritableRoots: []string{explicit}}, sandbox: "read-only",
			warn: "the writable roots are not applied: the helper's sandbox is read-only"},
		{name: "an unread config.toml takes the preset", launch: `{"agent":"codex"}`, refuse: true,
			sandbox: "workspace-write", network: true, workspace: true,
			warn: "config.toml of the Codex home was not read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			onTheStream(t, true)
			newGitFixture(t, p.Path)
			build, mod := cacheFixture(t)
			srv.Sandbox(tc.mode, tc.table)
			if tc.refuse {
				srv.Refuse("config/read")
			}
			p.OpenedBy = streamSID
			if tc.call.Sandbox != "" || tc.call.WritableRoots != nil {
				p.Codex = &tc.call
			}
			detail, _, err := open(t, e, p, tc.launch)
			if err != nil {
				t.Fatal(err)
			}
			starts := srv.Calls("thread/start")
			if len(starts) != 1 {
				t.Fatalf("the helper did not start once: %s", starts)
			}
			start := paramsOf(t, starts[0])
			config := start["config"].(map[string]any)
			var roots any
			if tc.workspace {
				want := []any{}
				for _, root := range append(tc.roots, filepath.Join(p.Path, ".git"), build, mod) {
					want = append(want, root)
				}
				roots = want
			}
			if start["sandbox"] != tc.sandbox || config["sandbox_workspace_write.network_access"] != tc.network ||
				!reflect.DeepEqual(config["sandbox_workspace_write.writable_roots"], roots) {
				t.Fatalf("the helper started with sandbox %v, network %v, roots %v; want %s, %v, %v",
					start["sandbox"], config["sandbox_workspace_write.network_access"],
					config["sandbox_workspace_write.writable_roots"], tc.sandbox, tc.network, roots)
			}
			if start["approvalPolicy"] != "on-request" || start["approvalsReviewer"] != "auto_review" {
				t.Fatalf("the helper lost its approvals: %v", start)
			}
			warned := strings.Contains(detail, "WARNING")
			if tc.warn != "" && !strings.Contains(detail, "WARNING: "+tc.warn) || tc.warn == "" && warned {
				t.Fatalf("the open said %q, want the warning %q", detail, tc.warn)
			}
		})
	}
}

// The sandbox a helper settled at its start is what its mark keeps for a
// recovery and what a restart gives its replacement, whatever config.toml
// names by then; a workspace replacement reads config.toml's network and
// roots again.
func TestARecoveredOrRestartedHelperKeepsTheSandboxItSettled(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		table   map[string]any
		sandbox string
		roots   any
	}{
		{name: "config.toml's full access", mode: "danger-full-access", sandbox: "danger-full-access"},
		{name: "config.toml's workspace without network", mode: "workspace-write",
			table:   map[string]any{"network_access": false, "writable_roots": []any{"/srv/shared"}},
			sandbox: "workspace-write", roots: []any{"/srv/shared"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			onTheStream(t, true)
			newTmuxStub(t, nil, "")
			srv.Sandbox(tc.mode, tc.table)
			p.OpenedBy = streamSID
			_, name, err := open(t, e, p, `{"agent":"codex"}`)
			if err != nil {
				t.Fatal(err)
			}
			old := e.codex.Find(name)[0]
			first := paramsOf(t, srv.Calls("thread/start")[0])
			config := first["config"].(map[string]any)
			if first["sandbox"] != tc.sandbox || config["sandbox_workspace_write.network_access"] != nil ||
				!reflect.DeepEqual(config["sandbox_workspace_write.writable_roots"], tc.roots) {
				t.Fatalf("the helper started with %v", first)
			}
			// A recovery resumes the thread with its mark, as the recovery
			// tests show; the mark is read here rather than waited for.
			raw, err := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "aacpanel-stream", "codex-held", "acme", old.ID))
			if err != nil {
				t.Fatal(err)
			}
			if mark := paramsOf(t, raw); mark["sandbox"] != tc.sandbox || !reflect.DeepEqual(mark["config"], first["config"]) {
				t.Fatalf("the helper's mark lost its sandbox: %s, the start %v", raw, first)
			}
			srv.Sandbox("read-only", tc.table)
			detail, _, err := e.Restart(t.Context(), action.Request{Kind: action.SessionRestart, Target: name,
				Conversation: old.ID, Project: p})
			if err != nil {
				t.Fatal(err)
			}
			starts := srv.Calls("thread/start")
			if len(starts) != 2 {
				t.Fatalf("the helper was not restarted: %s", starts)
			}
			again := paramsOf(t, starts[1])
			if again["sandbox"] != tc.sandbox || !reflect.DeepEqual(again["config"], first["config"]) {
				t.Fatalf("the replacement started with %v, the helper with %v", again, first)
			}
			// Full access has no roots to lose, and the restart does not say it has.
			if strings.Contains(detail, rootsNotCarried) != (tc.roots != nil) {
				t.Fatalf("the restart said %q", detail)
			}
		})
	}
}

// rootsNotCarried is how a restart says the roots of the replaced thread's
// start stay behind.
const rootsNotCarried = "writable roots given only to the replaced thread's start are not carried over"

// An ordinary session given Codex options keeps the sandbox of the call or
// the map, config.toml deciding where neither names one, and the person's
// network and roots where it writes in its workspace: config.toml's table
// decides the network, and the roots override carries config.toml's roots and
// the explicit ones. A helper's git and Go roots and its approvals are not
// an ordinary session's.
func TestAnOrdinaryCodexSessionKeepsThePersonsNetworkAndRoots(t *testing.T) {
	const shared, explicit = "/srv/shared", "/srv/explicit"
	cases := []struct {
		name    string
		launch  string
		call    action.CodexLaunch
		mode    string
		table   map[string]any
		refuse  bool
		sandbox any
		network any
		roots   any
		warn    string
	}{
		{name: "nothing in config.toml takes the preset network", launch: `{"agent":"codex"}`,
			call:    action.CodexLaunch{Sandbox: "workspace-write", WritableRoots: []string{explicit}},
			sandbox: "workspace-write", network: true, roots: []any{explicit}},
		{name: "config.toml's workspace without network", launch: `{"agent":"codex"}`, mode: "workspace-write",
			table:   map[string]any{"network_access": false, "writable_roots": []any{shared}},
			call:    action.CodexLaunch{Sandbox: "workspace-write", WritableRoots: []string{explicit}},
			sandbox: "workspace-write", roots: []any{shared, explicit}},
		{name: "a table of roots that says nothing of the network", launch: `{"agent":"codex"}`,
			table:   map[string]any{"writable_roots": []any{shared, explicit}},
			call:    action.CodexLaunch{Sandbox: "workspace-write", WritableRoots: []string{explicit}},
			sandbox: "workspace-write", roots: []any{shared, explicit}},
		{name: "the map's workspace with config.toml's roots", launch: `{"agent":"codex","codexSandbox":"workspace-write"}`,
			table:   map[string]any{"network_access": true, "writable_roots": []any{shared}},
			call:    action.CodexLaunch{WritableRoots: []string{explicit}},
			sandbox: "workspace-write", roots: []any{shared, explicit}},
		{name: "config.toml decides a sandbox nothing else names", launch: `{"agent":"codex"}`,
			mode: "danger-full-access", call: action.CodexLaunch{Model: "gpt-6.1-sol"}},
		{name: "the call's read-only", launch: `{"agent":"codex","codexSandbox":"workspace-write"}`,
			call: action.CodexLaunch{Sandbox: "read-only"}, sandbox: "read-only"},
		{name: "an unread config.toml takes the preset network", launch: `{"agent":"codex"}`, refuse: true,
			call:    action.CodexLaunch{Sandbox: "workspace-write", WritableRoots: []string{explicit}},
			sandbox: "workspace-write", network: true, roots: []any{explicit},
			warn: "config.toml of the Codex home was not read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			newGitFixture(t, p.Path)
			cacheFixture(t)
			srv.Sandbox(tc.mode, tc.table)
			if tc.refuse {
				srv.Refuse("config/read")
			}
			p.Codex = &tc.call
			detail, _, err := open(t, e, p, tc.launch)
			if err != nil {
				t.Fatal(err)
			}
			starts := srv.Calls("thread/start")
			if len(starts) != 1 {
				t.Fatalf("the session did not start once: %s", starts)
			}
			start := paramsOf(t, starts[0])
			config := start["config"].(map[string]any)
			if start["sandbox"] != tc.sandbox || config["sandbox_workspace_write.network_access"] != tc.network ||
				!reflect.DeepEqual(config["sandbox_workspace_write.writable_roots"], tc.roots) {
				t.Fatalf("the session started with sandbox %v, network %v, roots %v; want %v, %v, %v",
					start["sandbox"], config["sandbox_workspace_write.network_access"],
					config["sandbox_workspace_write.writable_roots"], tc.sandbox, tc.network, tc.roots)
			}
			if start["approvalsReviewer"] != nil {
				t.Fatalf("an ordinary session got a helper's reviewer: %v", start)
			}
			warned := strings.Contains(detail, "WARNING")
			if tc.warn != "" && !strings.Contains(detail, "WARNING: "+tc.warn) || tc.warn == "" && warned {
				t.Fatalf("the open said %q, want the warning %q", detail, tc.warn)
			}
		})
	}
}

// A restart of an ordinary session with explicit Codex options reads
// config.toml's network and roots again, and says which roots stay behind
// only where the replacement writes at all.
func TestAnOrdinaryCodexRestartKeepsThePersonsNetworkAndRoots(t *testing.T) {
	for _, sandbox := range []string{"workspace-write", "read-only"} {
		t.Run(sandbox, func(t *testing.T) {
			srv, e, p := onCodexContour(t)
			newTmuxStub(t, nil, "")
			srv.Sandbox("", map[string]any{"network_access": false, "writable_roots": []any{"/srv/shared"}})
			p.Codex = &action.CodexLaunch{Sandbox: sandbox}
			if sandbox == "workspace-write" {
				p.Codex.WritableRoots = []string{"/srv/explicit"}
			}
			_, name, err := open(t, e, p, `{"agent":"codex"}`)
			if err != nil {
				t.Fatal(err)
			}
			old := e.codex.Find(name)[0]
			detail, _, err := e.Restart(t.Context(), action.Request{Kind: action.SessionRestart, Target: name,
				Conversation: old.ID, Project: p})
			if err != nil {
				t.Fatal(err)
			}
			starts := srv.Calls("thread/start")
			if len(starts) != 2 {
				t.Fatalf("the session was not restarted: %s", starts)
			}
			want := any(nil)
			if sandbox == "workspace-write" {
				want = []any{"/srv/shared", "/srv/explicit"}
			}
			for _, raw := range starts {
				start := paramsOf(t, raw)
				config := start["config"].(map[string]any)
				if start["sandbox"] != sandbox || config["sandbox_workspace_write.network_access"] != nil ||
					!reflect.DeepEqual(config["sandbox_workspace_write.writable_roots"], want) {
					t.Fatalf("the session started as %v, want sandbox %s and roots %v", start, sandbox, want)
				}
			}
			if strings.Contains(detail, rootsNotCarried) != (sandbox == "workspace-write") {
				t.Fatalf("the restart said %q", detail)
			}
		})
	}
}
