package executor

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/action"
)

// What claude 2.1.282 answers to the requests for its read-only screens, in
// the shape the holder passes them on.
const (
	hooksAnswer = `{"subtype":"success","request_id":"panel-3","response":{
 "events":[{"name":"PreToolUse","summary":"Before tool execution","supportsMatcher":true,"hookCount":2},
           {"name":"Stop","summary":"Right before Claude concludes its response","supportsMatcher":false,"hookCount":1}],
 "hooks":[{"event":"PreToolUse","matcher":"AskUserQuestion","source":"userSettings",
           "sourceLabel":"User settings (~/.claude/settings.json)","type":"command",
           "displayText":"python3 /srv/agent/ask-hook.py","commandText":"python3 /srv/agent/ask-hook.py",
           "contentLabel":"Command","timeout":5},
          {"event":"PreToolUse","matcher":"","source":"pluginHook","sourceLabel":"Plugin hooks",
           "pluginName":"hookify","type":"command","displayText":"Checking the rules","commandText":"node rules.js",
           "contentLabel":"Command","disabled":true},
          {"event":"Stop","matcher":"","source":"userSettings","sourceLabel":"","type":"http",
           "displayText":"https://hooks.example/stop","commandText":"https://hooks.example/stop","contentLabel":"URL",
           "condition":"Bash(git *)"}],
 "policy":{"disabledByPolicy":false,"managedOnly":false,"pluginOnly":false,"allDisabled":true,"policyHookCount":0}}}`

	memoryAnswer = `{"subtype":"success","request_id":"panel-4","response":{
 "files":[{"kind":"user","path":"/home/u/.claude/CLAUDE.md","label":"User instructions",
           "description":"Saved in ~/.claude/CLAUDE.md","exists":true},
          {"kind":"project","path":"/srv/proj/CLAUDE.md","label":"Project instructions",
           "description":"Saved in ./CLAUDE.md","exists":false}],
 "folders":[{"kind":"auto","path":"/home/u/.claude/projects/-srv-proj/memory/","label":"Open auto-memory folder","description":""}],
 "memories":[{"name":"MEMORY.md","path":"/home/u/.claude/projects/-srv-proj/memory/MEMORY.md","description":null,"type":null,"modified_ms":1790000000000},
             {"name":"deploy.md","path":"/home/u/.claude/projects/-srv-proj/memory/deploy.md",
              "description":"How the stack is rolled out","type":"project","modified_ms":1789000000000}],
 "auto_memory":{"enabled":true,"toggleable":true,"status":"on"},
 "auto_dream":{"shown":false,"enabled":false,"toggleable":true,"status":"off","detail":""}}}`

	skillsAnswer = `{"subtype":"success","request_id":"panel-5","response":{"skills":[
 {"name":"pdf","display_name":"pdf","description":"Read and fill PDF files.","source":"claude.ai sync","tokens":150,
  "state":"on","advertised":true,"handles":{}},
 {"name":"cc-skills-golang:golang-cli","display_name":"cc-skills-golang:golang-cli","description":"Go CLI apps.",
  "source":"plugin","tokens":180,"state":"on","locked_by":"plugin","advertised":true},
 {"name":"brief","display_name":"","description":"","source":"user","tokens":262,"state":"name-only","advertised":false}]}}`

	// The holder passes the merged settings on without the environment, the
	// rules of permissions and the hooks; the schema address is not a setting.
	settingsAnswer = `{"subtype":"success","request_id":"panel-6","response":{
 "applied":{"model":"claude-opus-5-5","effort":"xhigh","ultracode":false},
 "effective":{"$schema":"https://json.schemastore.org/claude-code-settings.json","theme":"dark",
              "model":"opus[1m]","statusLine":{"type":"command","command":"~/statusline.sh"}}}}`
)

func TestSetupHooksAreTheRowsOfTheMenu(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.answers = map[string]string{"get_hooks_listing": hooksAnswer}
	f.mu.Unlock()
	e, _ := newTest(t, "")

	got, err := e.Setup(context.Background(), "demo", action.SetupHooks)
	if err != nil {
		t.Fatal(err)
	}
	if req := only(t, f); req.Subtype != "get_hooks_listing" {
		t.Errorf("the session was asked %q", req.Subtype)
	}
	if got.Transport != action.SwitchStream || got.Hooks == nil {
		t.Fatalf("the answer is %+v", got)
	}
	wantEvents := []action.HookEvent{{Name: "PreToolUse", Summary: "Before tool execution", Count: 2},
		{Name: "Stop", Summary: "Right before Claude concludes its response", Count: 1}}
	if !reflect.DeepEqual(got.Hooks.Events, wantEvents) {
		t.Errorf("the events are %+v", got.Hooks.Events)
	}
	want := action.Hook{Event: "PreToolUse", Matcher: "AskUserQuestion", Source: "User settings (~/.claude/settings.json)",
		Type: "command", Label: "python3 /srv/agent/ask-hook.py", Text: "python3 /srv/agent/ask-hook.py",
		TextLabel: "Command", Timeout: 5}
	if len(got.Hooks.Hooks) != 3 || !reflect.DeepEqual(got.Hooks.Hooks[0], want) {
		t.Fatalf("the first hook is %+v, expected %+v", got.Hooks.Hooks[0], want)
	}
	plugin, http := got.Hooks.Hooks[1], got.Hooks.Hooks[2]
	if plugin.Plugin != "hookify" || !plugin.Disabled || plugin.Label != "Checking the rules" || plugin.Text != "node rules.js" {
		t.Errorf("the plugin hook is %+v", plugin)
	}
	if http.Source != "userSettings" || http.TextLabel != "URL" || http.Condition != "Bash(git *)" {
		t.Errorf("a hook with no source label lost where it comes from or its condition: %+v", http)
	}
	if !strings.Contains(got.Hooks.Blocked, "turned off") {
		t.Errorf("hooks turned off in the settings are not said to be: %q", got.Hooks.Blocked)
	}
}

