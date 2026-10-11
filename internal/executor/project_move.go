package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"aacpanel/internal/action"
	registry "aacpanel/internal/contours"
	"aacpanel/internal/historymove"
	"aacpanel/internal/launcher"
	"aacpanel/internal/stream"
)

func (e *Executor) projectMove(ctx context.Context, req action.Request) (string, error) {
	if err := req.Validate(); err != nil {
		return "", err
	}
	e.projectMoves.Lock()
	defer e.projectMoves.Unlock()
	m := req.Move
	configs := registry.ConfigDirs()
	for _, config := range []string{m.From, m.To} {
		if !slices.Contains(configs, config) {
			return "", fmt.Errorf("config directory %s is not a known contour", config)
		}
		st, err := os.Lstat(config)
		if err != nil {
			return "", err
		}
		if !st.IsDir() {
			return "", fmt.Errorf("config directory %s must be a plain directory", config)
		}
	}
	fromReal, err := filepath.EvalSymlinks(m.From)
	if err != nil {
		return "", err
	}
	toReal, err := filepath.EvalSymlinks(m.To)
	if err != nil {
		return "", err
	}
	if fromReal == toReal || strings.HasPrefix(fromReal, toReal+"/") || strings.HasPrefix(toReal, fromReal+"/") {
		return "", fmt.Errorf("contour config directories must be separate, non-nested directories")
	}
	if m.Finish != "" {
		if err := historymove.Finish(m.ID, req.Target, m.From, m.To, m.Finish == "commit"); err != nil {
			return "", err
		}
		return "project move " + m.Finish + " finished", nil
	}
	if _, err := checkProjectDir(req.Target); err != nil {
		return "", err
	}
	// A directory router may discard CLAUDE_CONFIG_DIR. Moving its archive
	// alone would make Resume start in the old account with no transcript.
	if os.Getenv(registry.RegistryEnv) != "" {
		real, err := filepath.EvalSymlinks(req.Target)
		if err != nil {
			return "", err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		routed := registry.DefaultConfig(home)
		for _, route := range registry.Load() {
			if route.Prefix != "*" && !strings.HasPrefix(real+"/", route.Prefix) {
				continue
			}
			routed = route.Config
			break
		}
		if filepath.Clean(routed) != m.To {
			return "", fmt.Errorf("the host router sends %s to contour %s; route this project to %s before moving its history", req.Target, registry.ContourOf(routed), registry.ContourOf(m.To))
		}
	}
	quiet := func() error { return projectQuiet(req.Target) }
	if err := quiet(); err != nil {
		return "", err
	}
	move, err := historymove.Prepare(m.ID, req.Target, m.From, m.To)
	if err != nil {
		return "", err
	}
	if err := move.Apply(ctx, quiet); err != nil {
		return "", err
	}
	return fmt.Sprintf("Claude history of %s moved from %s to %s; Codex threads stay in their account", req.Target, registry.ContourOf(m.From), registry.ContourOf(m.To)), nil
}

func belongsToProject(cwd, project string) bool {
	cwd = filepath.Clean(cwd)
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	if real, err := filepath.EvalSymlinks(project); err == nil {
		project = real
	}
	if cwd == project || strings.HasPrefix(cwd, project+string(filepath.Separator)) {
		return true
	}
	// A linked worktree can stand outside the repository directory.
	raw, err := os.ReadFile(filepath.Join(cwd, ".git"))
	if err != nil {
		return false
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir:"))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(cwd, gitdir)
	}
	return strings.HasPrefix(filepath.Clean(gitdir), filepath.Join(project, ".git", "worktrees")+string(filepath.Separator))
}

func projectQuiet(project string) error {
	dirs := sessionsDirs()
	dirs = append(dirs, stream.Dir())
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot check live sessions in %s: %w", dir, err)
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			var live struct {
				PID       int    `json:"pid"`
				Holder    int    `json:"holder"`
				Name      string `json:"name"`
				CWD       string `json:"cwd"`
				ProcStart string `json:"procStart"`
				Contour   string `json:"contour"`
				Agent     string `json:"agent"`
			}
			if err := json.Unmarshal(raw, &live); err != nil {
				return fmt.Errorf("cannot check session file %s: %w", entry.Name(), err)
			}
			if !belongsToProject(live.CWD, project) {
				continue
			}
			pid := live.PID
			if live.Agent == "codex" {
				pid = live.Holder
			}
			start, alive := procStart(pid)
			if !alive || (live.ProcStart != "" && start != live.ProcStart) {
				continue
			}
			if live.Name == "" {
				live.Name = "pid " + strconv.Itoa(pid)
			}
			if live.Contour == "" {
				live.Contour = registry.ContourOf(filepath.Dir(dir))
			}
			return fmt.Errorf("close live session %s in contour %s before moving this project", live.Name, live.Contour)
		}
	}
	for _, live := range launcher.Open() {
		if belongsToProject(live.Dir, project) {
			return fmt.Errorf("close live session %s before moving this project", live.Session)
		}
	}
	return nil
}
