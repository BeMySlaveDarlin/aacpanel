package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"aacpanel/internal/hostcfg"
	"aacpanel/internal/launcher"
)

const (
	launcherEnv       = "AACP_LAUNCHER"
	systemdRunEnv     = "AACP_SYSTEMD_RUN"
	defaultSystemdRun = "systemd-run"
)

func (e *Executor) runLauncher(ctx context.Context, p project, resume string) (launcher.Report, error) {
	bin, err := launcherPath()
	if err != nil {
		return launcher.Report{}, err
	}
	name := p.RC
	if name == "" {
		name = p.Session
	}
	// The holder of a session on the stream is all its unit keeps, and the
	// unit is held to the ceilings of one session. The unit of a session in
	// tmux keeps the tmux server when this launch starts it, and with it every
	// session started on that server later: the ceilings of one would be those
	// of them all, so it gets the slice alone.
	var held hostcfg.Ceilings
	if launcher.OnStream(p.Launch) {
		held = e.memory.Session
	}
	return e.runInUnit(ctx, bin, launchFlag, "starting the session", held, launcher.Spec{
		Dir: p.Path, Session: name, Resume: resume, Launch: p.Launch,
		ClaudeBin: p.ClaudeBin, ConfigDir: p.ConfigDir, Parent: p.Parent, Role: p.Role, IntentKey: p.IntentKey,
	})
}

func (e *Executor) runWindowOpener(ctx context.Context, srv tmuxServer, dir, session string) (launcher.Report, error) {
	bin, err := launcherPath()
	if err != nil {
		return launcher.Report{}, err
	}
	return e.runInUnit(ctx, bin, windowFlag, "opening the window", hostcfg.Ceilings{},
		launcher.WindowSpec{Dir: dir, Session: session, Socket: string(srv)})
}

// runTermStarter starts a terminal of the panel. The first one starts the
// tmux server of them all in its unit, so the unit gets the slice alone, as a
// session in tmux does.
func (e *Executor) runTermStarter(ctx context.Context, dir, id string) (launcher.Report, error) {
	bin, err := launcherPath()
	if err != nil {
		return launcher.Report{}, err
	}
	return e.runInUnit(ctx, bin, termFlag, "starting the terminal", hostcfg.Ceilings{},
		launcher.TermSpec{Dir: dir, Session: id, Socket: string(panelTmux)})
}

func (e *Executor) runInUnit(ctx context.Context, bin, mode, what string, held hostcfg.Ceilings, spec any) (launcher.Report, error) {
	var rep launcher.Report
	err := e.runUnit(ctx, bin, mode, what, held, spec, &rep)
	return rep, err
}

func (e *Executor) runUnit(ctx context.Context, bin, mode, what string, held hostcfg.Ceilings, spec, report any) error {
	body, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("the task for the launcher was not built: %w", err)
	}

	env := cleanEnv(os.Environ())
	cmd := exec.CommandContext(ctx, systemdRunPath(), launchArgs(env, bin, mode, e.cores, held)...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(body)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs

	if err := cmd.Run(); err != nil {
		if text := strings.TrimSpace(errs.String()); text != "" {
			return fmt.Errorf("%s: %s", what, shortenLines(text))
		}
		return fmt.Errorf("%s: %w", what, err)
	}

	if err := json.Unmarshal(out.Bytes(), report); err != nil {
		return fmt.Errorf("the launcher answer was not parsed (%q): %w",
			shortenLines(strings.TrimSpace(out.String())), err)
	}
	return nil
}

func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDE") || unitEnv(kv) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func unitEnv(kv string) bool {
	name, _, _ := strings.Cut(kv, "=")
	switch name {
	case "INVOCATION_ID", "JOURNAL_STREAM", "NOTIFY_SOCKET", "MANAGERPID",
		"LISTEN_FDS", "LISTEN_PID", "LISTEN_FDNAMES", "SYSTEMD_EXEC_PID":
		return true
	}
	return false
}

// launchArgs start the launcher in a unit of its own in the agents' slice
// and wait for its report. What it starts stays in the unit — the holder of a
// session on the stream, a window, the tmux server when this launch is the
// one that starts it — and inherits the cores and the ceilings the unit is
// held to.
func launchArgs(env []string, bin, mode, cores string, held hostcfg.Ceilings) []string {
	killMode := "process"
	if mode == summaryFlag {
		killMode = "control-group"
	}
	args := []string{
		"--user",
		"--quiet",
		"--wait",
		"--pipe",
		"--collect",
		"--property=KillMode=" + killMode,
	}
	args = append(args, inSlice(held)...)
	args = append(args, affinity(cores)...)
	if mode == summaryFlag {
		args = append(args, "--property=RuntimeMaxSec=120s")
	}
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); passEnv(name) {
			args = append(args, "--setenv="+kv)
		}
	}
	return append(args, "--", bin, mode)
}

// affinity is the property that holds a unit to cores, and nothing for no
// cores: a unit started without it runs where the system puts it.
func affinity(cores string) []string {
	if cores == "" {
		return nil
	}
	return []string{"--property=CPUAffinity=" + cores}
}

const (
	launchFlag  = "-launch"
	windowFlag  = "-window"
	termFlag    = "-term"
	summaryFlag = "-summary"
)

func passEnv(name string) bool {
	switch name {
	case "DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "XDG_SESSION_TYPE",
		"DBUS_SESSION_BUS_ADDRESS", "HOME", "PATH", "USER", "LOGNAME", "SHELL":
		return true
	case "LANG", "LANGUAGE":
		return true
	}
	return strings.HasPrefix(name, "AACP_") || strings.HasPrefix(name, "LC_")
}

func describeConsole(rep launcher.Report) string {
	detail := "session " + rep.Session + " started"
	if rep.Intent != "" {
		detail += "; opening message: " + shortIntent(rep.Intent)
	}
	if len(rep.Leaks) > 0 {
		detail += "; WARNING: CLAUDE* leaked (" + strings.Join(rep.Leaks, ", ") +
			") — no transcript will be written"
	}
	for _, w := range rep.Warnings {
		detail += "; WARNING: " + w
	}
	return detail
}

func systemdRunPath() string {
	if p := os.Getenv(systemdRunEnv); p != "" {
		return p
	}
	return defaultSystemdRun
}

func launcherPath() (string, error) {
	if p := os.Getenv(launcherEnv); p != "" {
		return p, nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not find our own binary to start the session: %w", err)
	}
	return self, nil
}

func shortenLines(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return strings.Join(lines, "; ")
}

func shortIntent(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const max = 120
	if runes := []rune(flat); len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return flat
}
