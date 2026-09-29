package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/store"
)

func TestTermStreamAttachesToATerminalByItsID(t *testing.T) {
	screen, feed := io.Pipe()
	t.Cleanup(func() { feed.Close() })
	opener := &screenOpener{screen: &fakeScreen{out: screen}}
	srv := termStand(t, opener)
	web := httptest.NewServer(srv.routes(srv.localGate()))
	t.Cleanup(func() {
		web.CloseClientConnections()
		web.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		web.URL+"/api/term/stream?term=t-1a2b3c4d&cols=90&rows=30", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("the stream did not open: %v", err)
	}
	defer resp.Body.Close()
	if ev := readEvent(t, bufio.NewReader(resp.Body)); ev.event != "ready" {
		t.Fatalf("the first event was %q: %s", ev.event, ev.data)
	}

	if got := opener.askedTerm(); got != "t-1a2b3c4d" {
		t.Errorf("the executor was asked for terminal %q", got)
	}
	if target, cols, rows := opener.asked(); target != "" || cols != 90 || rows != 30 {
		t.Errorf("session %q %dx%d was asked for — the id went as the name of a session", target, cols, rows)
	}
}

func TestTermStreamNamesOneThingToAttachTo(t *testing.T) {
	opener := &screenOpener{}
	srv := termStand(t, opener)
	mux := srv.routes(srv.localGate())

	for _, q := range []string{
		"name=shop&term=t-1a2b3c4d&cols=80&rows=24",
		"term=t-1A2B3C4D&cols=80&rows=24",
		"term=t-1a2b3c4&cols=80&rows=24",
		"term=../t-1a2b3c4d&cols=80&rows=24",
		"cols=80&rows=24",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/term/stream?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q answered %d, expected 400", q, rec.Code)
		}
	}
	if target, _, _ := opener.asked(); target != "" || opener.askedTerm() != "" {
		t.Errorf("the executor was asked for %q / %q on a request that names nothing right", target, opener.askedTerm())
	}
}

func TestTermsAreListedWithLabelsByPlaceAndAge(t *testing.T) {
	client, exec := startFakeExec(t, action.Response{OK: true, Terms: []action.Term{
		{ID: "t-0000beef", Place: "/srv/proj/shop", Name: "logs", Command: "tail",
			Activity: 1790700500, Created: 1790695000},
		{ID: "t-1a2b3c4d", Place: "/srv/proj/shop", Name: "make check", Command: "make",
			Activity: 1790700000, Created: 1790690000, Clients: 1},
		{ID: "t-00c0ffee", Place: "/home/u", Name: "bash", Command: "bash",
			Activity: 1790680500, Created: 1790680000},
	}})
	srv := &Server{exec: client, home: "/home/u"}

	rec := httptest.NewRecorder()
	srv.routes(srv.localGate()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/terms", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	want := `{"terms":[` +
		`{"id":"t-00c0ffee","place":"/home/u","label":"Home","name":"bash","command":"bash","activity":1790680500,"created":1790680000,"clients":0},` +
		`{"id":"t-1a2b3c4d","place":"/srv/proj/shop","label":"shop","name":"make check","command":"make","activity":1790700000,"created":1790690000,"clients":1},` +
		`{"id":"t-0000beef","place":"/srv/proj/shop","label":"shop","name":"logs","command":"tail","activity":1790700500,"created":1790695000,"clients":0}` +
		`]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("the list is\n%s\ninstead of\n%s", got, want)
	}
	if got := <-exec.got; got.Ask != action.AskTerms {
		t.Errorf("the executor was asked %+v", got)
	}
}

func TestTermsWithNoneAreAnEmptyList(t *testing.T) {
	client, _ := startFakeExec(t, action.Response{OK: true})
	srv := &Server{exec: client, home: "/home/u"}

	rec := httptest.NewRecorder()
	srv.routes(srv.localGate()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/terms", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != `{"terms":[]}` {
		t.Errorf("no terminals are %s — the screen reads a list", got)
	}
}

func TestTermPlacesAreTheHomeAndTheProjectsOfTheMap(t *testing.T) {
	list := []store.Profile{{Groups: []store.ProfileGroup{{Projects: []store.ProfileProject{
		{Name: "Shop front", Path: "/srv/proj/shop/"},
		{Name: "Home base", Path: "/home/u"},
		{Name: "Shop again", Path: "/srv/proj/shop"},
	}}}}}
	places := placesFrom("/home/u", list)
	for place, want := range map[string]string{
		"/srv/proj/shop": "Shop front",
		"/home/u":        homeLabel,
	} {
		if got := places[place]; got != want {
			t.Errorf("%s is called %q, expected %q", place, got, want)
		}
	}
	if len(places) != 2 {
		t.Errorf("the places are %v", places)
	}
	if got := placesFrom("/home/u", nil)["/home/u"]; got != homeLabel {
		t.Errorf("the home directory is called %q", got)
	}
	if got := placeLabel(places, "/srv/proj/blog"); got != "blog" {
		t.Errorf("a place off the map is called %q, not by its directory", got)
	}
}

func TestTermStartOpensOnlyInAPlaceFromTheList(t *testing.T) {
	client, exec := startFakeExec(t, action.Response{OK: true, Detail: "terminal started"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client, home: "/home/u"}

	seen := map[string]bool{}
	for _, body := range []string{
		`{"kind":"term.start","target":"/home/u","params":{"place":"/home/u"}}`,
		`{"kind":"term.start","target":"/home/u/"}`,
		`{"kind":"term.start","params":{"place":"/home/u"}}`,
	} {
		w := post(t, srv, body)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, %s", body, w.Code, w.Body.String())
		}
		var out struct {
			OK bool   `json:"ok"`
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || !out.OK || !action.TermID(out.ID) {
			t.Fatalf("%s: the answer %s carries no id of a terminal", body, w.Body.String())
		}
		if seen[out.ID] {
			t.Errorf("id %s was given twice", out.ID)
		}
		seen[out.ID] = true
		got := <-exec.got
		if got.Kind != action.TermStart || got.Target != out.ID || got.Place != "/home/u" {
			t.Errorf("%s went to the executor as %+v", body, got)
		}
	}

	for name, body := range map[string]string{
		"a directory off the list":       `{"kind":"term.start","target":"/etc","params":{"place":"/etc"}}`,
		"a climb out of the home":        `{"kind":"term.start","params":{"place":"/home/u/../../etc"}}`,
		"a directory inside the home":    `{"kind":"term.start","params":{"place":"/home/u/code"}}`,
		"a relative path":                `{"kind":"term.start","params":{"place":"home/u"}}`,
		"no place at all":                `{"kind":"term.start"}`,
		"a target naming another place":  `{"kind":"term.start","target":"/srv/proj/shop","params":{"place":"/home/u"}}`,
		"an id the phone made up itself": `{"kind":"term.start","target":"t-1a2b3c4d","params":{"place":"/home/u"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if w := post(t, srv, body); w.Code != http.StatusBadRequest {
				t.Errorf("status %d, expected 400: %s", w.Code, w.Body.String())
			}
		})
	}
	select {
	case got := <-exec.got:
		t.Errorf("a refused start reached the executor: %+v", got)
	default:
	}
}

