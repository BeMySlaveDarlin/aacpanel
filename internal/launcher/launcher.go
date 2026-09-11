// Package launcher starts a claude session in tmux and opens a terminal window to it.
package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	registry "aacpanel/internal/contours"
	"aacpanel/internal/hostcfg"
)

// Spec describes what to launch.
type Spec struct {
	Dir       string          `json:"dir"`
	Session   string          `json:"session"`
	Resume    string          `json:"resume,omitempty"`
	Launch    json.RawMessage `json:"launch,omitempty"`
	ClaudeBin string          `json:"claudeBin,omitempty"`
	ConfigDir string          `json:"configDir,omitempty"`
}

// Report describes what came out of a launch.
type Report struct {
	Session  string   `json:"session"`
	Dir      string   `json:"dir"`
	Konsole  int      `json:"konsole"`
	Agent    int      `json:"agent"`
	Leaks    []string `json:"leaks,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Intent   string   `json:"intent,omitempty"`
}

const (
	tmuxEnv         = "AACP_TMUX"
	terminalEnv     = "AACP_TERMINAL"
	defaultTerminal = "konsole"
	terminalAutoEnv = "AACP_TERMINAL_AUTO"
	claudeEnv       = "AACP_CLAUDE"
	startWait       = 15 * time.Second
	startPoll       = 200 * time.Millisecond
)

// Run starts the session and reports what happened.
func Run(ctx context.Context, spec Spec) (Report, error) {
	if spec.Dir == "" || !strings.HasPrefix(spec.Dir, "/") {
		return Report{}, fmt.Errorf("project directory %q is not absolute", spec.Dir)
	}

	if err := checkResume(spec.Dir, spec.Resume); err != nil {
		return Report{}, err
	}

	params, warns := parseParams(spec.Launch)

	choice, err := claudeBin(spec.ClaudeBin)
	if err != nil {
		return Report{}, err
	}
	if choice.Warn != "" {
		warns = append(warns, choice.Warn)
	}
	if choice.OffRoute && params.Intent != "" {
		params.Intent = ""
		warns = append(warns, "the opening message was not sent: the session started outside its "+
			"profile's route, and the first message would have started work in another account")
	}

	configDir, err := contourConfig(spec.ClaudeBin, spec.ConfigDir)
	if err != nil {
		return Report{}, err
	}

	modeWarn, err := checkPermissionMode(ctx, spec.Dir, choice.Path, params.PermissionMode)
	if err != nil {
		return Report{}, err
	}
	if modeWarn != "" {
		warns = append(warns, modeWarn)
	}

	name, err := freeName(spec.Session, takenNames())
	if err != nil {
		return Report{}, err
	}

	host := hostcfg.Load()
	env, envWarns := childEnv(os.Environ(), params, host.Display, host.Lang, configDir)
	warns = append(warns, envWarns...)

	drop, dropWarns, err := writeSessionEnv(env)
	if err != nil {
		return Report{}, err
	}
	defer drop.remove()
	warns = append(warns, dropWarns...)

	windowPID, startWarns, err := start(spec.Dir, name, choice.Path, claudeArgs(name, spec.Resume, params), drop)
	if err != nil {
		return Report{}, err
	}
	warns = append(warns, startWarns...)

	agent, err := waitAgent(ctx, name)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		Session:  name,
		Dir:      spec.Dir,
		Konsole:  konsoleOf(agent),
		Agent:    agent,
		Leaks:    leaks(agent),
		Warnings: warns,
		Intent:   params.Intent,
	}
	if report.Konsole == 0 {
		report.Konsole = windowPID
	}
	return report, nil
}

func tool(env, fallback string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	return fallback
}

func start(dir, name, bin string, args []string, env *sessionEnv) (int, []string, error) {
	if err := startSession(dir, name, bin, args, env.path); err != nil {
		return 0, nil, err
	}
	var warns []string
	if w := enableMouse(name); w != "" {
		warns = append(warns, w)
	}
	if !terminalAuto() {
		return 0, warns, nil
	}
	pid, _, err := openWindow(dir, name, env.vars)
	if err != nil {
		return 0, append(warns, err.Error()+
			"; the session itself did start — open a window to it with the button in the conversation header"), nil
	}
	return pid, warns, nil
}

func enableMouse(name string) string {
	tmuxBin := tool(tmuxEnv, "tmux")
	out, err := exec.Command(tmuxBin, "set-option", "-t", name, "mouse", "on").CombinedOutput()
	if err == nil {
		return ""
	}
	msg := fmt.Sprintf("mouse support was not enabled for session %s (%s): %v", name, tmuxBin, err)
	if said := strings.TrimSpace(string(out)); said != "" {
		msg += ": " + said
	}
	return msg + " — the terminal in the panel will scroll neither by wheel nor by swipe"
}

func startSession(dir, name, bin string, args []string, envFile string) error {
	tmuxBin := tool(tmuxEnv, "tmux")

	argv := []string{"new-session", "-d", "-s", name, "-c", dir, "--",
		"env", "-i", "sh", envFile}
	argv = append(argv, bin)
	argv = append(argv, args...)

	cmd := exec.Command(tmuxBin, argv...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		said := strings.TrimSpace(string(out))
		if said == "" {
			return fmt.Errorf("tmux did not start session %s (%s): %w", name, tmuxBin, err)
		}
		return fmt.Errorf("tmux did not start session %s (%s): %w: %s", name, tmuxBin, err, said)
	}
	return nil
}

type claudeChoice struct {
	Path     string
	Warn     string
	OffRoute bool
}

func claudeBin(fromMap string) (claudeChoice, error) {
	if fromMap != "" {
		if err := executable(fromMap); err != nil {
			return claudeChoice{}, fmt.Errorf(
				"this project's profile starts claude through %s, and there is no such file on the host: %w. "+
					"Until the path in the profile map is fixed, the session does not start at all — "+
					"starting it with your personal claude would move the work into another account silently",
				fromMap, err)
		}
		return claudeChoice{Path: fromMap}, nil
	}
	if p := os.Getenv(claudeEnv); p != "" {
		if err := executable(p); err != nil {
			return claudeChoice{}, fmt.Errorf(
				"the machine description starts claude through %s=%s, and there is no such file on the host: %w. "+
					"Until the path is fixed, the session does not start at all — starting it with the claude "+
					"from PATH would move the work into another account silently",
				claudeEnv, p, err)
		}
		return claudeChoice{Path: p}, nil
	}

	path, err := exec.LookPath("claude")
	if err != nil {
		return claudeChoice{}, fmt.Errorf(
			"claude not found: not in this project's profile, not in %s, not in PATH (%s): %w. "+
				"The panel calls claude with the PATH of its own unit, not the one "+
				"from your shell — put the path into %s of the machine description",
			claudeEnv, os.Getenv("PATH"), err, claudeEnv)
	}
	if len(registry.ConfigDirs()) < 2 {
		return claudeChoice{Path: path}, nil
	}
	return claudeChoice{
		Path: path,
		Warn: "no profile wrapper found: not in the profile map, not in " + claudeEnv +
			" — the session was started with the first claude from PATH (" + path +
			"), that is, in your personal account",
		OffRoute: true,
	}, nil
}

func contourConfig(claudeBin, configDir string) (string, error) {
	if claudeBin != "" || configDir == "" {
		return "", nil
	}
	info, err := os.Stat(configDir)
	if err != nil {
		return "", fmt.Errorf(
			"this project's profile lives in %s, and there is no such directory on the host: %w. "+
				"Until the path in the profile map is fixed, the session does not start at all — "+
				"starting it with the personal directory would write the history into another account silently",
			configDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("this project's profile lives in %s, and that is not a directory", configDir)
	}
	return configDir, nil
}

func executable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a program", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func waitAgent(ctx context.Context, name string) (int, error) {
	deadline := time.Now().Add(startWait)
	for {
		for _, pid := range claudePIDs() {
			args := procArgs(pid)
			if oneShot(args) {
				continue
			}
			if got, ok := argValue(args, "-n", "--name"); ok && got == name {
				return pid, nil
			}
		}
		if !time.Now().Before(deadline) {
			return 0, fmt.Errorf(
				"session %s did not start within %s: no claude process with that name appeared. "+
					"The terminal window may have opened and closed right away — look on the host",
				name, startWait)
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("session %s did not start: the action ran out of time", name)
		case <-time.After(startPoll):
		}
	}
}
