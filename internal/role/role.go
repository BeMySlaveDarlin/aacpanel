// Package role keeps the role of a session in its project: a short text that
// tells it from the other sessions working in the same repository, one file a
// session. Several sessions of one project often run in one directory and on
// one branch, and after a restart each would take the notes and the handoffs
// left there for its own; the role, given to the session before its first
// word, says which work is its.
//
// A role is kept by the account the session runs in, by the repository it
// works in and by its name, so it survives a restart — afresh or going on
// with the conversation — a close and an open under the same name, and the
// session moving between the main checkout and a worktree of it. A session
// renamed by the panel takes its role along; one renamed otherwise, or opened
// again under another name, starts without one.
//
// Three hands set it: the session itself through the panel's role tool, the
// session that opens it through session_open, and the person in the panel.
// The collector reads the file into the row of the session.
package role

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/mcp"
	"aacpanel/internal/sessionfile"
)

// MaxText bounds a role: it stands in the system prompt of every start of the
// session, and a longer one is refused rather than cut without a word.
const MaxText = 500

// Who set a role: the session itself, the person in the panel, or the session
// that opened it, named after the prefix.
const (
	BySession = "session"
	ByPerson  = "person"
	byOpener  = "opener"
)

// ByOpener is who set the role a session was opened with: the session that
// opened it, by its name where it has one.
func ByOpener(name string) string {
	if name == "" {
		return byOpener
	}
	return byOpener + " " + name
}

// Stamp is how the time a role was set is written: UTC, to the second.
const Stamp = "2006-01-02T15:04:05Z"

// Role is what lies on disk for one session. Repo is what the key is made
// of; Dir is the directory the session worked in when the role was set, kept
// to be shown.
type Role struct {
	ConfigDir string `json:"configDir"`
	Repo      string `json:"repo"`
	Dir       string `json:"dir"`
	// Name is the name of the session, empty for a session without one.
	Name string `json:"name,omitempty"`
	Role string `json:"role"`
	By   string `json:"by"`
	At   string `json:"at"`
}

// Dir is where the roles lie: the owner's state, beside the rest of what the
// panel keeps on the host.
func Dir() string {
	return sessionfile.Dir("roles")
}

// Repo is the repository a directory works in, as the key of a role names
// it: the common git directory of the checkout the directory lies in, which a
// worktree shares with its main checkout, or the directory itself where no
// checkout holds it or its git files do not read. It is read from the files
// git leaves — the .git of the checkout and, for a worktree, the gitdir it
// points to and the commondir there — without running git; the collector
// reads it the same way. A .git directory with no HEAD in it is no
// repository, as git takes it, and the walk goes on above it.
func Repo(dir string) string {
	dir = filepath.Clean(dir)
	for at := dir; ; at = filepath.Dir(at) {
		dotgit := filepath.Join(at, ".git")
		info, err := os.Stat(dotgit)
		switch {
		case err == nil && info.IsDir():
			if _, err := os.Stat(filepath.Join(dotgit, "HEAD")); err == nil {
				return dotgit
			}
		case err == nil:
			if common, ok := commonDir(at, dotgit); ok {
				return common
			}
			return dir
		case !errors.Is(err, os.ErrNotExist):
			return dir
		}
		if filepath.Dir(at) == at {
			return dir
		}
	}
}

// commonDir follows the .git file of a worktree to the git directory it
// names, and that to the common directory its commondir names, each relative
// to where it is written. A git directory without a commondir is the common
// one itself, as git takes it.
func commonDir(at, dotgit string) (string, bool) {
	raw, err := os.ReadFile(dotgit)
	if err != nil {
		return "", false
	}
	gitdir, ok := strings.CutPrefix(string(bytes.TrimSpace(raw)), "gitdir:")
	gitdir = strings.TrimSpace(gitdir)
	if !ok || gitdir == "" {
		return "", false
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(at, gitdir)
	}
	gitdir = filepath.Clean(gitdir)
	raw, err = os.ReadFile(filepath.Join(gitdir, "commondir"))
	if errors.Is(err, os.ErrNotExist) {
		return gitdir, true
	}
	common := strings.TrimSpace(string(raw))
	if err != nil || common == "" {
		return "", false
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	return filepath.Clean(common), true
}

// File is the file of the role of a session — its account, its repository
// and its name — or empty for a place that is not one.
func File(p mcp.Place, name string) string {
	where, ok := p.Clean()
	if !ok {
		return ""
	}
	return sessionfile.Name(where.ConfigDir, Repo(where.Dir), name)
}

// Clean trims a role and refuses one longer than MaxText. An empty role is a
// role cleared.
func Clean(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n > MaxText {
		return "", fmt.Errorf("the role is %d characters long, at most %d are kept: say it shorter", n, MaxText)
	}
	return text, nil
}

// Keep sets the role of the session named name at place p, as by says, and
// returns what was written; an empty role removes the file and returns nil.
// The file is replaced whole by a rename, so a reader finds the old role or
// the new one, never half of each.
func Keep(dir string, p mcp.Place, name, text, by string, now time.Time) (*Role, error) {
	where, ok := p.Clean()
	if !ok {
		return nil, fmt.Errorf("the place of the session is not known: %q under the account %q", p.Dir, p.ConfigDir)
	}
	text, err := Clean(text)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, File(where, name))
	if text == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("the role was not cleared: %w", err)
		}
		return nil, nil
	}
	r := &Role{ConfigDir: where.ConfigDir, Repo: Repo(where.Dir), Dir: where.Dir, Name: name, Role: text, By: by,
		At: now.UTC().Format(Stamp)}
	if err := write(dir, path, r); err != nil {
		return nil, err
	}
	return r, nil
}

// Read returns the role of the session named name at place p, or nil when it
// has none.
func Read(dir string, p mcp.Place, name string) *Role {
	where, ok := p.Clean()
	if !ok {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, File(where, name)))
	if err != nil {
		return nil
	}
	var r Role
	if json.Unmarshal(raw, &r) != nil || r.ConfigDir != where.ConfigDir || r.Repo != Repo(where.Dir) ||
		r.Name != name || r.Role == "" {
		return nil
	}
	return &r
}

// Move carries the role of a session renamed at place p from its old name to
// its new one, as it was set. A session without a role moves nothing.
func Move(dir string, p mcp.Place, from, to string) error {
	r := Read(dir, p, from)
	if r == nil || from == to {
		return nil
	}
	where, _ := p.Clean()
	moved := *r
	moved.Name, moved.Dir = to, where.Dir
	if err := write(dir, filepath.Join(dir, File(where, to)), &moved); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, File(where, from))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the role under the old name was not removed: %w", err)
	}
	return nil
}

func write(dir, path string, r *Role) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("the roles directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".role-*")
	if err != nil {
		return fmt.Errorf("the role was not written: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("the role was not written: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("the role was not written: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("the role was not written: %w", err)
	}
	return nil
}