func TestTermActionsCarryTheIDAndTheName(t *testing.T) {
	client, exec := startFakeExec(t, action.Response{OK: true, Detail: "done"})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	for _, c := range []struct {
		body string
		kind action.Kind
		name string
	}{
		{`{"kind":"term.close","target":"t-1a2b3c4d","params":{"id":"t-1a2b3c4d"}}`, action.TermClose, ""},
		{`{"kind":"term.console","target":"t-1a2b3c4d"}`, action.TermConsole, ""},
		{`{"kind":"term.rename","target":"t-1a2b3c4d","params":{"id":"t-1a2b3c4d","name":"make check"}}`, action.TermRename, "make check"},
	} {
		if w := post(t, srv, c.body); w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, %s", c.body, w.Code, w.Body.String())
		}
		got := <-exec.got
		if got.Kind != c.kind || got.Target != "t-1a2b3c4d" || got.Rename != c.name || got.Place != "" {
			t.Errorf("%s went to the executor as %+v", c.body, got)
		}
	}
}

func TestTermIDAndNameAreCheckedBeforeTheExecutor(t *testing.T) {
	client, exec := startFakeExec(t, action.Response{OK: true})
	srv := &Server{hostName: "STAND-01", auth: &auth.Service{}, exec: client}

	var bodies []string
	for _, kind := range []string{"term.close", "term.console", "term.rename"} {
		for _, id := range []string{"t-1A2B3C4D", "t-1a2b3c4", "t-1a2b3c4d0", "../t-1a2b3c4d", "=t-1a2b3c4d", "shop", ""} {
			bodies = append(bodies, `{"kind":"`+kind+`","target":"`+id+`","params":{"name":"build"}}`)
		}
		bodies = append(bodies, `{"kind":"`+kind+`","target":"t-1a2b3c4d","params":{"id":"t-0000beef","name":"build"}}`)
	}
	for _, name := range []string{"", "   ", strings.Repeat("x", action.TermNameMax+1), "red \u001b[31m", "two\nlines"} {
		raw, _ := json.Marshal(name)
		bodies = append(bodies, `{"kind":"term.rename","target":"t-1a2b3c4d","params":{"name":`+string(raw)+`}}`)
	}
	for _, body := range bodies {
		if w := post(t, srv, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, expected 400", body, w.Code)
		}
	}
	if w := post(t, srv, `{"kind":"term.rename","target":"t-1a2b3c4d","params":{"name":"`+
		strings.Repeat("x", action.TermNameMax)+`"}}`); w.Code != http.StatusOK {
		t.Errorf("a name of %d characters was refused: %d %s", action.TermNameMax, w.Code, w.Body.String())
	}
	<-exec.got
	select {
	case got := <-exec.got:
		t.Errorf("a refused action reached the executor: %+v", got)
	default:
	}
}

