package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/session"
	"aacpanel/internal/taskreg"
)

const panelRegistryHint = "This project's task registry is in the panel.\nUse task_*/handoff_* tools (Claude or Codex), or aacpanel-exec task ... / aacpanel-exec handoff ... ."

func runCCTM(args []string, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "cctm: cannot read the current directory: "+err.Error())
		return 1
	}
	project, lookupErr := cctmProject(cwd)
	if lookupErr == nil && project.Writer == taskreg.PanelWriter {
		fmt.Fprintln(stderr, panelRegistryHint)
		return 1
	}
	program, err := nextCCTM()
	if err != nil {
		fmt.Fprintln(stderr, "cctm: "+err.Error())
		return 1
	}
	if program == "" {
		if lookupErr != nil {
			fmt.Fprintln(stderr, "The panel did not answer; nothing was written.")
		} else {
			fmt.Fprintln(stderr, panelRegistryHint)
		}
		return 1
	}
	// Exec preserves the caller's streams, signals and exit status; a child
	// process would make the shim another owner of a plugin command's lifetime.
	if err := syscall.Exec(program, append([]string{program}, args...), os.Environ()); err != nil {
		fmt.Fprintln(stderr, "cctm: cannot run the plugin: "+err.Error())
		return 1
	}
	return 0
}

func cctmProject(cwd string) (taskreg.Project, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := json.Marshal(struct {
		CWD string `json:"cwd"`
	}{cwd})
	if err != nil {
		return taskreg.Project{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(session.PanelURL(), "/")+"/api/tasks/writer", bytes.NewReader(raw))
	if err != nil {
		return taskreg.Project{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return taskreg.Project{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return taskreg.Project{}, fmt.Errorf("task writer: %s", resp.Status)
	}
	var project taskreg.Project
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&project); err != nil {
		return project, err
	}
	if project.Writer != taskreg.PanelWriter && project.Writer != taskreg.FilesWriter && !(project.ID == 0 && project.Writer == "") {
		return project, fmt.Errorf("unknown task writer %q", project.Writer)
	}
	return project, nil
}

func nextCCTM() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidate, err := filepath.Abs(filepath.Join(dir, "cctm"))
		if err != nil {
			continue
		}
		if _, err := exec.LookPath(candidate); err != nil {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		// PATH can hold both the symlink and another name of the executor.
		// Comparing names alone recurses into the shim indefinitely.
		if resolved == self || os.SameFile(selfInfo, info) {
			continue
		}
		return candidate, nil
	}
	return "", nil
}
