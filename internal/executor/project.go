package executor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const projectDirMode = 0o755

func projectCreate(path string) (string, error) {
	dir, made, err := ensureProjectDir(path)
	if err != nil {
		return "", err
	}
	granted, err := trustProject(dir)
	if err != nil {
		return "", err
	}

	trust := "trust for it was granted by the panel — claude itself never asked about it"
	if !granted {
		trust = "trust for it was already there"
	}
	switch len(made) {
	case 0:
		return fmt.Sprintf("directory %s was already on disk and was left as it is; %s", dir, trust), nil
	case 1:
		return fmt.Sprintf("directory %s created; %s", dir, trust), nil
	default:
		return fmt.Sprintf("directories %s created; %s", strings.Join(made, ", "), trust), nil
	}
}

func ensureProjectDir(path string) (string, []string, error) {
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", nil, fmt.Errorf("project path %q is not absolute", path)
	}
	clean := filepath.Clean(path)
	roots := projectRoots()
	if len(roots) == 0 {
		return "", nil, fmt.Errorf("no project roots are set: neither %s nor the home directory", projectRootsEnv)
	}
	if !insideRoots(clean, roots) {
		return "", nil, fmt.Errorf("path %s is outside the allowed roots (%s)", clean, strings.Join(roots, ", "))
	}

	var missing []string
	rest, real := "", ""
	for cur := clean; ; {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			real = filepath.Join(resolved, rest)
			break
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
			return "", nil, fmt.Errorf("path %s was not resolved: %w", cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			real = clean
			break
		}
		missing = append(missing, cur)
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
	if !insideRoots(real, roots) {
		return "", nil, fmt.Errorf("path %s is a link leading to %s — outside the allowed roots", clean, real)
	}

	if len(missing) == 0 {
		info, err := os.Stat(clean)
		if err != nil {
			return "", nil, fmt.Errorf("directory %s cannot be read: %w", clean, err)
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("%s is not a directory: a session would have nothing to open in it", clean)
		}
		return clean, nil, nil
	}
	if err := os.MkdirAll(clean, projectDirMode); err != nil {
		return "", nil, fmt.Errorf("directory %s cannot be created: %w", clean, err)
	}
	slices.Reverse(missing)
	return clean, missing, nil
}
