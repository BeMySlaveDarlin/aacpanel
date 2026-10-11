package observer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Flag starts the executor as the observer inside its unit: it runs the
// command of the observer kept in the directory named after it.
const Flag = "-observe"

// drainWait is how long the output of a command that ended is still read: a
// child it left behind may hold the pipe open, and the end is not held back
// for it — the unit stops it once the observer is gone.
var drainWait = 2 * time.Second

// stopWait is how long a command asked to stop gets before it is killed;
// the unit gives it as long.
const stopWait = 5 * time.Second

// Ceilings of the unit of an observer, measured against what a session on
// the stream eats while it waits: its claude holds 260 to 320 MB and the
// whole of its tree, MCP servers with it, 470 MB and more, and it spends
// 0.3 to 0.7 per cent of one core idle and about 3 per cent at work. An
// observer is a watcher and gets no more than the session it wakes: the
// memory of the claude of an idle one, with no swap to spill into past it,
// and a twentieth of a core — a command polling every few seconds runs at its
// pace, a loop that runs away gets no more. Its niceness gives way to
// everything a person waits on. Its cores are those of every unit of the
// executor, the ones the machine description names.
const (
	MemoryMax = "384M"
	CPUQuota  = "5%"
	Nice      = 10
)

// Run is the observer inside its unit: it runs the command of the observer
// kept in dir, writes what the command prints to the log of the observer and
// how the command ended beside it. The executor reads both. It runs the
// command in a shell of its own, as the session would have, and with nothing
// on its input; a cancelled ctx stops it.
func Run(ctx context.Context, dir string) error {
	rec, err := ReadRecord(dir)
	if err != nil {
		return err
	}
	if start, ok := procStart(os.Getpid()); ok {
		_ = os.WriteFile(filepath.Join(dir, pidFile), []byte(fmt.Sprintf("%d %s\n", os.Getpid(), start)), 0o600)
	}
	out := &logWriter{dir: dir}
	defer out.Close()

	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", "-c", rec.Spec.Command)
	cmd.Dir = rec.Spec.Dir
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.Env = commandEnv(os.Environ(), rec.Spec.Path)
	// A group of its own: what the command starts goes with it when the
	// observer is stopped outside a unit, as the unit takes its whole group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		fmt.Fprintf(out, "the command did not start: %v\n", err)
		return writeJSON(filepath.Join(dir, endFile), Ending{Code: 127, At: time.Now()})
	}
	pw.Close()

	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, pr)
		close(copied)
	}()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	var end Ending
	select {
	case err = <-waited:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case err = <-waited:
		case <-time.After(stopWait):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			err = <-waited
		}
	}
	select {
	case <-copied:
	case <-time.After(drainWait):
	}
	pr.Close()
	end = ending(cmd.ProcessState, err)
	end.Memory = end.Signal == "KILL" && oomKilled()
	end.At = time.Now()
	out.Close()
	return writeJSON(filepath.Join(dir, endFile), end)
}

// commandEnv is the environment of the unit with the PATH of the session in
// place of its own.
func commandEnv(env []string, path string) []string {
	if path == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+path)
}

func ending(state *os.ProcessState, err error) Ending {
	if state == nil {
		return Ending{Code: -1}
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return Ending{Code: -1, Signal: signalName(ws.Signal())}
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return Ending{Code: -1}
	}
	return Ending{Code: state.ExitCode()}
}

// signalName is a signal as kill names it: TERM, KILL.
func signalName(s syscall.Signal) string {
	names := map[syscall.Signal]string{
		syscall.SIGHUP: "HUP", syscall.SIGINT: "INT", syscall.SIGQUIT: "QUIT", syscall.SIGABRT: "ABRT",
		syscall.SIGKILL: "KILL", syscall.SIGSEGV: "SEGV", syscall.SIGPIPE: "PIPE", syscall.SIGTERM: "TERM",
	}
	if n, ok := names[s]; ok {
		return n
	}
	return strconv.Itoa(int(s))
}

// oomKilled says whether the kernel killed a process of this unit at its
// memory ceiling: the memory controller of its group counts such kills. Out
// of a unit, or with no such controller, it says no.
func oomKilled() bool {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}
	_, group, ok := strings.Cut(strings.TrimSpace(string(raw)), "::")
	if !ok {
		return false
	}
	f, err := os.Open(filepath.Join("/sys/fs/cgroup", group, "memory.events"))
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		if n, ok := strings.CutPrefix(scan.Text(), "oom_kill "); ok {
			count, _ := strconv.Atoi(n)
			return count > 0
		}
	}
	return false
}

// procStart is when a process started, in clock ticks since the boot, as
// /proc/<pid>/stat says it: a pid used again by another process has another.
func procStart(pid int) (string, bool) {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", false
	}
	line := string(raw)
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return "", false
	}
	fields := strings.Fields(line[i+1:])
	if len(fields) < 20 {
		return "", false
	}
	return fields[19], true
}

// running says whether the observer of dir still runs: the process its pid
// file names is alive and is the one that wrote it. known is false while the
// file is not written yet.
func running(dir string) (alive, known bool) {
	raw, err := os.ReadFile(filepath.Join(dir, pidFile))
	if err != nil {
		return false, false
	}
	pid, start, ok := strings.Cut(strings.TrimSpace(string(raw)), " ")
	if !ok {
		return false, false
	}
	n, err := strconv.Atoi(pid)
	if err != nil {
		return false, false
	}
	now, ok := procStart(n)
	return ok && now == start, true
}

// logMax is LogMax, smaller in a test.
var logMax int64 = LogMax

// logWriter writes the log of an observer, moving a full file aside: the log
// of one observer is two files of LogMax at most.
type logWriter struct {
	dir  string
	f    *os.File
	size int64
}

func (w *logWriter) Write(p []byte) (int, error) {
	if w.f != nil && w.size > 0 && w.size+int64(len(p)) > logMax {
		w.f.Close()
		w.f = nil
		if err := os.Rename(filepath.Join(w.dir, logFile), filepath.Join(w.dir, oldLogFile)); err != nil {
			return 0, err
		}
	}
	if w.f == nil {
		f, err := os.OpenFile(filepath.Join(w.dir, logFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return 0, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return 0, err
		}
		w.f, w.size = f, info.Size()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *logWriter) Close() {
	if w.f != nil {
		w.f.Close()
		w.f = nil
	}
}
