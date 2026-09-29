package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Machine is everything the check reads the machine through: files, the
// programs on PATH and their answers, free space, the network. It reads and
// never writes, and the check sees nothing past it, so a test hands the
// check a machine of its own.
type Machine interface {
	Euid() int
	User() (Account, error)
	Arch() string // as uname -m says it
	Env(key string) string
	ReadFile(path string) ([]byte, error)
	// Head reads the first n bytes of a file: enough of a program to see
	// what runs it, without reading all of a binary.
	Head(path string, n int) ([]byte, error)
	Stat(path string) (Stat, error)
	// List names what a directory holds, without following links: a link
	// found in a walk leads anywhere, /etc included.
	List(dir string) ([]DirEntry, error)
	Real(path string) (string, error) // the path with its symlinks resolved
	LookPath(name string) (string, error)
	// Run runs a program and gives its standard output. A program that
	// fails gives an error carrying what it said on standard error.
	Run(name string, args ...string) (string, error)
	Space(path string) (Space, error)
	Reach(url string) (status int, err error)
	// Terminal tells whether there is a person at a terminal: sudo can ask
	// for a password only then.
	Terminal() bool
}

// Account is the user the installer runs as.
type Account struct {
	Name     string
	UID, GID int
	Home     string
}

// Stat is what the check needs to know of a file.
type Stat struct {
	Mode fs.FileMode
	UID  int
	Mod  time.Time // the last change of its content
}

// DirEntry is a name in a directory and what it is.
type DirEntry struct {
	Name string
	Dir  bool
	Link bool
}

// Space is the file system a path lives on.
type Space struct {
	Free  uint64 // bytes an unprivileged user may still write
	Tmpfs bool
}

// Failure is a program that ran and failed, with what it said.
type Failure struct {
	Code   int
	Stderr string
}

func (f *Failure) Error() string {
	if f.Stderr == "" {
		return fmt.Sprintf("exit status %d", f.Code)
	}
	return f.Stderr
}

// Local is this machine.
type Local struct {
	// Tty is whether the installer has a person at its terminal; the
	// command line knows it, the machine does not.
	Tty bool
}

// commandTime is how long the check waits for a program. Every one it runs
// answers at once on a healthy machine; one that hangs is a finding, not a
// reason to hang the check.
const commandTime = 15 * time.Second

func (Local) Euid() int { return os.Geteuid() }

func (Local) User() (Account, error) {
	u, err := user.Current()
	if err != nil {
		return Account{}, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	home := os.Getenv("HOME")
	if home == "" {
		home = u.HomeDir
	}
	return Account{Name: u.Username, UID: uid, GID: gid, Home: home}, nil
}

func (Local) Arch() string {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return ""
	}
	return unix.ByteSliceToString(u.Machine[:])
}

func (Local) Env(key string) string                { return os.Getenv(key) }
func (Local) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (Local) Real(path string) (string, error)     { return filepath.EvalSymlinks(path) }

func (Local) Head(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	got, err := io.ReadFull(f, buf)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		err = nil
	}
	return buf[:got], err
}
func (Local) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (l Local) Terminal() bool                     { return l.Tty }

func (Local) Stat(path string) (Stat, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Stat{}, err
	}
	st := Stat{Mode: fi.Mode(), UID: -1, Mod: fi.ModTime()}
	if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
		st.UID = int(sys.Uid)
	}
	return st, nil
}

func (Local) List(dir string) ([]DirEntry, error) {
	entries, err := os.ReadDir(dir)
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, DirEntry{Name: e.Name(), Dir: e.IsDir(), Link: e.Type()&fs.ModeSymlink != 0})
	}
	return out, err
}

func (Local) Run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTime)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err == nil {
		return out.String(), nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), &Failure{Code: exit.ExitCode(), Stderr: strings.TrimSpace(errOut.String())}
	}
	return out.String(), err
}

func (Local) Space(path string) (Space, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Space{}, err
	}
	return Space{Free: st.Bavail * uint64(st.Bsize), Tmpfs: st.Type == unix.TMPFS_MAGIC}, nil
}

// reachTime is how long a probe of the network waits for an answer.
const reachTime = 8 * time.Second

func (Local) Reach(url string) (int, error) {
	c := http.Client{
		Timeout: reachTime,
		// The first answer is the one asked about: a redirect is an answer.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
