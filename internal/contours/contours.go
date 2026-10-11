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

// CodexHomes returns the codex homes of the contours, ~/.codex when none is
// named, each under the contour it pairs with.
//
// A home is named the way a claude config directory is — ~/.codex is the
// personal one, any other its directory's name without the leading dot — and
// pairs with the claude contour whose account bears that name, ContourOf's
// name: ~/.codex-profiles/claude-work with the account of ~/.claude-work.
// Failing that, it pairs with the contour the installer puts on the map under
// that name, SuggestedName's: ~/.codex-profiles/work with the same account,
// which the installer offers to call work. Either way the home stands under
// the account's name, so the archive, the threads and New of the contour find
// it the same.
//
// A contour pairs with one home. The home named after the account wins over
// the one named after the map, and of two of one name the first listed; a home
// left over, like one that claims no contour, stands as a contour of its own
// name, paired with no account. The host does not read the map: a contour
// renamed there after the install pairs only by its account's name.
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
	var dirs []string
	for _, dir := range strings.Split(raw, string(os.PathListSeparator)) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dir = filepath.Clean(expand(dir, home))
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return paired(dirs, home)
}

// paired puts each codex home under the contour it pairs with, by the rule of
// CodexHomes: every home named after an account first, whatever its place in
// the list, then the homes named after the map.
func paired(dirs []string, home string) []CodexHome {
	var accounts, suggested []string
	for _, dir := range ConfigDirs() {
		accounts = append(accounts, ContourOf(dir))
		suggested = append(suggested, SuggestedName(dir, home))
	}
	out := make([]CodexHome, len(dirs))
	claimed := map[string]bool{}
	for i, dir := range dirs {
		out[i].Dir = dir
		if name := codexName(dir); slices.Contains(accounts, name) && !claimed[name] {
			out[i].Contour, claimed[name] = name, true
		}
	}
	for i := range out {
		if out[i].Contour != "" {
			continue
		}
		name := codexName(out[i].Dir)
		if at := slices.Index(suggested, name); at >= 0 && !claimed[accounts[at]] {
			out[i].Contour, claimed[accounts[at]] = accounts[at], true
			continue
		}
		out[i].Contour = name
	}
	return out
}

// codexName is the name of a codex home the way a claude contour is named by
// its config directory: the default one is the personal contour, any other is
// its directory's name without the leading dot.
func codexName(dir string) string {
	name := filepath.Base(dir)
	if name == ".codex" {
		return Personal
	}
	return strings.TrimLeft(name, ".")
}

// CodexContour names the contour of a codex home: the one it pairs with where
// it is listed, and its own name otherwise — the contour a home of that
// directory would stand as on its own.
func CodexContour(dir string) string {
	dir = filepath.Clean(dir)
	for _, h := range CodexHomes() {
		if h.Dir == dir {
			return h.Contour
		}
	}
	return codexName(dir)
}

// SuggestedName is the name the installer offers for the contour of a claude
// config directory on the map: the personal contour for the default one, any
// other its directory's name without a leading ".claude-" or dot, in lower
// case — ~/.claude-work is work, where the account's own name is claude-work.
func SuggestedName(dir, home string) string {
	if IsDefaultConfig(dir, home) {
		return Personal
	}
	name := strings.TrimPrefix(filepath.Base(dir), ".claude-")
	return strings.ToLower(strings.TrimPrefix(name, "."))
}

// ContourOf names the contour of a claude config directory: as the registry
// names it, and otherwise after the directory, the way a codex home is named —
// the default one is the personal contour, any other is its name without the
// leading dot. No directory is the default one.
func ContourOf(configDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	if strings.TrimSpace(configDir) == "" {
		return Personal
	}
	dir := filepath.Clean(expand(strings.TrimSpace(configDir), home))
	for _, c := range fromRegistry(home) {
		if filepath.Clean(c.Config) == dir {
			return c.Profile
		}
	}
	if IsDefaultConfig(dir, home) {
		return Personal
	}
	return strings.TrimLeft(filepath.Base(dir), ".")
}

// CodexHomeOf is the codex home of the contour a claude config directory
// stands for: the home that pairs with it, by the rule of CodexHomes. A
// contour without one has no codex daemon for its projects.
func CodexHomeOf(configDir string) (CodexHome, bool) {
	contour := ContourOf(configDir)
	for _, h := range CodexHomes() {
		if h.Contour == contour {
			return h, true
		}
	}
	return CodexHome{Contour: contour}, false
}

func expand(dir, home string) string {
	if home != "" && strings.HasPrefix(dir, "~/") {
		return filepath.Join(home, dir[2:])
	}
	return dir
}
