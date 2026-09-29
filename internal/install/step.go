// Package install is the installer without its screen: the steps it takes,
// the manifest of what they changed, the journal of what they ran, and the
// check of the machine before any of it. A run tells what happens through
// events, and whoever shows them — the screen or the plain view — is a sink
// the run is handed; nothing here draws.
package install

import (
	"errors"
	"fmt"
	"os"
)

// Step is one thing the installer does to the machine. Every step is
// written to the same contract, which is what makes a run resumable and an
// install removable:
//
//   - Done looks at the machine itself, not at a file of progress, and says
//     whether there is nothing to do; a run that stopped halfway goes on
//     from the first step that is not done.
//   - Apply makes the change and records every piece of it in the manifest
//     as it goes, before the piece is made.
//   - Verify checks the machine after Apply; a failure stops the run with a
//     diagnosis. A step of only Verify is a check that changes nothing.
//   - Undo takes a line of the manifest back. It is idempotent, so a line
//     written for a change that never happened is harmless.
type Step struct {
	ID, Title string
	Done      func(*Run) (bool, error)
	Apply     func(*Run) error
	Verify    func(*Run) error
	Undo      func(*Run, Entry) error
}

// Mark is how a line under a step reads: passed, a warning, a stop, or a
// plain note.
type Mark int

const (
	Note Mark = iota
	Pass
	Warn
	Stop
)

// EventType is what happened in a run.
type EventType int

const (
	Opened  EventType = iota // a step began
	Said                     // a line under the step
	Changed                  // the step changed something on the machine
	Closed                   // the step ended; Err says how
)

// Event is what a run tells its sink.
type Event struct {
	Type  EventType
	Title string // the step's
	Mark  Mark   // Said
	Text  string // Said, Changed
	Err   error  // Closed: nil when the step went through
}

// Sink takes the events of a run: the screen, the plain view, a test.
type Sink func(Event)

// Run is one run of the installer: the manifest it records into, the journal
// it writes, and the sink that shows it. A run without a manifest is read
// only — the check and the plan — and refuses to record anything.
type Run struct {
	Manifest *Manifest
	Journal  *Journal
	Sink     Sink

	// Answers are the answers given as flags, by the name of the flag, and
	// Yes takes the suggested answer of every question the flags leave
	// open. Ask asks the person the rest; without it — no terminal, or the
	// plain view — a question left open stops the run.
	Answers map[string]string
	Yes     bool
	Ask     func(Question) (string, error)

	step    *Step
	changed []Entry
}

func (r *Run) emit(e Event) {
	if r.Sink != nil {
		r.Sink(e)
	}
}

// Say puts a line under the step at work.
func (r *Run) Say(m Mark, text string) {
	title := ""
	if r.step != nil {
		title = r.step.Title
	}
	r.emit(Event{Type: Said, Title: title, Mark: m, Text: text})
}

// ErrReadOnly is what Record answers in a run without a manifest.
var ErrReadOnly = errors.New("this run changes nothing, so it records nothing")

// Record writes a line of the manifest for a change the step is about to
// make. It comes before the change, never after: a run that dies in the
// middle of the change leaves the line, and uninstall still finds what may
// have been made. A kind without an undo is refused.
func (r *Run) Record(kind Kind, target, meta string) error {
	if r.Manifest == nil {
		return ErrReadOnly
	}
	if r.step == nil {
		return errors.New("a line of the manifest is recorded by a step, and no step is at work")
	}
	if r.step.Undo == nil || undos[kind] == nil {
		return fmt.Errorf("step %s records a %s with no undo: nothing would take it back", r.step.ID, kind)
	}
	e := Entry{Step: r.step.ID, Kind: string(kind), Target: target, Meta: meta}
	if err := r.Manifest.Append(e); err != nil {
		return fmt.Errorf("the manifest did not take %s %s: %w", kind, target, err)
	}
	r.changed = append(r.changed, e)
	r.emit(Event{Type: Changed, Title: r.step.Title, Text: target})
	return nil
}

// MakeDir makes a directory the step needs, recording it first. A directory
// that is already there is left out of the manifest: it is not the
// installer's to remove.
func (r *Run) MakeDir(path string, perm os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := r.Record(Dir, path, "created"); err != nil {
		return err
	}
	return os.Mkdir(path, perm)
}

// ChangedInRun is what this run recorded, in order: the summary a stop or
// a Ctrl+C prints.
func (r *Run) ChangedInRun() []Entry { return append([]Entry(nil), r.changed...) }

// Do takes a step through its contract: nothing if it is done, else Apply
// and then Verify. The sink sees the step open and close, with the error
// that stopped it.
func (r *Run) Do(s *Step) (err error) {
	r.step = s
	r.emit(Event{Type: Opened, Title: s.Title})
	defer func() {
		r.emit(Event{Type: Closed, Title: s.Title, Err: err})
		r.step = nil
	}()
	if s.Done != nil {
		done, err := s.Done(r)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	if s.Apply != nil {
		if err := s.Apply(r); err != nil {
			return err
		}
	}
	if s.Verify != nil {
		return s.Verify(r)
	}
	return nil
}

// Undo hands a line of the manifest to the undo of the step that wrote it.
func (r *Run) Undo(s *Step, e Entry) error {
	if s.Undo == nil {
		return fmt.Errorf("step %s has no undo for %s %s", s.ID, e.Kind, e.Target)
	}
	r.step = s
	defer func() { r.step = nil }()
	return s.Undo(r, e)
}

// UndoKind is the undo of a kind, for a step whose lines need nothing more
// than their kind knows.
func UndoKind(r *Run, e Entry) error {
	undo := undos[Kind(e.Kind)]
	if undo == nil {
		return fmt.Errorf("no undo knows the kind %q of %s", e.Kind, e.Target)
	}
	return undo(r, e)
}
