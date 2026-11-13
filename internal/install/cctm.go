package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (in *Install) cctmEnabled() bool {
	dirs := in.accounts()
	for _, account := range ClaudeHomes(in.M, in.home()) {
		dirs = append(dirs, account.Dir)
	}
	for _, dir := range dirs {
		raw, err := in.M.ReadFile(in.settingsPath(dir))
		if err != nil {
			continue
		}
		var settings struct {
			Plugins map[string]bool `json:"enabledPlugins"`
		}
		if json.Unmarshal(raw, &settings) != nil {
			continue
		}
		for name, enabled := range settings.Plugins {
			if enabled && strings.SplitN(name, "@", 2)[0] == "cc-task-manager" {
				return true
			}
		}
	}
	return false
}

func (in *Install) cctmPath() string { return filepath.Join(filepath.Dir(in.execPath()), "cctm") }

func (in *Install) cctmLink() bool {
	path := in.cctmPath()
	to, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(to) {
		to = filepath.Join(filepath.Dir(path), to)
	}
	return (&checker{m: in.M}).same(to, in.execPath())
}

func (in *Install) cctmStep() *Step {
	return &Step{ID: "cctm", Title: "Task registry command",
		Done: func(r *Run) (bool, error) {
			if !in.cctmEnabled() || in.cctmLink() {
				return true, nil
			}
			if _, err := os.Lstat(in.cctmPath()); err == nil {
				r.Say(Warn, in.short(in.cctmPath())+" belongs to another command and is left in place; cctm cannot guard panel task registries")
				return true, nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
			return false, nil
		},
		Apply: func(r *Run) error {
			if err := in.makeDirs(r, filepath.Dir(in.cctmPath())); err != nil {
				return err
			}
			if err := r.Once(File, in.cctmPath(), "created"); err != nil {
				return err
			}
			return os.Symlink("aacpanel-exec", in.cctmPath())
		},
		Verify: func(r *Run) error {
			if !in.cctmLink() {
				return &Failed{Diagnosis: fmt.Sprintf("%s does not link to the executor", in.short(in.cctmPath()))}
			}
			r.Say(Pass, in.short(in.cctmPath())+" sends panel projects to their task registry")
			return nil
		},
		Undo: UndoKind,
	}
}
