package install

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeMap is the map of a panel behind its local listener: contours with
// their groups and projects, the names and paths it refuses to take twice,
// and every request that changed it.
type fakeMap struct {
	t        *testing.T
	mu       sync.Mutex
	contours []fakeContour
	next     int
	changes  []string
}

type fakeContour struct {
	mapContour
	Launch json.RawMessage `json:"launch"`
}

func (f *fakeMap) id() int { f.next++; return f.next }

func (f *fakeMap) view() map[string]any { return map[string]any{"profiles": f.contours} }

func (f *fakeMap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The local listener takes only a request of this machine.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "an Origin on the local listener", http.StatusForbidden)
		return
	}
	if host, _, _ := net.SplitHostPort(r.Host); host != "127.0.0.1" {
		http.Error(w, "not a loopback Host", http.StatusForbidden)
		return
	}
	reply := f.view()
	var body map[string]any
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.changes = append(f.changes, r.Method+" "+r.URL.Path)
	}
	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/profiles":
	case r.Method == http.MethodPost && r.URL.Path == "/api/profiles":
		name, _ := body["name"].(string)
		for _, c := range f.contours {
			if c.Name == name {
				http.Error(w, "the profile name "+name+" is taken", http.StatusConflict)
				return
			}
		}
		launch, _ := json.Marshal(body["launch"])
		c := fakeContour{mapContour: mapContour{ID: f.id(), Name: name, ConfigDir: body["configDir"].(string), Groups: []mapGroup{}}, Launch: launch}
		f.contours = append(f.contours, c)
		reply = f.view()
		reply["profile"] = c
	case r.Method == http.MethodPost && len(path) == 4 && path[1] == "profiles" && path[3] == "groups":
		id, _ := strconv.Atoi(path[2])
		for i := range f.contours {
			if f.contours[i].ID == id {
				g := mapGroup{ID: f.id(), Name: body["name"].(string), Projects: []mapProject{}}
				f.contours[i].Groups = append(f.contours[i].Groups, g)
				reply = f.view()
				reply["group"] = g
			}
		}
	case r.Method == http.MethodPost && len(path) == 4 && path[1] == "groups" && path[3] == "projects":
		id, _ := strconv.Atoi(path[2])
		for i := range f.contours {
			for j := range f.contours[i].Groups {
				if f.contours[i].Groups[j].ID == id {
					g := &f.contours[i].Groups[j]
					g.Projects = append(g.Projects, mapProject{ID: f.id(), Path: body["path"].(string)})
				}
			}
		}
		reply = f.view()
	default:
		http.Error(w, "no route "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

// withMap gives the rig a panel whose local listener holds the map.
func (g *rig) withMap(contours ...fakeContour) *fakeMap {
	f := &fakeMap{t: g.t, contours: contours, next: 100}
	srv := httptest.NewServer(f)
	g.t.Cleanup(srv.Close)
	g.in.Local = srv.URL
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\nAACP_DB_PASSWORD=p\n", 0o600)
	return f
}

func TestTheMapIsMadeOnceThroughTheLocalListener(t *testing.T) {
	g := newRig(t, answer("--account", "~/.claude,~/.claude-work"), answer("--transport", "tmux"),
		answer("--project", "~/code/shop,~/code/landing"))
	g.in.S.run.Answers["--contour "+filepath.Join(g.home, ".claude-work")] = "job"
	if open, err := g.in.S.Settle(BlockM); err != nil || len(open) > 0 {
		t.Fatal(err, open)
	}
	f := g.withMap()
	if !slices.ContainsFunc(g.in.Steps(), func(s *Step) bool { return s.ID == "map" }) {
		t.Fatal("the install has no step for the map")
	}
	step := g.step("map")
	if err := g.do(step); err != nil {
		t.Fatal(err)
	}
	if len(f.contours) != 2 {
		t.Fatalf("the map holds %+v", f.contours)
	}
	personal, job := f.contours[0], f.contours[1]
	if personal.Name != "personal" || personal.ConfigDir != filepath.Join(g.home, ".claude") || string(personal.Launch) != `{"transport":"tmux"}` ||
		job.Name != "job" || job.ConfigDir != filepath.Join(g.home, ".claude-work") {
		t.Errorf("the contours are %+v and %+v", personal, job)
	}
	if len(personal.Groups) != 1 || personal.Groups[0].Name != "Projects" || len(personal.Groups[0].Projects) != 2 ||
		personal.Groups[0].Projects[0].Path != filepath.Join(g.home, "code/shop") {
		t.Errorf("the group is %+v", personal.Groups)
	}
	want := []string{"profile contour personal created", "profile contour job created", "profile group Projects created",
		"profile project " + filepath.Join(g.home, "code/shop") + " created", "profile project " + filepath.Join(g.home, "code/landing") + " created"}
	if !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q, want %q", g.lines(), want)
	}
	// Again: the map holds it all, and nothing is made twice.
	made := len(f.changes)
	if !g.already(step) {
		t.Error("a map that holds the answers was made again")
	}
	if err := step.Apply(g.r); err != nil {
		t.Fatal(err)
	}
	if len(f.changes) != made || len(f.contours) != 2 || len(f.contours[0].Groups) != 1 || len(f.contours[0].Groups[0].Projects) != 2 {
		t.Errorf("a second pass changed the map: %q, %+v", f.changes[made:], f.contours)
	}
}

