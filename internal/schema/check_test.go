package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func launch(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return out
}

// What the map holds today passes as it is: a schema that refused a stored
// value would leave a project that cannot be saved without losing it.
func TestCheckPassesWhatTheMapHolds(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"remoteControl": true}`,
		`{"remoteControl": false}`,
		`{"remoteControl": true, "permissionMode": "auto"}`,
		`{"transport": "stream", "remoteControl": true}`,
		`{"intent": "Summary"}`,
		`{"intent": ""}`,
		`{"effort": "xhigh", "remoteControl": true, "permissionMode": "auto"}`,
		`{"permissionMode": "bypassPermissions"}`,
		`{"model": "opus[1m]", "finalizeAt": 80}`,
		`{"model": "claude-opus-5-5"}`,
		`{"env": {"FOO": "bar"}, "args": ["--verbose", "--add-dir=/opt/x"]}`,
	} {
		if got := Check(LevelProject, launch(t, raw)); len(got) != 0 {
			t.Errorf("%s was refused: %v", raw, got)
		}
	}
}

// Every way a value goes wrong is named by its key, with a reason a person
// can act on.
func TestCheckNamesWhatTheLaunchWouldNotTake(t *testing.T) {
	cases := map[string]struct{ raw, key, says string }{
		"unknown key":          {`{"colour": "red"}`, "colour", "does not know"},
		"retired key":          {`{"room": "Work"}`, "room", "retired"},
		"ultracode at launch":  {`{"effort": "ultracode"}`, "effort", "is not one of"},
		"effort not a word":    {`{"effort": 3}`, "effort", "a word is expected"},
		"no such model":        {`{"model": "gpt-4"}`, "model", "neither an alias"},
		"switch as a word":     {`{"remoteControl": "yes"}`, "remoteControl", "on or off"},
		"threshold past 99":    {`{"finalizeAt": 120}`, "finalizeAt", "outside 1–99"},
		"threshold a fraction": {`{"finalizeAt": 80.5}`, "finalizeAt", "whole number"},
		"message too long":     {`{"intent": "` + strings.Repeat("é", 501) + `"}`, "intent", "longer than 500"},
		"message with a bell":  {`{"intent": "go\u0007"}`, "intent", "forbidden character"},
		"env as a list":        {`{"env": ["FOO=bar"]}`, "env", "an object"},
		"env with a bad name":  {`{"env": {"FOO-BAR": "x"}}`, "env", "not a variable name"},
		"env moving account":   {`{"env": {"CLAUDE_CONFIG_DIR": "/x"}}`, "env", "another account"},
		"env value a number":   {`{"env": {"FOO": 1}}`, "env", "not a text"},
		"args as a line":       {`{"args": "--add-dir /opt/x"}`, "args", "a list of words"},
		"args naming a model":  {`{"args": ["--model", "opus"]}`, "args", "--model"},
		"args with = form":     {`{"args": ["--effort=max"]}`, "args", "--effort"},
		"args resuming":        {`{"args": ["--resume"]}`, "args", "resumes"},
		"mode off the list":    {`{"permissionMode": "yolo"}`, "permissionMode", "is not one of"},
		"transport off list":   {`{"transport": "screen"}`, "transport", "is not one of"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := Check(LevelProject, launch(t, c.raw))
			if len(got) != 1 || got[0].Key != c.key || !strings.Contains(got[0].Why, c.says) {
				t.Errorf("%s: %v — meant one problem of %s saying %q", c.raw, got, c.key, c.says)
			}
		})
	}
}

// Options are offered by value and each carries a label; a held value is
// accepted where stored but never offered.
func TestSchemaIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Params() {
		if seen[p.Key] {
			t.Errorf("%s is in the schema twice", p.Key)
		}
		seen[p.Key] = true
		if p.Label == "" || p.Unset == "" || len(p.Levels) == 0 || p.Merge == "" {
			t.Errorf("%s lacks a label, an unset outcome, levels or a merge: %+v", p.Key, p)
		}
		if p.Live[TransportTmux] == "" || p.Live[TransportStream] == "" {
			t.Errorf("%s does not say when a live session takes it", p.Key)
		}
		if p.Kind == KindEnum && len(p.Options) == 0 {
			t.Errorf("%s is an enum without options", p.Key)
		}
		for _, o := range p.Options {
			if o.Label == "" {
				t.Errorf("%s: option %s has no label", p.Key, o.Value)
			}
		}
		if _, gone := Retire(p.Key); gone {
			t.Errorf("%s is live and retired at once", p.Key)
		}
	}
	if p, _ := Find("effort"); p.Offers("ultracode") {
		t.Error("ultracode is offered at launch, which claude drops")
	}
}