func TestTermActionsExistOnlyWhereTheTerminalDoes(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: 24 * time.Hour})
	client, exec := startFakeExec(t, action.Response{OK: true})
	s.exec, s.home = client, "/home/u"

	send := func(mux http.Handler, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/actions", strings.NewReader(body))
		for _, c := range withSession(t, s, http.MethodGet, "/api/actions").Cookies() {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec.Code
	}
	bodies := []string{
		`{"kind":"term.start","params":{"place":"/home/u"}}`,
		`{"kind":"term.close","target":"t-1a2b3c4d"}`,
		`{"kind":"term.rename","target":"t-1a2b3c4d","params":{"name":"build"}}`,
		`{"kind":"term.console","target":"t-1a2b3c4d"}`,
	}

	for _, gate := range []struct {
		name string
		mux  http.Handler
	}{
		{"main", s.routes(s.publicGate())},
		{"local network", s.routes(s.lanGate())},
		{"tailscale", s.routes(s.tsGate())},
	} {
		for _, body := range bodies {
			if code := send(gate.mux, body); code != http.StatusNotFound {
				t.Errorf("%s without the terminal: %s answered %d, expected 404", gate.name, body, code)
			}
		}
	}
	select {
	case got := <-exec.got:
		t.Fatalf("an action on a terminal reached the executor from a listener without one: %+v", got)
	default:
	}
	if code := send(s.routes(s.publicGate()), `{"kind":"container.stop","target":"app"}`); code != http.StatusOK {
		t.Errorf("the other actions went away with the terminal: %d", code)
	}
	<-exec.got

	s.termPublic = true
	for _, body := range bodies {
		if code := send(s.routes(s.publicGate()), body); code != http.StatusOK {
			t.Errorf("with the terminal switched on %s answered %d", body, code)
		}
		<-exec.got
	}
	if code := send(s.routes(s.localGate()), bodies[1]); code != http.StatusOK {
		t.Errorf("the local panel refused %s: %d", bodies[1], code)
	}
}

func TestTermPlacesComeFromTheMapPG(t *testing.T) {
	srv, root := profilesServer(t)
	dir := filepath.Join(root, "aacpanel")
	name, group, project := "personal", "Services", "Shop front"
	profile, err := srv.db.CreateProfile(t.Context(), store.ProfileEdit{Name: &name, ConfigDir: &root})
	if err != nil {
		t.Fatal(err)
	}
	g, err := srv.db.CreateGroup(t.Context(), profile.ID, store.GroupEdit{Name: &group})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.CreateProject(t.Context(), g.ID, store.ProjectEdit{Name: &project, Path: &dir}); err != nil {
		t.Fatal(err)
	}
	client, exec := startFakeExec(t, action.Response{OK: true, Terms: []action.Term{
		{ID: "t-1a2b3c4d", Place: dir, Name: "bash", Command: "bash"},
	}})
	srv.exec, srv.auth, srv.home = client, &auth.Service{}, "/home/u"

	if w := post(t, srv, `{"kind":"term.start","params":{"place":"`+dir+`"}}`); w.Code != http.StatusOK {
		t.Fatalf("a project of the map was refused: %d %s", w.Code, w.Body.String())
	}
	if got := <-exec.got; got.Place != dir {
		t.Errorf("the terminal went to %q instead of the project %s", got.Place, dir)
	}
	if w := post(t, srv, `{"kind":"term.start","params":{"place":"`+root+`"}}`); w.Code != http.StatusBadRequest {
		t.Errorf("the root of the projects, which is no project, answered %d", w.Code)
	}

	rec := httptest.NewRecorder()
	srv.routes(srv.localGate()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/terms", nil))
	if !strings.Contains(rec.Body.String(), `"label":"Shop front"`) {
		t.Errorf("the terminal of a project is not called by the project: %s", rec.Body.String())
	}
}
