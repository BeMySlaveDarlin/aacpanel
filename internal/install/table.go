package install

import (
	"errors"
	"io/fs"
	"os/exec"
	"slices"
	"strings"
	"syscall"
)

// Table is a machine made of tables: the files it has, the programs on its
// PATH and what each command answers. Anything not in a table is not there,
// so whatever reads it past the tables finds nothing rather than the machine
// it runs on. Tests hand it to the check, the survey and the screen.
type Table struct {
	EUID   int
	Acct   Account
	Uname  string
	Vars   map[string]string
	Files  map[string]string
	Stats  map[string]Stat
	Links  map[string]string
	Path   map[string]string
	Cmds   map[string]Reply
	Spaces map[string]Space
	Web    map[string]int
	Tty    bool
}

// Reply is what a command of a Table gives: its output, or a failure with
// what it said on standard error.
type Reply struct {
	Out  string
	Fail string
	Code int
}

// Says is a command that answers out; FailsWith one that fails saying
// stderr.
func Says(out string) Reply         { return Reply{Out: out} }
func FailsWith(stderr string) Reply { return Reply{Fail: stderr, Code: 1} }
func (r Reply) failed() bool        { return r.Code != 0 }
func Command(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

func (t *Table) Euid() int              { return t.EUID }
func (t *Table) User() (Account, error) { return t.Acct, nil }
func (t *Table) Arch() string           { return t.Uname }
func (t *Table) Env(k string) string    { return t.Vars[k] }
func (t *Table) Terminal() bool         { return t.Tty }

func (t *Table) ReadFile(path string) ([]byte, error) {
	if s, ok := t.Files[path]; ok {
		return []byte(s), nil
	}
	return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
}

func (t *Table) Head(path string, n int) ([]byte, error) {
	raw, err := t.ReadFile(path)
	if len(raw) > n {
		raw = raw[:n]
	}
	return raw, err
}

func (t *Table) Stat(path string) (Stat, error) {
	if st, ok := t.Stats[path]; ok {
		return st, nil
	}
	if _, ok := t.Files[path]; ok {
		return Stat{Mode: 0o644, UID: t.Acct.UID}, nil
	}
	// A directory the tables only imply, by a path under it.
	if entries, err := t.List(path); err == nil && len(entries) > 0 {
		return Stat{Mode: fs.ModeDir | 0o755, UID: t.Acct.UID}, nil
	}
	return Stat{}, &fs.PathError{Op: "stat", Path: path, Err: fs.ErrNotExist}
}

// List finds the names under dir among every path the tables know: a path
// deeper than one level makes its first step a directory.
func (t *Table) List(dir string) ([]DirEntry, error) {
	st, ok := t.Stats[dir]
	if ok && !st.Mode.IsDir() {
		return nil, &fs.PathError{Op: "readdirent", Path: dir, Err: syscall.ENOTDIR}
	}
	seen := map[string]*DirEntry{}
	var names []string
	add := func(path string, mode fs.FileMode, known bool) {
		rest, ok := strings.CutPrefix(path, strings.TrimSuffix(dir, "/")+"/")
		if !ok || rest == "" {
			return
		}
		name, deeper, _ := strings.Cut(rest, "/")
		e := seen[name]
		if e == nil {
			e = &DirEntry{Name: name}
			seen[name] = e
			names = append(names, name)
		}
		if deeper != "" {
			e.Dir = true
			return
		}
		if known {
			e.Dir = e.Dir || mode.IsDir()
			e.Link = mode&fs.ModeSymlink != 0
		}
	}
	for p := range t.Files {
		add(p, 0, false)
	}
	for p, st := range t.Stats {
		add(p, st.Mode, true)
	}
	if len(names) == 0 && !ok {
		return nil, &fs.PathError{Op: "open", Path: dir, Err: fs.ErrNotExist}
	}
	slices.Sort(names)
	out := make([]DirEntry, len(names))
	for i, n := range names {
		out[i] = *seen[n]
	}
	return out, nil
}

func (t *Table) Real(path string) (string, error) {
	if to, ok := t.Links[path]; ok {
		return to, nil
	}
	return path, nil
}

func (t *Table) LookPath(name string) (string, error) {
	if p, ok := t.Path[name]; ok {
		return p, nil
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func (t *Table) Run(name string, args ...string) (string, error) {
	r, ok := t.Cmds[Command(name, args...)]
	if !ok {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	if r.failed() {
		return r.Out, &Failure{Code: r.Code, Stderr: r.Fail}
	}
	return r.Out, nil
}

func (t *Table) Space(path string) (Space, error) {
	if sp, ok := t.Spaces[path]; ok {
		return sp, nil
	}
	return Space{}, errors.New("no such file system")
}

func (t *Table) Reach(url string) (int, error) {
	if s, ok := t.Web[url]; ok {
		return s, nil
	}
	return 0, errors.New("dial tcp: i/o timeout")
}
