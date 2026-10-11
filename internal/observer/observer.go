// Package observer keeps the commands a session hands the panel to watch,
// and the alarms a session sets to be woken at a moment.
//
// A session that waits on something outside it — a CI run, a log, a build
// that takes the night — has its own ways to wait, and each is held by the
// process of the session: a background shell or a Monitor of claude has a
// ceiling on how long it lives, and a restart of the session ends it. An
// observer is held by the host instead. The executor runs the command in a
// transient unit of the user's manager, reads what it prints and wakes the
// session that started it with a letter — when a line comes, when a line the
// session named comes, when the command ends — however long that takes.
//
// An observer lives as long as the session that started it and no longer:
// the process of a claude session, the thread of a codex one. Any end of
// that — a close, a restart, the restart past the context cap, a holder that
// died — ends the observer, since a session started again knows nothing of
// what the one before it was waiting for, and a command nobody waits on is a
// process left on the host.
//
// An alarm waits on nothing but a moment, so it has no command and no unit:
// the executor rings it itself and keeps it on the disk to ring after its own
// restart. It wakes its session with the same letter and ends with the session
// by the same rule.
package observer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// When says what of a command wakes its session. The end of the command
// wakes it whatever is chosen: a session that waits for a line of a command
// that died would otherwise wait for good.
type When string

const (
	// EveryLine wakes the session on every line the command prints.
	EveryLine When = "line"
	// Matching wakes it on a line the pattern of the observer finds.
	Matching When = "match"
	// AtExit wakes it only when the command ends.
	AtExit When = "exit"
)

// The bounds of what a session asks for.
const (
	NameMax    = 40
	CommandMax = 4000
	PatternMax = 500
	TextMax    = 2000
	// GatherMax bounds how long the lines of one wake are gathered, in
	// seconds, and GatherDefault is what a session that names nothing gets:
	// a command that prints a burst wakes its session once for the burst.
	GatherMax     = 3600
	GatherDefault = 10
)

// The bounds of what the host keeps. A session keeps PerSession observers at
// most and the host Total, and the log of one is two files of LogMax at most:
// the full one is moved aside once and the older one beside it goes. What
// the observers of the host write to the disk is never more than
// Total × 2 × LogMax, and they all go with their sessions.
const (
	PerSession = 5
	Total      = 20
	LogMax     = 1 << 20
)

// UnitPrefix opens the name of the unit of every observer, so the units of
// the observers stand apart among the units of the user's manager and an
// executor started again finds its own.
const UnitPrefix = "aacpanel-observer-"

// Unit is the name of the unit an observer runs in.
func Unit(id string) string { return UnitPrefix + id + ".service" }

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Spec is what a session asks an observer for.
type Spec struct {
	// Name is what the session calls the observer by: the name its wakes are
	// signed with and the one it stops it by.
	Name    string `json:"name"`
	Command string `json:"command"`
	// Dir is the directory the command runs in.
	Dir     string `json:"dir"`
	Wake    When   `json:"wake"`
	Pattern string `json:"pattern,omitempty"`
	// Gather is how many seconds after the first line of a wake its other
	// lines are gathered before the wake goes: the lines a command prints
	// together reach the session as one letter, and a session is woken no
	// more often than that.
	Gather int `json:"gather"`
	// Text is what the session told itself to do when woken, said first in
	// every wake.
	Text string `json:"text,omitempty"`
	// Path is the PATH of the session, which the command runs with: the
	// tools the session reaches by name are reached by its observer the same
	// way. Nothing else of the session's environment goes with it — a token
	// it holds in a variable stays out of a unit and off the disk.
	Path string `json:"path,omitempty"`
}

// Check fills in what a session left to the panel and refuses what the panel
// does not take, with what to change.
func (s *Spec) Check() (*regexp.Regexp, error) {
	switch {
	case !namePattern.MatchString(s.Name) || len(s.Name) > NameMax:
		return nil, fmt.Errorf("the name %q is not one: up to %d letters, digits, dots, dashes and underscores, "+
			"starting with a letter or a digit", s.Name, NameMax)
	case strings.TrimSpace(s.Command) == "":
		return nil, errors.New("there is no command to watch")
	case len(s.Command) > CommandMax:
		return nil, fmt.Errorf("the command is longer than %d bytes: put it in a script and watch the script", CommandMax)
	case !filepath.IsAbs(s.Dir):
		return nil, fmt.Errorf("the directory %q is not absolute", s.Dir)
	case utf8.RuneCountInString(s.Text) > TextMax:
		return nil, fmt.Errorf("the text of the wake is longer than %d characters", TextMax)
	case s.Gather < 0 || s.Gather > GatherMax:
		return nil, fmt.Errorf("lines are gathered for 1 to %d seconds, not %d", GatherMax, s.Gather)
	}
	if s.Gather == 0 {
		s.Gather = GatherDefault
	}
	switch s.Wake {
	case EveryLine, AtExit:
		if s.Pattern != "" {
			return nil, fmt.Errorf("a pattern is for wake %q only: wake %q takes none", Matching, s.Wake)
		}
		return nil, nil
	case Matching:
		if s.Pattern == "" {
			return nil, fmt.Errorf("wake %q needs the pattern a line is matched by", Matching)
		}
		if len(s.Pattern) > PatternMax {
			return nil, fmt.Errorf("the pattern is longer than %d bytes", PatternMax)
		}
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("the pattern is not one Go's regexp takes: %w", err)
		}
		return re, nil
	}
	return nil, fmt.Errorf("wake %q is not one: %q, %q or %q", s.Wake, EveryLine, Matching, AtExit)
}

