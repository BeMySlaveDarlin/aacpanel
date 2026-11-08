package store

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

// UseProjectRoots sets the directories inside which the map's paths are allowed.
func (s *Store) UseProjectRoots(roots []string) error {
	clean := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return fmt.Errorf("the path root %q is not absolute", root)
		}
		root = filepath.Clean(root)
		if root == string(filepath.Separator) {
			return errors.New(`the root "/" allows any path — that is not a root`)
		}
		clean = append(clean, root)
	}
	s.projectRoots = clean
	return nil
}

// ProjectRoots returns the roots that were set.
func (s *Store) ProjectRoots() []string {
	return append([]string(nil), s.projectRoots...)
}

// CheckProjectPath checks a project path against the roots and returns it normalised.
func CheckProjectPath(path string, roots []string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", badRequest("the path is empty")
	}
	if len(path) > profilePathMax {
		return "", badRequest("the path is longer than %d characters", profilePathMax)
	}
	if strings.ContainsRune(path, 0) {
		return "", badRequest("the path holds a NUL byte")
	}
	if !filepath.IsAbs(path) {
		return "", badRequest("the path %q is not absolute", path)
	}
	if len(roots) == 0 {
		return "", errors.New("no path roots are set: map paths cannot be written")
	}

	clean := filepath.Clean(path)
	if !inside(clean, roots) {
		return "", badRequest("the path %s is outside the allowed roots (%s)", clean, strings.Join(roots, ", "))
	}

	real, err := resolve(clean)
	if err != nil {
		return "", badRequest("the path %s was not resolved: %v", clean, err)
	}
	if !inside(real, roots) {
		return "", badRequest("the path %s is a link leading to %s — outside the allowed roots", clean, real)
	}
	return clean, nil
}

func resolve(path string) (string, error) {
	rest := ""
	for cur := path; ; {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func inside(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Store) checkPath(path string) (string, error) {
	return CheckProjectPath(path, s.projectRoots)
}

// Former directories identify conversations, not places the executor may open:
// they can outlive both the directory and the configured roots.
func previousProjectPaths(edit *[]string, kept []string, before, current string) ([]string, error) {
	paths := kept
	if edit != nil {
		paths = *edit
	}
	paths = slices.Clone(paths)
	if before != "" && before != current {
		paths = append(paths, before)
	}
	out := []string{}
	for _, raw := range paths {
		p := strings.TrimSpace(raw)
		if !filepath.IsAbs(p) || len(p) > profilePathMax || strings.ContainsAny(p, "\x00\n\r") {
			return nil, badRequest("the previous project path %q must be an absolute path without NUL bytes or line breaks, at most %d characters", p, profilePathMax)
		}
		p = filepath.Clean(p)
		if p != current && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// CheckConfigDir checks the form of a contour's configuration directory.
func CheckConfigDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if len(path) > profilePathMax {
		return "", badRequest("the path to the profile config directory is longer than %d characters", profilePathMax)
	}
	if strings.ContainsRune(path, 0) {
		return "", badRequest("the path to the profile config directory holds a NUL byte")
	}
	if strings.ContainsAny(path, "\n\r") {
		return "", badRequest("the path to the profile config directory holds a line break")
	}
	if !filepath.IsAbs(path) {
		return "", badRequest("the path to the profile config directory %q is not absolute: the service in the container has its own working directory, and a relative path would resolve against it", path)
	}
	return filepath.Clean(path), nil
}

// CheckClaudeBin checks the form of the path to the claude binary.
func CheckClaudeBin(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if len(path) > profilePathMax {
		return "", badRequest("the path to claude is longer than %d characters", profilePathMax)
	}
	if strings.ContainsRune(path, 0) {
		return "", badRequest("the path to claude holds a NUL byte")
	}
	if strings.ContainsAny(path, "\n\r") {
		return "", badRequest("the path to claude holds a line break")
	}
	if !filepath.IsAbs(path) {
		return "", badRequest("the path to claude %q is not absolute: otherwise PATH picks it, and PATH differs between the unit and your shell", path)
	}
	return filepath.Clean(path), nil
}
