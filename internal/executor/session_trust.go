package executor

import (
	"fmt"
	"path/filepath"

	"aacpanel/internal/claudecfg"
)

func projectTrusted(dir string) (bool, error) {
	path, err := configFileFor(dir)
	if err != nil {
		return false, err
	}
	return claudecfg.Trusted(path, dir)
}

func trustProject(dir string) (bool, error) {
	path, err := configFileFor(dir)
	if err != nil {
		return false, err
	}
	added, err := claudecfg.Grant(path, dir)
	if err != nil {
		return false, fmt.Errorf("trust for directory %s: %w", dir, err)
	}
	return added > 0, nil
}

// The launcher gives an explicit config directory to Claude when no account
// wrapper owns the route. Trust must be written where that launch reads it,
// particularly after a project changes accounts while its cwd stays put.
func projectTrustFile(p project) (string, error) {
	if p.ConfigDir != "" && p.ClaudeBin == "" {
		return filepath.Join(p.ConfigDir, ".claude.json"), nil
	}
	return configFileFor(p.Path)
}
func projectTrustedFor(p project) (bool, error) {
	file, err := projectTrustFile(p)
	if err != nil {
		return false, err
	}
	return claudecfg.Trusted(file, p.Path)
}
func trustProjectFor(p project) (bool, error) {
	file, err := projectTrustFile(p)
	if err != nil {
		return false, err
	}
	added, err := claudecfg.Grant(file, p.Path)
	return added > 0, err
}
