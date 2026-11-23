package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"aacpanel/internal/codex"
)

// presetSandbox is the sandbox of a helper that neither the call, the map nor
// config.toml of its home gives one.
const presetSandbox = "workspace-write"

// sandboxModes are the sandbox modes thread/start takes.
var sandboxModes = []string{"read-only", "workspace-write", "danger-full-access"}

// prepareCodexHelper applies the same permissions to every helper launch and
// returns what the start should say about them; the link retains the
// resulting parameters for a daemon's later recovery, so a recovered helper
// keeps the sandbox it started with. The person's settings win and the
// panel's preset fills only what none of them says: the sandbox is the one
// already chosen — by the call, the map or the thread a restart replaces —
// else the one config.toml gives the helper's directory, else workspace-write.
// The thread gets that sandbox by name, read-only and full access with no
// workspace overrides. A workspace helper's writable roots replace codex's
// list, so they carry config.toml's roots beside the caller's and the
// checkout's. Its network is config.toml's where config.toml has a
// sandbox_workspace_write table, as codex reads that table for an ordinary
// session — off without network_access — and on where it has none.
func prepareCodexHelper(ctx context.Context, link *codex.Link, begin *codex.Begin) []string {
	if begin.Owner.Conversation == "" {
		return nil
	}
	begin.ReviewAutomatically()
	var warns []string
	var home codex.HomeSandbox
	if begin.Sandbox == "" || begin.Sandbox == "workspace-write" {
		read, err := link.HomeSandbox(ctx, begin.CWD)
		if err != nil {
			warns = append(warns, "config.toml of the Codex home was not read, so the helper runs with the panel's "+
				"preset where the call and the map chose nothing — workspace-write with network, without "+
				"config.toml's writable roots: "+err.Error())
		}
		home = read
	}
	if begin.Sandbox == "" && slices.Contains(sandboxModes, home.Mode) {
		begin.Sandbox = home.Mode
	}
	if begin.Sandbox == "" {
		begin.Sandbox = presetSandbox
	}
	begin.Workspace = begin.Sandbox == "workspace-write"
	if !begin.Workspace {
		if begin.Sandbox == "read-only" && len(begin.WritableRoots) > 0 {
			warns = append(warns, "the writable roots are not applied: the helper's sandbox is read-only")
		}
		return warns
	}
	begin.HomeNetwork = home.Table
	begin.WritableRoots = helperWritableRoots(ctx, begin.CWD, append(slices.Clone(home.Roots), begin.WritableRoots...))
	return warns
}

// A worktree needs both git directories: Codex protects its own gitdir even
// when the common directory is writable. Cache defaults cannot depend on Go
// being installed in the executor's PATH; explicit environment values win.
func helperWritableRoots(ctx context.Context, dir string, explicit []string) []string {
	roots := make([]string, 0, len(explicit)+4)
	add := func(path string) {
		path = filepath.Clean(path)
		if filepath.IsAbs(path) && !slices.Contains(roots, path) {
			roots = append(roots, path)
		}
	}
	for _, path := range explicit {
		add(path)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir", "--absolute-git-dir")
	if out, err := cmd.Output(); err == nil {
		for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			add(path)
		}
	}
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		if base, err := os.UserCacheDir(); err == nil {
			cache = filepath.Join(base, "go-build")
		}
	}
	mod := os.Getenv("GOMODCACHE")
	if mod == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			if home, err := os.UserHomeDir(); err == nil {
				gopath = filepath.Join(home, "go")
			}
		}
		if paths := filepath.SplitList(gopath); len(paths) > 0 {
			mod = filepath.Join(paths[0], "pkg", "mod")
		}
	}
	for _, path := range []string{cache, mod} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			add(path)
		}
	}
	return roots
}
