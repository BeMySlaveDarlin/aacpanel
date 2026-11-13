package action

import "testing"

func TestReadinessBelongsToTheServiceAndFreshIntentToItsCycle(t *testing.T) {
	if Valid(SessionFinalize) {
		t.Fatal("wrap up reached the executor's closed list")
	}
	if Valid(SessionReady) {
		t.Fatal("readiness reached the executor's closed list")
	}
	if err := (Request{ID: "ready", Kind: SessionReady, Target: "lab"}).Validate(); err == nil {
		t.Fatal("the executor accepts readiness")
	}
	const conversation = "88888888-8888-4888-8888-888888888888"
	for _, c := range []struct {
		req Request
		ok  bool
	}{
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Text: "Read the handoff", Conversation: conversation, AutoRestart: true}, true},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Text: "Read the handoff", Conversation: conversation, AutoRestart: true, Anyway: true}, true},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Text: "Read the handoff", Conversation: conversation}, false},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Text: "Read the handoff", AutoRestart: true}, false},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Conversation: conversation, AutoRestart: true, Resume: conversation}, false},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Anyway: true}, false},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Clean: true}, true},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Clean: true, Text: "Continue"}, false},
		{Request{ID: "restart", Kind: SessionRestart, Target: "lab", Clean: true, Resume: conversation}, false},
		{Request{ID: "send", Kind: SessionSend, Target: "lab", Clean: true, Text: "Continue"}, false},
	} {
		if err := c.req.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c.req, err)
		}
	}
}

func TestFinalizeCannotBeSentAsAPluginCommand(t *testing.T) {
	if _, ok := Commands["finalize"]; ok {
		t.Fatal("finalize is still a slash command")
	}
	for _, r := range []Request{
		{ID: "command", Kind: SessionCommand, Target: "lab", Command: &Command{Name: "finalize"}},
		{ID: "send", Kind: SessionSend, Target: "lab", Text: "/finalize"},
		{ID: "send", Kind: SessionSend, Target: "lab", Text: "/Finalize save everything"},
	} {
		if err := r.Validate(); err == nil {
			t.Fatalf("the panel can still send /finalize: %+v", r)
		}
	}
}
