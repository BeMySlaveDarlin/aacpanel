// Package contours reads the layout of claude contours.
package contours

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// RegistryEnv is the variable holding the path to the wrapper registry.
const RegistryEnv = "AACP_CLAUDE_REGISTRY"

// HomeEnv lists the config directories of the contours, separated by colons.
const HomeEnv = "AACP_CLAUDE_HOME"

// Personal is the name of the personal contour.
const Personal = "personal"

// DefaultConfig is the config directory claude reads when nothing names
// another: .claude in the home directory. The contour kept there is the
// personal one, whatever the map calls it and wherever it stands on it.
func DefaultConfig(home string) string {
	return filepath.Join(home, ".claude")
}

// IsDefaultConfig reports that dir, a leading ~ read as home, is the default
// config directory of home.
func IsDefaultConfig(dir, home string) bool {
	return home != "" && filepath.Clean(expand(dir, home)) == DefaultConfig(home)
}

// Contour is a claude contour: its own config directory for a group of projects.
type Contour struct {
	Profile string
	Prefix  string
	Config  string
	// Token is the file the wrapper signs the account in with, instead of
	// a sign-in claude keeps in the directory; empty when there is none.
	Token string
}

// Load returns the contours from the registry, in file order.
func Load() []Contour {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := fromRegistry(home)
	if len(out) == 0 {
		return []Contour{{Profile: "personal", Prefix: "*", Config: DefaultConfig(home)}}
	}
	return out
}

func fromRegistry(home string) []Contour {
	reg := os.Getenv(RegistryEnv)
	if reg == "" {
		return nil
	}
	raw, err := os.ReadFile(reg)
	if err != nil {
		return nil
	}
	return ParseRegistry(raw, home)
}

// ParseRegistry reads a wrapper registry: a contour a line, as
// profile|prefix|config directory|token, with # for a comment. The token is
// the file the wrapper signs the account in with, "-" for none; the fields
// after it are not the contour's.
func ParseRegistry(raw []byte, home string) []Contour {
	var out []Contour
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		c := Contour{
			Profile: strings.TrimSpace(parts[0]),
			Prefix:  strings.TrimSpace(parts[1]),
			Config:  expand(strings.TrimSpace(parts[2]), home),
		}
		if len(parts) > 3 {
			if token := strings.TrimSpace(parts[3]); token != "-" {
				c.Token = expand(token, home)
			}
		}
		out = append(out, c)
	}
	return out
}

// ConfigDirs returns the config directories of all contours, the personal one first.
func ConfigDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	var out []string
	add := func(dir string) {
		dir = expand(strings.TrimSpace(dir), home)
		if dir == "" || slices.Contains(out, dir) {
			return
		}
		out = append(out, dir)
	}
	for _, dir := range envDirs(home) {
		add(dir)
	}
	for _, c := range fromRegistry(home) {
		add(c.Config)
	}
	return out
}

func envDirs(home string) []string {
	raw, ok := os.LookupEnv(HomeEnv)
	if !ok || strings.TrimSpace(raw) == "" {
		if home == "" {
			return nil
		}
		return []string{DefaultConfig(home)}
	}
	return strings.Split(raw, string(os.PathListSeparator))
}

// CodexHomesEnv lists the CODEX_HOME directories of the codex contours,
// separated by colons.
const CodexHomesEnv = "AACP_CODEX_HOMES"

// CodexHome is the home directory of one codex contour: where its daemon keeps
// its control socket and its conversations.
type CodexHome struct {
	Dir     string
	Contour string
}

// CodexHomes returns the codex homes of the contours, ~/.codex when none is named.
func CodexHomes() []CodexHome {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	raw := strings.TrimSpace(os.Getenv(CodexHomesEnv))
	if raw == "" {
		if home == "" {
			return nil
		}
		raw = filepath.Join(home, ".codex")
	}
	var out []CodexHome
	for _, dir := range strings.Split(raw, string(os.PathListSeparator)) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dir = filepath.Clean(expand(dir, home))
		if slices.ContainsFunc(out, func(h CodexHome) bool { return h.Dir == dir }) {
			continue
		}
		out = append(out, CodexHome{Dir: dir, Contour: CodexContour(dir)})
	}
	return out
}

// CodexContour names the contour of a codex home the way a claude contour is
// named by its config directory: the default one is the personal contour, any
// other is its directory's name without the leading dot.
func CodexContour(dir string) string {
	name := filepath.Base(dir)
	if name == ".codex" {
		return Personal
	}
	return strings.TrimLeft(name, ".")
}

func expand(dir, home string) string {
	if home != "" && strings.HasPrefix(dir, "~/") {
		return filepath.Join(home, dir[2:])
	}
	return dir
}
