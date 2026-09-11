package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	busctlEnv             = "AACP_BUSCTL"
	defaultBusctl         = "busctl"
	konsoleTimeoutDefault = 5 * time.Second
	maxParentHops         = 5
)

var konsoleTimeout = konsoleTimeoutDefault

var errBusStall = errors.New("konsole did not answer")

type konsoleTab struct {
	Service  string
	Path     string
	Bus      string
	Attempts int
}

func konsoleTabFor(ctx context.Context, pid int) (konsoleTab, error) {
	owner, ok := konsoleOwner(pid)
	if !ok {
		return konsoleTab{}, fmt.Errorf(
			"the session does not run under konsole (ssh, VS Code or something else)")
	}
	service := "org.kde.konsole-" + strconv.Itoa(owner)

	tab, err := konsoleTabOnce(ctx, service, pid)
	if errors.Is(err, errBusStall) && ctx.Err() == nil {
		tab, err = konsoleTabOnce(ctx, service, pid)
		tab.Attempts = 2
	}
	return tab, err
}

func konsoleTabOnce(ctx context.Context, service string, pid int) (konsoleTab, error) {
	var last error
	for _, bus := range busCandidates() {
		paths, err := konsoleTabs(ctx, bus, service)
		if errors.Is(err, errBusStall) {
			return konsoleTab{}, err
		}
		if err != nil {
			last = err
			continue
		}
		var tabErr error
		for _, path := range paths {
			got, err := tabPID(ctx, bus, service, path)
			if err != nil {
				tabErr = err
				continue
			}
			if got == pid {
				return konsoleTab{Service: service, Path: path, Bus: bus, Attempts: 1}, nil
			}
		}
		if tabErr != nil {
			return konsoleTab{}, fmt.Errorf("konsole window %s was found, but its tabs do not answer: %w", service, tabErr)
		}
		return konsoleTab{}, fmt.Errorf("a konsole window was found (%s), but it has no tab with process %d", service, pid)
	}
	if last != nil {
		return konsoleTab{}, last
	}
	return konsoleTab{}, fmt.Errorf("konsole window %s answered on no bus", service)
}

func konsoleOwner(pid int) (int, bool) {
	for range maxParentHops {
		parent, ok := procParent(pid)
		if !ok || parent <= 1 {
			return 0, false
		}
		if procComm(parent) == "konsole" {
			return parent, true
		}
		pid = parent
	}
	return 0, false
}

func procComm(pid int) string {
	raw, err := os.ReadFile(filepath.Join(procDir(), strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func busCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(addr string) {
		if addr != "" && !seen[addr] {
			seen[addr] = true
			out = append(out, addr)
		}
	}
	add(os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		add("unix:path=" + filepath.Join(rt, "bus"))
	}
	paths, err := filepath.Glob("/tmp/dbus-*")
	if err != nil {
		return out
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}
		add("unix:path=" + path)
	}
	return out
}

func konsoleTabs(ctx context.Context, bus, service string) ([]string, error) {
	out, err := busctl(ctx, bus, "--no-legend", "tree", service)
	if err != nil {
		return nil, fmt.Errorf("konsole window %s does not answer on bus %s: %w", service, bus, err)
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "/Sessions/"); i >= 0 {
			path := strings.TrimSpace(line[i:])
			if path != "/Sessions" {
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("konsole window %s has no tabs at all", service)
	}
	return paths, nil
}

func tabPID(ctx context.Context, bus, service, path string) (int, error) {
	out, err := busctl(ctx, bus, "--no-legend", "call", service, path, "org.kde.konsole.Session", "processId")
	if err != nil {
		return 0, fmt.Errorf("tab %s: %w", path, err)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[0] != "i" {
		return 0, fmt.Errorf("tab %s: processId answered %q", path, strings.TrimSpace(out))
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("tab %s: processId answered %q", path, strings.TrimSpace(out))
	}
	return pid, nil
}

func busctlString(out string) (string, bool) {
	i := strings.Index(out, `"`)
	j := strings.LastIndex(out, `"`)
	if i < 0 || j <= i {
		return "", false
	}
	return unescape(out[i+1 : j]), true
}

func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		next := s[i+1]
		switch {
		case next >= '0' && next <= '7' && i+3 < len(s):
			v, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
			if err != nil {
				b.WriteByte(next)
				i += 2
				continue
			}
			b.WriteByte(byte(v))
			i += 4
		case next == 'n':
			b.WriteByte('\n')
			i += 2
		case next == 't':
			b.WriteByte('\t')
			i += 2
		case next == 'r':
			b.WriteByte('\r')
			i += 2
		default:
			b.WriteByte(next)
			i += 2
		}
	}
	return b.String()
}

func (tab konsoleTab) kind() string { return "konsole" }

func (tab konsoleTab) attempts() int {
	if tab.Attempts == 0 {
		return 1
	}
	return tab.Attempts
}

func (tab konsoleTab) screen(ctx context.Context) (string, bool) {
	out, err := busctl(ctx, tab.Bus, "--no-legend", "call", tab.Service, tab.Path,
		"org.kde.konsole.Session", "getAllDisplayedText")
	if err != nil {
		return "", false
	}
	return busctlString(out)
}

func (tab konsoleTab) send(ctx context.Context, payload string) error {
	if strings.ContainsRune(payload, 0) {
		return fmt.Errorf("there is a NUL byte in the text — terminals are not written to like that")
	}
	_, err := busctl(ctx, tab.Bus, "--no-legend", "call", tab.Service, tab.Path,
		"org.kde.konsole.Session", "sendText", "s", payload)
	if errors.Is(err, errBusStall) {
		return fmt.Errorf("%w — whether the text was typed is unknown: look at the feed before sending again", err)
	}
	if err != nil {
		return fmt.Errorf("konsole did not accept the input: %w", err)
	}
	return nil
}

func busctl(ctx context.Context, bus string, args ...string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, konsoleTimeout)
	defer cancel()

	bin := os.Getenv(busctlEnv)
	if bin == "" {
		bin = defaultBusctl
	}
	cmd := exec.CommandContext(callCtx, bin, append([]string{"--user"}, args...)...)
	if bus != "" {
		cmd.Env = withBus(os.Environ(), bus)
	}
	out, err := cmd.Output()
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return "", fmt.Errorf("%w within %s on call %s", errBusStall, konsoleTimeout, busVerb(args))
		}
		return "", errWithStderr(err)
	}
	return string(out), nil
}

func busVerb(args []string) string {
	for i, a := range args {
		if strings.HasPrefix(a, "--") {
			continue
		}
		if a == "call" && i+4 < len(args) {
			return args[i+4]
		}
		return a
	}
	return "busctl"
}

func withBus(env []string, bus string) []string {
	const key = "DBUS_SESSION_BUS_ADDRESS="
	out := make([]string, 0, len(env)+1)
	for _, pair := range env {
		if !strings.HasPrefix(pair, key) {
			out = append(out, pair)
		}
	}
	return append(out, key+bus)
}

func errWithStderr(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
