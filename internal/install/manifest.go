package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Kind is what a line of the manifest stands for. Every kind has its undo in
// undos: uninstall walks the manifest back and hands each line to the undo of
// its kind, so a kind without one would be a change nothing takes back, and
// Record refuses it.
type Kind string

// Dir is a directory the installer made; its meta is "created", or
// "cache" for the installer's own cache of Go, its modules and its builds.
const Dir Kind = "dir"

// undos take back what a line stands for. Each is idempotent: a line is
// written before its change, so the change may never have happened, and
// undoing it then is a no-op rather than an error.
var undos = map[Kind]func(*Run, Entry) error{
	Dir: undoDir,
}

// undoDir removes a directory the installer made, if nothing else lives in
// it by now: what somebody put there since is theirs. The cache is the
// installer's alone and goes whole; Go keeps its modules read-only, so the
// tree is made writable first.
func undoDir(r *Run, e Entry) error {
	if metaHas(e.Meta, Adopted) {
		return nil
	}
	if metaHas(e.Meta, "cache") {
		_ = filepath.WalkDir(e.Target, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
		return os.RemoveAll(e.Target)
	}
	err := os.Remove(e.Target)
	switch {
	case err == nil, errors.Is(err, os.ErrNotExist):
		return nil
	case errors.Is(err, os.ErrExist): // ENOTEMPTY counts as one
		r.Say(Note, e.Target+" is left: something else lives in it now")
		return nil
	}
	return err
}

// Entry is a line of the manifest: the step that made the change, what kind
// of thing it is, where it is, and what uninstall needs to know about it —
// "created", "adopted", "sha=…", "orig=…", "by-installer".
type Entry struct {
	Step, Kind, Target, Meta string
}

func (e Entry) line() string {
	return strings.Join([]string{e.Step, e.Kind, e.Target, e.Meta}, "\t") + "\n"
}

// ManifestName is the file of the manifest in the installer's directory.
const ManifestName = "manifest.tsv"

// StateDir is the installer's own directory: the manifest, the journal, the
// backups. It is apart from ~/.local/state/aacpanel, which is the executor's:
// here lies the account of the install, there the data of the panel.
func StateDir(env func(string) string) string {
	if base := env("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "aacpanel-install")
	}
	return filepath.Join(env("HOME"), ".local", "state", "aacpanel-install")
}

// Manifest is the ledger of what the installer changed on the machine, one
// line a change, appended as the run goes. It is the one sign of an install:
// uninstall takes back what it lists and nothing else.
type Manifest struct {
	Path string
}

// OpenManifest opens the manifest in dir for appending. A first run makes the
// directory, and the first line of the manifest is that directory, so that
// uninstall takes it away last.
func OpenManifest(dir string) (*Manifest, error) {
	m := &Manifest{Path: filepath.Join(dir, ManifestName)}
	if _, err := os.Stat(dir); err == nil {
		return m, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("the installer's directory %s was not made: %w", dir, err)
	}
	if err := m.Append(Entry{Step: "manifest", Kind: string(Dir), Target: dir, Meta: "created"}); err != nil {
		return nil, err
	}
	return m, nil
}

// Append writes a line and puts it on the disk before it returns: the line
// has to outlive a run that dies right after it.
func (m *Manifest) Append(e Entry) error {
	for _, field := range []string{e.Step, e.Kind, e.Target, e.Meta} {
		if strings.ContainsAny(field, "\t\n") {
			return fmt.Errorf("a manifest line cannot carry a tab or a newline: %q", field)
		}
	}
	f, err := os.OpenFile(m.Path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(e.line()); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Drop takes the lines gone picks out of the manifest: a thing a run took
// back before uninstall — a part of the kit left out — is on record no
// more. The manifest is written whole beside itself and renamed over, so a
// run that dies in the middle leaves it as it was or as it is meant to be.
func (m *Manifest) Drop(gone func(Entry) bool) error {
	es, err := ReadManifest(m.Path)
	if err != nil {
		return err
	}
	var kept strings.Builder
	for _, e := range es {
		if !gone(e) {
			kept.WriteString(e.line())
		}
	}
	return writeFile(m.Path, []byte(kept.String()), 0o600)
}

// ReadManifest reads the lines of the manifest at path in the order they were
// written. A line that does not split into four fields is an error: the
// manifest is the only account of the install, and a guess at a torn line
// could take back the wrong thing.
func ReadManifest(path string) ([]Entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for n, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			return nil, fmt.Errorf("%s:%d: %d fields where a line has 4", path, n+1, len(f))
		}
		out = append(out, Entry{Step: f[0], Kind: f[1], Target: f[2], Meta: f[3]})
	}
	return out, nil
}
