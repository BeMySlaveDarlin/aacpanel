// Package historymove moves only Claude data owned by a project's slug or
// conversation IDs. The source is kept outside the archive until the map commits.
package historymove

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const shelf = ".aacpanel-project-moves"

var sessionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Slug is Claude's lossy encoding of a project directory.
func Slug(path string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, path)
}

type entry struct {
	Path     string
	Mode     fs.FileMode
	Hash     [32]byte
	Existing bool
	Created  bool
}

// Move is a recoverable transfer. Paths in Entries and Roots are relative to
// the two config directories, and the backup has the same layout as From.
type Move struct {
	ID      string
	Project string
	From    string
	To      string
	Roots   []string
	Entries []entry
	Dirs    []string
	Ready   bool
}

func (m *Move) dir() string               { return filepath.Join(m.From, shelf, m.ID) }
func (m *Move) backup(path string) string { return filepath.Join(m.dir(), "source", path) }
func (m *Move) save() error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	path := filepath.Join(m.dir(), "move.json")
	if err := os.WriteFile(path+".tmp", raw, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// Pending refuses a new launch or move until an unfinished move has been
// resolved, including after an executor restart.
func Pending(configs []string, project string) error {
	for _, config := range configs {
		dirs, err := os.ReadDir(filepath.Join(config, shelf))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, dir := range dirs {
			raw, err := os.ReadFile(filepath.Join(config, shelf, dir.Name(), "move.json"))
			if err != nil {
				return fmt.Errorf("unfinished project move %s: %w", dir.Name(), err)
			}
			var m Move
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			if project == m.Project || strings.HasPrefix(project, m.Project+string(filepath.Separator)) {
				return fmt.Errorf("project %s has unfinished move %s; resolve its map commit before starting a session or another move", m.Project, m.ID)
			}
		}
	}
	return nil
}

// Prepare checks the complete merge before either account is changed. Links
// and special files are refused: following one would move another project's data.
func Prepare(id, project, from, to string) (*Move, error) {
	m := &Move{ID: id, Project: project, From: from, To: to}
	if err := Pending([]string{from, to}, project); err != nil {
		return nil, err
	}
	roots, err := owned(from, Slug(project))
	if err != nil {
		return nil, err
	}
	m.Roots = roots
	for _, root := range roots {
		if err := plainParents(from, root); err != nil {
			return nil, err
		}
		err := filepath.WalkDir(filepath.Join(from, root), func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !d.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("cannot move special file %s", path)
			}
			rel, err := filepath.Rel(from, path)
			if err != nil {
				return err
			}
			e := entry{Path: rel, Mode: info.Mode()}
			dst := filepath.Join(to, rel)
			if err := plainParents(to, rel); err != nil {
				return err
			}
			other, err := os.Lstat(dst)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			e.Existing = err == nil
			if e.Existing && (d.IsDir() != other.IsDir() || (!other.IsDir() && !other.Mode().IsRegular())) {
				return fmt.Errorf("destination clash: %s has a different file type", dst)
			}
			if !d.IsDir() {
				e.Hash, err = digest(path)
				if err != nil {
					return err
				}
				if e.Existing {
					sum, err := digest(dst)
					if err != nil {
						return err
					}
					if sum != e.Hash {
						return fmt.Errorf("destination clash: %s differs; nothing was moved", dst)
					}
				}
			}
			m.Entries = append(m.Entries, e)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Apply publishes copies without replacing any file, then renames the source
// roots into the undo. A failure returns both accounts to their original state.
func (m *Move) Apply(ctx context.Context, quiet func() error) (err error) {
	if err := os.MkdirAll(filepath.Dir(m.dir()), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(m.dir(), 0o700); err != nil {
		return err
	}
	if err := m.save(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, m.Rollback())
		}
	}()
	for i := range m.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		e := &m.Entries[i]
		if e.Existing {
			continue
		}
		dst := filepath.Join(m.To, e.Path)
		if err := m.mkdir(filepath.Dir(dst)); err != nil {
			return err
		}
		if e.Mode.IsDir() {
			if err := m.mkdir(dst); err != nil {
				return err
			}
			continue
		}
		if err := copyNew(filepath.Join(m.From, e.Path), dst, e.Mode); err != nil {
			return err
		}
		e.Created = true
		if err := m.save(); err != nil {
			return err
		}
	}
	// A session opened at the keyboard while the copies were made must not
	// have its transcript renamed out from under it.
	if err := quiet(); err != nil {
		return err
	}
	roots, err := owned(m.From, Slug(m.Project))
	if err != nil {
		return err
	}
	if !slices.Equal(roots, m.Roots) {
		return fmt.Errorf("project session files changed during the move")
	}
	paths := make(map[string]bool, len(m.Entries))
	for _, e := range m.Entries {
		paths[e.Path] = true
	}
	for _, root := range m.Roots {
		if err := filepath.WalkDir(filepath.Join(m.From, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(m.From, path)
			if err != nil {
				return err
			}
			if !paths[rel] {
				return fmt.Errorf("%s appeared during the move", rel)
			}
			delete(paths, rel)
			return nil
		}); err != nil {
			return err
		}
	}
	if len(paths) != 0 {
		return fmt.Errorf("project files disappeared during the move")
	}
	for _, e := range m.Entries {
		if e.Mode.IsDir() {
			continue
		}
		sum, err := digest(filepath.Join(m.From, e.Path))
		if err != nil {
			return err
		}
		if sum != e.Hash {
			return fmt.Errorf("%s changed during the move", e.Path)
		}
		sum, err = digest(filepath.Join(m.To, e.Path))
		if err != nil {
			return err
		}
		if sum != e.Hash {
			return fmt.Errorf("destination %s changed during the move", e.Path)
		}
	}
	for _, root := range m.Roots {
		dst := m.backup(root)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(m.From, root), dst); err != nil {
			return err
		}
	}
	m.Ready = true
	return m.save()
}

