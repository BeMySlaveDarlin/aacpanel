package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// shared are the cases the hook shares with the delivery's
// background-reminder.py, which answers the same stops the same way.
type shared struct {
	Session string                    `json:"session"`
	T0      int64                     `json:"t0"`
	Tasks   map[string]map[string]any `json:"tasks"`
	Cases   []struct {
		What   string           `json:"what"`
		Before *string          `json:"before"`
		Aged   map[string]int64 `json:"aged"`
		Stops  []sharedStop     `json:"stops"`
	} `json:"cases"`
	Silent []struct {
		Raw     *string `json:"raw"`
		Payload any     `json:"payload"`
		Deep    int     `json:"deep"`
	} `json:"silent"`
}

type sharedStop struct {
	At      int64          `json:"at"`
	Tasks   []string       `json:"tasks"`
	Payload map[string]any `json:"payload"`
	Says    *struct {
		Text  *string  `json:"text"`
		Has   []string `json:"has"`
		Lacks []string `json:"lacks"`
	} `json:"says"`
	Marks   json.RawMessage     `json:"marks"`
	Exact   map[string]mark     `json:"exact"`
	Listing map[string][]string `json:"listing"`
}

func readShared(t *testing.T) shared {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "background-reminder.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s shared
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Cases) == 0 || len(s.Silent) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	return s
}

// payloadOf is the payload of a stop: a Stop of the session with the tasks
// named, the stop's own fields laid over it, as JSON reads it back.
func (s shared) payloadOf(t *testing.T, st sharedStop) any {
	tasks := []any{}
	for _, n := range st.Tasks {
		tasks = append(tasks, s.Tasks[n])
	}
	p := map[string]any{"hook_event_name": "Stop", "session_id": s.Session, "stop_hook_active": false,
		"background_tasks": tasks}
	maps.Copy(p, st.Payload)
	raw, _ := json.Marshal(p)
	var back any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	return back
}

