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

// Contour is a claude contour: its own config directory for a group of projects.
type Contour struct {
	Profile string
	Prefix  string
	Config  string
}

// Load returns the contours from the registry, in file order.
func Load() []Contour {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := fromRegistry(home)
	if len(out) == 0 {
		return []Contour{{Profile: "personal", Prefix: "*", Config: filepath.Join(home, ".claude")}}
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
		out = append(out, Contour{
			Profile: strings.TrimSpace(parts[0]),
			Prefix:  strings.TrimSpace(parts[1]),
			Config:  expand(strings.TrimSpace(parts[2]), home),
		})
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
		return []string{filepath.Join(home, ".claude")}
	}
	return strings.Split(raw, string(os.PathListSeparator))
}

func expand(dir, home string) string {
	if home != "" && strings.HasPrefix(dir, "~/") {
		return filepath.Join(home, dir[2:])
	}
	return dir
}
