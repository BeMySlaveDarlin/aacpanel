package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

// A session restarting itself names its conversation, and comes back as its
// project from the map: the same place, the same setup, and the message the
// map says to send after a restart as its first — not the one the project
// opens with, which would start the work anew.
func TestSessionRestartComesBackAsItsProjectPG(t *testing.T) {
	srv, fake, dir := switchServer(t, "stream", "stream")
	list, err := srv.db.Profiles(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatalf("the map: %v, %v", list, err)
	}
	if _, err := srv.db.UpdateProfile(t.Context(), list[0].ID, store.ProfileEdit{LaunchSet: map[string]any{
		"restartIntent": "Carry on", "intent": "read the queue",
	}}); err != nil {
		t.Fatal(err)
	}

	w := post(t, srv, `{"kind":"session.restart","params":{"conversation":"`+switchSID+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	if got.Target != "aacpanel-2" {
		t.Errorf("the restart went to %q, not to the session the conversation runs in", got.Target)
	}
	if got.Project == nil || got.Project.Path != dir || got.Project.Session != "aacpanel-2" {
		t.Fatalf("the project did not come along: %+v", got.Project)
	}
	var launch map[string]any
	if err := json.Unmarshal(got.Project.Launch, &launch); err != nil {
		t.Fatal(err)
	}
	if launch["intent"] != "Carry on" || launch["transport"] != "stream" || launch["model"] != "opus" {
		t.Errorf("the session would come back as %v — on the stream, with its model, saying the message after a restart", launch)
	}
	if got.Resume != "" {
		t.Errorf("a restart not asked to go on resumes %q", got.Resume)
	}

	w = post(t, srv, `{"kind":"session.restart","params":{"conversation":"`+switchSID+`","resume":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	got = <-fake.got
	if got.Resume != switchSID || got.Project == nil || got.Project.Path != dir {
		t.Errorf("a restart going on reached the executor as %+v", got)
	}

	w = post(t, srv, `{"kind":"session.restart","params":{"conversation":"99999999-9999-4999-8999-999999999999"}}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a conversation no session runs gave %d", w.Code)
	}
}

// A restart asked to go on hands the executor the conversation the session
// runs: the one the session named, or the one the snapshot knows it by. A
// restart not asked to go on hands none, and one whose conversation is not
// known is refused rather than started anew. The conversation a session names
// goes to the executor either way, to be checked against the one it runs.
func TestARestartGoesOnWithTheConversationTheSessionRuns(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "restarted"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[{"session":"lab","sessionId":"`+switchSID+
		`","cwd":"/srv/proj/lab"},{"session":"quiet","cwd":"/srv/proj/quiet"}]}`))

	for body, want := range map[string]struct{ resume, named string }{
		`{"kind":"session.restart","params":{"conversation":"` + switchSID + `","resume":true}}`:  {switchSID, switchSID},
		`{"kind":"session.restart","target":"lab","params":{"resume":true}}`:                      {switchSID, ""},
		`{"kind":"session.restart","params":{"conversation":"` + switchSID + `"}}`:                {"", switchSID},
		`{"kind":"session.restart","params":{"conversation":"` + switchSID + `","resume":false}}`: {"", switchSID},
		`{"kind":"session.restart","target":"lab"}`:                                               {"", ""},
	} {
		if w := post(t, srv, body); w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", body, w.Code, w.Body.String())
		}
		select {
		case got := <-fake.got:
			if got.Target != "lab" || got.Resume != want.resume || got.Conversation != want.named {
				t.Errorf("%s reached the executor as %q resuming %q asked by %q, meant %+v",
					body, got.Target, got.Resume, got.Conversation, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the executor did not get the request")
		}
	}

	w := post(t, srv, `{"kind":"session.restart","target":"quiet","params":{"resume":true}}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "cannot go on") {
		t.Errorf("a restart going on with no conversation known gave %d: %s", w.Code, w.Body.String())
	}

	// With no snapshot the conversation the session named is the one to go
	// on with, and the executor checks the session runs it.
	srv.host = nil
	if w := post(t, srv, `{"kind":"session.restart","target":"lab","params":{"conversation":"`+switchSID+`","resume":true}}`); w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Target != "lab" || got.Resume != switchSID {
			t.Errorf("with no snapshot the restart reached the executor as %q resuming %q", got.Target, got.Resume)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}
	if w := post(t, srv, `{"kind":"session.restart","target":"lab","params":{"resume":true}}`); w.Code != http.StatusBadRequest {
		t.Errorf("with no snapshot and no conversation named, a restart going on gave %d", w.Code)
	}
}

// A session writing to another names its own conversation, and the executor
// gets it with the letter, so it sends the letter from that session.
func TestALetterReachesTheExecutorWithItsSender(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "a letter from lab to shop"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	w := post(t, srv, `{"kind":"session.letter","target":"shop","params":{"text":"hello","from":"`+switchSID+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.Kind != action.SessionLetter || got.From != switchSID || got.Text != "hello" || got.Target != "shop" {
			t.Errorf("the letter reached the executor as %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the request")
	}

	for _, body := range []string{
		`{"kind":"session.letter","target":"shop","params":{"text":"hello","from":"lab"}}`,
		`{"kind":"session.letter","target":"shop","params":{"text":"hello"}}`,
	} {
		if w := post(t, srv, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s gave %d: a letter with no conversation to come from has no sender", body, w.Code)
		}
	}
}

// Where nobody names a message after a restart, the session comes back
// without one — neither a message the panel makes up nor the one the project
// opens with.
func TestRestartLaunchWithoutAMessageSaysNothing(t *testing.T) {
	raw, err := restartLaunch(json.RawMessage(`{"intent":"read the queue","model":"opus"}`))
	if err != nil {
		t.Fatal(err)
	}
	var launch map[string]any
	json.Unmarshal(raw, &launch)
	if launch["intent"] != "" || launch["model"] != "opus" {
		t.Errorf("the restart launch is %v: with no message after a restart set, the session would be told %q",
			launch, launch["intent"])
	}
	raw, _ = restartLaunch(json.RawMessage(`{"intent":"read the queue","restartIntent":""}`))
	json.Unmarshal(raw, &launch)
	if launch["intent"] != "" {
		t.Errorf("an explicit empty message after a restart became %q", launch["intent"])
	}
}
