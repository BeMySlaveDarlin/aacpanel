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
	At      int64                      `json:"at"`
	Event   string                     `json:"event"`
	State   json.RawMessage            `json:"state"`
	Set     map[string]json.RawMessage `json:"set"`
	Raw     *string                    `json:"raw"`
	Input   *string                    `json:"input"`
	Payload json.RawMessage            `json:"payload"`
	Says    []string                   `json:"says"`
	// Mark is the mark of the role of the conversation after the step: a
	// string, or null for no mark; left out, it is not looked at.
	Mark    json.RawMessage `json:"mark"`
	Listing []string        `json:"listing"`
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
				var p struct {
					Session string `json:"session_id"`
				}
				_ = json.Unmarshal(st.Payload, &p)
				if st.Mark != nil {
					var want *string
					if err := json.Unmarshal(st.Mark, &want); err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(filepath.Join(state, "role-"+p.Session+".state"))
					switch {
					case want == nil && err == nil:
						t.Errorf("step %d: a mark of the role is left: %q", i, got)
					case want != nil && (err != nil || string(got) != *want):
						t.Errorf("step %d: the mark of the role reads %q (%v), want %q", i, got, err, *want)
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

func TestCodexStampUsesTheThreadsAccount(t *testing.T) {
	for _, tc := range []struct {
		name, row, installed, env, turn, want string
	}{
		{"thread beats the hook environment", `{"agent":"codex","codexHome":"/home/u/.codex-work"}`, "/home/u/.codex", "/home/u/.codex", "", "Limits:   week 32%"},
		{"new thread uses the installed home", "", "/home/u/.codex-work", "", "t", "Limits:   week 32%"},
		{"old hook inherits a custom home", "", "", "/home/u/.codex-work", "t", "Limits:   week 32%"},
		{"old default hook needs no home variable", "", "", "", "t", "Limits:   week 45%"},
		{"unknown home borrows no limits", "", "/home/u/.missing", "", "t", ""},
		{"claude keeps its contour despite inherited codex home", `{"agent":"claude"}`, "", "/home/u/.codex-work", "", "Limits:   5h 12% · week 10%"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("AACP_STATE_DIR", state)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("HOME", "/home/u")
			t.Setenv(CodexHomeEnv, tc.installed)
			t.Setenv("CODEX_HOME", tc.env)
			t.Setenv("CLAUDE_CONFIG_DIR", "/home/u/.claude-work")
			rows := []any{}
			if tc.row != "" {
				row := decodeKeeping(t, []byte(tc.row)).(map[string]any)
				row["sessionId"] = "thread"
				rows = append(rows, row)
			}
			limits := decodeKeeping(t, []byte(`{"contours":[
				{"profile":"personal","fiveHour":{"pct":2},"sevenDay":{"pct":93},"codex":{"codexHome":"/home/u/.codex","windows":[{"pct":45,"windowMins":10080}]}},
				{"profile":"work","fiveHour":{"pct":12},"sevenDay":{"pct":10},"codex":{"codexHome":"/home/u/.codex-work","windows":[{"pct":32,"windowMins":10080}]}}
			]}`))
			now := time.Unix(1700000000, 0)
			writeJSON(t, filepath.Join(state, "state.json"), map[string]any{"at": now.Unix(), "sessions": rows, "limits": limits})
			payload, _ := json.Marshal(map[string]any{"session_id": "thread", "turn_id": tc.turn})
			got := ""
			for _, line := range strings.Split(stampPrompt("UserPromptSubmit", payload, now).Context, "\n") {
				if strings.HasPrefix(line, "Limits:") {
					got = line
				}
			}
			if got != tc.want {
				t.Fatalf("limits %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodexStampNamesOnlyKnownWindows(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`"windows":[{"pct":0,"windowMins":300},{"pct":45,"windowMins":10080}],"ageSec":180`, "Limits:   5h 0% · week 45% (the snapshot is 3 min old)"},
		{`"windows":[{"pct":14,"windowMins":90},{"pct":2,"windowMins":null}]`, "Limits:   90m 14% · window 2%"},
		{`"sevenDay":{"pct":45}`, "Limits:   week 45%"},
		{`"fiveHour":{"pct":8}`, "Limits:   5h 8%"},
		{`"windows":[],"ageSec":180`, ""},
	} {
		state := decodeKeeping(t, []byte(`{"limits":{"ageSec":9999,"codex":{"codexHome":"/home/u/.codex",`+tc.body+`}}}`)).(map[string]any)
		if got := lineCodexLimits(state, "/home/u/.codex"); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.body, got, tc.want)
		}
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
		{[]string{"prompt-stamp", "Stop"}, "", "does not answer Stop"},
	} {
		code, out, errs := run(t, c.args, `{"session_id": "s1"}`)
		if code != 0 || !strings.Contains(errs, c.errs) || (c.errs == "" && errs != "") {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, errs)
		}
		if c.event == "" {
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

func TestTheStampRejectsToolBatches(t *testing.T) {
	t.Setenv("AACP_STATE_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	code, out, errs := run(t, []string{"prompt-stamp", "PostToolBatch"}, `{}`)
	if code != 0 || out != "" || !strings.Contains(errs, "does not answer PostToolBatch") {
		t.Errorf("a tool batch: exit %d, stdout %q, stderr %q", code, out, errs)
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

func TestCodexRoleStampFindsTheThreadAndKeepsItsToldMark(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AACP_STATE_DIR", state)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const id = "019a1f00-0000-7000-8000-00000000abcd"
	row := map[string]any{"agent": "codex", "sessionId": id, "codexHome": "/srv/codex", "role": "the db split"}
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		text, agent, want string
		compacts          int
		mark              bool
	}{
		{"the db split", "worker", "", 0, false},
		{"the db split", "", "Role:     the db split", 0, true},
		{"the db split", "", "", 0, true},
		{"the db split", "", "Role:     the db split", 1, true},
		{"the schema review", "", "Role:     the schema review", 1, true},
		{"", "", "Role:     none now: the role this session was told is cleared", 1, false},
	} {
		row["role"], row["compacts"] = tc.text, tc.compacts
		writeJSON(t, filepath.Join(state, "state.json"), map[string]any{"at": now.Unix(), "sessions": []any{row}})
		payload, _ := json.Marshal(map[string]any{"session_id": id, "turn_id": "turn", "agent_id": tc.agent})
		got := ""
		for _, line := range strings.Split(stampPrompt("UserPromptSubmit", payload, now).Context, "\n") {
			if strings.HasPrefix(line, "Role:") {
				got = line
			}
		}
		if got != tc.want {
			t.Fatalf("role %q at compaction %d: %q, want %q", tc.text, tc.compacts, got, tc.want)
		}
		_, err := os.Stat(filepath.Join(state, "role-"+id+".state"))
		if (err == nil) != tc.mark {
			t.Fatalf("the thread's told mark: %v, want present %v", err, tc.mark)
		}
	}
}
