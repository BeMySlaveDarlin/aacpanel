package executor

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	registry "aacpanel/internal/contours"
)

func sessionsDirs() []string {
	if p := os.Getenv(sessionsEnv); p != "" {
		return []string{p}
	}
	var out []string
	for i, conf := range registry.ConfigDirs() {
		dir := filepath.Join(conf, "sessions")
		if i > 0 {
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				continue
			}
		}
		if !slices.Contains(out, dir) {
			out = append(out, dir)
		}
	}
	return out
}

func contours() []registry.Contour {
	return registry.Load()
}

func configFileFor(dir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	for _, c := range contours() {
		if c.Prefix != "*" && !strings.HasPrefix(strings.TrimRight(dir, "/")+"/", c.Prefix) {
			continue
		}
		if c.Config == filepath.Join(home, ".claude") {
			break
		}
		return filepath.Join(c.Config, ".claude.json"), nil
	}
	return filepath.Join(home, ".claude.json"), nil
}

const sessionsEnv = "AACP_SESSIONS"

const registryEnv = registry.RegistryEnv
