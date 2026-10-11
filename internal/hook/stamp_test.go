package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stampCases holds the fixture of stamp output. The numbers of the
// snapshots keep the form they are written in: python says 41 and 41.0
// apart, and so must the snapshot the test writes.
type stampCases struct {
	Now       int64                      `json:"now"`
	Offset    int                        `json:"offset"`
	Snapshots map[string]json.RawMessage `json:"snapshots"`
	Cases     []struct {
		What   string            `json:"what"`
		Guards *string           `json:"guards"`
		Env    map[string]string `json:"env"`
		Files  map[string]struct {
			Text string `json:"text"`
			Age  int64  `json:"age"`
		} `json:"files"`
		Steps []stampStep `json:"steps"`
	} `json:"cases"`
}

type stampStep struct {
	At       int64                      `json:"at"`
	Event    string                     `json:"event"`
	State    json.RawMessage            `json:"state"`
	Set      map[string]json.RawMessage `json:"set"`
	Raw      *string                    `json:"raw"`
	Input    *string                    `json:"input"`
	Payload  json.RawMessage            `json:"payload"`
	Says     []string                   `json:"says"`
	Throttle *string                    `json:"throttle"`
	Listing  []string                   `json:"listing"`
}

func readStampCases(t *testing.T) stampCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "prompt-stamp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c stampCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	return c
}

// decodeKeeping reads JSON with its numbers as they are written.
func decodeKeeping(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// layOver sets values at dotted paths of a snapshot, a list index as a number.
func layOver(t *testing.T, snapshot any, set map[string]json.RawMessage) any {
	t.Helper()
	for path, value := range set {
		parts := strings.Split(path, ".")
		node := snapshot
		for i, part := range parts {
			last := i == len(parts)-1
			switch n := node.(type) {
			case map[string]any:
				if last {
					n[part] = decodeKeeping(t, value)
				}
				node = n[part]
			case []any:
				idx, err := strconv.Atoi(part)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				if last {
					n[idx] = decodeKeeping(t, value)
				}
				node = n[idx]
			default:
				t.Fatalf("%s: no %s in the snapshot", path, part)
			}
		}
	}
	return snapshot
}

func TestTheSharedCasesOfTheStamp(t *testing.T) {
	c := readStampCases(t)
	zone := time.FixedZone("", c.Offset)
	for _, cs := range c.Cases {
		t.Run(cs.What, func(t *testing.T) {
			xdg, state := t.TempDir(), t.TempDir()
			t.Setenv("XDG_STATE_HOME", xdg)
			t.Setenv("AACP_STATE_DIR", state)
			t.Setenv("CLAUDE_PROJECT_DIR", "")
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			for k, v := range cs.Env {
				t.Setenv(k, v)
			}
			if cs.Guards != nil {
				writeFile(t, filepath.Join(xdg, "aacpanel", "guards.tsv"), *cs.Guards)
			}
			for name, f := range cs.Files {
				path := filepath.Join(state, name)
				writeFile(t, path, f.Text)
				then := time.Unix(c.Now-f.Age, 0)
				if err := os.Chtimes(path, then, then); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := filepath.Join(state, "state.json")
			for i, st := range cs.Steps {
				switch {
				case st.Raw != nil:
					writeFile(t, snapshot, *st.Raw)
				case st.State != nil:
					body := []byte(st.State)
					var name string
					if st.State[0] == '"' && json.Unmarshal(st.State, &name) == nil {
						data := layOver(t, decodeKeeping(t, c.Snapshots[name]), st.Set)
						var err error
						if body, err = json.Marshal(data); err != nil {
							t.Fatal(err)
						}
					}
					writeFile(t, snapshot, string(body))
				}
				event := st.Event
				if event == "" {
					event = "UserPromptSubmit"
				}
				input := string(st.Payload)
				if st.Input != nil {
					input = *st.Input
				}
				now := time.Unix(c.Now+st.At, 0).In(zone)
				said := stampPrompt(event, []byte(input), now).Context
				if want := strings.Join(st.Says, "\n"); said != want {
					t.Errorf("step %d said:\n%s\nwant:\n%s", i, said, want)
				}
				if st.Throttle != nil {
					var p struct {
						Session string `json:"session_id"`
					}
					_ = json.Unmarshal(st.Payload, &p)
					if p.Session == "" {
						p.Session = "common"
					}
					got, err := os.ReadFile(filepath.Join(state, "stamp-"+p.Session+".state"))
					if err != nil || string(got) != *st.Throttle {
						t.Errorf("step %d: the throttle file reads %q (%v), want %q", i, got, err, *st.Throttle)
					}
				}
				if st.Listing != nil {
					if got := names(t, state); !slices.Equal(got, st.Listing) {
						t.Errorf("step %d: the state directory holds %v, want %v", i, got, st.Listing)
					}
				}
			}
		})
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The stamp answers the event its command names after its name, and the
// prompt before every one when none is named; an event it does not answer is
// said on stderr and answered with nothing, exit 0 all the same.
func TestTheStampAnswersTheEventItsCommandNames(t *testing.T) {
	t.Setenv("AACP_STATE_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, c := range []struct {
		args  []string
		event string
		errs  string
	}{
		{[]string{"prompt-stamp"}, "UserPromptSubmit", ""},
		{[]string{"prompt-stamp", "UserPromptSubmit"}, "UserPromptSubmit", ""},
		{[]string{"prompt-stamp", "PostToolBatch"}, "", ""},
		{[]string{"prompt-stamp", "Stop"}, "", "does not answer Stop"},
	} {
		code, out, errs := run(t, c.args, `{"session_id": "s1"}`)
		if code != 0 || !strings.Contains(errs, c.errs) || (c.errs == "" && errs != "") {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, errs)
		}
		if c.event == "" {
			// Without a snapshot between prompts there is nothing to say.
			if out != "" {
				t.Errorf("%v said %q", c.args, out)
			}
			continue
		}
		var reply struct {
			Out struct {
				Event   string `json:"hookEventName"`
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(out), &reply); err != nil || reply.Out.Event != c.event ||
			!strings.Contains(reply.Out.Context, "The host snapshot is unavailable") {
			t.Errorf("%v answered %q (%v)", c.args, out, err)
		}
	}
}

// python's str of a number of the JSON: an int whole, a float with its point
// or its exponent.
func TestNumbersAreSaidAsPythonSaysThem(t *testing.T) {
	for in, want := range map[string]string{
		"41": "41", "-0": "0", "41.0": "41.0", "41.5": "41.5", "1e3": "1000.0", "1E3": "1000.0",
		"0.0001": "0.0001", "0.00001": "1e-05", "1e16": "1e+16", "1e15": "1000000000000000.0",
		"1.5e300": "1.5e+300", "-0.0": "-0.0", "1e400": "inf", "123456789012345678.0": "1.2345678901234568e+17",
	} {
		if got := pyStr(json.Number(in)); got != want {
			t.Errorf("%s is said %q, python says %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{0.5: "0", 1.5: "2", 2.5: "2", -0.4: "0", 79.5: "80", 1e20: "100000000000000000000"} {
		if got := whole(in); got != want {
			t.Errorf("round(%v) is %q, python's is %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{0.5: "0", 2.5: "2", 3.5: "4", -0.4: "-0", 43.6: "44"} {
		if got := fixed(in); got != want {
			t.Errorf("%v:.0f is %q, python's is %q", in, got, want)
		}
	}
}