func names(t *testing.T, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestTheSharedCasesOfTheBackgroundReminder(t *testing.T) {
	s := readShared(t)
	for _, c := range s.Cases {
		t.Run(c.What, func(t *testing.T) {
			state := t.TempDir()
			dir := filepath.Join(state, "background-reminder")
			file := filepath.Join(dir, s.Session+".json")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if c.Before != nil {
				if err := os.WriteFile(file, []byte(*c.Before), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// The hook prunes by the moment of the stop.
			for name, age := range c.Aged {
				p := filepath.Join(dir, name)
				then := time.Unix(s.T0+c.Stops[0].At-age, 0)
				if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(p, then, then); err != nil {
					t.Fatal(err)
				}
			}
			for _, st := range c.Stops {
				now := time.Unix(s.T0+st.At, 0)
				text := remind(s.payloadOf(t, st), now, dir)
				at := "the stop at " + time.Duration(st.At*int64(time.Second)).String()
				switch {
				case st.Says == nil && text != "":
					t.Fatalf("%s said:\n%s", at, text)
				case st.Says != nil && text == "":
					t.Fatalf("%s said nothing", at)
				case st.Says != nil:
					if st.Says.Text != nil && text != *st.Says.Text {
						t.Errorf("%s said:\n%s\nwant:\n%s", at, text, *st.Says.Text)
					}
					for _, w := range st.Says.Has {
						if !strings.Contains(text, w) {
							t.Errorf("%s does not say %q:\n%s", at, w, text)
						}
					}
					for _, w := range st.Says.Lacks {
						if strings.Contains(text, w) {
							t.Errorf("%s says %q:\n%s", at, w, text)
						}
					}
				}
				if st.Marks != nil {
					checkMarks(t, at, file, st.Marks)
				}
				if st.Exact != nil {
					if got := loadMarks(file); !maps.Equal(got, st.Exact) {
						t.Errorf("%s left the marks %v, want %v", at, got, st.Exact)
					}
				}
				if want, ok := st.Listing["state"]; ok && !slices.Equal(names(t, state), want) {
					t.Errorf("%s left the state directory %v, want %v", at, names(t, state), want)
				}
				if want, ok := st.Listing["marks"]; ok && !slices.Equal(names(t, dir), want) {
					t.Errorf("%s left the marks directory %v, want %v", at, names(t, dir), want)
				}
			}
		})
	}
}

// checkMarks holds the marks file to what a stop of the shared cases names:
// null for no file, else the tasks it keeps.
func checkMarks(t *testing.T, at, file string, want json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if string(want) == "null" {
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left a marks file: %s (%v)", at, raw, err)
		}
		return
	}
	var ids []string
	if err := json.Unmarshal(want, &ids); err != nil {
		t.Fatal(err)
	}
	var marks map[string]any
	if err := json.Unmarshal(raw, &marks); err != nil {
		t.Fatalf("%s: the marks file %s: %v", at, raw, err)
	}
	got := slices.Sorted(maps.Keys(marks))
	slices.Sort(ids)
	if !slices.Equal(got, ids) {
		t.Errorf("%s keeps the marks of %v, want %v", at, got, ids)
	}
}

// The inputs of the shared cases the hook answers with nothing: through the
// dispatcher, whose exit is 0 for them all.
func TestEveryBrokenInputExitsZeroWithoutAWord(t *testing.T) {
	s := readShared(t)
	t.Setenv("AACP_STATE_DIR", t.TempDir())
	for _, in := range s.Silent {
		var raw string
		switch {
		case in.Raw != nil:
			raw = *in.Raw
		case in.Payload != nil:
			b, _ := json.Marshal(in.Payload)
			raw = string(b)
		default:
			raw = strings.Repeat("[", in.Deep)
		}
		var out, errs bytes.Buffer
		if code := Run([]string{"background-reminder"}, strings.NewReader(raw), &out, &errs); code != 0 || out.Len() != 0 {
			t.Errorf("%.40q: exit %d, answer %q", raw, code, out.String())
		}
	}
}

func TestAnUnusableStateDirectorySaysNothing(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AACP_STATE_DIR", blocker)
	var out, errs bytes.Buffer
	payload := `{"hook_event_name": "Stop", "session_id": "s1", "background_tasks": [{"id": "b1", "type": "shell"}]}`
	if code := Run([]string{"background-reminder"}, strings.NewReader(payload), &out, &errs); code != 0 || out.Len() != 0 {
		t.Errorf("exit %d, answer %q", code, out.String())
	}
}

// A reminder whose mark did not reach the disk is not spoken: it would be
// spoken again at every stop. The next stop that can write names the task.
func TestAReminderThatCannotBeMarkedIsNotSpoken(t *testing.T) {
	s := readShared(t)
	dir := t.TempDir()
	stop := func(at int64) string {
		return remind(s.payloadOf(t, sharedStop{Tasks: []string{"shell"}}), time.Unix(s.T0+at, 0), dir)
	}
	stop(0)
	writeMarks = func(string, []byte) error { return errors.New("the disk takes no write") }
	text := stop(3600)
	writeMarks = replaceFile
	if text != "" {
		t.Fatalf("a reminder without its mark was spoken:\n%s", text)
	}
	if stop(3601) == "" {
		t.Error("the next stop that can write said nothing")
	}
}

// The reminder goes as context of the Stop and never holds it: what the
// dispatcher writes is the additional context of the event, with no
// decision, and its exit is 0.
func TestTheReminderGoesAsContextNeverAsABlock(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AACP_STATE_DIR", state)
	dir := filepath.Join(state, "background-reminder")
	first, _ := json.Marshal(map[string]mark{"b1": {First: float64(time.Now().Add(-2 * time.Hour).Unix())}})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s1.json"), first, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	payload := `{"hook_event_name": "Stop", "session_id": "s1", "background_tasks": [{"id": "b1", "type": "shell", "command": "npm run dev"}]}`
	if code := Run([]string{"background-reminder"}, strings.NewReader(payload), &out, &errs); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var reply map[string]map[string]string
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatalf("the answer %q: %v", out.String(), err)
	}
	if _, held := reply["decision"]; held || reply["hookSpecificOutput"]["hookEventName"] != "Stop" ||
		!strings.Contains(reply["hookSpecificOutput"]["additionalContext"], "`b1`, 120 min: npm run dev") {
		t.Errorf("the answer: %s", out.String())
	}
}
