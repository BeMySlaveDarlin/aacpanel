package hostcfg

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectRootsEnv lists the directories projects may be created and opened in.
const ProjectRootsEnv = "AACP_PROJECT_ROOTS"

// ProjectRoots returns the project roots of this machine.
func ProjectRoots(home string) []string {
	raw := described(ProjectRootsEnv)
	if strings.TrimSpace(raw) == "" {
		home = strings.TrimSpace(home)
		if home == "" {
			return nil
		}
		return []string{filepath.Clean(home)}
	}
	var out []string
	for _, root := range strings.Split(raw, ":") {
		if root = strings.TrimSpace(root); root != "" {
			out = append(out, root)
		}
	}
	return out
}

func described(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return parseEnvFile(path())[name]
}
