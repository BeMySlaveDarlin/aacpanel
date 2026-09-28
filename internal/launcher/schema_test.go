package launcher

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"aacpanel/internal/schema"
)

// sample returns a value of the parameter's kind the schema accepts.
func sample(t *testing.T, p schema.Param) any {
	t.Helper()
	switch p.Kind {
	case schema.KindEnum:
		return p.Options[len(p.Options)-1].Value
	case schema.KindModel:
		return "opus"
	case schema.KindBool:
		return true
	case schema.KindInt:
		return p.Max
	case schema.KindText:
		return "read the queue"
	case schema.KindKV:
		return map[string]string{"FOO": "bar"}
	case schema.KindTokens:
		return []string{"--verbose"}
	}
	t.Fatalf("parameter %s has a kind the test does not know: %s", p.Key, p.Kind)
	return nil
}

// The schema is the list of what a launch can say, and the launcher reads it
// whole: every key of it lands in the parameters of the launch, a key the
// host reads and a retired one pass without a word, and a key outside it is
// named — so a parameter added to the screens without a launcher behind it
// fails here, not on the host.
func TestTheLauncherTakesEveryKeyOfTheSchema(t *testing.T) {
	for _, p := range schema.Params() {
		raw, err := json.Marshal(map[string]any{p.Key: sample(t, p)})
		if err != nil {
			t.Fatal(err)
		}
		got, warns := parseParams(raw)
		if len(warns) != 0 {
			t.Errorf("%s: the launcher complained about a value the schema accepts: %v", p.Key, warns)
		}
		switch empty := reflect.DeepEqual(got, Params{}); {
		case p.Host && !empty:
			t.Errorf("%s is the host's, and the launch took it: %+v", p.Key, got)
		case !p.Host && empty:
			t.Errorf("%s: the launcher read the key and kept nothing of it", p.Key)
		}
	}
	for _, r := range schema.RetiredKeys() {
		if _, warns := parseParams(json.RawMessage(`{"` + r.Key + `":"x"}`)); len(warns) != 0 {
			t.Errorf("retired %s drew complaints: %v", r.Key, warns)
		}
	}
	if _, warns := parseParams(json.RawMessage(`{"colour":"red"}`)); len(warns) == 0 {
		t.Error("a key outside the schema passed in silence")
	}
}

// The line a screen shows is the command a launch runs: the same words in the
// same order, each with the parameter that put it there. A session on the
// stream folds nothing away here — the screen does — and says what its holder
// does past the handshake.
func TestPreviewIsTheCommandTheLaunchRuns(t *testing.T) {
	raw := json.RawMessage(`{"model":"opus","effort":"high","remoteControl":true,"intent":"go","args":["--verbose"]}`)
	p, _ := parseParams(raw)
	// The configuration of the panel's tools names the executor's path on the
	// host, which the panel's container does not know: the preview holds a
	// stand-in in its place, and every other word is the launch's.
	p.tools = toolsPreview
	line := Preview("panel", raw)
	if got, want := texts(line.Words), append([]string{"claude"}, claudeArgs("panel", "", p)...); !reflect.DeepEqual(got, want) {
		t.Errorf("the preview reads %v, the launch runs %v", got, want)
	}
	keys := map[string]string{}
	for _, w := range line.Words {
		keys[w.Text] = w.Key
	}
	if keys["--effort"] != keyEffort || keys["--remote-control"] != keyRemoteControl || keys["go"] != keyIntent ||
		keys["--verbose"] != keyArgs || keys["-n"] != "" || keys["--mcp-config"] != keyPanelTools ||
		keys[toolsPreview] != keyPanelTools || keys["mcp__aacpanel__checklist"] != keyPanelTools {
		t.Errorf("the words name their parameters as %v", keys)
	}
	if off := texts(Preview("panel", json.RawMessage(`{"panelTools":false}`)).Words); slices.Contains(off, "--mcp-config") {
		t.Errorf("a project with the panel's tools off previews them: %v", off)
	}

	stream := Preview("panel", json.RawMessage(`{"transport":"stream","remoteControl":true,"intent":"go"}`))
	if stream.Words[1].Key != keyTransport || len(stream.Then) != 2 || stream.Then[0].Key != keyRemoteControl {
		t.Errorf("the stream line is %+v", stream)
	}
	for _, w := range stream.Words {
		if w.Text == "go" || w.Text == "--remote-control" {
			t.Errorf("the stream command carries %q, which the holder does itself", w.Text)
		}
	}
}
