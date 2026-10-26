package hook

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/checklist"
	"aacpanel/internal/mcp"
)

// checklistCases holds the fixture of checklist reminders.
type checklistCases struct {
	Setup   func(*testing.T, string, checklistStop) `json:"-"`
	Session string                                  `json:"session"`
	Place   string                                  `json:"place"`
	Name    string                                  `json:"name"`
	Claude  struct {
		PID   int    `json:"pid"`
		Start string `json:"start"`
	} `json:"claude"`
	Shell struct {
		PID   int      `json:"pid"`
		Start string   `json:"start"`
		Args  []string `json:"args"`
	} `json:"shell"`
	Began       float64             `json:"began"`
	Args        map[string][]string `json:"args"`
	Reason      string              `json:"reason"`
	Transcripts map[string][]string `json:"transcripts"`
	Cases       []struct {
		What   string          `json:"what"`
		Claude map[string]any  `json:"claude"`
		Stops  []checklistStop `json:"stops"`
	} `json:"cases"`
	Names        [][4]*string `json:"names"`
	SessionNames []struct {
		Args []string `json:"args"`
		File *string  `json:"file"`
		Name *string  `json:"name"`
	} `json:"sessionNames"`
	Stamps [][2]any `json:"stamps"`
}

type checklistStop struct {
	Checklists []map[string]any `json:"checklists"`
	// Transcript names a transcript of the cases; null is a path to nothing,
	// and a stop that names none reads the transcript of a turn that worked.
	Transcript json.RawMessage `json:"transcript"`
	Payload    map[string]any  `json:"payload"`
	Drop       []string        `json:"drop"`
	Input      *string         `json:"input"`
	Tail       int64           `json:"tail"`
	Holds      bool            `json:"holds"`
	Reason     string          `json:"reason"`
	Files      map[string]any  `json:"files"`
	Missing    []string        `json:"missing"`
}

func readChecklistCases(t *testing.T) checklistCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "checklist-reminder.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Numbers stay as the cases write them: a file of the case holds 5555.0
	// where python writes 5555.0.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var c checklistCases
	if err := dec.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 || len(c.Names) == 0 || len(c.SessionNames) == 0 || len(c.Stamps) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	return c
}

