package main

import (
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/store"
)

func TestSessionOpenCarriesContourConfigDirToExecutorPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	id := fillMap(t, srv, root)
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "session aacpanel started"})
	srv.exec, srv.auth = client, &auth.Service{}

	list, err := srv.db.Profiles(t.Context())
	if err != nil || len(list) == 0 {
		t.Fatalf("the map is empty: %v", err)
	}
	conf := filepath.Join(root, "home", ".claude-contours", "work")
	if _, err := srv.db.UpdateProfile(t.Context(), list[0].ID, store.ProfileEdit{ConfigDir: &conf}); err != nil {
		t.Fatalf("the directory was not written to the contour: %v", err)
	}

	w := post(t, srv, `{"kind":"session.open","target":"aacpanel","params":{"project":`+strconv.Itoa(id)+`}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	got := <-fake.got
	if got.Project == nil {
		t.Fatal("a name without a project went to the executor")
	}
	if got.Project.ConfigDir != conf {
		t.Errorf("%q went to the executor instead of %q — the session will come up in the wrong account, and silently",
			got.Project.ConfigDir, conf)
	}
}
