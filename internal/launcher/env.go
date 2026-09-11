package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"aacpanel/internal/hostcfg"
)

var routeVars = []string{"CLAUDE_PROFILE", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN"}

func isRouteVar(name string) bool { return slices.Contains(routeVars, name) }

var grantedVars = append(append([]string{}, routeVars...), "CLAUDE_CODE_TMPDIR")

func isGrantedVar(name string) bool { return slices.Contains(grantedVars, name) }

var graphicalVars = []string{
	"DBUS_SESSION_BUS_ADDRESS", "DISPLAY", "XAUTHORITY",
	"XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "XDG_SESSION_CLASS", "XDG_SESSION_ID",
	"KDE_FULL_SESSION", "KDE_SESSION_VERSION",
}

var graphicalProcs = []string{
	"plasmashell", "ksmserver", "kwin_x11", "kded6",
	"gnome-shell", "gnome-session-binary",
	"xfce4-session", "mate-session", "cinnamon-session",
}

var unitVars = []string{
	"INVOCATION_ID", "JOURNAL_STREAM", "NOTIFY_SOCKET", "MANAGERPID",
	"LISTEN_FDS", "LISTEN_PID", "LISTEN_FDNAMES", "SYSTEMD_EXEC_PID",
}

const scratchDir = ".cache/claude-tmp"

const (
	defaultTerm = "xterm-256color"
	defaultLang = hostcfg.DefaultLang
)

func childEnv(own []string, params Params, display, lang, configDir string) ([]string, []string) {
	var warns []string
	env := map[string]string{}
	for _, kv := range own {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(name, "CLAUDE") && !isRouteVar(name) {
			continue
		}
		if slices.Contains(unitVars, name) {
			continue
		}
		env[name] = value
	}

	if graphical, ok := graphicalEnv(display); ok {
		for name, value := range graphical {
			env[name] = value
		}
	} else if !windowless() {
		warns = append(warns, fmt.Sprintf(
			"no graphical session found on DISPLAY=%s — the environment stayed the one of the caller; "+
				"if this is a start from a unit, the session gets the systemd bus and Chrome hangs in it", display))
	}
	if warn := checkBus(env["DBUS_SESSION_BUS_ADDRESS"]); warn != "" {
		warns = append(warns, warn)
	}

	if home := env["HOME"]; home != "" {
		env["CLAUDE_CODE_TMPDIR"] = filepath.Join(home, scratchDir)
	}

	if configDir != "" {
		env["CLAUDE_CONFIG_DIR"] = configDir
	}

	if env["TERM"] == "" {
		env["TERM"] = defaultTerm
	}
	if env["LANG"] == "" {
		if lang == "" {
			lang = defaultLang
		}
		env["LANG"] = lang
	}

	for name, value := range params.Env {
		env[name] = value
	}

	out := make([]string, 0, len(env))
	for name, value := range env {
		out = append(out, name+"="+value)
	}
	slices.Sort(out)
	return out, warns
}

func graphicalEnv(display string) (map[string]string, bool) {
	want := screenless(display)
	for _, comm := range graphicalProcs {
		for _, pid := range pidsByComm(comm) {
			if !ownProcess(pid) {
				continue
			}
			env := map[string]string{}
			for _, kv := range procEnviron(pid) {
				name, value, ok := strings.Cut(kv, "=")
				if ok && slices.Contains(graphicalVars, name) {
					env[name] = value
				}
			}
			if screenless(env["DISPLAY"]) != want {
				continue
			}
			return env, true
		}
	}
	return nil, false
}

func screenless(display string) string {
	if i := strings.LastIndex(display, "."); i > 0 {
		return display[:i]
	}
	return display
}

func ownProcess(pid int) bool {
	info, err := os.Stat(filepath.Join(procRoot(), fmt.Sprint(pid)))
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

func checkBus(addr string) string {
	path, ok := strings.CutPrefix(addr, "unix:path=")
	if !ok {
		return ""
	}
	path, _, _ = strings.Cut(path, ",")
	if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
		return ""
	}
	return "bus " + addr + " is named, but there is no socket at that path"
}

func leaks(pid int) []string {
	var out []string
	for _, kv := range procEnviron(pid) {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLAUDE") && !isGrantedVar(name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}
