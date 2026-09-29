// Package checklist keeps the checklist a session makes of its work: the list
// of steps the model sends through the panel's checklist tool, one file a
// session. A session is told by its place — the account it runs in and the
// directory it works in — and by its name, so the sessions of one directory
// keep checklists of their own, and a session started again under its name,
// afresh or going on with its conversation, finds its checklist where it was
// left. A session without a name is told by its place alone: nothing else
// tells it from another session there. The collector reads the file into the
// row of the session and into the state of its conversation; nothing else
// writes it.
package checklist

import (
	"crypto/sha256"
	"encoding/hex"
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

	"aacpanel/internal/mcp"
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

// A checklist is a line of steps a person reads on a phone, not a document:
// past these the tool refuses, and the model shortens the checklist rather
// than having it cut without a word.
const (
	MaxItems = 40
	MaxText  = 200
	MaxNote  = 300
)

// Stamp is how every time of a checklist is written: UTC, to the second.
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

// File is the file of the checklist of a session — its place and its name,
// or its place alone for a session without a name — or empty for a place that
// is not one. A path does not make a file name, so the name is a hash of the
// two paths and the name of the session — the collector and the reminder hook
// compute the same; the file holds them, and a reader takes it only for the
// session it names.
func File(p mcp.Place, name string) string {
	where, ok := p.Clean()
	if !ok {
		return ""
	}
	key := where.ConfigDir + "\x00" + where.Dir
	if name != "" {
		key += "\x00" + name
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + ".json"
}

// Checklist is what lies on disk for one session.
type Checklist struct {
	ConfigDir string `json:"configDir"`
	Dir       string `json:"dir"`
	// Name is the name of the session, empty for a session without one.
	Name string `json:"name,omitempty"`
	// SessionID and PID are the conversation and the claude process that sent
	// the checklist last. The session is the checklist's key; these say who
	// holds it now: the feed of a conversation that is over shows the checklist
	// only while it is the one that sent it last, and a process that sent it
	// has the tool to send it again.
	SessionID string `json:"sessionId,omitempty"`
	PID       int    `json:"pid"`
	At        string `json:"at"`
	Note      string `json:"note,omitempty"`
	Items     []Item `json:"items"`
}

// Dir is where the checklists lie: the owner's state, beside the rest of what
// the panel keeps on the host.
func Dir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "aacpanel", "checklists")
}

var conversationID = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z-]*$`)

// filedName is the name of a checklist filed under a conversation: its id,
// which claude makes a UUID. The name of a session's checklist is a hash and
// never one.
var filedName = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\.json$`)

// Refusal is a checklist the tool does not keep, with what to change: the model
// reads it and sends the checklist again.
type Refusal struct{ Why string }

func (r Refusal) Error() string { return r.Why }

// Clean squeezes the text of every step and the note to one line and says
// what of them the tool does not take.
func Clean(items []Item, note string) ([]Item, string, error) {
	if len(items) > MaxItems {
		return nil, "", Refusal{fmt.Sprintf("the checklist has %d steps, at most %d are kept: join the small ones", len(items), MaxItems)}
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

// Keep writes the checklist of a session and returns what was written; an
// empty list removes the checklist and returns nil. The file is replaced
// whole by a rename, so the collector reads the old checklist or the new one,
// never half of each. A step that keeps its text and its status keeps the
// time it took it, across a restart of the session as well.
func Keep(dir string, b mcp.Binding, items []Item, note string, now time.Time) (*Checklist, error) {
	where, ok := b.Place.Clean()
	if !ok {
		return nil, fmt.Errorf("the place of the session is not known: %q under the account %q", b.Place.Dir, b.Place.ConfigDir)
	}
	if b.SessionID != "" && !conversationID.MatchString(b.SessionID) {
		return nil, fmt.Errorf("%q does not look like a conversation id", b.SessionID)
	}
	items, note, err := Clean(items, note)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, File(where, b.Name))
	if len(items) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("the checklist was not cleared: %w", err)
		}
		return nil, nil
	}

	stamp := now.UTC().Format(Stamp)
	was := sinceOf(Read(dir, where, b.Name))
	for i := range items {
		if items[i].Status != Active && items[i].Status != Done {
			continue
		}
		items[i].Since = stamp
		if at, ok := was[items[i].Status+"\x00"+items[i].Text]; ok {
			items[i].Since = at
		}
	}
	p := &Checklist{ConfigDir: where.ConfigDir, Dir: where.Dir, Name: b.Name, SessionID: b.SessionID, PID: b.PID,
		At: stamp, Note: note, Items: items}
	if err := write(dir, path, p); err != nil {
		return nil, err
	}
	return p, nil
}

