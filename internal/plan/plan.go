// Package plan keeps the plan a session makes of its work: the list of steps
// the model sends through the panel's plan tool, one file a conversation. The
// collector reads the file into the row of the session and into the state of
// its conversation; nothing else writes it.
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// The statuses of a step. A dropped step stays in the list: the person reads
// what was given up as well as what was done.
const (
	Pending = "pending"
	Active  = "active"
	Done    = "done"
	Dropped = "dropped"
)

// Statuses are the statuses a step takes, in the order the tool names them.
var Statuses = []string{Pending, Active, Done, Dropped}

// A plan is a line of steps a person reads on a phone, not a document: past
// these the tool refuses, and the model shortens the plan rather than having
// it cut without a word.
const (
	MaxItems = 40
	MaxText  = 200
	MaxNote  = 300
)

// Stamp is how every time of a plan is written: UTC, to the second.
const Stamp = "2006-01-02T15:04:05Z"

// Item is one step.
type Item struct {
	Text   string `json:"text"`
	Status string `json:"status"`
	// Since is when the step took its status, kept for a step at work and a
	// step done: the person reads how long one has been going and when the
	// other ended. A step keeps its time while its text and status stay.
	Since string `json:"since,omitempty"`
}

// Plan is what lies on disk for one conversation.
type Plan struct {
	SessionID string `json:"sessionId"`
	// PID is the claude process that sent the plan. The same conversation
	// resumed in another process may have no plan tool at all — started by
	// hand, outside the panel — and is not asked to update a plan it cannot.
	PID   int    `json:"pid"`
	At    string `json:"at"`
	Note  string `json:"note,omitempty"`
	Items []Item `json:"items"`
}

// Dir is where the plans lie: the owner's state, beside the rest of what the
// panel keeps on the host.
func Dir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "aacpanel", "plans")
}

var conversationID = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z-]*$`)

// Path is the file of one conversation's plan.
func Path(dir, sessionID string) string {
	return filepath.Join(dir, sessionID+".json")
}

// Refusal is a plan the tool does not keep, with what to change: the model
// reads it and sends the plan again.
type Refusal struct{ Why string }

func (r Refusal) Error() string { return r.Why }

// Clean squeezes the text of every step and the note to one line and says
// what of them the tool does not take.
func Clean(items []Item, note string) ([]Item, string, error) {
	if len(items) > MaxItems {
		return nil, "", Refusal{fmt.Sprintf("the plan has %d steps, at most %d are kept: join the small ones", len(items), MaxItems)}
	}
	out := make([]Item, 0, len(items))
	for i, it := range items {
		text := oneLine(it.Text)
		switch {
		case text == "":
			return nil, "", Refusal{fmt.Sprintf("step %d has no text", i+1)}
		case utf8.RuneCountInString(text) > MaxText:
			return nil, "", Refusal{fmt.Sprintf("step %d is longer than %d characters: say it shorter", i+1, MaxText)}
		case !slices.Contains(Statuses, it.Status):
			return nil, "", Refusal{fmt.Sprintf("step %d has the status %q, not one of %s",
				i+1, it.Status, strings.Join(Statuses, ", "))}
		}
		out = append(out, Item{Text: text, Status: it.Status})
	}
	note = oneLine(note)
	if utf8.RuneCountInString(note) > MaxNote {
		return nil, "", Refusal{fmt.Sprintf("the note is longer than %d characters: say it shorter", MaxNote)}
	}
	return out, note, nil
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Keep writes the plan of a conversation and returns what was written; an
// empty list removes the plan and returns nil. The file is replaced whole by
// a rename, so the collector reads the old plan or the new one, never half of
// each. A step that keeps its text and its status keeps the time it took it.
func Keep(dir, sessionID string, pid int, items []Item, note string, now time.Time) (*Plan, error) {
	if !conversationID.MatchString(sessionID) {
		return nil, fmt.Errorf("%q does not look like a conversation id", sessionID)
	}
	items, note, err := Clean(items, note)
	if err != nil {
		return nil, err
	}
	path := Path(dir, sessionID)
	if len(items) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("the plan was not cleared: %w", err)
		}
		return nil, nil
	}

	stamp := now.UTC().Format(Stamp)
	was := sinceOf(Read(dir, sessionID))
	for i := range items {
		if items[i].Status != Active && items[i].Status != Done {
			continue
		}
		items[i].Since = stamp
		if at, ok := was[items[i].Status+"\x00"+items[i].Text]; ok {
			items[i].Since = at
		}
	}
	p := &Plan{SessionID: sessionID, PID: pid, At: stamp, Note: note, Items: items}
	if err := write(dir, path, p); err != nil {
		return nil, err
	}
	return p, nil
}

func sinceOf(p *Plan) map[string]string {
	out := map[string]string{}
	if p == nil {
		return out
	}
	for _, it := range p.Items {
		key := it.Status + "\x00" + it.Text
		if _, seen := out[key]; !seen && it.Since != "" {
			out[key] = it.Since
		}
	}
	return out
}

func write(dir, path string, p *Plan) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("the plans directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".plan-*")
	if err != nil {
		return fmt.Errorf("the plan was not written: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("the plan was not written: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("the plan was not written: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("the plan was not written: %w", err)
	}
	return nil
}

// Read returns the plan of a conversation, or nil when there is none.
func Read(dir, sessionID string) *Plan {
	if !conversationID.MatchString(sessionID) {
		return nil
	}
	raw, err := os.ReadFile(Path(dir, sessionID))
	if err != nil {
		return nil
	}
	var p Plan
	if json.Unmarshal(raw, &p) != nil || p.SessionID != sessionID {
		return nil
	}
	return &p
}

// MaxAge is how long a plan nobody wrote to stays on disk. A conversation
// that has not touched its plan for this long is over, and there is one file
// for every conversation that ever kept one.
const MaxAge = 30 * 24 * time.Hour

// Sweep removes the plans older than MaxAge, and what a write that never
// finished left behind, and says how many went.
func Sweep(dir string, now time.Time) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	gone := 0
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".plan-")) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) <= MaxAge {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			gone++
		}
	}
	return gone
}
