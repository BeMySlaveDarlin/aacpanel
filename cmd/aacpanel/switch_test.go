package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

const switchSID = "88888888-8888-4888-8888-888888888888"

// switchServer is a map with one project that lives where it is told, and a
// live session of it under a name of its own.
func switchServer(t *testing.T, projectTransport, liveTransport string) (*Server, *fakeExec, string) {
	t.Helper()
	srv, root := profilesServer(t)
	dir := filepath.Join(root, "aacpanel")
	snapshot := `{"at":1,"sessions":[{"session":"aacpanel-2","sessionId":"` + switchSID + `","cwd":"` + dir +
		`","transport":"` + liveTransport + `"}]}`
	srv.host = host.NewReader(snapshotWith(t, snapshot))

	name, group, project := "personal", "Services", "aacpanel"
	profile, err := srv.db.CreateProfile(t.Context(), store.ProfileEdit{
		Name: &name, ConfigDir: &root, Launch: json.RawMessage(`{"model":"opus"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := srv.db.CreateGroup(t.Context(), profile.ID, store.GroupEdit{Name: &group})
	if err != nil {
		t.Fatal(err)
	}
	launch := json.RawMessage(`{"room":"work"}`)
	if projectTransport != "" {
		launch = json.RawMessage(`{"room":"work","transport":"` + projectTransport + `"}`)
	}
	if _, err := srv.db.CreateProject(t.Context(), g.ID, store.ProjectEdit{
		Name: &project, Path: &dir, Launch: launch,
	}); err != nil {
		t.Fatal(err)
	}
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "moved"})
	srv.exec, srv.auth = client, &auth.Service{}
	return srv, fake, dir
}

func switchWayOf(t *testing.T, srv *Server) (to, reason string) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.apiSessionSwitch(w, httptest.NewRequest(http.MethodGet, "/api/session/switch?name=aacpanel-2", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	var out struct {
		To     string `json:"to"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.To, out.Reason
}

func TestSessionSwitchWayPG(t *testing.T) {
	cases := []struct {
		name, project, live, to, reason string
	}{
		{"the stream goes to the console", "stream", "stream", "console", ""},
		{"the stream goes to the console even when its project was moved back", "", "stream", "console", ""},
		{"a console of a project that lives in the feed goes to the feed", "stream", "", "stream", ""},
		{"a console of a project that lives in the console stays", "", "", "", "lives in the console"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _, _ := switchServer(t, c.project, c.live)
			to, reason := switchWayOf(t, srv)
			if to != c.to || !strings.Contains(reason, c.reason) {
				t.Errorf("the way is %q (%q), expected %q (%q)", to, reason, c.to, c.reason)
			}
		})
	}
}

func TestSessionSwitchCarriesTheLiveNameAndProjectPG(t *testing.T) {
	srv, fake, dir := switchServer(t, "stream", "stream")

	w := post(t, srv, `{"kind":"session.switch","target":"aacpanel-2","params":{"to":"console","force":true,"window":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	if got.Switch == nil || got.Switch.To != action.SwitchConsole || !got.Switch.Force || !got.Switch.Window {
		t.Fatalf("where to move, the agreement and the window did not reach the executor: %+v", got.Switch)
	}
	if got.Project == nil || got.Project.Path != dir {
		t.Fatalf("the project did not reach the executor: %+v", got.Project)
	}
	if got.Project.Session != "aacpanel-2" {
		t.Errorf("the session would come back as %q, not under its own name", got.Project.Session)
	}
	var launch map[string]any
	if err := json.Unmarshal(got.Project.Launch, &launch); err != nil || launch["model"] != "opus" || launch["room"] != "work" {
		t.Errorf("the launch parameters of the map did not come along: %s", got.Project.Launch)
	}
}

func TestSessionSwitchRefusesTheWayThatIsNotOpenPG(t *testing.T) {
	cases := []struct {
		name, project, live, to, says string
	}{
		{"a console of a console project to the feed", "", "", "stream", "lives in the console"},
		{"the stream to the feed", "stream", "stream", "stream", "in the feed already"},
		{"a console to the console", "stream", "", "console", "in the console already"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, fake, _ := switchServer(t, c.project, c.live)
			w := post(t, srv, `{"kind":"session.switch","target":"aacpanel-2","params":{"to":"`+c.to+`"}}`)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.says) {
				t.Fatalf("status %d, body %q; expected 400 saying %q", w.Code, w.Body.String(), c.says)
			}
			select {
			case got := <-fake.got:
				t.Fatalf("the request went to the executor after all: %+v", got)
			default:
			}
		})
	}
}