func (m *Move) mkdir(path string) error {
	if path == m.To {
		return nil
	}
	st, err := os.Lstat(path)
	if err == nil {
		if !st.IsDir() {
			return fmt.Errorf("%s is not a directory", path)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := m.mkdir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return err
	}
	m.Dirs = append(m.Dirs, path)
	return m.save()
}

// Finish addresses the same transfer even if the service lost its first reply.
func Finish(id, project, from, to string, commit bool) error {
	raw, err := os.ReadFile(filepath.Join(from, shelf, id, "move.json"))
	if errors.Is(err, fs.ErrNotExist) && !commit {
		return nil
	}
	if err != nil {
		return err
	}
	var m Move
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if m.ID != id || m.Project != project || m.From != from || m.To != to {
		return fmt.Errorf("project move identity changed")
	}
	if !commit {
		return m.Rollback()
	}
	if !m.Ready {
		return fmt.Errorf("project move %s has not finished copying", id)
	}
	return os.RemoveAll(m.dir())
}

// Rollback never removes a destination file that predated the move.
func (m *Move) Rollback() error {
	var errs []error
	for _, root := range m.Roots {
		backup := m.backup(root)
		if _, err := os.Lstat(backup); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			errs = append(errs, err)
			continue
		}
		original := filepath.Join(m.From, root)
		if _, err := os.Lstat(original); err == nil {
			errs = append(errs, fmt.Errorf("rollback cannot replace %s", original))
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		if err := os.Rename(backup, original); err != nil {
			errs = append(errs, err)
		}
	}
	// Keep the destination readable if restoring the source failed.
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	for _, e := range m.Entries {
		if !e.Created {
			continue
		}
		path := filepath.Join(m.To, e.Path)
		sum, err := digest(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if sum != e.Hash {
			errs = append(errs, fmt.Errorf("rollback cannot remove changed file %s", path))
			continue
		}
		if err := os.Remove(path); err != nil {
			errs = append(errs, err)
		}
	}
	for _, dir := range slices.Backward(m.Dirs) {
		_ = os.Remove(dir)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return os.RemoveAll(m.dir())
}

func plainParents(base, rel string) error {
	for dir := filepath.Dir(rel); dir != "."; dir = filepath.Dir(dir) {
		st, err := os.Lstat(filepath.Join(base, dir))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !st.IsDir() {
			return fmt.Errorf("%s is not a plain directory", filepath.Join(base, dir))
		}
	}
	return nil
}

func digest(path string) ([32]byte, error) {
	var out [32]byte
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return out, err
	}
	copy(out[:], h.Sum(nil))
	return out, nil
}

func copyNew(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	err = errors.Join(err, out.Close())
	if err != nil {
		return errors.Join(err, os.Remove(dst))
	}
	st, err := in.Stat()
	if err != nil {
		return errors.Join(err, os.Remove(dst))
	}
	if err := os.Chtimes(dst, st.ModTime(), st.ModTime()); err != nil {
		return errors.Join(err, os.Remove(dst))
	}
	return nil
}