// fakeProcess puts a process into the /proc of a test: its parent, when it
// started and its arguments.
func fakeProcess(t *testing.T, proc string, pid, parent int, start string, args []string) {
	t.Helper()
	root := filepath.Join(proc, strconv.Itoa(pid))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	fields := append([]string{"S", strconv.Itoa(parent)}, slices.Repeat([]string{"0"}, 17)...)
	stat := strconv.Itoa(pid) + " (claude) " + strings.Join(append(fields, start), " ") + "\n"
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte(stat), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmdline"), []byte(strings.Join(args, "\x00")+"\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// inPlace gives a test a /proc, a state and a config directory of its own.
func inPlace(t *testing.T) (dir, proc, config string) {
	t.Helper()
	dir = t.TempDir()
	proc, config = filepath.Join(dir, "proc"), filepath.Join(dir, "claude")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	wasProc, wasParent, wasTail := procDir, parentPID, transcriptTail
	t.Cleanup(func() { procDir, parentPID, transcriptTail = wasProc, wasParent, wasTail })
	procDir = proc
	return dir, proc, config
}

// laid is a JSON object with the fields of over laid over it.
func laid(base map[string]any, over any) map[string]any {
	out := maps.Clone(base)
	if o, ok := over.(map[string]any); ok {
		maps.Copy(out, o)
	}
	return out
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheSharedCasesOfTheChecklistReminder(t *testing.T) {
	runReminderCases(t, readChecklistCases(t), "checklist-reminder")
}

func runReminderCases(t *testing.T, c checklistCases, hook string) {
	t.Helper()
	for _, cs := range c.Cases {
		t.Run(cs.What, func(t *testing.T) {
			dir, proc, config := inPlace(t)
			// A shell stands between claude and the hook, and has no file of a session.
			fakeProcess(t, proc, c.Shell.PID, c.Claude.PID, c.Shell.Start, c.Shell.Args)
			parentPID = func() int { return c.Shell.PID }
			placeClaude(t, c, proc, config, cs.Claude)

			for i, st := range cs.Stops {
				at := "stop " + strconv.Itoa(i)
				if c.Setup != nil {
					c.Setup(t, dir, st)
				}
				for _, cl := range st.Checklists {
					writeChecklist(t, c, config, cl)
				}
				path := filepath.Join(dir, "none.jsonl")
				if string(st.Transcript) != "null" {
					name := "worked"
					if st.Transcript != nil {
						_ = json.Unmarshal(st.Transcript, &name)
					}
					path = filepath.Join(dir, "transcript.jsonl")
					if err := os.WriteFile(path, []byte(strings.Join(c.Transcripts[name], "\n")+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				transcriptTail = 4 << 20
				if st.Tail > 0 {
					transcriptTail = st.Tail
				}
				payload := laid(map[string]any{"hook_event_name": "Stop", "session_id": c.Session,
					"transcript_path": path, "stop_hook_active": false}, st.Payload)
				for _, k := range st.Drop {
					delete(payload, k)
				}
				input, _ := json.Marshal(payload)
				if st.Input != nil {
					input = []byte(*st.Input)
				}
				var out, errs bytes.Buffer
				if code := Run([]string{hook}, bytes.NewReader(input), &out, &errs); code != 0 || errs.Len() > 0 {
					t.Fatalf("%s: exit %d, stderr %q", at, code, errs.String())
				}
				switch {
				case !st.Holds && out.Len() > 0:
					t.Errorf("%s was held: %s", at, out.String())
				case st.Holds:
					var reply map[string]string
					reason := c.Reason
					if st.Reason != "" {
						reason = st.Reason
					}
					if err := json.Unmarshal(out.Bytes(), &reply); err != nil || !maps.Equal(reply, map[string]string{
						"decision": "block", "reason": reason}) {
						t.Errorf("%s was not held with the reason: %q (%v)", at, out.String(), err)
					}
				}
			}
		})
	}
}

// placeClaude puts the claude running the hook in place: its process and the
// file it keeps of itself, the case's fields laid over both.
func placeClaude(t *testing.T, c checklistCases, proc, config string, over map[string]any) {
	t.Helper()
	args := c.Args["withTool"]
	switch a := over["args"].(type) {
	case string:
		args = c.Args[a]
	case []any:
		args = nil
		for _, w := range a {
			args = append(args, w.(string))
		}
	}
	fakeProcess(t, proc, c.Claude.PID, 1, c.Claude.Start, args)
	file := map[string]any{"pid": c.Claude.PID, "sessionId": c.Session, "cwd": c.Place,
		"procStart": c.Claude.Start, "kind": "interactive", "name": c.Name}
	for k, field := range map[string]string{"session": "sessionId", "start": "procStart", "name": "name"} {
		if v, ok := over[k]; ok {
			file[field] = v
		}
	}
	if file["name"] == nil {
		delete(file, "name")
	}
	writeJSON(t, filepath.Join(config, "sessions", strconv.Itoa(c.Claude.PID)+".json"), laid(file, over["over"]))
}

// writeChecklist writes the checklist of a session of the place; without a
// name, the one of the place.
func writeChecklist(t *testing.T, c checklistCases, config string, cl map[string]any) {
	t.Helper()
	cwd, name := c.Place, any(c.Name)
	if v, ok := cl["cwd"]; ok {
		cwd = v.(string)
	}
	if v, ok := cl["name"]; ok {
		name = v
	}
	own, _ := name.(string)
	path := filepath.Join(checklist.Dir(), checklist.File(mcp.Place{ConfigDir: config, Dir: cwd}, own))
	var items []any
	for i, s := range cl["statuses"].([]any) {
		items = append(items, map[string]any{"text": "step " + strconv.Itoa(i), "status": s})
	}
	file := map[string]any{"configDir": config, "dir": cwd, "sessionId": c.Session, "pid": c.Claude.PID,
		"at": "2026-09-28T09:59:00Z", "items": items}
	if v, ok := cl["session"]; ok {
		file["sessionId"] = v
	}
	if v, ok := cl["pid"]; ok {
		file["pid"] = v
	}
	if name != nil {
		file["name"] = name
	}
	writeJSON(t, path, laid(file, cl["over"]))
	written := -60.0
	if v, ok := cl["written"]; ok {
		written, _ = number(v)
	}
	then := time.Unix(0, int64((c.Began+written)*1e9))
	if err := os.Chtimes(path, then, then); err != nil {
		t.Fatal(err)
	}
}

func TestThePartsOfTheChecklistReminder(t *testing.T) {
	c := readChecklistCases(t)
	_, proc, _ := inPlace(t)
	str := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	// The name of the checklist is the one the executor computes, pinned
	// beside the executor's own test of the same session.
	for _, n := range c.Names {
		if got := checklist.File(mcp.Place{ConfigDir: *n[0], Dir: *n[1]}, str(n[2])); got != str(n[3]) {
			t.Errorf("%s %s %s: the checklist %q, want %q", *n[0], *n[1], str(n[2]), got, str(n[3]))
		}
	}
	// The name of the session is taken in either form claude takes.
	for _, s := range c.SessionNames {
		fakeProcess(t, proc, 7, 1, "1", s.Args)
		file := map[string]any{}
		if s.File != nil {
			file["name"] = *s.File
		}
		if got := sessionName(7, file); got != str(s.Name) {
			t.Errorf("%q with the file %v: the name %q, want %q", s.Args, file, got, str(s.Name))
		}
	}
	// The time of a record is read as python reads it.
	for _, s := range c.Stamps {
		got, ok := stampOf(map[string]any{"timestamp": s[0]})
		want, known := number(s[1])
		if ok != known || got != want {
			t.Errorf("%v: %v %v, want %v", s[0], got, ok, s[1])
		}
	}
}

// The ancestors are read up the chain of parents, the name of a process with
// a parenthesis of its own in it.
func TestTheAncestorsAreReadUpTheChainOfParents(t *testing.T) {
	_, proc, _ := inPlace(t)
	for pid, parent := range map[int]int{4300: 777, 777: 1} {
		if err := os.MkdirAll(filepath.Join(proc, strconv.Itoa(pid)), 0o700); err != nil {
			t.Fatal(err)
		}
		stat := strconv.Itoa(pid) + " (a (b) c) S " + strconv.Itoa(parent) + " 0 0\n"
		if err := os.WriteFile(filepath.Join(proc, strconv.Itoa(pid), "stat"), []byte(stat), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	parentPID = func() int { return 4300 }
	if got := ancestors(); !slices.Equal(got, []int{4300, 777}) {
		t.Errorf("the ancestors %v", got)
	}
}
