package hook

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
)

// The background reminder: at the end of a turn a session is reminded of
// background work it left running long.
//
// Claude passes the Stop hook background_tasks, everything of the session
// still at work. That there is background work is no reason to speak: a
// session may be waiting for a build in earnest. Age is. The hook notes when
// it first saw each task and names only those running past firstRemind, each
// at most once in repeatRemind. Subagents and workflows are left out: they
// end by themselves and wake the session when done, so a session that waits
// for them is right to. What is forgotten are shell commands, monitors and
// the like — a wait loop with no time limit among them never ends at all.
//
// The reminder asks for no answer and goes as additional context of the Stop
// event, which lets the turn go on for a round; the stop that follows that
// round comes with stop_hook_active, and the hook is silent then. Blocking the
// stop instead would hold every end of a turn while the work runs. A task is
// named once in repeatRemind even without stop_hook_active, and the reminder
// is spoken only once its mark is on the disk: a mark that cannot be written
// would repeat it at every stop.
//
// Not for SubagentStop: background_tasks names no owner of a task, and a
// subagent would see itself and its neighbours in the list and stop what is
// not its own.
//
// The marks live in the state directory, a file a conversation. Their format
// stays compatible across updates so each task keeps when it was first seen.
// A file untouched for keepMarks goes. The cases live in
// deploy/claude/testdata/background-reminder.json.

const (
	firstRemind  = 60 * 60.0  // how long a task runs before it is first named, in seconds
	repeatRemind = 120 * 60.0 // and how long before it is named again
	keepMarks    = 7 * 24 * time.Hour
	listLimit    = 15
	fieldLimit   = 140
)

// selfEnding are the kinds of task that end by themselves and wake the
// session when done.
var selfEnding = map[string]bool{"subagent": true, "workflow": true}

var (
	// A wait loop without a time limit never ends by itself, and that is
	// worth a word of its own.
	waitLoop   = regexp.MustCompile(`\b(until|while)\b`)
	hasLimit   = regexp.MustCompile(`\btimeout\b|\bSECONDS\b|deadline|--max-time`)
	notSession = regexp.MustCompile(`[^A-Za-z0-9_-]`)
)

// writeMarks puts the marks of a conversation on the disk; a test puts a disk
// that takes no write in its place.
var writeMarks = replaceFile

// marksDir is the directory of the marks, under the state directory.
func marksDir() string {
	return filepath.Join(hostcfg.Load().StateDir, "background-reminder")
}

func remindBackground(_ string, payload []byte, now time.Time) Reply {
	var p any
	if json.Unmarshal(payload, &p) != nil {
		return Reply{}
	}
	return Reply{Context: remind(p, now, marksDir())}
}

// mark is when a task was first seen and last named, in seconds of the Unix
// clock.
type mark struct {
	First    float64 `json:"first"`
	Reminded float64 `json:"reminded"`
}

// remind is the reminder for a stop, or nothing when there is nothing to say.
func remind(payload any, now time.Time, dir string) string {
	p, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if event, ok := p["hook_event_name"]; ok && event != "Stop" {
		return ""
	}
	// The round after a reminder is the answer to it: it is never reminded again.
	if truthy(p["stop_hook_active"]) {
		return ""
	}
	session, _ := p["session_id"].(string)
	session = notSession.ReplaceAllString(session, "")
	if session == "" {
		return ""
	}
	path := filepath.Join(dir, session+".json")
	listed, _ := p["background_tasks"].([]any)
	var tasks []map[string]any
	for _, t := range listed {
		task, ok := t.(map[string]any)
		if !ok {
			continue
		}
		id, _ := task["id"].(string)
		kind, _ := task["type"].(string)
		if id == "" || selfEnding[kind] {
			continue
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 {
		_ = os.Remove(path)
		return ""
	}

	at := float64(now.UnixNano()) / 1e9
	seen := loadMarks(path)
	marks := map[string]mark{}
	type due struct {
		task map[string]any
		age  float64
	}
	var dues []due
	for _, task := range tasks {
		id := task["id"].(string)
		m, ok := seen[id]
		if !ok {
			m = mark{First: at}
		}
		age := at - m.First
		if age >= firstRemind && at-m.Reminded >= repeatRemind {
			dues = append(dues, due{task, age})
			m.Reminded = at
		}
		// A task no longer listed is not carried over; a task listed twice
		// is named once.
		seen[id], marks[id] = m, m
	}
	if !saveMarks(path, marks, now) || len(dues) == 0 {
		return ""
	}

	var lines []string
	for i, d := range dues {
		if i == listLimit {
			lines = append(lines, fmt.Sprintf("- … and %d more", len(dues)-listLimit))
			break
		}
		lines = append(lines, describe(d.task, d.age))
	}
	return "Background work of this session has been running a long time:\n" +
		strings.Join(lines, "\n") +
		"\n\nIf any of it has done its job or is no longer needed, stop it with TaskStop by its id. " +
		fmt.Sprintf("If it is still needed, go on; no answer is needed. Each task is named at most once in %d hours.",
			int(repeatRemind/3600))
}

// describe is the line of a task in the reminder.
func describe(task map[string]any, age float64) string {
	kind := first(task, "type")
	if kind == "" {
		kind = "?"
	}
	what := first(task, "description", "command", "name")
	extra := first(task, "agent_type", "server")
	if extra != "" {
		extra = " (" + extra + ")"
	}
	line := fmt.Sprintf("- %s%s `%s`, %d min: %s", kind, extra, task["id"], int(math.Floor(age/60)), clip(what))
	if command, ok := task["command"].(string); ok && waitLoop.MatchString(command) && !hasLimit.MatchString(command) {
		line += " — a wait loop without a time limit, it will not end by itself"
	}
	return line
}

// first is the text of the first of the fields that says anything: a string
// as it is, a number or anything else as its JSON. An absent field, null,
// false, zero and an empty string, list or object say nothing.
func first(task map[string]any, keys ...string) string {
	for _, k := range keys {
		v := task[k]
		if !truthy(v) {
			continue
		}
		if s, ok := v.(string); ok {
			return s
		}
		raw, _ := json.Marshal(v)
		return string(raw)
	}
	return ""
}

// truthy tells a value of the payload that says yes from one that says
// nothing, the way the delivery's script reads the same payload.
func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case json.Number:
		f, ok := number(v)
		return !ok || f != 0
	case string:
		return v != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}

// clip puts a field on one line and cuts it to fieldLimit characters.
func clip(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) <= fieldLimit {
		return text
	}
	return string(r[:fieldLimit-1]) + "…"
}

// loadMarks reads the marks of a conversation. Marks that do not read start
// afresh, and so does a mark without its two numbers.
func loadMarks(path string) map[string]mark {
	out := map[string]mark{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var all map[string]any
	if json.Unmarshal(raw, &all) != nil {
		return out
	}
	for id, v := range all {
		m, _ := v.(map[string]any)
		f, okFirst := m["first"].(float64)
		r, okReminded := m["reminded"].(float64)
		if okFirst && okReminded {
			out[id] = mark{First: f, Reminded: r}
		}
	}
	return out
}

// saveMarks writes the marks and prunes the files past keepMarks; it says
// whether the marks are on the disk.
func saveMarks(path string, marks map[string]mark, now time.Time) bool {
	raw, err := json.Marshal(marks)
	if err != nil {
		return false
	}
	if writeMarks(path, raw) != nil {
		return false
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	old := now.Add(-keepMarks)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && info.ModTime().Before(old) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return true
}

// replaceFile writes a file whole or not at all: into a file of its own
// first, renamed over the old one.
func replaceFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".marks-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
