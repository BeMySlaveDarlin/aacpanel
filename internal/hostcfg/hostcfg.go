// Package hostcfg describes one machine the panel is deployed on.
package hostcfg

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// HostEnv and the names next to it are the environment variables the description understands.
const (
	HostEnv     = "AACP_HOST"
	RepoEnv     = "AACP_REPO"
	UnixUserEnv = "AACP_UNIX_USER"
	DisplayEnv  = "DISPLAY"
	// HomeSessionEnv is the name of the main host session, the one living in the home directory.
	HomeSessionEnv = "AACP_HOME_SESSION"

	// LangEnv is the locale a claude session is started with.
	LangEnv = "AACP_LANG"

	// StateDirEnv is the state directory.
	StateDirEnv = "AACP_STATE_DIR"

	// PathEnv is where to look for the description file itself.
	PathEnv = "AACP_HOSTCFG"
)

const (
	defaultStateDir = "/var/lib/aacpanel"
	defaultPath     = defaultStateDir + "/host.env"
)

// DefaultLang is the claude session locale when the machine description does not name one.
const DefaultLang = "C.UTF-8"

// Config is what Go code needs to know about this machine.
type Config struct {
	Name        string
	RepoRoot    string
	UnixUser    string
	Display     string
	HomeSession string
	Lang        string
	StateDir    string
}

func defaults() Config { return defaultsFor(hostname(), currentUser()) }

func defaultsFor(host, unixUser string) Config {
	return Config{
		Name:        host,
		RepoRoot:    "/opt/aacpanel",
		UnixUser:    unixUser,
		Display:     ":0",
		HomeSession: strings.ToLower(host),
		Lang:        DefaultLang,
		StateDir:    defaultStateDir,
	}
}

func hostname() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return "aacpanel"
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// Load reads the description from the default path, with environment variables laid over it.
func Load() Config {
	return LoadFrom(path())
}

// Path is where the service looks for the machine description.
func Path() string { return path() }

// ParseFile parses a machine description by the same rules as systemd.
func ParseFile(filePath string) map[string]string { return parseEnvFile(filePath) }

func path() string {
	if p := os.Getenv(PathEnv); p != "" {
		return p
	}
	if dir := os.Getenv(StateDirEnv); dir != "" {
		return filepath.Join(dir, "host.env")
	}
	return defaultPath
}

// LoadFrom is the same, but with an explicit path to the file.
func LoadFrom(filePath string) Config {
	file := parseEnvFile(filePath)
	get := func(env, fallback string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		if v, ok := file[env]; ok && v != "" {
			return v
		}
		return fallback
	}
	base := defaults()
	return Config{
		Name:        get(HostEnv, base.Name),
		RepoRoot:    get(RepoEnv, base.RepoRoot),
		UnixUser:    get(UnixUserEnv, base.UnixUser),
		Display:     get(DisplayEnv, base.Display),
		HomeSession: get(HomeSessionEnv, base.HomeSession),
		Lang:        get(LangEnv, base.Lang),
		StateDir:    get(StateDirEnv, base.StateDir),
	}
}

func parseEnvFile(filePath string) map[string]string {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return map[string]string{}
	}
	return parseEnv(raw)
}

func parseEnv(raw []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return out
}
