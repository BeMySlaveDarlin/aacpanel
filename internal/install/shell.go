package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Cmd is a command a step runs.
type Cmd struct {
	Argv []string
	// Env is added to the environment of the command. A secret travels
	// here and never in Argv: argv is seen by ps and written to the journal.
	Env []string
	Dir string
	// Quiet keeps the output out of the journal and off the screen: the
	// output of a command that prints a secret back.
	Quiet bool
	// Limit ends a command that hangs; zero lets it run to its end, as a
	// build must.
	Limit time.Duration
}

// Shell runs the commands of the steps: the processes of this machine, or
// the tables of a test.
type Shell interface {
	// Exec runs c to its end and gives its standard output. Every line of
	// either output goes to line as it comes. A command that fails gives a
	// *Failure with what it said on standard error.
	Exec(ctx context.Context, c Cmd, line func(string)) (string, error)
}

// Clock is the time of a run: a test waits for nothing.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// Exec runs a command of this machine. The command gets a process group of
// its own, so a Ctrl+C at the terminal does not tear it in the middle: the
// run stops after the step, at a safe point. Cancelling ctx — the second
// Ctrl+C — ends the whole group.
func (Local) Exec(ctx context.Context, c Cmd, line func(string)) (string, error) {
	if c.Limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Limit)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	lw := &lineWriter{line: line}
	var out, errOut bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out, lw)
	cmd.Stderr = io.MultiWriter(&errOut, lw)
	err := cmd.Run()
	lw.flush()
	if err == nil {
		return out.String(), nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), &Failure{Code: exit.ExitCode(), Stderr: strings.TrimSpace(errOut.String())}
	}
	return out.String(), err
}

// lineWriter hands what a command writes to line, a whole line at a time.
// Both outputs of a command write into one, so it holds a lock.
type lineWriter struct {
	mu   sync.Mutex
	line func(string)
	part []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part = append(w.part, p...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			return len(p), nil
		}
		if w.line != nil {
			w.line(strings.TrimRight(string(w.part[:i]), "\r"))
		}
		w.part = w.part[i+1:]
	}
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.part) > 0 && w.line != nil {
		w.line(string(w.part))
	}
	w.part = nil
}

// tailLines is how much of a command's output a stop shows under its step:
// the rest is in the journal.
const tailLines = 20

// Ran is a command of a step that failed: what it was, how it ended, and
// the last lines it said.
type Ran struct {
	Argv []string
	Err  error
	Tail []string
}

func (r *Ran) Error() string {
	return fmt.Sprintf("%s: %s", strings.Join(r.Argv, " "), firstLine(r.Err.Error()))
}

func (r *Ran) Unwrap() error { return r.Err }

// Exec runs a command of the step at work through the run's shell: the
// environment the run has learnt is laid under the command's own, every
// line goes to the sink as the step's output, and the command with its
// whole output goes into the journal — a quiet one without its output.
func (r *Run) Exec(c Cmd) (string, error) {
	return r.exec(c, nil)
}

func (r *Run) exec(c Cmd, each func(string)) (string, error) {
	if r.Shell == nil {
		return "", errors.New("this run has no shell to run " + c.Argv[0] + " with")
	}
	c.Env = append(r.envList(), c.Env...)
	var all []string
	var mu sync.Mutex
	line := func(l string) {
		if each != nil {
			each(l)
		}
		if c.Quiet {
			return
		}
		mu.Lock()
		all = append(all, l)
		mu.Unlock()
		r.emit(Event{Type: Output, Title: r.title(), Text: l})
	}
	out, err := r.Shell.Exec(r.context(), c, line)
	if r.Journal != nil {
		kept := strings.Join(all, "\n")
		if c.Quiet {
			kept = "[the output is not kept: it carries secrets]"
		}
		_ = r.Journal.Command(c.Argv, kept, err)
	}
	if err != nil {
		tail := all
		if len(tail) > tailLines {
			tail = tail[len(tail)-tailLines:]
		}
		return out, &Ran{Argv: c.Argv, Err: err, Tail: tail}
	}
	return out, nil
}

func (r *Run) envList() []string {
	var out []string
	for _, k := range sortedKeys(r.Env) {
		out = append(out, k+"="+r.Env[k])
	}
	return out
}

func (r *Run) title() string {
	if r.step != nil {
		return r.step.Title
	}
	return ""
}

func (r *Run) context() context.Context {
	if r.Ctx != nil {
		return r.Ctx
	}
	return context.Background()
}

func (r *Run) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return realClock{}
}

// Until asks cond every so often until it holds or limit passes, and says
// whether it held. An error of cond ends the wait at once.
func (r *Run) Until(limit, every time.Duration, cond func() (bool, error)) (bool, error) {
	c := r.clock()
	end := c.Now().Add(limit)
	for {
		ok, err := cond()
		if err != nil || ok {
			return ok, err
		}
		if !c.Now().Before(end) {
			return false, nil
		}
		c.Sleep(every)
	}
}

// Handover is a command that needs the person's terminal: sudo, which asks
// for a password there. The screen frames it and asks first — and shows
// the script when asked — then gives the terminal to it and takes it back.
type Handover struct {
	Title  string   // of the frame
	Argv   []string // what runs
	Says   string   // what it does, in a sentence or two
	Script string   // the file shown before a yes
	Admin  string   // what an administrator runs instead, on a no
	// Line takes every line the command prints on standard output.
	Line func(string)
}

// ErrDeclined is the person saying no to a command that needs root.
var ErrDeclined = errors.New("the person declined the command")
