package launcher

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
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
	case schema.KindCodexModel:
		return "gpt-5.5"
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

// The schema is the list of what a launch can say, and the claude launcher
// reads its part whole: every common key and every key of claude lands in the
// parameters of the launch, a key the host reads, a key of codex and a retired
// one pass without a word, and a key outside it is named — so a parameter
// added to the screens without a launcher behind it fails here, not on the
// host.
func TestTheLauncherTakesEveryKeyOfTheSchema(t *testing.T) {
	agents := map[string]int{}
	for _, p := range schema.Params() {
		agents[p.Agent]++
		raw, err := json.Marshal(map[string]any{p.Key: sample(t, p)})
		if err != nil {
			t.Fatal(err)
		}
		got, warns := parseParams(raw)
		if len(warns) != 0 {
			t.Errorf("%s: the launcher complained about a value the schema accepts: %v", p.Key, warns)
		}
		passed := p.Host || p.Agent == schema.AgentCodex
		switch empty := reflect.DeepEqual(got, Params{}); {
		case passed && !empty:
			t.Errorf("%s is not the claude launch's (host %v, agent %q), and the launch took it: %+v", p.Key, p.Host, p.Agent, got)
		case !passed && empty:
			t.Errorf("%s: the launcher read the key and kept nothing of it", p.Key)
		}
	}
	for agent, n := range agents {
		if agent != "" && agent != schema.AgentClaude && agent != schema.AgentCodex {
			t.Errorf("%d parameters name agent %q, which no launcher reads", n, agent)
		}
	}
	if agents[""] == 0 || agents[schema.AgentClaude] == 0 || agents[schema.AgentCodex] == 0 {
		t.Errorf("the parameters by agent are %v: common, claude's and codex's are each expected", agents)
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

	// The keys of codex and the agent itself put no word into the command of
	// claude: the line of a project that names them is the line without them.
	codex := json.RawMessage(`{"model":"opus","effort":"high","remoteControl":true,"intent":"go","args":["--verbose"],` +
		`"agent":"claude","codexTransport":"tmux","codexModel":"gpt-5.5","codexEffort":"ultra",` +
		`"codexApproval":"never","codexSandbox":"danger-full-access"}`)
	if got := Preview("panel", codex); !reflect.DeepEqual(got, line) {
		t.Errorf("the keys of codex changed the claude line:\n%+v\nagainst\n%+v", got, line)
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

// A project whose agent is codex starts no claude: New is refused with the
// reason before anything is looked for, while a conversation that goes on is
// claude's and the agent of the project does not stand in its way.
func TestANewConversationOfACodexProjectIsRefused(t *testing.T) {
	_, err := Run(context.Background(), Spec{Dir: "/srv/proj", Session: "shop", Launch: json.RawMessage(`{"agent":"codex"}`)})
	if err == nil || !strings.Contains(err.Error(), "claude is not started in its place") {
		t.Fatalf("a new conversation of a codex project: %v", err)
	}
	p, warns := parseParams(json.RawMessage(`{"agent":"cursor"}`))
	if p.Agent != "" || len(warns) != 1 || !strings.Contains(warns[0], "the session starts claude") {
		t.Errorf("an agent no launcher knows reads as %q with %v", p.Agent, warns)
	}
}
