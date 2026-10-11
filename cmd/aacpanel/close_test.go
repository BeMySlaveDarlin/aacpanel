package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
)

const closeThread = "019a1f00-0000-7000-8000-00000000abcd"

// A session closing itself names its conversation — a thread of codex its
// thread — and the close goes to the session the snapshot finds running it,
// with the conversation for the executor to check against the one the session
// runs. A conversation no live session runs, one that runs under another name
// than the one given, and a codex out of the panel's reach are refused before
// the executor hears of them; a close the screen asks for goes by the name
// alone.
func TestACloseOfItselfGoesToTheSessionRunningTheConversation(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "closed"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[`+
		`{"session":"lab","sessionId":"`+switchSID+`","cwd":"/srv/proj/lab"},`+
		`{"session":"shop","sessionId":"77777777-7777-4777-8777-777777777777","cwd":"/srv/proj/shop"},`+
		`{"session":"codex-0000abcd","sessionId":"`+closeThread+`","cwd":"/srv/proj/docs","agent":"codex","transport":"stream"},`+
		`{"session":"codex-0000f00d","sessionId":"019a1f00-0000-7000-8000-00000000f00d","cwd":"/srv/proj/own","agent":"codex","outside":true}]}`))

	for body, want := range map[string]struct{ target, conversation string }{
		`{"kind":"session.close","params":{"conversation":"` + switchSID + `"}}`:                {"lab", switchSID},
		`{"kind":"session.close","target":"lab","params":{"conversation":"` + switchSID + `"}}`: {"lab", switchSID},
		`{"kind":"session.close","params":{"conversation":"` + closeThread + `"}}`:              {"codex-0000abcd", closeThread},
		`{"kind":"session.close","target":"shop"}`:                                              {"shop", ""},
	} {
		if w := post(t, srv, body); w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", body, w.Code, w.Body.String())
		}
		select {
		case got := <-fake.got:
			if got.Kind != action.SessionClose || got.Target != want.target || got.Conversation != want.conversation {
				t.Errorf("%s reached the executor as %q asked by %q, meant %+v", body, got.Target, got.Conversation, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the executor did not get the request")
		}
	}

	for body, says := range map[string]string{
		`{"kind":"session.close","params":{"conversation":"99999999-9999-4999-8999-999999999999"}}`: "no live session runs conversation",
		`{"kind":"session.close","target":"shop","params":{"conversation":"` + switchSID + `"}}`:    "runs in session lab, not in shop",
		`{"kind":"session.close","params":{"conversation":"019a1f00-0000-7000-8000-00000000f00d"}}`: "codex running on its own",
	} {
		w := post(t, srv, body)
		if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), says) {
			t.Errorf("%s gave %d: %s, meant a refusal saying %q", body, w.Code, w.Body.String(), says)
		}
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a refused close reached the executor: %+v", got)
	default:
	}

	// With no snapshot the session is not found by its conversation: a close
	// that names the session goes with the conversation for the executor to
	// check, and one that names none is refused.
	srv.host = nil
	if w := post(t, srv, `{"kind":"session.close","params":{"conversation":"`+switchSID+`"}}`); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "not known") {
		t.Errorf("with no snapshot a close by its conversation alone gave %d: %s", w.Code, w.Body.String())
	}
	if w := post(t, srv, `{"kind":"session.close","target":"lab","params":{"conversation":"`+switchSID+`"}}`); w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Target != "lab" || got.Conversation != switchSID {
			t.Errorf("with no snapshot the close reached the executor as %q asked by %q", got.Target, got.Conversation)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
}
