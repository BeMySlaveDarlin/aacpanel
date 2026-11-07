package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

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
