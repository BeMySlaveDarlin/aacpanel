package install

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

	// Disk is a directory whose files are read from the disk itself: the
	// temporary tree a test lets the steps write into. The tables come
	// first, so a test still names what the system around it holds.
	Disk string
	// Effects are what a command does besides answering, for the steps to
	// find afterwards: a build leaves its binary.
	Effects map[string]func(Cmd) error
	// Ran are the commands the steps ran through the table, in order, and
	// Envs the environment each was given; Given are the commands whole.
	Ran   []string
	Envs  [][]string
	Given []Cmd
	mu    sync.Mutex
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
	if t.onDisk(path) {
		return os.ReadFile(path)
	}
	return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
}

func (t *Table) onDisk(path string) bool {
	return t.Disk != "" && (path == t.Disk || strings.HasPrefix(path, strings.TrimSuffix(t.Disk, "/")+"/"))
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
	if t.onDisk(path) {
		return Local{}.Stat(path)
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
	if t.onDisk(dir) {
		return Local{}.List(dir)
	}
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

// Exec runs a command of the steps from the table: it notes the command
// and its environment, does its effect, and gives its answer line by line.
// A command the table does not know fails, so a test learns of every
// command a step runs.
func (t *Table) Exec(_ context.Context, c Cmd, line func(string)) (string, error) {
	key := Command(c.Argv[0], c.Argv[1:]...)
	t.mu.Lock()
	t.Ran = append(t.Ran, key)
	t.Envs = append(t.Envs, c.Env)
	t.Given = append(t.Given, c)
	effect := t.Effects[key]
	t.mu.Unlock()
	if effect != nil {
		if err := effect(c); err != nil {
			return "", err
		}
	}
	r, ok := t.Cmds[key]
	if !ok {
		return "", &Failure{Code: 127, Stderr: "the table of the test has no " + key}
	}
	for _, l := range strings.Split(strings.TrimSuffix(r.Out+r.Fail, "\n"), "\n") {
		if l != "" && line != nil {
			line(l)
		}
	}
	if r.failed() {
		return r.Out, &Failure{Code: r.Code, Stderr: r.Fail}
	}
	return r.Out, nil
}

// Writes makes a command of the table write a file, as the command would.
func Writes(path, body string) func(Cmd) error {
	return func(Cmd) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(body), 0o755)
	}
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
