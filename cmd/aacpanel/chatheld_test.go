package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aacpanel/internal/chat"
)

// The observers and the alarms a session asked the panel for come down with
// the state of its conversation, every field the collector reads of them: a
// field the service did not declare would be dropped on the way, and a row
// would lose when it last woke the session or how its command ended.
func TestTheObserversAndAlarmsOfASessionComeWithItsState(t *testing.T) {
	reply := okReply()
	reply["state"] = map[string]any{
		"tasks": []any{}, "agents": []any{},
		"observers": []any{
			map[string]any{"id": "0123456789abcdef", "name": "ci", "command": "gh run watch 42", "dir": "/opt/x",
				"wake": "match", "pattern": "fail", "at": "2026-10-11T20:00:00+03:00", "woken": 2,
				"lastWake": "2026-10-11T20:30:00+03:00", "done": true, "doneAt": "2026-10-11T20:31:00+03:00",
				"ending": map[string]any{"code": 0}},
		},
		"alarms": []any{
			map[string]any{"id": "fedcba9876543210", "name": "standup", "note": "post the summary",
				"at": "2026-10-11T19:00:00+03:00", "next": "2026-10-11T21:00:00+03:00", "every": 3600, "rang": 1,
				"lastRing": "2026-10-11T20:00:00+03:00"},
		},
	}
	agent := startAgent(t, reply)
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChat(w, httptest.NewRequest(http.MethodGet, "/api/chat?session=sentinel", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		State struct {
			Observers []map[string]any `json:"observers"`
			Alarms    []map[string]any `json:"alarms"`
		} `json:"state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.State.Observers) != 1 || len(got.State.Alarms) != 1 {
		t.Fatalf("the state carries %+v", got.State)
	}
	o := got.State.Observers[0]
	for _, key := range []string{"id", "name", "command", "dir", "wake", "pattern", "at", "woken", "lastWake",
		"done", "doneAt"} {
		if _, ok := o[key]; !ok {
			t.Errorf("the observer lost %q on the way: %v", key, o)
		}
	}
	if ending, _ := o["ending"].(map[string]any); ending == nil || ending["code"] != float64(0) {
		t.Errorf("a command that exited with 0 reads %v: the code 0 is a code, not an absent one", o["ending"])
	}
	a := got.State.Alarms[0]
	for _, key := range []string{"id", "name", "note", "at", "next", "every", "rang", "lastRing"} {
		if _, ok := a[key]; !ok {
			t.Errorf("the alarm lost %q on the way: %v", key, a)
		}
	}
}

// The log of an observer is asked of the collector by the conversation the
// session runs and the id of the observer, and comes back as the output of a
// background command does.
func TestTheLogOfAnObserverIsAskedForByItsConversation(t *testing.T) {
	agent := startAgent(t, map[string]any{"ok": true, "text": "run 42 failed\n", "cut": true, "size": 40000})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatObserver(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/observer?session=sentinel&observer=0123456789abcdef", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	req := <-agent.got
	if req.Session != "567f4d24-cd5f-48fa-bdc1-04c89d203494" || req.Observer == nil ||
		req.Observer.ID != "0123456789abcdef" {
		t.Errorf("the collector was asked %+v, observer %+v", req, req.Observer)
	}
	var got struct {
		Text string `json:"text"`
		Cut  bool   `json:"cut"`
		Size int    `json:"size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != "run 42 failed\n" || !got.Cut || got.Size != 40000 {
		t.Errorf("the log came back as %+v", got)
	}

	w = httptest.NewRecorder()
	srv.apiChatObserver(w, httptest.NewRequest(http.MethodGet, "/api/chat/observer?session=sentinel", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("a reading that names no observer answered %d", w.Code)
	}
}
