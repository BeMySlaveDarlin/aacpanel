package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/host"
	"aacpanel/internal/store"
)

func TestDiskReportSubtractsMap(t *testing.T) {
	disk := host.Disk{State: host.DiskOK, At: 7, Roots: []string{"/srv/proj"}, Depth: 4, Dirs: []host.DiskDir{
		{Path: "/srv/proj/Labs", Kind: host.DirFolder},
		{Path: "/srv/proj/Labs/shop", Kind: host.DirProject, Git: true},
		{Path: "/srv/proj/Labs/auth", Kind: host.DirProject, Git: true},
		{Path: "/srv/proj/Link", Kind: host.DirLink},
	}}
	list := mapWith(map[int]string{1: "/srv/proj/Labs/shop/"})

	got := diskReport(disk, list)
	if got.State != host.DiskOK || got.At != 7 {
		t.Fatalf("the state or the time was lost: %+v", got)
	}
	if !reflect.DeepEqual(got.Roots, []string{"/srv/proj"}) {
		t.Errorf("the roots did not arrive: %v", got.Roots)
	}
	if len(got.Dirs) != 1 || got.Dirs[0].Path != "/srv/proj/Labs/auth" {
		t.Errorf("only auth should be left in the hint — folders and links are not projects, and shop is already on the map: %+v", got.Dirs)
	}
	if !got.Dirs[0].Git {
		t.Errorf("the project flags were lost on the way: %+v", got.Dirs[0])
	}
	if len(got.Missing) != 0 {
		t.Errorf("shop lies on the disk and is marked as gone: %v", got.Missing)
	}
}

func TestDiskReportMarksMissingOnlyWhereItLooked(t *testing.T) {
	disk := host.Disk{State: host.DiskOK, Roots: []string{"/srv/proj"}, Depth: 3, Dirs: []host.DiskDir{
		{Path: "/srv/proj/Labs", Kind: host.DirFolder},
		{Path: "/srv/proj/Labs/shop", Kind: host.DirProject},
		{Path: "/srv/proj/Deep", Kind: host.DirFolder},
		{Path: "/srv/proj/Deep/a", Kind: host.DirFolder},
		{Path: "/srv/proj/Deep/a/b", Kind: host.DirFolder},
		{Path: "/srv/proj/Link", Kind: host.DirLink},
	}}
	list := mapWith(map[int]string{
		1: "/srv/proj/Labs/shop",
		2: "/srv/proj/Labs/gone",
		3: "/srv/proj/Gone",
		4: "/srv/proj/Labs/shop/sub",
		5: "/srv/proj/Deep/a/b/c",
		6: "/srv/proj/Link/x",
		7: "/home/u",
		8: "/srv/proj/Nope/x",
	})

	got := diskReport(disk, list)
	if !reflect.DeepEqual(got.Missing, []int{2, 3}) {
		t.Errorf("exactly 2 and 3 should be gone, got %v", got.Missing)
	}
}

func TestDiskReportUnknownWithoutSnapshot(t *testing.T) {
	list := mapWith(map[int]string{1: "/srv/proj/Labs/shop"})
	got := diskReport(host.Disk{State: host.DiskUnknown}, list)
	if got.State != host.DiskUnknown || len(got.Dirs) != 0 || len(got.Missing) != 0 {
		t.Errorf("with an unknown disk the answer has to be an empty unknown: %+v", got)
	}
	srv := &Server{hostName: "STAND-01"}
	if got := srv.diskReport(t.Context(), list); got.State != host.DiskUnknown {
		t.Errorf("with no snapshot at all: %+v", got)
	}
}