func sinceOf(p *Checklist) map[string]string {
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

func write(dir, path string, p *Checklist) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("the checklists directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".checklist-*")
	if err != nil {
		return fmt.Errorf("the checklist was not written: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("the checklist was not written: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("the checklist was not written: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("the checklist was not written: %w", err)
	}
	return nil
}

// Read returns the checklist of a session, told by its place and its name,
// or nil when there is none.
func Read(dir string, place mcp.Place, name string) *Checklist {
	where, ok := place.Clean()
	if !ok {
		return nil
	}
	return readFile(filepath.Join(dir, File(where, name)), where, name)
}

func readFile(path string, where mcp.Place, name string) *Checklist {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var p Checklist
	if json.Unmarshal(raw, &p) != nil || p.ConfigDir != where.ConfigDir || p.Dir != where.Dir || p.Name != name {
		return nil
	}
	return &p
}

// Adopt returns the checklist of the session bound, taking over first what a
// server that keys checklists otherwise left for it: the server a live
// session was started with writes the file it knows until the session starts
// again.
//
// A server that does not tell the sessions of a place apart keeps one
// checklist a place, whichever session sent it. A named session with no
// checklist of its own takes that one over only while its conversation is
// the one that sent it last: which session sent the checklist of another
// conversation is not to be told, and it stays where it is.
//
// A checklist filed under a conversation is named by the conversation and
// names no place. Its place is told by the transcript of its conversation,
// which claude keeps under the account in the directory of the project; a
// session without a name takes over the newest of them, a named session only
// the one of its own conversation.
//
// A session that already has a checklist takes over nothing. What is taken
// over is written as the session's and the files it came from go; a file
// nobody took over goes with the sweep.
func Adopt(dir string, b mcp.Binding) *Checklist {
	where, ok := b.Place.Clean()
	if !ok {
		return nil
	}
	if p := Read(dir, where, b.Name); p != nil {
		return p
	}
	var newest *Checklist
	var taken []string
	if b.Name != "" && b.SessionID != "" {
		placed := filepath.Join(dir, File(where, ""))
		if p := readFile(placed, where, ""); p != nil && p.SessionID == b.SessionID {
			newest, taken = p, []string{placed}
		}
	}
	if newest == nil {
		newest, taken = filedFor(dir, where, b)
	}
	if newest == nil {
		return nil
	}
	newest.ConfigDir, newest.Dir, newest.Name = where.ConfigDir, where.Dir, b.Name
	if write(dir, filepath.Join(dir, File(where, b.Name)), newest) != nil {
		return nil
	}
	for _, path := range taken {
		os.Remove(path)
	}
	return newest
}

// filedFor returns the newest checklist filed under a conversation of the
// place that the session takes over, with the files it takes over.
func filedFor(dir string, where mcp.Place, b mcp.Binding) (*Checklist, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	project := filepath.Join(where.ConfigDir, "projects", projectSlug(where.Dir))
	var newest *Checklist
	var taken []string
	for _, e := range entries {
		if e.IsDir() || !filedName.MatchString(e.Name()) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if b.Name != "" && id != b.SessionID {
			continue
		}
		if _, err := os.Stat(filepath.Join(project, id+".jsonl")); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p Checklist
		if json.Unmarshal(raw, &p) != nil || p.SessionID != id || p.Dir != "" || len(p.Items) == 0 {
			continue
		}
		taken = append(taken, filepath.Join(dir, e.Name()))
		if newest == nil || p.At > newest.At {
			newest = &p
		}
	}
	return newest, taken
}

// projectSlug is the name claude gives the directory of a project's
// transcripts: the path with every character but a Latin letter or a digit
// made a dash.
func projectSlug(dir string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, dir)
}

// MaxAge is how long a checklist nobody wrote to stays on disk. A session
// whose checklist has not been touched for this long has moved on, and a
// directory that is gone leaves its checklist behind.
const MaxAge = 30 * 24 * time.Hour

// Sweep removes the checklists older than MaxAge, and what a write that never
// finished left behind, and says how many went.
func Sweep(dir string, now time.Time) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	gone := 0
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".checklist-")) {
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
