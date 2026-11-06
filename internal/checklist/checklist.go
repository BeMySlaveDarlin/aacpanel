// Package checklist keeps the checklist a session makes of its work: the list
// of steps the model sends through the panel's checklist tool, one file a
// session. A session is told as its role is: by the account it runs in, by
// the repository it works in and by its name, so the sessions of one
// repository keep checklists of their own, and a session started again under
// its name — afresh or going on with its conversation, in the main checkout
// or in a worktree of it — finds its checklist where it was left. A session
// without a name is told by its account and its repository alone: nothing
// else tells it from another session there. A thread of codex is told by its
// thread. The collector reads the file into the row of the session and into
// the state of its conversation; nothing else writes it.
package checklist

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

	"aacpanel/internal/mcp"
	"aacpanel/internal/role"
	"aacpanel/internal/sessionfile"
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

// File is the file of the checklist of a session — its account, its
// repository and its name, or the first two alone for a session without a
// name — or empty for a place that is not one. The repository is the one the
// key of a role names, so a session moving between the main checkout and a
// worktree of it keeps its checklist, and sessions of one name in two
// worktrees of a repository share one; the collector and the reminder hook
// compute the same name, the file holds what it is made of, and a reader
// takes it only for the session it names.
func File(p mcp.Place, name string) string {
	where, ok := p.Clean()
	if !ok {
		return ""
	}
	return sessionfile.Name(where.ConfigDir, role.Repo(where.Dir), name)
}

// Key is a file the checklist of a session is looked for in, with what of the
// file tells it is the session's beside the account and the name: the field
// that names where the checklist is kept, and what it holds there.
type Key struct {
	File  string
	Field string
	Where string
}

// Keys are the files the checklist of a session is looked for in, in order:
// the one it is kept in, under its repository, and then the one of its
// account, its directory and its name. A server that keys checklists by the
// directory writes the second until its session starts again; a session with
// no checklist under its repository reads that one, and its next write — or
// the server's first read of it — moves it under the repository. The price:
// a file kept by the directory is found only from that directory, and one no
// session moves goes with the sweep. For a directory no checkout holds the two
// are one file. None for a place that is not one.
func Keys(p mcp.Place, name string) []Key {
	where, ok := p.Clean()
	if !ok {
		return nil
	}
	return []Key{
		{File: File(where, name), Field: "repo", Where: role.Repo(where.Dir)},
		{File: sessionfile.Name(where.ConfigDir, where.Dir, name), Field: "dir", Where: where.Dir},
	}
}

// holds tells whether a checklist names the place the key looks for it by.
func (k Key) holds(p *Checklist) bool {
	if k.Field == "repo" {
		return p.Repo == k.Where
	}
	return p.Dir == k.Where
}

// ThreadFile is the file of the checklist of a codex thread, or empty for what
// is no id of one. A thread is told by its id alone: codex keeps it for the
// life of the thread — resumed, joined by another client, under a daemon
// started again — and no thread of any home takes it again. Its place would
// tell it no better and could tell it worse: the server of the thread finds
// the home by the lock the kernel names, the collector by the home the
// executor was given, and a link on the way makes the two paths differ. The
// prefix keeps the name apart from a checklist filed under a conversation.
func ThreadFile(thread string) string {
	if !threadID.MatchString(thread) {
		return ""
	}
	return threadPrefix + thread + ".json"
}

const threadPrefix = "codex-"

// threadID is the id codex gives a thread, a UUID.
var threadID = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

// fileOf is the file of the checklist of the session bound: its thread for a
// thread of codex, its account, its repository and its name for a claude.
func fileOf(where mcp.Place, b mcp.Binding) (string, error) {
	if !b.Codex {
		return File(where, b.Name), nil
	}
	if file := ThreadFile(b.SessionID); file != "" {
		return file, nil
	}
	return "", fmt.Errorf("%q is not the id of a codex thread", b.SessionID)
}