func TestSetupMemoryIsTheDialog(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.answers = map[string]string{"get_memory_dialog": memoryAnswer}
	f.mu.Unlock()
	e, _ := newTest(t, "")

	got, err := e.Setup(context.Background(), "demo", action.SetupMemory)
	if err != nil {
		t.Fatal(err)
	}
	m := got.Memory
	if m == nil || m.Auto != "on" || len(m.Files) != 2 || len(m.Folders) != 1 || len(m.Memories) != 2 {
		t.Fatalf("the memory is %+v", m)
	}
	if m.Files[0].Missing || !m.Files[1].Missing || m.Files[1].Label != "Project instructions" {
		t.Errorf("a file claude has not created is not said to be missing: %+v", m.Files)
	}
	if m.Memories[0].Description != "" || m.Memories[1] != (action.SavedMemory{Name: "deploy.md",
		Description: "How the stack is rolled out", Type: "project", Modified: 1789000000000}) {
		t.Errorf("the memories are %+v", m.Memories)
	}
}

func TestSetupSkillsAreTheRowsOfTheMenu(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.answers = map[string]string{"get_skills_dialog": skillsAnswer}
	f.mu.Unlock()
	e, _ := newTest(t, "")

	got, err := e.Setup(context.Background(), "demo", action.SetupSkills)
	if err != nil {
		t.Fatal(err)
	}
	want := []action.Skill{
		{Name: "pdf", Description: "Read and fill PDF files.", Source: "claude.ai sync", Tokens: 150, State: "on"},
		{Name: "cc-skills-golang:golang-cli", Description: "Go CLI apps.", Source: "plugin", Tokens: 180, State: "on",
			LockedBy: "plugin"},
		{Name: "brief", Source: "user", Tokens: 262, State: "name-only"},
	}
	if !reflect.DeepEqual(got.Skills, want) {
		t.Errorf("the skills are %+v", got.Skills)
	}
}

// The kinds of subagents are those claude named at the handshake: the session
// is not asked again.
func TestSetupAgentsComeFromTheHandshake(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.state.Init = json.RawMessage(`{"agents":[{"name":"Explore","description":"Read-only search.","model":"haiku"},
		{"name":"reviewer","description":"Reviews a diff."},{"description":"no name"}]}`)
	f.mu.Unlock()
	e, _ := newTest(t, "")

	got, err := e.Setup(context.Background(), "demo", action.SetupAgents)
	if err != nil {
		t.Fatal(err)
	}
	want := []action.AgentType{{Name: "Explore", Description: "Read-only search.", Model: "haiku"},
		{Name: "reviewer", Description: "Reviews a diff."}}
	if !reflect.DeepEqual(got.Agents, want) {
		t.Errorf("the agents are %+v", got.Agents)
	}
	if asked := f.asked(); len(asked) != 0 {
		t.Errorf("the session was asked %+v for what it said at the handshake", asked)
	}
}

func TestSetupConfigIsTheMergedSettingsByName(t *testing.T) {
	f := onTheStream(t, false)
	f.mu.Lock()
	f.answers = map[string]string{"get_settings": settingsAnswer}
	f.mu.Unlock()
	e, _ := newTest(t, "")

	got, err := e.Setup(context.Background(), "demo", action.SetupConfig)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, c := range got.Config {
		keys = append(keys, c.Key)
	}
	if strings.Join(keys, ",") != "model,statusLine,theme" {
		t.Fatalf("the settings are %v, expected them by name without the schema address", keys)
	}
	if string(got.Config[0].Value) != `"opus[1m]"` || !strings.Contains(string(got.Config[1].Value), "statusline.sh") {
		t.Errorf("the values are %s and %s", got.Config[0].Value, got.Config[1].Value)
	}
}

// A terminal draws these screens itself: the answer says where they live.
func TestSetupOfATerminalSaysWhereItLives(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002"})
	e, _ := newTest(t, "")
	got, err := e.Setup(context.Background(), "term", action.SetupHooks)
	if err != nil {
		t.Fatal(err)
	}
	if got.Transport != action.SwitchConsole || got.Hooks != nil {
		t.Errorf("the answer is %+v, expected only where the screens live", got)
	}
	if _, err := e.Setup(context.Background(), "term", "permissions"); err == nil {
		t.Error("a screen the panel does not show was answered")
	}
}
