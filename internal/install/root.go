package install

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Place is what a run installs for: the clone and the user. A line of the
// manifest names only its target; an undo that runs root.sh or compose
// takes the rest from here.
type Place struct {
	Clone, User string
}

// RootScript is the part of the installer that runs as root.
func (p Place) RootScript() string { return filepath.Join(p.Clone, "deploy", "install", "root.sh") }

// manifestLine is how root.sh hands a line of the manifest over: it prints
// one before every change it makes.
const manifestLine = "MANIFEST\t"

// AsRoot runs root.sh with args as root. With a person at the terminal the
// screen frames the command, asks, and gives the terminal to sudo, which
// asks for the password there; a plain run has nobody to type it and runs
// sudo -n. Each MANIFEST line root.sh prints is recorded by the step at
// work the moment it comes — root.sh prints it before the change, so the
// manifest has the line before the change is made.
func (r *Run) AsRoot(title, says string, args ...string) error {
	if r.Place == nil {
		return errors.New("this run does not know its clone, and root.sh lives there")
	}
	script := r.Place.RootScript()
	admin := "sudo bash " + script + " " + strings.Join(args, " ")
	var (
		mu       sync.Mutex
		recorded error
		said     []string
	)
	line := func(l string) {
		mu.Lock()
		defer mu.Unlock()
		rest, ok := strings.CutPrefix(l, manifestLine)
		if !ok {
			said = append(said, l)
			return
		}
		f := strings.Split(rest, "\t")
		var err error
		if len(f) != 3 {
			err = fmt.Errorf("root.sh printed a manifest line of %d fields: %q", len(f), l)
		} else {
			err = r.Record(Kind(f[0]), f[1], f[2])
		}
		if err != nil && recorded == nil {
			recorded = err
		}
	}
	if r.Hand == nil {
		argv := append([]string{"sudo", "-n", "bash", script}, args...)
		_, err := r.exec(Cmd{Argv: argv}, line)
		if err != nil {
			return rootFailed(err, admin)
		}
		return recorded
	}
	argv := append([]string{"sudo", "bash", script}, args...)
	err := r.Hand(Handover{Title: title, Argv: argv, Says: says, Script: script, Admin: admin, Line: func(l string) {
		line(l)
		r.emit(Event{Type: Output, Title: r.title(), Text: l})
	}})
	if r.Journal != nil {
		_ = r.Journal.Command(argv, strings.Join(said, "\n"), err)
	}
	if err != nil {
		if errors.Is(err, ErrDeclined) {
			return &Failed{Diagnosis: "the root part did not run: you said no",
				Fix: []string{"An administrator runs, as root:", admin, "and then ./install.sh goes on from here."}}
		}
		tail := said
		if len(tail) > tailLines {
			tail = tail[len(tail)-tailLines:]
		}
		return &Failed{Diagnosis: rootDiagnosis(err, said), Tail: tail,
			Fix: []string{"Nothing after the last line above ran as root. Run ./install.sh again when it can go through."}}
	}
	return recorded
}

// rootFailed is a root command of a plain run that failed: sudo that wants
// a password nobody can type, or root.sh that stopped.
func rootFailed(err error, admin string) *Failed {
	f := fail("root.sh failed", err)
	var ran *Ran
	if errors.As(err, &ran) {
		f.Diagnosis = rootDiagnosis(ran.Err, ran.Tail)
	}
	if strings.Contains(f.Diagnosis, "password") {
		f.Fix = []string{"Ask an administrator to run, as root:", admin, "and then run ./install.sh again."}
	}
	return f
}

// rootDiagnosis is the line that says why the root part stopped: the stop
// root.sh printed, or what sudo said.
func rootDiagnosis(err error, said []string) string {
	for i := len(said) - 1; i >= 0; i-- {
		if strings.HasPrefix(said[i], "stop: ") {
			return strings.TrimPrefix(said[i], "stop: ")
		}
	}
	var fl *Failure
	if errors.As(err, &fl) {
		why := strings.ToLower(fl.Stderr)
		switch {
		case strings.Contains(why, "a password is required"), strings.Contains(why, "a terminal is required"):
			return "sudo needs a password, and there is no terminal to type it at"
		case strings.Contains(why, "incorrect password"):
			return "sudo: 3 incorrect password attempts"
		case fl.Stderr != "":
			return "root.sh stopped: " + firstLine(fl.Stderr)
		}
		return fmt.Sprintf("root.sh stopped with status %d", fl.Code)
	}
	return "root.sh did not run: " + err.Error()
}
