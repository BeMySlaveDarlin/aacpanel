package main

import (
	"encoding/json"
	"net/http"
	"strconv"
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

	// A thread of codex names itself and where it runs.
	w = post(t, srv, `{"kind":"session.letter","target":"shop","params":{"text":"hello","from":"`+switchSID+`",`+
		`"fromCodex":{"home":"/home/u/.codex","dir":"/srv/proj/lab"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	select {
	case got := <-fake.got:
		if got.From != switchSID || got.FromCodex == nil || *got.FromCodex != (action.CodexSender{Home: "/home/u/.codex", Dir: "/srv/proj/lab"}) {
			t.Errorf("the letter of codex reached the executor as %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the executor did not get the letter of codex")
	}

	for _, body := range []string{
		`{"kind":"session.letter","target":"shop","params":{"text":"hello","from":"lab"}}`,
		`{"kind":"session.letter","target":"shop","params":{"text":"hello"}}`,
		`{"kind":"session.letter","target":"shop","params":{"text":"hello","from":"` + switchSID + `","fromCodex":{"dir":"/srv"}}}`,
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

// New of a project whose agent is codex goes to the executor with the
// project's launch, and its answer names the session the executor brought up:
// a codex thread is named by its id, which the screen does not know before. A
// restart is refused before the executor hears of it — of a codex thread,
// since its daemon keeps it, and of a claude session of the project, since a
// codex thread does not take the place of a claude conversation. A move
// between tmux and the stream is the claude session's own and goes on.
func TestACodexProjectStartsCodexAndIsNotRestartedPG(t *testing.T) {
	srv, fake, dir := switchServer(t, "stream", "")
	list, err := srv.db.Profiles(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatalf("the map: %v, %v", list, err)
	}
	if _, err := srv.db.UpdateProfile(t.Context(), list[0].ID, store.ProfileEdit{LaunchSet: map[string]any{
		"agent": "codex", "codexModel": "gpt-5.5",
	}}); err != nil {
		t.Fatal(err)
	}
	project := list[0].Groups[0].Projects[0].ID
	open := `{"kind":"session.open","target":"aacpanel","params":{"project":` + strconv.Itoa(project) + `}}`

	for name, body := range map[string]string{
		"a restart": `{"kind":"session.restart","params":{"conversation":"` + switchSID + `"}}`,
		"a restart going on": `{"kind":"session.restart","params":{"conversation":"` + switchSID +
			`","resume":true}}`,
	} {
		w := post(t, srv, body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "does not take the place of a claude") {
			t.Errorf("%s of the claude session of a codex project: %d %s", name, w.Code, w.Body.String())
		}
	}
	select {
	case got := <-fake.got:
		t.Fatalf("a refused restart reached the executor: %+v", got)
	default:
	}

	w := post(t, srv, `{"kind":"session.switch","target":"aacpanel-2","params":{"to":"stream"}}`)
	if w.Code != http.StatusOK {
		t.Errorf("a move of the claude session of a codex project: %d %s", w.Code, w.Body.String())
	}
	<-fake.got

	client, opened := startFakeExec(t, action.Response{OK: true, Detail: "started", Session: "codex-0000beef"})
	srv.exec = client
	w = post(t, srv, open)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"session":"codex-0000beef"`) {
		t.Fatalf("New of a codex project: %d %s", w.Code, w.Body.String())
	}
	if got := <-opened.got; got.Project == nil || !strings.Contains(string(got.Project.Launch), `"agent":"codex"`) {
		t.Errorf("New of a codex project reached the executor as %+v", got)
	}

	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"sessions":[{"session":"codex-0000abcd","sessionId":`+
		`"019a1f00-0000-7000-8000-00000000abcd","cwd":"`+dir+`","transport":"stream","agent":"codex"}]}`))
	w = post(t, srv, `{"kind":"session.restart","target":"codex-0000abcd"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), action.CodexNotRestarted) {
		t.Errorf("a restart of a codex thread: %d %s", w.Code, w.Body.String())
	}
	select {
	case got := <-opened.got:
		t.Fatalf("a refused restart reached the executor: %+v", got)
	default:
	}
}
