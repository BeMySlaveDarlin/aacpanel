package executor

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

func backgroundReadOnly(target string) error {
	return fmt.Errorf("%s is a Claude Code background session: open its attachment terminal, stop it, or move it to the panel before writing", target)
}

func backgroundClaude() (string, error) {
	if path := nativeClaude(); executableFile(path) {
		return path, nil
	}
	return "", fmt.Errorf("the native Claude Code binary is not installed at %s", nativeClaude())
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func (e *Executor) backgroundAction(ctx context.Context, req action.Request) (string, error) {
	s, err := findNamedSession(req.Target)
	if err != nil {
		return "", err
	}
	if s.Kind != "bg" {
		return "", fmt.Errorf("%s is not a Claude Code background session", req.Target)
	}
	if raw, err := hex.DecodeString(s.JobID); err != nil || len(raw) != 4 {
		return "", fmt.Errorf("background session %s has no valid job id", s.Name)
	}
	binary, err := backgroundClaude()
	if err != nil {
		return "", fmt.Errorf("finding Claude Code: %w", err)
	}
	if req.Kind == action.SessionBGAttach {
		if !action.TermID(req.Terminal) {
			return "", fmt.Errorf("a background attachment needs a panel terminal id")
		}
		quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\\''") + "'" }
		command := "exec env CLAUDE_CONFIG_DIR=" + quote(s.Config) + " " + quote(binary) + " attach " + quote(s.JobID)
		return e.termStart(ctx, req.Terminal, s.CWD, command)
	}
	if !e.restarting.take(s.Name) {
		return "", fmt.Errorf("session %s is already stopping or moving", s.Name)
	}
	defer e.restarting.give(s.Name)
	var want *project
	if req.Kind == action.SessionBGAdopt {
		if req.Project == nil || s.SessionID == "" {
			return "", fmt.Errorf("moving a background session needs its project and conversation")
		}
		copy := *req.Project
		copy.Path, copy.Session, copy.ClaudeBin, copy.ConfigDir = s.CWD, s.Name, binary, s.Config
		// The real binary takes its account explicitly. A profile wrapper may
		// choose another account from cwd, and cannot own this handover.
		copy.Launch, err = switchedLaunch(copy.Launch, launcher.TransportStream, carried{})
		if err != nil {
			return "", err
		}
		p, err := chooseProject(s.Name, &copy)
		if err != nil {
			return "", err
		}
		p.ConfigOverride = true
		want = &p
	}
	start, _ := procStart(s.PID)
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	cmd := exec.CommandContext(call, binary, "stop", s.JobID)
	cmd.Env = append(cleanEnv(os.Environ()), "CLAUDE_CONFIG_DIR="+s.Config)
	cmd.Dir = s.CWD
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		return "", fmt.Errorf("claude stop %s: %w: %s", s.JobID, err, shortenLines(strings.TrimSpace(string(out))))
	}
	detail := "background session " + s.Name + " stopped"
	if text := strings.TrimSpace(string(out)); text != "" {
		detail += "; " + text
	}
	if want == nil {
		return detail, nil
	}
	// stop can return before the PTY host reaps its child. Resuming now would
	// put two processes on the same transcript.
	deadline := time.NewTimer(e.softWait())
	defer deadline.Stop()
	tick := time.NewTicker(e.pollEvery())
	defer tick.Stop()
	for {
		now, alive := procStart(s.PID)
		if !alive || now != start || exited(s.PID) {
			break
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("the background process has not exited; no resume was started: %w", ctx.Err())
		case <-deadline.C:
			return "", fmt.Errorf("the background process has not exited; no resume was started")
		case <-tick.C:
		}
	}
	e.projectMoves.Lock()
	defer e.projectMoves.Unlock()
	resumed, err := e.resumeChosen(ctx, *want, action.Request{Kind: action.SessionResume, Target: s.Name,
		Resume: s.SessionID, Silent: true})
	if err != nil {
		return "", fmt.Errorf("%s; resume failed: %w", detail, err)
	}
	return detail + "; " + resumed, nil
}
