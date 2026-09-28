package stream

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const shellID = "22222222-3333-4444-8555-666666666666"

// recordedShell is a command run once through claude -p 2.1.283 on the stream,
// and recordedShellLines what claude wrote for it after it had the command,
// word for word but for the ids: the fake plays them back for this command.
// The echo of the command is escaped for markup, the streams and the exit code
// come as a message of their own, the end names the command by its id, and a
// status follows that changes nothing; no turn of the model starts.
const recordedShell = `printf 'out\n'; printf 'err\n' >&2; exit 3`

var recordedShellLines = []string{
	`{"type":"user","message":{"role":"user","content":"<bash-input>printf 'out\\n'; printf 'err\\n' &gt;&amp;2; exit 3</bash-input>"},"session_id":"{session}","parent_tool_use_id":null,"uuid":"0b1c2d3e-4f50-4617-8829-3a4b5c6d7e8f","timestamp":"2026-01-01T00:00:00.795Z","isReplay":true}`,
	`{"type":"user","message":{"role":"user","content":"<bash-stdout>out</bash-stdout><bash-stderr>err</bash-stderr><bash-exit-code>3</bash-exit-code>"},"session_id":"{session}","parent_tool_use_id":null,"uuid":"1c2d3e4f-5061-4728-939a-4b5c6d7e8f90","timestamp":"2026-01-01T00:00:00.796Z","isReplay":true}`,
	`{"type":"command_lifecycle","command_uuid":"{command}","state":"completed","uuid":"2d3e4f50-6172-4839-a4ab-5c6d7e8f9001","session_id":"{session}"}`,
	`{"type":"system","subtype":"status","status":null,"permissionMode":"default","uuid":"3e4f5061-7283-494a-b5bc-6d7e8f900112","session_id":"{session}"}`,
}

// lines returns what claude read, one message a line, of one type.
func (r *rig) lines(kind string) []map[string]any {
	r.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(r.received(), "\n") {
		var msg map[string]any
		if json.Unmarshal([]byte(line), &msg) == nil && msg["type"] == kind {
			out = append(out, msg)
		}
	}
	return out
}

// passed returns the messages that carried the output of a command into the
// conversation.
func (r *rig) passed() []map[string]any {
	r.t.Helper()
	var out []map[string]any
	for _, msg := range r.lines("user") {
		text, _ := msg["message"].(map[string]any)["content"].(string)
		if strings.HasPrefix(text, "<bash-input>") {
			out = append(out, msg)
		}
	}
	return out
}

// passedFor waits for the output of a command to go into the conversation.
func (r *rig) passedFor(command string) map[string]any {
	r.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, msg := range r.passed() {
			if strings.HasPrefix(content(msg), "<bash-input>"+command+"</bash-input>") {
				return msg
			}
		}
	}
	r.t.Fatalf("the output of %q never went into the conversation:\n%s", command, r.received())
	return nil
}

func content(msg map[string]any) string {
	text, _ := msg["message"].(map[string]any)["content"].(string)
	return text
}

// A command after "!" is claude's to run, not the model's: it goes as a
// command, and what it printed goes into the conversation in the shape a
// terminal writes it — the command, then the streams — for the model to read.
func TestAShellCommandIsRunByClaudeAndItsOutputJoinsTheConversation(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })

	reply := r.ask(Request{Op: OpShell, Text: "make check", UUID: shellID})
	if !reply.OK || reply.UUID != shellID {
		t.Fatalf("the command was answered %+v", reply)
	}
	r.eventually(`"type":"bash_command"`)
	commands := r.lines("bash_command")
	if len(commands) != 1 || commands[0]["command"] != "make check" || commands[0]["uuid"] != shellID {
		t.Fatalf("claude was handed %+v", commands)
	}

	r.passedFor("make check")
	passed := r.passed()
	if prompts := r.lines("user"); len(prompts) != 1 || len(passed) != 1 {
		t.Fatalf("claude read %d messages, %d of them the output: the command itself went as a message, "+
			"or the output went twice: %+v", len(prompts), len(passed), prompts)
	}
	want := "<bash-input>make check</bash-input><bash-stdout>ran: make check</bash-stdout>" +
		"<bash-stderr></bash-stderr><bash-exit-code>0</bash-exit-code>"
	if got := content(passed[0]); got != want {
		t.Errorf("the conversation got\n%s\nwhere the terminal writes\n%s", got, want)
	}
	if passed[0]["client_composed"] != true {
		t.Errorf("the output goes as typed by the person: an @ in it would look up a file, %+v", passed[0])
	}
	s := r.waitFor("the command to be over", func(s State) bool { return len(s.Shells) == 0 && !s.Busy })
	if len(s.Queue) != 0 {
		t.Errorf("the output stays in the queue after claude read it: %+v", s.Queue)
	}
}

// What claude itself writes for a command is read as it is: the escaped echo
// is not the output, the output goes into the conversation under the command
// as typed, and nothing else reaches claude.
func TestTheLinesClaudeWritesForACommandPassItsOutputOn(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })

	if reply := r.ask(Request{Op: OpShell, Text: recordedShell, UUID: shellID}); !reply.OK {
		t.Fatalf("the command was answered %+v", reply)
	}
	msg := r.passedFor(recordedShell)
	want := "<bash-input>" + recordedShell + "</bash-input>" +
		"<bash-stdout>out</bash-stdout><bash-stderr>err</bash-stderr><bash-exit-code>3</bash-exit-code>"
	if got := content(msg); got != want {
		t.Errorf("the conversation got\n%s\nwhere the terminal writes\n%s", got, want)
	}
	if prompts := r.lines("user"); len(prompts) != 1 {
		t.Errorf("claude read %d messages where the output is the only one: %+v", len(prompts), prompts)
	}
	s := r.waitFor("the command to be over", func(s State) bool { return len(s.Shells) == 0 && !s.Busy })
	if s.Compacting != nil || s.Mode != "default" {
		t.Errorf("the status after the command changed the session: compacting %v, mode %q", s.Compacting, s.Mode)
	}
}

