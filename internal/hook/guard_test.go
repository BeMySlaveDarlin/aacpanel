package hook

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/session"
)

// guardCases holds the fixture of context guard stops.
type guardCases struct {
	Project string         `json:"project"`
	Guards  string         `json:"guards"`
	Clock   int64          `json:"clock"`
	Row     map[string]any `json:"row"`
	Cases   []struct {
		What       string            `json:"what"`
		Guards     json.RawMessage   `json:"guards"`
		Env        map[string]string `json:"env"`
		Row        map[string]any    `json:"row"`
		NoState    bool              `json:"nostate"`
		SessionsAt *int64            `json:"sessionsAt"`
		Fresh      map[string]any    `json:"fresh"`
		Restarting map[string]int64  `json:"restarting"`
		Sent       map[string]int64  `json:"sent"`
		Stops      []struct {
			Guards     json.RawMessage  `json:"guards"`
			Restarting map[string]int64 `json:"restarting"`
			Row        map[string]any   `json:"row"`
			Payload    map[string]any   `json:"payload"`
			Input      *string          `json:"input"`
			Says       *struct {
				Text *string  `json:"text"`
				Has  []string `json:"has"`
			} `json:"says"`
			Marks          json.RawMessage `json:"marks"`
			RestartingLeft []string        `json:"restartingLeft"`
		} `json:"stops"`
	} `json:"cases"`
}

func readGuardCases(t *testing.T) guardCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "context-guard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c guardCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	return c
}

// lookClock is the clock of the look at the work in the background: it moves
// only when the look sleeps, and the collector writes its next snapshot at the
// first sleep.
type lookClock struct {
	now   time.Time
	fresh func(at time.Time)
}

func (c *lookClock) Now() time.Time { return c.now }

func (c *lookClock) Sleep(d time.Duration) {
	c.now = c.now.Add(d)
	if c.fresh != nil {
		c.fresh(c.now)
		c.fresh = nil
	}
}

func TestTheSharedCasesOfTheContextGuard(t *testing.T) {
	c := readGuardCases(t)
	was := busy
	t.Cleanup(func() { busy = was })
	for _, cs := range c.Cases {
		t.Run(cs.What, func(t *testing.T) {
			state, xdg := t.TempDir(), t.TempDir()
			t.Setenv("AACP_STATE_DIR", state)
			t.Setenv("XDG_STATE_HOME", xdg)
			t.Setenv("CLAUDE_PROJECT_DIR", "")
			for k, v := range cs.Env {
				t.Setenv(k, v)
			}
			home := filepath.Join(xdg, "aacpanel")
			placeGuards(t, home, cs.Guards, c.Guards)
			aged(t, filepath.Join(home, "context-guard"), cs.Sent)
			aged(t, filepath.Join(home, "restarting"), cs.Restarting)
			row := maps.Clone(c.Row)
			maps.Copy(row, cs.Row)
			snapshot := filepath.Join(state, "state.json")
			for i, st := range cs.Stops {
				if st.Guards != nil {
					placeGuards(t, home, st.Guards, "")
				}
				if st.Restarting != nil {
					aged(t, filepath.Join(home, "restarting"), st.Restarting)
				}
				stopRow := maps.Clone(row)
				maps.Copy(stopRow, st.Row)
				if !cs.NoState {
					var at *float64
					if cs.SessionsAt != nil {
						v := float64(c.Clock + *cs.SessionsAt)
						at = &v
					}
					writeSnapshot(t, snapshot, stopRow, at)
				}
				clock := &lookClock{now: time.Unix(c.Clock, 0)}
				if cs.Fresh != nil {
					clock.fresh = func(now time.Time) {
						fresh := maps.Clone(stopRow)
						maps.Copy(fresh, cs.Fresh)
						v := seconds(now)
						writeSnapshot(t, snapshot, fresh, &v)
					}
				}
				busy = func(path, id string) bool {
					return session.Host{State: path, Now: clock.Now, Sleep: clock.Sleep}.Busy(id)
				}
				payload := map[string]any{"hook_event_name": "Stop", "session_id": "mine", "cwd": c.Project,
					"stop_hook_active": false}
				maps.Copy(payload, st.Payload)
				input, _ := json.Marshal(payload)
				if st.Input != nil {
					input = []byte(*st.Input)
				}
				got := guardContext("Stop", input, time.Now()).Block

				switch {
				case st.Says == nil && got != "":
					t.Errorf("stop %d asked for a restart:\n%s", i, got)
				case st.Says != nil && got == "":
					t.Errorf("stop %d asked for nothing", i)
				case st.Says != nil:
					if st.Says.Text != nil && got != *st.Says.Text {
						t.Errorf("stop %d said:\n%s\nwant:\n%s", i, got, *st.Says.Text)
					}
					for _, w := range st.Says.Has {
						if !strings.Contains(got, w) {
							t.Errorf("stop %d does not say %q:\n%s", i, w, got)
						}
					}
				}
				if st.Marks != nil {
					var want []string
					_ = json.Unmarshal(st.Marks, &want)
					if got := listed(filepath.Join(home, "context-guard")); !slices.Equal(got, want) {
						t.Errorf("stop %d: the marks are %v, want %v", i, got, want)
					}
				}
				if st.RestartingLeft != nil {
					if got := listed(filepath.Join(home, "restarting")); !slices.Equal(got, st.RestartingLeft) {
						t.Errorf("stop %d: the markers left are %v, want %v", i, got, st.RestartingLeft)
					}
				}
			}
		})
	}
}

// placeGuards writes guards.tsv: the body given, the fallback where none is
// given, no file for null.
func placeGuards(t *testing.T, home string, given json.RawMessage, fallback string) {
	t.Helper()
	path := filepath.Join(home, "guards.tsv")
	if string(given) == "null" {
		_ = os.Remove(path)
		return
	}
	body := fallback
	if given != nil {
		if err := json.Unmarshal(given, &body); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, path, body)
}

// aged puts empty files named by their age into a directory, in place of
// what it held, aged by the wall clock the hook prunes by.
func aged(t *testing.T, dir string, ages map[string]int64) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	for name, age := range ages {
		path := filepath.Join(dir, name)
		writeFile(t, path, "")
		then := time.Now().Add(-time.Duration(age) * time.Second)
		if err := os.Chtimes(path, then, then); err != nil {
			t.Fatal(err)
		}
	}
}

// listed are the names in a directory, nil when it is not there.
func listed(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func writeSnapshot(t *testing.T, path string, row map[string]any, at *float64) {
	t.Helper()
	data := map[string]any{"sessions": []any{row}}
	if at != nil {
		data["sessionsAt"] = *at
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path+".tmp", string(raw))
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}

// The guard looks at the work of the session for up to session.LongestLook,
// and its handler says so: the kit's timeout for it is checked against that.
func TestTheGuardSaysHowLongItMayLook(t *testing.T) {
	if Wait("context-guard") != session.LongestLook {
		t.Errorf("the guard may take %v, the look at the work %v", Wait("context-guard"), session.LongestLook)
	}
	if Wait("prompt-stamp") != 0 || Wait("background-reminder") != 0 {
		t.Error("a hook that answers at once says it waits")
	}
}
