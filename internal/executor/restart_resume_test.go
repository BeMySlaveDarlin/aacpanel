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

// A restart that goes on brings the session up with the message it wrote
// itself as the first one, in place of the map's message after a restart, in
// tmux and on the stream alike. A conversation on the stream nobody said a
// word in starts anew, and a session that starts anew opens with the map's
// message: the words were written for a conversation it does not have.
func TestARestartGoesOnWithTheMessageTheSessionWrote(t *testing.T) {
	const own = "Writing the restart tests; next, the executor's case."
	for name, c := range map[string]struct {
		stand        func(t *testing.T) (string, string)
		transport    string
		resume, says string
	}{
		"in tmux": {func(t *testing.T) (string, string) { return consoleStand(t, "idle"), consoleSID },
			launcher.TransportTmux, consoleSID, own},
		"on the stream": {func(t *testing.T) (string, string) {
			dir, _ := streamStand(t, func(*stream.State) {})
			return dir, streamSID
		}, launcher.TransportStream, streamSID, own},
		"on the stream with no word said": {func(t *testing.T) (string, string) {
			dir, _ := streamStand(t, func(s *stream.State) { s.Said = false })
			return dir, streamSID
		}, launcher.TransportStream, "", "Carry on"},
	} {
		t.Run(name, func(t *testing.T) {
			dir, sid := c.stand(t)
			log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: c.transport})
			e, _ := newTest(t, "")
			withSignals(t, e, map[int]bool{1004: true, 5001: true}, map[int]int{1004: 1, 5001: 1})

			r := restartOf(dir, sid)
			r.Text = own
			if _, err := e.Execute(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			got := launched(t, log)
			if got["_resume"] != c.resume || got["intent"] != c.says || got["model"] != "opus" {
				t.Errorf("the session came back as %v, meant resuming %q and saying %q first", got, c.resume, c.says)
			}
		})
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

// A restart a session asks for without going on names the conversation it is
// asked by, and goes ahead only while the session runs it: a late repeat of
// the call would close the session an earlier restart brought up in its place.
func TestARestartFromScratchClosesOnlyTheConversationItNames(t *testing.T) {
	dir := consoleStand(t, "idle")
	log := fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportTmux})
	e, _ := newTest(t, "")
	signals := withSignals(t, e, map[int]bool{1004: true}, map[int]int{1004: 1})

	late := restartOf(dir, "")
	late.Conversation = streamSID
	_, err := e.Execute(context.Background(), late)
	if err == nil || !strings.Contains(err.Error(), "runs conversation "+consoleSID) {
		t.Fatalf("a restart asked for by a conversation the session no longer runs answered %v", err)
	}
	if len(signals.sent) != 0 {
		t.Errorf("signals %v went out before the refusal", signals.sent)
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("the launcher was called after the refusal")
	}

	own := restartOf(dir, "")
	own.ID = "restart-own"
	own.Conversation = consoleSID
	detail, err := e.Execute(context.Background(), own)
	if err != nil {
		t.Fatal(err)
	}
	if got := launched(t, log); got["_resume"] != "" || got["intent"] != "Carry on" {
		t.Errorf("a restart from scratch came back as %v, not anew with the map's message after a restart", got)
	}
	if !strings.Contains(detail, "with an empty context") {
		t.Errorf("the report %q does not say the session started anew", detail)
	}
}

// A session restarted anew runs a new conversation, and the sessions it had
// opened stay its children: their records move to the new conversation, so it
// can still close them and their reports still reach it. A restart that goes
// on with the conversation keeps it, and the records stay as they are; the
// children of another conversation are not touched either way.
func TestARestartHandsTheChildrenToTheConversationItComesBackWith(t *testing.T) {
	const (
		anew  = "88888888-8888-4888-8888-888888888888"
		other = "99999999-9999-4999-8999-999999999999"
	)
	for _, c := range []struct {
		name, resume, back, owner string
	}{
		{"anew", "", anew, anew},
		{"going on", streamSID, streamSID, streamSID},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			mine := []string{"process-7001-100", "019a2000-0000-7000-8000-00000000c0de"}
			for _, id := range mine {
				if err := stream.KeepOwner(id, stream.SessionOwner{Name: "demo", Conversation: streamSID}); err != nil {
					t.Fatal(err)
				}
			}
			if err := stream.KeepOwner("process-7002-100", stream.SessionOwner{Name: "lab", Conversation: other}); err != nil {
				t.Fatal(err)
			}
			dir, _ := streamStand(t, func(*stream.State) {})
			fakeLauncher(t, launcher.Report{Session: "demo", Transport: launcher.TransportStream, Conversation: c.back})
			e, _ := newTest(t, "")
			withSignals(t, e, map[int]bool{5001: true}, map[int]int{5001: 1})

			detail, err := e.Execute(context.Background(), restartOf(dir, c.resume))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range mine {
				if got := stream.OwnerOf(id); got.Conversation != c.owner || got.Name != "demo" {
					t.Errorf("child %s is recorded as opened by %+v, meant conversation %s: the restarted session "+
						"could not close it (%s)", id, got, c.owner, detail)
				}
			}
			if got := stream.OwnerOf("process-7002-100"); got.Conversation != other || got.Name != "lab" {
				t.Errorf("a child of another conversation moved to %+v", got)
			}
		})
	}
}
