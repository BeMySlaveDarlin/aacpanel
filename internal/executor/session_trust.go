package executor

import (
	"fmt"

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
