package action

import "testing"

func TestReadinessBelongsToTheServiceAndFreshIntentToItsCycle(t *testing.T) {
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
	} {
		if err := c.req.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c.req, err)
		}
	}
}
