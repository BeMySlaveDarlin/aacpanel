package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

func pick(t *testing.T, target string, set action.Setting) (string, error) {
	t.Helper()
	e, _ := newTest(t, "")
	r := req(action.SessionSet, target)
	r.Setting = &set
	return e.Execute(context.Background(), r)
}

func TestUltracodeOnTheStreamIsAFlagOfTheSession(t *testing.T) {
	f := onTheStream(t, false)
	detail, err := pick(t, "demo", action.Setting{Effort: action.Ultracode})
	if err != nil {
		t.Fatal(err)
	}
	got := only(t, f)
	settings, _ := got.Fields["settings"].(map[string]any)
	if got.Op != stream.OpControl || got.Subtype != "apply_flag_settings" || settings["ultracode"] != true || len(settings) != 1 {
		t.Errorf("the holder was asked %+v", got)
	}
	if !strings.Contains(detail, "this session only") {
		t.Errorf("the report does not say ultracode holds for the session alone: %q", detail)
	}
}

func TestUltracodeThatDidNotTakeIsAFailure(t *testing.T) {
	f := onTheStream(t, false)
	f.fails = map[string]string{stream.OpControl: "ultracode did not take in this session"}
	if _, err := pick(t, "demo", action.Setting{Effort: action.Ultracode}); err == nil {
		t.Fatal("ultracode the session refused was reported on")
	}
}

func TestADefaultEffortOnTheStreamIsSavedByClaude(t *testing.T) {
	f := onTheStream(t, false)
	detail, err := pick(t, "demo", action.Setting{Effort: "high", Scope: action.ScopeDefault})
	if err != nil {
		t.Fatal(err)
	}
	got := f.asked()
	if len(got) != 2 {
		t.Fatalf("the holder was asked %d times: %+v", len(got), got)
	}
	save, set := got[0], got[1]
	settings, _ := save.Fields["settings"].(map[string]any)
	if save.Subtype != "update_settings" || save.Fields["source"] != "userSettings" || settings["effortLevel"] != "high" {
		t.Errorf("the default was asked for as %+v", save)
	}
	if set.Op != stream.OpSend || set.Text != "/effort high" {
		t.Errorf("the session itself was asked %+v, expected /effort high", set)
	}
	if !strings.Contains(detail, "default effort") {
		t.Errorf("the report does not say the default was saved: %q", detail)
	}
}

func TestAnEffortForTheSessionOnTheStreamSavesNothing(t *testing.T) {
	f := onTheStream(t, false)
	if _, err := pick(t, "demo", action.Setting{Effort: "high", Scope: action.ScopeSession}); err != nil {
		t.Fatal(err)
	}
	if got := only(t, f); got.Op != stream.OpSend || got.Text != "/effort high" {
		t.Errorf("the holder was asked %+v", got)
	}
}

func TestMaxAsADefaultHoldsForTheSession(t *testing.T) {
	f := onTheStream(t, false)
	detail, err := pick(t, "demo", action.Setting{Effort: "max", Scope: action.ScopeDefault})
	if err != nil {
		t.Fatal(err)
	}
	if got := only(t, f); got.Op != stream.OpSend || got.Text != "/effort max" {
		t.Errorf("the holder was asked %+v, expected only /effort max", got)
	}
	if !strings.Contains(detail, "does not save it as a default") {
		t.Errorf("the report does not say max was not saved: %q", detail)
	}
}

func TestADefaultThatWasNotSavedLeavesTheSessionAlone(t *testing.T) {
	f := onTheStream(t, false)
	f.fails = map[string]string{stream.OpControl: "refused"}
	if _, err := pick(t, "demo", action.Setting{Effort: "high", Scope: action.ScopeDefault}); err == nil {
		t.Fatal("a default claude refused to save was reported saved")
	}
	for _, r := range f.asked() {
		if r.Op == stream.OpSend {
			t.Errorf("the session was changed although its default was not saved: %+v", r)
		}
	}
}

