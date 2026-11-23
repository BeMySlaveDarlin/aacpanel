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

// prepareCodexLaunch settles what the panel overrides of a thread's sandbox
// and returns what the start should say about it; the link retains the
// resulting parameters for a daemon's later recovery, so a recovered thread
// keeps the sandbox it started with. The person's settings win and the
// panel's values fill only what none of them says.
//
// A helper's sandbox is the one already chosen — by the call, the map or the
// thread a restart replaces — else the one config.toml gives the helper's
// directory, else workspace-write; the thread gets it by name, read-only and
// full access with no workspace overrides. An ordinary session passes on only
// the sandbox of the call or the map, config.toml deciding where they say
// nothing, and gets workspace overrides only where explicit Codex options of
// its start meet workspace-write: begin.Workspace says so on the way in.
//
// The writable roots of a workspace thread replace codex's list, so they carry
// config.toml's roots beside the explicit ones, and a helper's the checkout's
// as well. Its network is config.toml's where config.toml has a
// sandbox_workspace_write table, as codex reads that table for an ordinary
// session — off without network_access — and on where it has none.
func prepareCodexLaunch(ctx context.Context, link *codex.Link, begin *codex.Begin) []string {
	helper := begin.Owner.Conversation != ""
	if !helper && !begin.Workspace {
		return nil
	}
	if helper {
		begin.ReviewAutomatically()
	}
	var warns []string
	var home codex.HomeSandbox
	if begin.Sandbox == "" || begin.Sandbox == "workspace-write" {
		read, err := link.HomeSandbox(ctx, begin.CWD)
		if err != nil {
			warns = append(warns, "config.toml of the Codex home was not read, so the panel's preset fills what the "+
				"call and the map leave — workspace-write with network, without config.toml's writable roots: "+
				err.Error())
		}
		home = read
	}
	if helper {
		if begin.Sandbox == "" && slices.Contains(sandboxModes, home.Mode) {
			begin.Sandbox = home.Mode
		}
		if begin.Sandbox == "" {
			begin.Sandbox = presetSandbox
		}
		begin.Workspace = begin.Sandbox == "workspace-write"
	}
	if !begin.Workspace {
		if begin.Sandbox == "read-only" && len(begin.WritableRoots) > 0 {
			warns = append(warns, "the writable roots are not applied: the helper's sandbox is read-only")
		}
		return warns
	}
	begin.HomeNetwork = home.Table
	roots := append(slices.Clone(home.Roots), begin.WritableRoots...)
	if helper {
		begin.WritableRoots = helperWritableRoots(ctx, begin.CWD, roots)
	} else {
		begin.WritableRoots = distinctRoots(roots)
	}
	return warns
}

// distinctRoots keeps every absolute root once, in the order given.
func distinctRoots(paths []string) []string {
	roots := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if filepath.IsAbs(path) && !slices.Contains(roots, path) {
			roots = append(roots, path)
		}
	}
	return roots
}

// A worktree needs both git directories: Codex protects its own gitdir even
// when the common directory is writable. Cache defaults cannot depend on Go
// being installed in the executor's PATH; explicit environment values win.
func helperWritableRoots(ctx context.Context, dir string, explicit []string) []string {
	roots := slices.Clone(explicit)
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir", "--absolute-git-dir")
	if out, err := cmd.Output(); err == nil {
		roots = append(roots, strings.Split(strings.TrimSpace(string(out)), "\n")...)
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
			roots = append(roots, path)
		}
	}
	return distinctRoots(roots)
}
