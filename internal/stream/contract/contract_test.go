package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func events(t *testing.T, text string) []object {
	t.Helper()
	var out []object
	readEvents(strings.NewReader(text), func(e eventRead) bool {
		if e.err != nil {
			t.Fatal(e.err)
		}
		out = append(out, e.event)
		return true
	})
	return out
}
func sample(t *testing.T, name string) []object {
	t.Helper()
	return events(t, sampleText(t, name))
}
func sampleText(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func one(t *testing.T, text string) object {
	t.Helper()
	got := events(t, text)
	if len(got) != 1 {
		t.Fatalf("wanted one event, got %d", len(got))
	}
	return got[0]
}
func want(t *testing.T, r Result, status string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: got %s (%s), want %s", r.Check, r.Status, r.Detail, status)
	}
}
func TestEnvironmentAndHelp(t *testing.T) {
	env := cleanEnv([]string{"HOME=/home/u", "PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=abc", "CLAUDE_CODE_MESSAGING_SOCKET=/run/x.sock", "CLAUDE_CONFIG_DIR=/home/u/.claude-work"})
	if !reflect.DeepEqual(env, []string{"HOME=/home/u", "PATH=/bin", "LANG=C.UTF-8", "CLAUDE_CONFIG_DIR=/home/u/.claude-work"}) {
		t.Fatal(env)
	}
	if configDir(env) != "/home/u/.claude-work" {
		t.Fatal(configDir(env))
	}
	if len(directConnect("usage: claude [options]")) != 0 {
		t.Fatal("invented a console attachment")
	}
	if got := directConnect("claude connect <cc://host>"); !reflect.DeepEqual(got, []string{"cc://", "claude connect"}) {
		t.Fatal(got)
	}
	if !strings.Contains(strings.Join(streamFlags, " "), "--permission-prompt-tool") {
		t.Fatal("the permission prompt flag is not checked")
	}
}
func TestReportFormatAndExitCode(t *testing.T) {
	r := Report{verdict("turn", true, "ready"), {"notes", Fail, "unavailable", false}}
	if r.ExitCode() != 0 {
		t.Fatal("an optional check failed the run")
	}
	r = append(r, verdict("ask", false, "missing"))
	if r.ExitCode() != 1 {
		t.Fatal("a required check did not fail the run")
	}
	wantText := "turn   ok    ready\nnotes  FAIL  (info) unavailable\nask    FAIL  missing\n"
	if r.Text() != wantText {
		t.Fatalf("%q != %q", r.Text(), wantText)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `[{"check":"turn","status":"ok","detail":"ready","required":true},{"check":"notes","status":"FAIL","detail":"unavailable","required":false},{"check":"ask","status":"FAIL","detail":"missing","required":true}]` {
		t.Fatal(string(b))
	}
}
func TestRecordedProtocol(t *testing.T) {
	local := sample(t, "local-command.jsonl")
	if text := assistantText(local); text != "/plugins isn't available in this environment." {
		t.Fatal(text)
	}
	want(t, slashVerdict(local), OK)
	want(t, slashVerdict(nil), Fail)
	if !success(lastResult(local)) {
		t.Fatal("the recorded command result was lost")
	}
	models := sample(t, "list-models.jsonl")[0].obj("response")
	got := dataVerdict("list_models", models)
	want(t, got, OK)
	if got.Detail != "models: 1, each with description, displayName, resolvedModel, supportedEffortLevels, supportsAdaptiveThinking, supportsAutoMode, supportsEffort, supportsFastMode, value" {
		t.Fatal(got.Detail)
	}
	want(t, dataVerdict("list_models", one(t, `{"subtype":"error","error":"unknown request"}`)), Fail)
	if keys(one(t, `{"b":1,"a":2}`)) != "a, b" {
		t.Fatal("response keys are not sorted")
	}
	// Diagnostics and an oversized frame cannot hide the valid frame after them.
	huge := strings.Repeat("x", 128*1024)
	body, _ := json.Marshal(object{"type": "assistant", "message": object{"content": []object{{"type": "text", "text": huge}}}})
	parsed := events(t, "diagnostic\n"+string(body)+"\n"+`{"type":"result","subtype":"success"}`)
	if assistantText(parsed) != huge || !success(lastResult(parsed)) {
		t.Fatal("stream framing lost data")
	}
}
func TestPermissionsAndQuestions(t *testing.T) {
	bash := one(t, `{"type":"control_request","request_id":"ask-1","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"tool-1","input":{"command":"touch contract-marker.txt"},"permission_suggestions":[]}}`)
	if !isAsk(bash) || toolOf(bash) != "Bash" {
		t.Fatal("the permission was not recognized")
	}
	want(t, permissionVerdict([]object{bash}, true), OK)
	want(t, permissionVerdict([]object{bash}, false), Fail)
	want(t, permissionVerdict(nil, true), Fail)
	delete(bash.obj("request"), "permission_suggestions")
	want(t, permissionVerdict([]object{bash}, true), Fail)
	denied := events(t, `{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"the contract says no"}]}]}}`)
	want(t, denyVerdict([]object{bash}, denied, false), OK)
	want(t, denyVerdict([]object{bash}, denied, true), Fail)
	want(t, denyVerdict([]object{bash}, nil, false), Fail)
	ask := one(t, `{"type":"control_request","request_id":"ask-2","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Pick a colour","options":[{"label":"Red","preview":"RED BLOCK"},{"label":"Blue","preview":"BLUE BLOCK"}]},{"question":"Pick a size","options":[{"label":"Small"},{"label":"Large"}]}]}}}`)
	answer := askAnswer(ask)
	if answer.obj("answers").text("Pick a colour") != "Blue" || answer.obj("annotations").obj("Pick a colour").text("notes") != "contract-note" {
		t.Fatal(answer)
	}
	heard := events(t, `{"type":"user","message":{"content":[{"type":"tool_result","content":"Blue Large contract-note"}]}}`)
	out := askVerdict([]object{ask}, heard)
	want(t, out[0], OK)
	want(t, out[1], OK)
	out = askVerdict([]object{ask}, nil)
	want(t, out[0], Fail)
	want(t, out[1], Info)
	if out[1].Required {
		t.Fatal("annotations became a required check")
	}
	want(t, askVerdict(nil, nil)[0], Fail)
}
func TestTurnAndBackgroundVerdicts(t *testing.T) {
	ack := one(t, `{"subtype":"success","response":{}}`)
	want(t, planVerdict(one(t, `{"plan":"make a file"}`), ack), OK)
	want(t, planVerdict(nil, ack), Fail)
	want(t, planVerdict(one(t, `{"plan":"make a file"}`), nil), Fail)
	want(t, interruptVerdict(ack, one(t, `{"subtype":"error_during_execution"}`)), OK)
	want(t, interruptVerdict(ack, one(t, `{"subtype":"success"}`)), Fail)
	running := events(t, `{"type":"system","subtype":"task_started","task_id":"task-1"}
{"type":"system","subtype":"task_updated","task_id":"task-1","patch":{"status":"running"}}
{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"task-1"}]}`)
	want(t, backgroundVerdict("task-1", running), OK)
	for _, status := range []string{"completed", "failed", "killed", "stopped"} {
		end := one(t, `{"type":"system","subtype":"task_updated","task_id":"task-1","patch":{"status":"`+status+`"}}`)
		if !taskEnded(end, "task-1") || taskEnded(end, "other") {
			t.Fatal(end)
		}
		want(t, backgroundVerdict("task-1", append(running, end)), Fail)
		want(t, stopVerdict("task-1", ack, []object{end}), OK)
	}
	want(t, stopVerdict("task-1", ack, running), Fail)
	note := one(t, `{"type":"system","subtype":"task_notification","task_id":"task-1"}`)
	if !taskEnded(note, "task-1") {
		t.Fatal("a notification did not end its task")
	}
	want(t, resumeVerdict("sid", one(t, `{"session_id":"sid","result":"RESUMED"}`)), OK)
	want(t, resumeVerdict("sid", one(t, `{"session_id":"other","result":"RESUMED"}`)), Fail)
	want(t, resumeVerdict("sid", nil), Fail)
}
func TestQueue(t *testing.T) {
	cancelled := one(t, `{"subtype":"success","response":{"cancelled":true}}`)
	peer := one(t, `{"subtype":"success","response":{"cancelled":false}}`)
	kept := events(t, `{"type":"assistant","message":{"content":[{"type":"text","text":"KEPTWORD LETTERWORD"}]}}`)
	for _, r := range queueVerdict(cancelled, kept) {
		want(t, r, OK)
	}
	want(t, queueVerdict(peer, kept)[0], Fail)
	dropped := events(t, `{"type":"assistant","message":{"content":[{"type":"text","text":"KEPTWORD DROPPEDWORD"}]}}`)
	want(t, queueVerdict(cancelled, dropped)[1], Fail)
}
func TestFilesAndConsoleTrust(t *testing.T) {
	dir := t.TempDir()
	if err := writeHooks(dir); err != nil {
		t.Fatal(err)
	}
	want(t, checkHooks(dir), Fail)
	if err := os.WriteFile(filepath.Join(dir, "hooks.log"), []byte("SessionStart\nStop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := checkHooks(dir)
	want(t, got, Fail)
	if !strings.Contains(got.Detail, "UserPromptSubmit") {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.log"), []byte("SessionStart\nUserPromptSubmit\nStop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want(t, checkHooks(dir), OK)
	project := filepath.Join(dir, "projects", "-some-where")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "sid.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if findTranscript(dir, "sid") != path || findTranscript(dir, "missing") != "" {
		t.Fatal("transcript lookup ignored the session id")
	}
	for screen, want := range map[string]string{"loading": "", "Yes, I trust this folder\n❯ No, exit": "Down", "❯ Yes, I trust this folder\nNo, exit": "Enter"} {
		if got := trustKey(screen); got != want {
			t.Fatalf("%q: %s != %s", screen, got, want)
		}
	}
}

type bufferCloser struct{ bytes.Buffer }

func (*bufferCloser) Close() error { return nil }
func TestControlResponseMatchingKeepsInterleavedEvents(t *testing.T) {
	read := make(chan eventRead, 8)
	for _, ev := range sample(t, "list-models.jsonl") {
		read <- eventRead{event: ev}
	}
	for _, ev := range events(t, `{"type":"result","subtype":"error_during_execution"}
{"type":"control_response","response":{"subtype":"success","request_id":"contract-1","response":{"still_queued":[]}}}`) {
		read <- eventRead{event: ev}
	}
	close(read)
	input := &bufferCloser{}
	s := &session{events: read, input: input}
	got := s.request("interrupt", nil)
	if !success(got) || got.text("request_id") != "contract-1" || len(s.seen) != 3 || lastResult(s.seen).text("subtype") != "error_during_execution" {
		t.Fatal(got, s.seen)
	}
	sent := one(t, input.String())
	if sent.obj("request").text("subtype") != "interrupt" || sent.text("request_id") != "contract-1" {
		t.Fatal(sent)
	}
	if s.pump(isResult, time.Second, nil) != nil {
		t.Fatal("a closed stream invented an event")
	}
}
func TestStreamReadFailureIsNotATimeout(t *testing.T) {
	r, w := io.Pipe()
	go w.CloseWithError(io.ErrUnexpectedEOF)
	var got error
	readEvents(r, func(e eventRead) bool { got = e.err; return true })
	if got != io.ErrUnexpectedEOF {
		t.Fatal(got)
	}
	r.Close()
}

func replaySession(t *testing.T, text string) (*session, *bufferCloser) {
	t.Helper()
	parsed := events(t, text)
	read := make(chan eventRead, len(parsed))
	for _, ev := range parsed {
		read <- eventRead{event: ev}
	}
	close(read)
	input := &bufferCloser{}
	return &session{sid: "sid", input: input, events: read}, input
}
func TestInitializeTurnModelAndEffort(t *testing.T) {
	s, _ := replaySession(t, `{"type":"control_response","response":{"subtype":"success","request_id":"contract-1","response":{"commands":[{"name":"context"}],"models":[{"value":"haiku"}],"current_permission_mode":"default"}}}`)
	want(t, checkInitialize(s), OK)
	s, _ = replaySession(t, `{"type":"control_response","response":{"subtype":"success","request_id":"contract-1","response":{"commands":[],"models":[]}}}`)
	want(t, checkInitialize(s), Fail)
	dir := t.TempDir()
	project := filepath.Join(dir, "projects", "-project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "sid.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ = replaySession(t, `{"type":"result","subtype":"success","result":"ready"}`)
	out := checkTurn(s, dir)
	for _, r := range out {
		want(t, r, OK)
	}
	s, _ = replaySession(t, `{"type":"result","subtype":"error_during_execution"}`)
	want(t, checkTurn(s, dir)[0], Fail)
	s, input := replaySession(t, `{"type":"control_response","response":{"subtype":"success","request_id":"contract-1"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Sonnet context"}]}}
{"type":"result","subtype":"success"}
{"type":"control_response","response":{"subtype":"success","request_id":"contract-2"}}`)
	want(t, checkModel(s, "haiku"), OK)
	sent := events(t, input.String())
	if sent[0].obj("request").text("model") != "sonnet" || sent[len(sent)-1].obj("request").text("model") != "haiku" {
		t.Fatal("the model was not restored", sent)
	}
	s, _ = replaySession(t, `{"type":"control_response","response":{"subtype":"success","request_id":"contract-1"}}
{"type":"user","message":{"content":"effort high"}}
{"type":"result","subtype":"success"}`)
	for _, r := range checkEffort(s) {
		want(t, r, OK)
	}
}
func TestPermissionAnswersKeepTheRequestInput(t *testing.T) {
	s, input := replaySession(t, "")
	ev := one(t, `{"request_id":"ask-1","request":{"input":{"command":"echo ready","description":"a probe"}}}`)
	s.allow(ev, object{"answers": object{"Pick a size": "Large"}})
	sent := one(t, input.String()).obj("response")
	body := sent.obj("response").obj("updatedInput")
	if sent.text("request_id") != "ask-1" || sent.text("subtype") != "success" || body.text("command") != "echo ready" || body.text("description") != "a probe" || body.obj("answers").text("Pick a size") != "Large" {
		t.Fatal(sent)
	}
}
