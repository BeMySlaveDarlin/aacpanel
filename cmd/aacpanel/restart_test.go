package main

import (
	"encoding/json"
	"net/http"
	"testing"

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

	w = post(t, srv, `{"kind":"session.restart","params":{"conversation":"99999999-9999-4999-8999-999999999999"}}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a conversation no session runs gave %d", w.Code)
	}
}

// Where nobody names a message after a restart, the panel's own goes.
func TestRestartLaunchSaysThePanelsDefault(t *testing.T) {
	raw, err := restartLaunch(json.RawMessage(`{"intent":"read the queue","model":"opus"}`))
	if err != nil {
		t.Fatal(err)
	}
	var launch map[string]any
	json.Unmarshal(raw, &launch)
	if launch["intent"] != "Continue" || launch["model"] != "opus" {
		t.Errorf("the restart launch is %v", launch)
	}
	raw, _ = restartLaunch(json.RawMessage(`{"intent":"read the queue","restartIntent":""}`))
	json.Unmarshal(raw, &launch)
	if launch["intent"] != "" {
		t.Errorf("an explicit empty message after a restart became %q", launch["intent"])
	}
}
