// Package browser keeps the panel's own browser profiles on the host.
package browser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"aacpanel/internal/hostcfg"
)

// EnabledEnv is a host-only opt-in; only the literal value 1 enables browsing.
const EnabledEnv = "AACP_BROWSER"

// ProgramEnv names the host executable, never a program supplied by a request.
const ProgramEnv = "AACP_CHROME"

// UnitPrefix separates browser holders from terminal and observer units.
const UnitPrefix = "aacpanel-browser-"

// Config comes only from the host environment. No socket request can change it.
type Config struct {
	Enabled bool
	Program string
	Root    string
	Runtime string
}

// Load reads host settings and disables browsing if storage could enter a
// service mount. A captured service cannot amend these environment values.
func Load() Config {
	home, _ := os.UserHomeDir()
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	c := Config{
		Enabled: os.Getenv(EnabledEnv) == "1",
		Program: os.Getenv(ProgramEnv),
		Root:    filepath.Join(state, "aacpanel", "browser"),
		Runtime: filepath.Join(runtime, "aacpanel-browser"),
	}
	// The whole collector directory is mounted into the service; browser
	// credentials cannot be placed under it, even by a mistaken XDG setting.
	shared := hostcfg.Load().StateDir
	if !filepath.IsAbs(runtime) || !filepath.IsAbs(state) || c.PrivateFrom(shared) != nil {
		c.Enabled = false
	}
	return c
}

func within(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && (r == "." || r != ".." && !filepath.IsAbs(r) && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}

// Check refuses actions while disabled or without a usable host executable.
func (c Config) Check() error {
	if !c.Enabled {
		return errors.New("the browser is disabled in the host configuration")
	}
	if !filepath.IsAbs(c.Program) {
		return errors.New("AACP_CHROME must name an absolute executable path")
	}
	if _, err := exec.LookPath(c.Program); err != nil {
		return errors.New("the configured browser executable is unavailable")
	}
	return nil
}

// Resolve the existing part of a path too: a profile below an XDG symlink
// must not silently land inside the read-only mount shared with the service.
func realPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("browser storage paths must be absolute")
	}
	tail := []string{}
	for {
		p, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				p = filepath.Join(p, tail[i])
			}
			return p, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		tail = append(tail, filepath.Base(path))
		path = parent
	}
}

// PrivateFrom rejects storage or sockets that resolve inside a service mount,
// including paths whose existing parent is a symlink.
func (c Config) PrivateFrom(shared string) error {
	mount, err := realPath(shared)
	if err != nil {
		return err
	}
	for _, path := range []string{c.Root, c.Runtime} {
		p, err := realPath(path)
		if err != nil {
			return err
		}
		if within(mount, p) {
			return errors.New("browser profiles and holder sockets must be outside service mounts")
		}
	}
	return nil
}
