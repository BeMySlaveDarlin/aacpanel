package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/host"
)

func TestBackgroundAdoptionCarriesMappedLaunchPG(t *testing.T) {
	srv, fake, dir := switchServer(t, "tmux", "bg")
	w := post(t, srv, `{"kind":"session.bgadopt","target":"aacpanel-2"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	got := <-fake.got
	if got.Kind != action.SessionBGAdopt || got.Project == nil || got.Project.Path != dir || got.Project.Session != "aacpanel-2" {
		t.Fatalf("background adoption lost its live name or mapped project: %+v", got)
	}
	var launch map[string]any
	if err := json.Unmarshal(got.Project.Launch, &launch); err != nil {
		t.Fatal(err)
	}
	if launch["model"] != "opus" || launch["effort"] != "high" {
		t.Fatalf("mapped launch lost: %s", got.Project.Launch)
	}
}

func TestBackgroundAttachmentReturnsThePanelTerminalPG(t *testing.T) {
	srv, fake, _ := switchServer(t, "tmux", "bg")
	w := post(t, srv, `{"kind":"session.bgattach","target":"aacpanel-2"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	got := <-fake.got
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !action.TermID(got.Terminal) || got.Terminal != out.ID {
		t.Fatalf("terminal reply %+v differs from request %+v", out, got)
	}
	denied := httptest.NewRecorder()
	srv.runAction(denied, httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(`{"kind":"session.bgattach","target":"aacpanel-2"}`)), false)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("attachment is reachable without the terminal listener: %d", denied.Code)
	}
}

func TestBackgroundSessionsRefuseInputBeforePreparation(t *testing.T) {
	srv := &Server{host: host.NewReader(snapshotWith(t, `{"sessions":[{"session":"lead","sessionId":"`+switchSID+`","cwd":"/srv/proj","transport":"bg"}]}`))}
	for _, kind := range []string{"session.send", "session.answer", "session.permit", "session.restart", "session.finalize", "session.close", "window.open"} {
		w := post(t, srv, `{"kind":"`+kind+`","target":"lead","params":{"prepare":true}}`)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "background sessions") {
			t.Errorf("%s: status %d: %s", kind, w.Code, w.Body)
		}
	}
}

func TestBackgroundAdoptionRequiresAMappedProject(t *testing.T) {
	srv := &Server{host: host.NewReader(snapshotWith(t, `{"sessions":[{"session":"lead","sessionId":"`+switchSID+`","cwd":"/srv/proj","transport":"bg"}]}`))}
	srv.exec, _ = startFakeExec(t, action.Response{OK: true})
	w := post(t, srv, `{"kind":"session.bgadopt","target":"lead"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "project from the map") {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

func TestBackgroundSessionsNeverEnterAutomaticRestart(t *testing.T) {
	srv := &Server{}
	row := restartSession{LiveSession: host.LiveSession{Name: "lead", SessionID: switchSID, Transport: "bg"}}
	if _, ok, err := srv.restartGuard(t.Context(), row, nil, nil); err != nil || ok {
		t.Fatalf("background session entered auto restart: %v, %v", ok, err)
	}
}