func TestADefaultModelOnTheStreamIsWrittenIntoTheContour(t *testing.T) {
	f := onTheStream(t, false)
	config := filepath.Dir(os.Getenv(sessionsEnv))
	path := filepath.Join(config, "settings.json")
	before := "{\n  \"$schema\": \"https://json.schemastore.org/claude-code-settings.json\",\n" +
		"  \"model\": \"opus[1m]\",\n  \"env\": {\n    \"B\": \"2\",\n    \"A\": \"1\"\n  },\n  \"effortLevel\": \"xhigh\"\n}\n"
	if err := os.WriteFile(path, []byte(before), 0o640); err != nil {
		t.Fatal(err)
	}

	detail, err := pick(t, "demo", action.Setting{Model: "fable", Scope: action.ScopeDefault})
	if err != nil {
		t.Fatal(err)
	}
	if got := only(t, f); got.Op != stream.OpSend || got.Text != "/model fable" {
		t.Errorf("the session itself was asked %+v", got)
	}
	raw, _ := os.ReadFile(path)
	want := strings.Replace(before, `"model": "opus[1m]"`, `"model": "fable"`, 1)
	if string(raw) != want {
		t.Errorf("the settings were not changed in one line:\n%s\nexpected:\n%s", raw, want)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o640 {
		t.Errorf("the settings lost their mode: %v", st.Mode().Perm())
	}
	if !strings.Contains(detail, "default model") {
		t.Errorf("the report does not say the default was saved: %q", detail)
	}
}

func TestAModelForTheSessionOnTheStreamWritesNoSettings(t *testing.T) {
	onTheStream(t, false)
	config := filepath.Dir(os.Getenv(sessionsEnv))
	if _, err := pick(t, "demo", action.Setting{Model: "fable", Scope: action.ScopeSession}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("a pick for the session wrote the settings of the contour: %v", err)
	}
}

func TestATerminalRefusesAPickForTheSessionAlone(t *testing.T) {
	procFS(t, fakeProc{pid: 5003, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5557",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5003, name: "term", start: "5557", sid: "s-5003"})
	log := fakeTmux(t, []string{"5003 term:0.0"}, busyScreen)

	for _, set := range []action.Setting{{Model: "fable", Scope: action.ScopeSession}, {Effort: "high", Scope: action.ScopeSession}} {
		_, err := pick(t, "term", set)
		if err == nil || !strings.Contains(err.Error(), "terminal") {
			t.Errorf("%+v: a terminal took a pick for the session alone: %v", set, err)
		}
	}
	for _, line := range tmuxArgv(t, log) {
		if line == "send-keys" {
			t.Errorf("keys went into the terminal for a pick it refused")
		}
	}
}

func TestWithKeyChangesOneLine(t *testing.T) {
	src := "{\n  \"a\": 1,\n  \"nested\": {\n    \"z\": [],\n    \"y\": {}\n  },\n  \"list\": [\n    \"x\",\n    \"y\"\n  ],\n  \"text\": \"—\"\n}\n"
	same, err := withKey([]byte(src), "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != src {
		t.Errorf("a file claude wrote came back changed:\n%s\nwas:\n%s", same, src)
	}
	added, err := withKey([]byte(src), "model", "fable")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "  \"text\": \"—\"\n}", "  \"text\": \"—\",\n  \"model\": \"fable\"\n}", 1)
	if string(added) != want {
		t.Errorf("a new key did not go at the end:\n%s", added)
	}
	empty, err := withKey([]byte("{}"), "model", "fable")
	if err != nil || string(empty) != "{\n  \"model\": \"fable\"\n}\n" {
		t.Errorf("an empty object became %q (%v)", empty, err)
	}
	for _, bad := range []string{"[]", "", "{\"a\": 1", "null"} {
		if _, err := withKey([]byte(bad), "model", "fable"); err == nil {
			t.Errorf("%q was taken for an object of settings", bad)
		}
	}
}

func TestTheDefaultModelIsWrittenThroughALink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "shared-settings.json")
	if err := os.WriteFile(real, []byte("{\n  \"model\": \"opus\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "profile")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(config, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := saveDefaultModel(config, "sonnet"); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(filepath.Join(config, "settings.json")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link to the settings was replaced by a file: %v", err)
	}
	raw, _ := os.ReadFile(real)
	if !strings.Contains(string(raw), `"model": "sonnet"`) {
		t.Errorf("the file the link points to was not written: %s", raw)
	}
}

func TestAQuestionAsideOnTheStreamIsClaudesOwnRequest(t *testing.T) {
	f := onTheStream(t, false)
	f.answers = map[string]string{"side_question": `{"subtype":"success","request_id":"x","response":{"response":"tangerine","synthetic":false}}`}
	e, _ := newTest(t, "")
	side, err := e.Side(context.Background(), "demo", "which word?", []action.SideTurn{{Question: "q", Response: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if side.Answer != "tangerine" {
		t.Errorf("the answer came back as %q", side.Answer)
	}
	got := only(t, f)
	history, _ := got.Fields["history"].([]any)
	if got.Subtype != "side_question" || got.Fields["question"] != "which word?" || len(history) != 1 {
		t.Errorf("the holder was asked %+v", got)
	}
}

func TestATerminalIsAskedNothingAside(t *testing.T) {
	procFS(t, fakeProc{pid: 5004, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5558",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5004, name: "term", start: "5558", sid: "s-5004"})
	e, _ := newTest(t, "")
	if _, err := e.Side(context.Background(), "term", "why?", nil); err == nil || !strings.Contains(err.Error(), "/btw") {
		t.Errorf("a terminal was asked aside: %v", err)
	}
	cmds, err := e.Commands(context.Background(), "term")
	if err != nil || cmds.Transport != action.SwitchConsole || len(cmds.List) != 0 {
		t.Errorf("a terminal listed %+v (%v)", cmds, err)
	}
}

func TestTheCommandsOfAStreamSessionAreTheOnesClaudeListed(t *testing.T) {
	f := onTheStream(t, false)
	f.state.Init = []byte(`{"commands":[{"name":"brief","description":"Publishes a brief","argumentHint":"[topic]"},{"name":""},{"name":"compact","description":"Clear the history but keep a summary"}]}`)
	e, _ := newTest(t, "")
	cmds, err := e.Commands(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if cmds.Transport != action.SwitchStream || len(cmds.List) != 2 || cmds.List[0].Hint != "[topic]" || cmds.List[1].Name != "compact" {
		t.Errorf("the commands came back as %+v", cmds)
	}
}
