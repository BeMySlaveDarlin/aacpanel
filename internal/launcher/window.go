package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"aacpanel/internal/hostcfg"
)

var windowsInFlight sync.WaitGroup

// WindowSpec is a request to open a window onto an existing tmux session.
type WindowSpec struct {
	Dir     string `json:"dir"`
	Session string `json:"session"`
}

// Window opens a terminal window onto a live tmux session.
func Window(spec WindowSpec) (Report, error) {
	if spec.Session == "" {
		return Report{}, fmt.Errorf("window without a session name: there is nothing to attach to")
	}
	if spec.Dir == "" || !strings.HasPrefix(spec.Dir, "/") {
		return Report{}, fmt.Errorf("window directory %q is not absolute", spec.Dir)
	}

	host := hostcfg.Load()
	env, warns := childEnv(os.Environ(), Params{}, host.Display, host.Lang, "")

	pid, _, err := openWindow(spec.Dir, spec.Session, env)
	if err != nil {
		return Report{}, err
	}
	if pid == 0 {
		return Report{}, fmt.Errorf(
			"there is nothing to open a window with: %s is empty, so this machine does not open windows at all", terminalEnv)
	}
	return Report{Session: spec.Session, Dir: spec.Dir, Konsole: pid, Warnings: warns}, nil
}

// WindowPossible reports whether this machine has anything to open a window with.
func WindowPossible() bool { return strings.TrimSpace(windowTemplate()) != "" }

func windowTemplate() string {
	spec, ok := os.LookupEnv(terminalEnv)
	if !ok {
		return defaultTerminal
	}
	return spec
}

func windowless() bool { return strings.TrimSpace(windowTemplate()) == "" }

func terminalAuto() bool { return !terminalAutoOff(os.Getenv(terminalAutoEnv)) }

func terminalAutoOff(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "no", "off", "false":
		return true
	}
	return false
}

func openWindow(dir, name string, env []string) (int, bool, error) {
	spec := windowTemplate()
	if windowless() {
		return 0, false, nil
	}

	tmuxBin := tool(tmuxEnv, "tmux")
	argv, konsole := windowArgv(spec, dir, name, []string{tmuxBin, "attach", "-t", name})
	if len(argv) == 0 {
		return 0, false, nil
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, konsole, fmt.Errorf("the window did not open (%s): %w", argv[0], err)
	}
	windowsInFlight.Add(1)
	go func() {
		defer windowsInFlight.Done()
		_ = cmd.Wait()
	}()
	return cmd.Process.Pid, konsole, nil
}

func windowArgv(spec, dir, name string, attach []string) ([]string, bool) {
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return nil, false
	}

	if filepath.Base(fields[0]) == "konsole" && len(fields) == 1 {
		return append([]string{
			fields[0],
			"--separate",
			"--workdir", dir,
			"-p", "LocalTabTitleFormat=" + name,
			"-p", "RemoteTabTitleFormat=" + name,
			"-e",
		}, attach...), true
	}

	var argv []string
	for _, f := range fields {
		if f == "%s" {
			argv = append(argv, strings.Join(attach, " "))
			continue
		}
		argv = append(argv, f)
	}
	if !strings.Contains(spec, "%s") {
		argv = append(argv, attach...)
	}
	return argv, filepath.Base(fields[0]) == "konsole"
}