// Caller is the session a call comes from, as the panel's server under it
// found it: the conversation and the process of a claude, or the thread of a
// codex with the home of its codex and its directory. The keeper does not
// take it on trust: the executor finds the live session it names first.
type Caller struct {
	Conversation string `json:"conversation"`
	PID          int    `json:"pid,omitempty"`
	Name         string `json:"name,omitempty"`
	Codex        bool   `json:"codex,omitempty"`
	Home         string `json:"home,omitempty"`
	Dir          string `json:"dir,omitempty"`
}

// Owner is the session an observer belongs to: a claude by its process and
// the start of it, which no restart keeps, or a codex by its thread.
type Owner struct {
	// Session is the name the session goes by, for the log of the executor.
	Session      string `json:"session"`
	Conversation string `json:"conversation"`
	PID          int    `json:"pid,omitempty"`
	Start        string `json:"procStart,omitempty"`
	Codex        bool   `json:"codex,omitempty"`
}

// Same says whether two owners are one session.
func (o Owner) Same(other Owner) bool {
	if o.Codex || other.Codex {
		return o.Codex && other.Codex && o.Conversation == other.Conversation
	}
	return o.PID == other.PID && o.Start == other.Start && o.Conversation == other.Conversation
}

// Ending is how the command of an observer ended.
type Ending struct {
	Code   int    `json:"code"`
	Signal string `json:"signal,omitempty"`
	// Memory is a command the kernel killed at the memory ceiling of its unit.
	Memory bool `json:"memory,omitempty"`
	// Lost is a unit that ended with no word of how: whatever ran the
	// command was killed with it, or never started.
	Lost bool      `json:"lost,omitempty"`
	At   time.Time `json:"at"`
}

// String says how the command ended, as a wake and a list say it.
func (e Ending) String() string {
	switch {
	case e.Lost:
		return "its unit ended with no word of how"
	case e.Memory:
		return fmt.Sprintf("killed at the memory ceiling of its unit, %s", MemoryMax)
	case e.Signal != "":
		return "killed by signal " + e.Signal
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

// Record is an observer as the keeper keeps it on the disk: what it was
// asked for, whose it is and how far its log is read. It outlives the
// executor, which reads it back when it starts again.
type Record struct {
	ID      string    `json:"id"`
	Spec    Spec      `json:"spec"`
	Owner   Owner     `json:"owner"`
	Started time.Time `json:"started"`
	// Inode and Offset are where the reading of the log stands: the file by
	// its inode, since a full one is moved aside, and the first byte of it
	// not read yet. Only whole lines are read past.
	Inode  uint64 `json:"inode,omitempty"`
	Offset int64  `json:"offset"`
	// Woken counts the wakes the session was given, LastWake is the last.
	Woken    int       `json:"woken"`
	LastWake time.Time `json:"lastWake,omitzero"`
	// Ended is how the command ended, once it has and the session was told.
	Ended *Ending `json:"ended,omitempty"`
}

// Dir is where the observers of the host are kept, a directory each: the
// state of the owner's own, outside the state directory of the panel, which
// is mounted into the service's container. The alarms are kept beside it, in
// alarms/, a file each.
func Dir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "aacpanel", "observers")
}

// The files of one observer.
const (
	recordFile = "observer.json"
	logFile    = "log"
	oldLogFile = "log.1"
	endFile    = "exit.json"
	pidFile    = "pid"
)

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ReadRecord reads the record of the observer kept in dir.
func ReadRecord(dir string) (Record, error) {
	raw, err := os.ReadFile(filepath.Join(dir, recordFile))
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return Record{}, fmt.Errorf("the record %s is not read: %w", filepath.Join(dir, recordFile), err)
	}
	return r, nil
}

// writeJSON replaces a file whole: a reader meets the old file or the new
// one, never half of one.
func writeJSON(path string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeRecord(dir string, r Record) error {
	return writeJSON(filepath.Join(dir, recordFile), r)
}

func readEnding(dir string) (*Ending, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, endFile))
	if err != nil {
		return nil, false
	}
	var e Ending
	if json.Unmarshal(raw, &e) != nil {
		return nil, false
	}
	return &e, true
}
