package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"aacpanel/internal/mcp"
)

// The close is allowed, since a session opened for one job closes itself with
// nobody at the screen; it says when to reach for it in a line the model reads
// without the description. Both agents can open helpers; the word to Codex
// says its turn is interrupted,
// since a thread is not closed at the end of one.
func TestACloseIsAllowedAndSaysWhenToReachForIt(t *testing.T) {
	for _, tool := range []mcp.Tool{Close(Host{}), CodexClose(Host{})} {
		if !tool.Allowed {
			t.Errorf("%s asks the person: a session done with its job would wait on a prompt to close", tool.Name)
		}
		if tool.Name != "session_close" || tool.Instructions == "" || strings.Contains(tool.Instructions, "\n") ||
			!strings.Contains(tool.Instructions, tool.Name) {
			t.Errorf("%s says when to reach for it as %q", tool.Name, tool.Instructions)
		}
		if n := utf8.RuneCountInString(tool.Instructions); n > 400 {
			t.Errorf("a line of %d characters: %q", n, tool.Instructions)
		}
		if n := len(tool.Description); n > 1536 {
			t.Errorf("the description of %s is %d bytes", tool.Name, n)
		}
		for _, want := range []string{"only this session", "anyway", "only on the person's word", "stays open for follow-up turns"} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("the word of %s does not say %q: %s", tool.Name, want, tool.Description)
			}
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
			Additional *bool                      `json:"additionalProperties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil || schema.Type != "object" || schema.Required != nil ||
			schema.Additional == nil || *schema.Additional || len(schema.Properties) != 2 || schema.Properties["anyway"] == nil {
			t.Errorf("%s takes %s: anyway and nothing more, never a session to close", tool.Name, raw)
		}
	}
	if !strings.Contains(CloseInstructions, "session_open") || !strings.Contains(codexCloseInstructions, "session_open") {
		t.Error("both agents must know the helper cycle")
	}
	if !strings.Contains(CloseDescription, "at the end of the current turn") ||
		!strings.Contains(codexCloseDescription, "is interrupted") {
		t.Error("the word of each says when its session goes")
	}
}

// A close names the conversation of the session that calls it — the one its
// server finds, or the thread a call of codex proved — and never a session of
// the model's choosing: no target, only the conversation the panel finds the
// session by.
func TestACloseNamesTheConversationOfTheSessionCallingIt(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"session lab closed gracefully, the transcript is complete"}`)
	h := hostWith(t, "", url)

	said, failed := call(t, Close(h), boundAs("lab", mine), map[string]any{})
	if failed || !strings.Contains(said, "session lab closed gracefully") || !strings.Contains(said, "End the turn") {
		t.Errorf("the close answered %q (error %v)", said, failed)
	}
	if said, failed := call(t, CodexClose(h), codexBound, map[string]any{}); failed {
		t.Errorf("the close of a codex thread answered %q", said)
	}

	asked := p.calls()
	if len(asked) != 2 {
		t.Fatalf("the panel was asked %v", asked)
	}
	for i, conversation := range []string{mine, codexThread} {
		params, _ := asked[i]["params"].(map[string]any)
		if asked[i]["kind"] != "session.close" || asked[i]["target"] != "" || len(params) != 1 ||
			params["conversation"] != conversation {
			t.Errorf("call %d asked the panel %v, meant the close of conversation %s", i, asked[i], conversation)
		}
	}
}

// The close is taken when the panel does not answer in time: a close it took
// on the stream answers only once the session is gone, which is after this
// turn.
func TestACloseThePanelKeepsAnsweringIsUnderWay(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true}`)
	p.hold = make(chan struct{})
	t.Cleanup(func() { close(p.hold) })
	was := closeWait
	closeWait = 100 * time.Millisecond
	t.Cleanup(func() { closeWait = was })

	said, failed := call(t, Close(hostWith(t, "", url)), bound(mine), map[string]any{})
	if failed || !strings.Contains(said, "closing this session") || !strings.Contains(said, "end the turn") {
		t.Errorf("a close under way answered %q (error %v)", said, failed)
	}
}

// Nothing is closed while the background of the session is at work — the
// agents of a codex thread as well — and the answer says what is at work;
// anyway, on the person's word, closes all the same. The work of another
// session holds nothing.
func TestACloseWaitsForTheWorkOfTheSession(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"closed"}`)
	h := hostWith(t, `{"sessionsAt":9999999999,"sessions":[{"sessionId":"`+mine+`","work":{"agents":2,"tasks":1}},`+
		`{"sessionId":"`+codexThread+`","agent":"codex","work":{"tasks":0,"agents":1}},`+
		`{"sessionId":"`+other+`","work":{"agents":5}}]}`, url)

	said, failed := call(t, Close(h), bound(mine), map[string]any{})
	if !failed || !strings.HasPrefix(said, "WAIT: nothing was closed.") || !strings.Contains(said, "2 agents and 1 background task") ||
		!strings.Contains(said, "a close ends them") || !strings.Contains(said, "anyway") {
		t.Errorf("a close with work at work answered %q (error %v)", said, failed)
	}
	said, failed = call(t, CodexClose(h), codexBound, map[string]any{})
	if !failed || !strings.Contains(said, "1 agent of this session is at work") {
		t.Errorf("a close of a codex thread with an agent at work answered %q (error %v)", said, failed)
	}
	if len(p.calls()) != 0 {
		t.Fatalf("the panel was asked to close with work at work: %v", p.calls())
	}

	if said, failed := call(t, Close(h), bound(mine), map[string]any{"anyway": true}); failed {
		t.Errorf("anyway answered %q", said)
	}
	if said, failed := call(t, Close(h), bound(other+"-not"), map[string]any{}); failed {
		t.Errorf("the work of another session held the close: %q", said)
	}
	if len(p.calls()) != 2 {
		t.Errorf("the panel was asked %v", p.calls())
	}
}

// What stops a close comes back as the tool's error, and nothing more is
// asked: a session not known yet, a panel that refuses or is not there.
func TestWhatStopsACloseIsTheToolsError(t *testing.T) {
	refusing, refusingURL := startPanel(t, http.StatusBadRequest, "no live session runs conversation "+mine+"\n")
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	for name, c := range map[string]struct {
		bind  mcp.Bind
		panel string
		args  any
		says  string
	}{
		"no conversation yet":    {bound(""), refusingURL, map[string]any{}, "not known yet"},
		"no place":               {lost, refusingURL, map[string]any{}, "where claude process 9"},
		"arguments of another":   {bound(mine), refusingURL, map[string]any{"anyway": "yes"}, "not the tool's"},
		"a session named":        {bound(mine), refusingURL, map[string]any{"session": "shop"}, "not the tool's"},
		"the panel refuses":      {bound(mine), refusingURL, map[string]any{}, "no live session runs conversation"},
		"the panel is not there": {bound(mine), goneURL, map[string]any{}, "does not answer"},
	} {
		t.Run(name, func(t *testing.T) {
			said, failed := call(t, Close(hostWith(t, "", c.panel)), c.bind, c.args)
			if !failed || !strings.Contains(said, c.says) {
				t.Errorf("answered %q (error %v), meant an error saying %q", said, failed, c.says)
			}
		})
	}
	if n := len(refusing.calls()); n != 1 {
		t.Errorf("the panel was asked %d times, only by the call it refused", n)
	}
}
