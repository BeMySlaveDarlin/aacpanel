package schema

import (
	"encoding/json"
	"slices"
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
		`{"model": "opus[1m]", "contextCap": 80, "autoRestart": true, "restartIntent": "Continue"}`,
		`{"autoRestart": false, "restartIntent": ""}`,
		`{"panelTools": false}`,
		`{"model": "claude-opus-5-5"}`,
		`{"env": {"FOO": "bar"}, "args": ["--verbose", "--add-dir=/opt/x"]}`,
		`{"agent": "codex"}`,
		`{"agent": "claude", "codexTransport": "tmux", "codexModel": "gpt-5.5-codex", "codexEffort": "ultra"}`,
		`{"codexApproval": "on-request", "codexSandbox": "workspace-write"}`,
		`{"codexApproval": "never", "codexSandbox": "danger-full-access"}`,
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
		"unknown key":            {`{"colour": "red"}`, "colour", "does not know"},
		"retired key":            {`{"room": "Work"}`, "room", "retired"},
		"ultracode at launch":    {`{"effort": "ultracode"}`, "effort", "is not one of"},
		"effort not a word":      {`{"effort": 3}`, "effort", "a word is expected"},
		"no such model":          {`{"model": "gpt-4"}`, "model", "neither an alias"},
		"switch as a word":       {`{"remoteControl": "yes"}`, "remoteControl", "on or off"},
		"the old threshold":      {`{"finalizeAt": 80}`, "finalizeAt", "retired"},
		"cap past the ceiling":   {`{"contextCap": 99}`, "contextCap", "outside 50–95"},
		"cap under the floor":    {`{"contextCap": 20}`, "contextCap", "outside 50–95"},
		"cap a fraction":         {`{"contextCap": 80.5}`, "contextCap", "whole number"},
		"restart as a word":      {`{"autoRestart": "on"}`, "autoRestart", "on or off"},
		"tools as a word":        {`{"panelTools": "off"}`, "panelTools", "on or off"},
		"message too long":       {`{"intent": "` + strings.Repeat("é", 501) + `"}`, "intent", "longer than 500"},
		"message with a bell":    {`{"intent": "go\u0007"}`, "intent", "forbidden character"},
		"env as a list":          {`{"env": ["FOO=bar"]}`, "env", "an object"},
		"env with a bad name":    {`{"env": {"FOO-BAR": "x"}}`, "env", "not a variable name"},
		"env moving account":     {`{"env": {"CLAUDE_CONFIG_DIR": "/x"}}`, "env", "another account"},
		"env value a number":     {`{"env": {"FOO": 1}}`, "env", "not a text"},
		"args as a line":         {`{"args": "--add-dir /opt/x"}`, "args", "a list of words"},
		"args naming a model":    {`{"args": ["--model", "opus"]}`, "args", "--model"},
		"args with = form":       {`{"args": ["--effort=max"]}`, "args", "--effort"},
		"args resuming":          {`{"args": ["--resume"]}`, "args", "resumes"},
		"mode off the list":      {`{"permissionMode": "yolo"}`, "permissionMode", "is not one of"},
		"transport off list":     {`{"transport": "screen"}`, "transport", "is not one of"},
		"agent off the list":     {`{"agent": "cursor"}`, "agent", "is not one of"},
		"codex model a flag":     {`{"codexModel": "--yolo"}`, "codexModel", "not the name of a codex model"},
		"codex model spaced":     {`{"codexModel": "gpt 5"}`, "codexModel", "not the name of a codex model"},
		"codex model a number":   {`{"codexModel": 5}`, "codexModel", "a model name is expected"},
		"codex effort of claude": {`{"codexEffort": "ultracode"}`, "codexEffort", "is not one of"},
		"approval off the list":  {`{"codexApproval": "always"}`, "codexApproval", "is not one of"},
		"sandbox off the list":   {`{"codexSandbox": "none"}`, "codexSandbox", "is not one of"},
		"codex place off list":   {`{"codexTransport": "stream"}`, "codexTransport", "is not one of"},
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

// Every parameter says the agent it is for: the agent and the first message
// are both agents', the keys of codex are codex's and the rest claude's. The
// screen lays them out under the tab of their agent, and the claude launcher
// reads only the common ones and its own.
func TestEveryParameterNamesItsAgent(t *testing.T) {
	common := map[string]bool{"agent": true, "intent": true}
	for _, p := range Params() {
		want := AgentClaude
		switch {
		case common[p.Key]:
			want = ""
		case strings.HasPrefix(p.Key, "codex"):
			want = AgentCodex
		}
		if p.Agent != want {
			t.Errorf("%s is for agent %q, meant %q", p.Key, p.Agent, want)
		}
	}
}

// Not asking at all and the sandbox off are kept where a map stores them and
// never offered, as bypassPermissions is for claude: a tap on a phone is not
// how a session stops asking before it acts.
func TestCodexHoldsWhatAPhoneDoesNotChoose(t *testing.T) {
	for key, held := range map[string]string{"codexApproval": "never", "codexSandbox": "danger-full-access"} {
		p, ok := Find(key)
		if !ok {
			t.Fatalf("%s is not in the schema", key)
		}
		if !p.Offers(held) || !slices.Contains(p.Held, held) {
			t.Errorf("%s does not keep %s a map stores", key, held)
		}
		if slices.ContainsFunc(p.Options, func(o Option) bool { return o.Value == held }) {
			t.Errorf("%s offers %s", key, held)
		}
	}
	agent, _ := Find("agent")
	if agent.Unset != "Claude Code" || len(agent.Options) != 2 {
		t.Errorf("an unset agent comes to %q among %v", agent.Unset, agent.Options)
	}
}
