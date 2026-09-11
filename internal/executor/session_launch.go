package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	return runInUnit(ctx, bin, launchFlag, "starting the session", launcher.Spec{
		Dir: p.Path, Session: name, Resume: resume, Launch: p.Launch,
		ClaudeBin: p.ClaudeBin, ConfigDir: p.ConfigDir,
	})
}

func (e *Executor) runWindowOpener(ctx context.Context, dir, session string) (launcher.Report, error) {
	bin, err := launcherPath()
	if err != nil {
		return launcher.Report{}, err
	}
	return runInUnit(ctx, bin, windowFlag, "opening the window",
		launcher.WindowSpec{Dir: dir, Session: session})
}

func runInUnit(ctx context.Context, bin, mode, what string, spec any) (launcher.Report, error) {
	body, err := json.Marshal(spec)
	if err != nil {
		return launcher.Report{}, fmt.Errorf("the task for the launcher was not built: %w", err)
	}

	env := cleanEnv(os.Environ())
	cmd := exec.CommandContext(ctx, systemdRunPath(), launchArgs(env, bin, mode)...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(body)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs

	if err := cmd.Run(); err != nil {
		if text := strings.TrimSpace(errs.String()); text != "" {
			return launcher.Report{}, fmt.Errorf("%s: %s", what, shortenLines(text))
		}
		return launcher.Report{}, fmt.Errorf("%s: %w", what, err)
	}

	var rep launcher.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		return launcher.Report{}, fmt.Errorf("the launcher answer was not parsed (%q): %w",
			shortenLines(strings.TrimSpace(out.String())), err)
	}
	return rep, nil
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

func launchArgs(env []string, bin, mode string) []string {
	args := []string{
		"--user",
		"--quiet",
		"--wait",
		"--pipe",
		"--collect",
		"--property=KillMode=process",
	}
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); passEnv(name) {
			args = append(args, "--setenv="+kv)
		}
	}
	return append(args, "--", bin, mode)
}

const (
	launchFlag = "-launch"
	windowFlag = "-window"
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