// Claude runs a command beside the conversation: the session is not busy
// while it runs, and the command is known to be running until it ends.
func TestARunningCommandIsKnownAndStartsNoTurn(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })

	r.ask(Request{Op: OpShell, Text: "sleep 60", UUID: shellID})
	r.eventually(`"type":"bash_command"`)
	s := r.waitFor("the command to run", func(s State) bool { return len(s.Shells) == 1 })
	if s.Shells[0].UUID != shellID || s.Shells[0].Command != "sleep 60" || s.Shells[0].Since.IsZero() {
		t.Errorf("the running command is %+v", s.Shells[0])
	}
	if s.Busy {
		t.Error("a running command made the session busy, and it would take a message for a queued one")
	}
	if len(r.passed()) != 0 {
		t.Errorf("an output went into the conversation before the command ended: %+v", r.passed())
	}
	raw := r.summary(func(Summary) bool { return true })
	if body, _ := json.Marshal(raw); strings.Contains(string(body), "sleep 60") {
		t.Errorf("the command reached the state file the collector reads: %s", body)
	}
}

// Claude names the command in the word after its output, and two commands
// end in any order: each output goes in under its own command.
func TestEachOutputGoesInUnderItsOwnCommand(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })

	r.ask(Request{Op: OpShell, Text: "sleep 5"})
	r.eventually("sleep 5")
	r.ask(Request{Op: OpShell, Text: "false"})
	r.passedFor("false")
	r.ask(Request{Op: OpSend, Text: "wake"})
	r.passedFor("sleep 5")

	got := map[string]string{}
	for _, msg := range r.passed() {
		text := content(msg)
		got[text[:strings.Index(text, "</bash-input>")]] = text
	}
	if want := "<bash-stdout></bash-stdout><bash-stderr>it failed &amp; said so</bash-stderr><bash-exit-code>1</bash-exit-code>"; !strings.HasSuffix(got["<bash-input>false"], want) {
		t.Errorf("the failed command went in as %q", got["<bash-input>false"])
	}
	if want := "<bash-stdout>ran: sleep 5</bash-stdout>"; !strings.Contains(got["<bash-input>sleep 5"], want) {
		t.Errorf("the long command went in as %q", got["<bash-input>sleep 5"])
	}
	r.waitFor("both commands to be over", func(s State) bool { return len(s.Shells) == 0 })
}

// A command's end claude reports for a message the panel sent is no command
// of the panel's: it takes the message out of the queue and hands nothing on.
func TestTheEndOfAMessageIsNotTakenForACommand(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	r.ask(Request{Op: OpSend, Text: "/plugins"})
	r.waitFor("the command to leave the queue", func(s State) bool { return len(s.Queue) == 0 })
	if len(r.passed()) != 0 {
		t.Errorf("an output went into the conversation for a message: %+v", r.passed())
	}
}

func TestAShellCommandNeedsWordsAndAnID(t *testing.T) {
	r := start(t, nil)
	r.waitFor("the handshake", func(s State) bool { return len(s.Init) > 0 })
	if reply := r.ask(Request{Op: OpShell, Text: "  "}); reply.OK {
		t.Error("an empty command was run")
	}
	if reply := r.ask(Request{Op: OpShell, Text: "ls", UUID: "$(rm -rf ~)"}); reply.OK {
		t.Error("a command went with an id that is not a uuid")
	}
	if got := r.lines("bash_command"); len(got) != 0 {
		t.Errorf("claude was handed %+v", got)
	}
}

// The model gets no more of a stream than a terminal gives it, and what is
// cut is the middle: the first lines say what ran, the last how it ended.
func TestALongOutputIsCutInTheMiddle(t *testing.T) {
	long := "first\n" + strings.Repeat("x", 3*shellKeep) + "\nlast"
	out := "<bash-stdout>" + long + "</bash-stdout><bash-stderr>oops</bash-stderr><bash-exit-code>2</bash-exit-code>"
	got := shellMessage("cat big", out)
	if !strings.HasPrefix(got, "<bash-input>cat big</bash-input><bash-stdout>first\n") {
		t.Errorf("the start is lost: %.80q", got)
	}
	if !strings.HasSuffix(got, "\nlast</bash-stdout><bash-stderr>oops</bash-stderr><bash-exit-code>2</bash-exit-code>") {
		t.Errorf("the end is lost: …%q", got[len(got)-120:])
	}
	if !strings.Contains(got, "characters cut") {
		t.Error("the cut is not said")
	}
	if n := len([]rune(got)); n > shellKeep+200 {
		t.Errorf("%d characters went to the model against a ceiling of %d", n, shellKeep)
	}
	if short := shellMessage("ls", "<bash-stdout>a</bash-stdout><bash-stderr></bash-stderr>"); short !=
		"<bash-input>ls</bash-input><bash-stdout>a</bash-stdout><bash-stderr></bash-stderr>" {
		t.Errorf("a short output was changed: %q", short)
	}
}
