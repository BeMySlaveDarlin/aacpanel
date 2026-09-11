// Package check holds the front-end checks.
package check

import "path/filepath"

const (
	webDir  = ".."
	repoDir = "../.."
)

func webPath(rel string) string {
	return filepath.Join(webDir, filepath.FromSlash(rel))
}

func repoPath(rel string) string {
	return filepath.Join(repoDir, filepath.FromSlash(rel))
}

func webRel(path string) string {
	rel, err := filepath.Rel(webDir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
