package executor

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
	"aacpanel/internal/stream"
)

// restartOf is a restart of the project session demo in dir, going on with
// the conversation resume.
func restartOf(dir, resume string) action.Request {
	r := req(action.SessionRestart, "demo")
	r.Resume = resume
	r.Project = &action.Project{Path: dir, Session: "demo",
		Launch: json.RawMessage(`{"model":"opus","intent":"Carry on"}`)}
	return r
}

// A restart that goes on with the conversation brings the console up again
// resuming the one it closed, with the project's parameters and the message
// after a restart.
func TestARestartGoesOnWithTheConversationOfAConsole(t *testing.T) {
	dir := consoleStand(t, "idle")
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	detail, err := e.Execute(context.Background(), restartOf(dir, consoleSID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(signals.sent, ",") != "1004:terminated" {
		t.Errorf("signals %v: the console goes the gentle way, and only it", signals.sent)
	}
	got := launched(t, log)
	if got["_resume"] != consoleSID || got["model"] != "opus" || got["intent"] != "Carry on" {
		t.Errorf("the console came back as %v", got)
	}
	if !strings.Contains(detail, "going on with conversation "+consoleSID) {
		t.Errorf("the report %q does not say the conversation goes on", detail)
	}
}

// A session on the stream goes on the same way, closed by its input.
func TestARestartGoesOnWithTheConversationOnTheStream(t *testing.T) {
	dir, holder := streamStand(t, func(*stream.State) {})
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	if _, err := e.Execute(context.Background(), restartOf(dir, streamSID)); err != nil {
		t.Fatal(err)
	}
	if got := launched(t, log)["_resume"]; got != streamSID {
		t.Errorf("the session came back resuming %v", got)
	}
	if asked := holder.asked(); len(asked) != 1 || asked[0].Op != stream.OpClose {
		t.Errorf("the holder was asked %+v", asked)
	}
}

// A conversation on the stream nobody has said a word in has no transcript:
// the session starts anew rather than close and bring nothing up.
func TestARestartOfAConversationWithNoWordStartsAnew(t *testing.T) {
	dir, _ := streamStand(t, func(s *stream.State) { s.Said = false })
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream})
	e, _ := newTest(t, "")
	withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

	detail, err := e.Execute(context.Background(), restartOf(dir, streamSID))
	if err != nil {
		t.Fatal(err)
	}
	if got := launched(t, log)["_resume"]; got != "" {
		t.Errorf("a conversation with no word was resumed as %v", got)
	}
	if !strings.Contains(detail, "started anew") {
		t.Errorf("the report %q does not say the session started anew", detail)
	}
}

// A restart goes on only with the conversation it closes: any other would
// come up in the place of the one the person was looking at.
func TestARestartGoesOnOnlyWithItsOwnConversation(t *testing.T) {
	dir := consoleStand(t, "idle")
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	_, err := e.Execute(context.Background(), restartOf(dir, streamSID))
	if err == nil || !strings.Contains(err.Error(), "runs conversation "+consoleSID) {
		t.Fatalf("a restart going on with another conversation answered %v", err)
	}
	if len(signals.sent) != 0 {
		t.Errorf("signals %v went out before the refusal", signals.sent)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("the launcher was called after the refusal")
	}
}