func TestProfilesReplyCarriesDiskPG(t *testing.T) {
	srv, root := profilesServer(t)
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"projects":{"at":5,"roots":["`+root+`"],"depth":4,"dirs":[
		{"path":"`+filepath.Join(root, "aacpanel")+`","kind":"project","git":true},
		{"path":"`+other+`","kind":"project","claude":true}
	]}}`))
	mux := profilesMux(srv)
	profilePost(t, mux, "/api/profiles", `{"name":"personal","configDir":"`+root+`"}`)
	body := profileGet(t, mux, "/api/profiles")
	profiles := body["profiles"].([]any)
	profileID := int(profiles[0].(map[string]any)["id"].(float64))
	group := profilePost(t, mux, "/api/profiles/"+strconv.Itoa(profileID)+"/groups", `{"name":"Services"}`)["group"].(map[string]any)
	groupID := int(group["id"].(float64))
	profilePost(t, mux, "/api/groups/"+strconv.Itoa(groupID)+"/projects",
		`{"name":"aacpanel","path":"`+filepath.Join(root, "aacpanel")+`"}`)
	gone := profilePost(t, mux, "/api/groups/"+strconv.Itoa(groupID)+"/projects",
		`{"name":"gone","path":"`+filepath.Join(root, "gone")+`"}`)

	disk, ok := gone["disk"].(map[string]any)
	if !ok {
		t.Fatalf("the response to the change has no disk field: %v", gone)
	}
	if disk["state"] != "ok" {
		t.Fatalf("the disk was not read although the snapshot is alive: %v", disk)
	}
	dirs, _ := disk["dirs"].([]any)
	if len(dirs) != 1 || dirs[0].(map[string]any)["path"] != other {
		t.Errorf("only other should be left beyond the map: %v", dirs)
	}
	if dirs[0].(map[string]any)["claude"] != true {
		t.Errorf("the claude flag was lost on the way: %v", dirs[0])
	}
	goneID := gone["project"].(map[string]any)["id"].(float64)
	if missing, _ := disk["missing"].([]any); len(missing) != 1 || missing[0] != goneID {
		t.Errorf("exactly gone (%v) should be missing: %v", goneID, missing)
	}

	srv.host = nil
	if state := profileGet(t, mux, "/api/profiles")["disk"].(map[string]any)["state"]; state != "unknown" {
		t.Errorf("without a snapshot the state is %v, expected unknown", state)
	}
}

func mapWith(paths map[int]string) []store.Profile {
	group := store.ProfileGroup{ID: 1, Name: "Services"}
	for id, path := range paths {
		group.Projects = append(group.Projects, store.ProfileProject{ID: id, GroupID: 1, Path: path})
	}
	return []store.Profile{{ID: 1, Name: "personal", Groups: []store.ProfileGroup{group}}}
}

func TestHiddenLeavesTheQueueAndIsNamed(t *testing.T) {
	hidden := map[string]bool{}
	want := []string{}
	for _, name := range []string{"f", "b", "e", "a", "d", "c"} {
		path := "/srv/proj/Vault/" + name
		hidden[path] = true
		want = append(want, path)
	}
	sort.Strings(want)

	out := diskReply{State: host.DiskOK, Dirs: []host.DiskDir{
		{Path: "/srv/proj/Vault/a"},
		{Path: "/srv/proj/Demo/sample"},
		{Path: "/srv/proj/Vault/b"},
	}}

	same := withHidden(out, nil)
	if len(same.Dirs) != 3 || len(same.Hidden) != 0 {
		t.Errorf("with nothing hidden the queue has to stay as it was: %+v", same)
	}

	for i := 0; i < 5; i++ {
		got := withHidden(out, hidden)
		if len(got.Dirs) != 1 || got.Dirs[0].Path != "/srv/proj/Demo/sample" {
			t.Fatalf("the hidden entries stayed in the queue: %+v", got.Dirs)
		}
		if !reflect.DeepEqual(got.Hidden, want) {
			t.Fatalf("run %d: the hidden list is %v, expected %v in alphabetical order — the answer "+
				"has to be the same from request to request", i, got.Hidden, want)
		}
	}

	for i, path := range []string{"/srv/proj/Vault/a", "/srv/proj/Demo/sample", "/srv/proj/Vault/b"} {
		if out.Dirs[i].Path != path {
			t.Errorf("withHidden rewrote the original report: position %d now holds %s, and it was %s",
				i, out.Dirs[i].Path, path)
		}
	}
}

func diskPaths(t *testing.T, body map[string]any) []string {
	t.Helper()
	disk, ok := body["disk"].(map[string]any)
	if !ok {
		t.Fatalf("the response has no disk field: %v", body)
	}
	out := []string{}
	for _, d := range disk["dirs"].([]any) {
		out = append(out, d.(map[string]any)["path"].(string))
	}
	sort.Strings(out)
	return out
}

func hiddenPaths(t *testing.T, body map[string]any) []string {
	t.Helper()
	disk, _ := body["disk"].(map[string]any)
	out := []string{}
	list, _ := disk["hidden"].([]any)
	for _, p := range list {
		out = append(out, p.(string))
	}
	return out
}

func TestHiddenDirRoundTripPG(t *testing.T) {
	srv, root := profilesServer(t)
	junk := filepath.Join(root, "Storage")
	keep := filepath.Join(root, "other")
	for _, dir := range []string{junk, keep} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	srv.host = host.NewReader(snapshotWith(t, `{"at":1,"projects":{"at":5,"roots":["`+root+`"],"depth":4,"dirs":[
		{"path":"`+junk+`","kind":"project","git":true},
		{"path":"`+keep+`","kind":"project","claude":true}
	]}}`))
	mux := profilesMux(srv)
	pool, err := srv.db.Pool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM disk_hidden"); err != nil {
		t.Fatal(err)
	}

	if got := diskPaths(t, profileGet(t, mux, "/api/profiles")); len(got) != 2 {
		t.Fatalf("the queue holds %v, expected both directories", got)
	}

	body := profileCall(t, mux, http.MethodPost, "/api/disk/hidden", `{"path":"`+junk+`"}`)
	if got := diskPaths(t, body); len(got) != 1 || got[0] != keep {
		t.Errorf("after hiding, the queue holds %v, expected only %s", got, keep)
	}
	if got := hiddenPaths(t, body); len(got) != 1 || got[0] != junk {
		t.Errorf("what was hidden is not named in the response: %v — there will be nothing to bring it back with", got)
	}

	body = profileCall(t, mux, http.MethodDelete, "/api/disk/hidden", `{"path":"`+junk+`"}`)
	if got := diskPaths(t, body); len(got) != 2 {
		t.Errorf("after the return, the queue holds %v, expected both directories", got)
	}
	if got := hiddenPaths(t, body); len(got) != 0 {
		t.Errorf("what was returned stayed in the hidden list: %v", got)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/disk/hidden", strings.NewReader(`{"path":"/etc"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a path outside the roots was hidden with %d, expected 400", w.Code)
	}
}