// Checklist is what lies on disk for one session. Repo is what the key of a
// claude session is made of, beside the account and the name; Dir is the
// directory it sent the checklist from last, and tells a file kept by the
// directory. A thread of codex keeps the home of its codex for the config
// directory, its thread for the conversation and the codex that holds it for
// the process, and no repository.
type Checklist struct {
	ConfigDir string `json:"configDir"`
	Repo      string `json:"repo,omitempty"`
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
	return sessionfile.Dir("checklists")
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
	file, err := fileOf(where, b)
	if err != nil {
		return nil, err
	}
	items, note, err = Clean(items, note)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, file)
	byDir := byDirOf(dir, where, b)
	if len(items) == 0 {
		for _, gone := range []string{path, byDir} {
			if gone == "" {
				continue
			}
			if err := os.Remove(gone); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("the checklist was not cleared: %w", err)
			}
		}
		return nil, nil
	}

	stamp := now.UTC().Format(Stamp)
	was := sinceOf(readBound(dir, where, b))
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
	if !b.Codex {
		p.Repo = role.Repo(where.Dir)
	}
	if err := write(dir, path, p); err != nil {
		return nil, err
	}
	if byDir != "" {
		os.Remove(byDir)
	}
	return p, nil
}

// byDirOf is the file a server keying checklists by the directory keeps the
// checklist of the session bound in, no longer read once the session writes
// under its repository; empty for a thread of codex, and for a directory no
// checkout holds, where the two are one file.
func byDirOf(dir string, where mcp.Place, b mcp.Binding) string {
	if b.Codex {
		return ""
	}
	keys := Keys(where, b.Name)
	if keys[1].File == keys[0].File {
		return ""
	}
	return filepath.Join(dir, keys[1].File)
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

// Read returns the checklist of a session, told by its account, its
// repository and its name, or nil when there is none. A session with no
// checklist under its repository reads the one kept by its directory.
func Read(dir string, place mcp.Place, name string) *Checklist {
	p, _, _ := find(dir, place, name)
	return p
}

// find returns the checklist of a session, the key it was found under and its
// file: the first of its keys whose file is the session's.
func find(dir string, place mcp.Place, name string) (*Checklist, int, string) {
	where, _ := place.Clean()
	for i, k := range Keys(where, name) {
		path := filepath.Join(dir, k.File)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p Checklist
		if json.Unmarshal(raw, &p) != nil || p.ConfigDir != where.ConfigDir || p.Name != name || !k.holds(&p) {
			continue
		}
		return &p, i, path
	}
	return nil, -1, ""
}

// ReadThread returns the checklist of a codex thread, or nil when there is
// none.
func ReadThread(dir, thread string) *Checklist {
	file := ThreadFile(thread)
	if file == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return nil
	}
	var p Checklist
	if json.Unmarshal(raw, &p) != nil || p.SessionID != thread {
		return nil
	}
	return &p
}

// readBound returns the checklist of the session bound as it lies, taking
// over nothing.
func readBound(dir string, where mcp.Place, b mcp.Binding) *Checklist {
	if b.Codex {
		return ReadThread(dir, b.SessionID)
	}
	return Read(dir, where, b.Name)
}

// Adopt returns the checklist of the session bound, taking over first what a
// server that keys checklists otherwise left for it: the server a live
// session was started with writes the file it knows until the session starts
// again.
//
// A server that keys checklists by the directory keeps the checklist of the
// session under its account, its directory and its name; a session with no
// checklist under its repository takes that one over.
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
// A session that already has a checklist under its repository takes over
// nothing, and neither does a thread of codex: no server ever kept its
// checklist under another key. What is taken over is written as the session's
// and the files it came from go; a file nobody took over goes with the sweep.
func Adopt(dir string, b mcp.Binding) *Checklist {
	where, ok := b.Place.Clean()
	if !ok {
		return nil
	}
	if b.Codex {
		return ReadThread(dir, b.SessionID)
	}
	newest, at, from := find(dir, where, b.Name)
	if at == 0 {
		return newest
	}
	var taken []string
	if newest != nil {
		taken = []string{from}
	}
	if newest == nil && b.Name != "" && b.SessionID != "" {
		if p, _, placed := find(dir, where, ""); p != nil && p.SessionID == b.SessionID {
			newest, taken = p, []string{placed}
		}
	}
	if newest == nil {
		newest, taken = filedFor(dir, where, b)
	}
	if newest == nil {
		return nil
	}
	newest.ConfigDir, newest.Repo, newest.Dir, newest.Name = where.ConfigDir, role.Repo(where.Dir), where.Dir, b.Name
	own := filepath.Join(dir, File(where, b.Name))
	if write(dir, own, newest) != nil {
		return nil
	}
	for _, path := range taken {
		if path != own {
			os.Remove(path)
		}
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