// TestAContourThereStandsForItsAccount: a contour of the account's
// directory, or one of its name without a directory, is the account's; a
// project anywhere on the map stays where it is.
func TestAContourThereStandsForItsAccount(t *testing.T) {
	g := newRig(t, answer("--project", "~/code/shop"))
	shop := filepath.Join(g.home, "code/shop")
	f := g.withMap(fakeContour{mapContour: mapContour{ID: 1, Name: "personal", Groups: []mapGroup{
		{ID: 2, Name: "Old", Projects: []mapProject{{ID: 3, Path: shop}}}}}})
	if err := g.do(g.step("map")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"POST /api/profiles/1/groups"}; !slices.Equal(f.changes, want) {
		t.Errorf("the map was changed by %q, want %q", f.changes, want)
	}
	if want := []string{"profile group Projects created"}; !slices.Equal(g.lines(), want) {
		t.Errorf("the manifest holds %q", g.lines())
	}
}

func TestANameTakenByAnotherAccountStops(t *testing.T) {
	g := newRig(t)
	g.withMap(fakeContour{mapContour: mapContour{ID: 1, Name: "personal", ConfigDir: "/srv/other/.claude"}})
	f := failedWith(t, g.do(g.step("map")), "the contour personal of ~/.claude was not made: the panel answered 409")
	if len(f.Fix) == 0 || !strings.Contains(f.Fix[0], "--contour ~/.claude=<name>") {
		t.Errorf("the fix is %q", f.Fix)
	}
}

func TestWithTheLocalListenerOffTheMapIsLeftToThePerson(t *testing.T) {
	g := newRig(t)
	f := g.withMap()
	g.write(filepath.Join(g.clone, ".env"), "AACP_SECRET=s\nAACP_DB_PASSWORD=p\nAACP_LOCAL_ADDR=\n", 0o600)
	if err := g.do(g.step("map")); err != nil {
		t.Fatal(err)
	}
	if len(f.changes) != 0 || len(g.r.Reminders()) != 1 || !strings.Contains(g.r.Reminders()[0], "Profiles screen") {
		t.Errorf("changes %q, reminders %q", f.changes, g.r.Reminders())
	}
}

func TestAKeptMapHasNoStep(t *testing.T) {
	g := newRig(t)
	g.in.S.Keep(Given{Value: "yes"})
	g.in.S.Clear(BlockM)
	for _, s := range g.in.Steps() {
		if s.ID == "map" {
			t.Errorf("a run that kept the settings makes the map: %v", fmt.Sprint(s.Title))
		}
	}
}
