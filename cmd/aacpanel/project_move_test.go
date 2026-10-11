package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/contours"
	"aacpanel/internal/executor"
	"aacpanel/internal/historymove"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/store"
)

func TestProjectMoveCrossesTheActionSocketAndJournalsPG(t *testing.T) {
	for _, clash := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "clash"}[clash], func(t *testing.T) {
			srv, root := profilesServer(t)
			from, to := filepath.Join(root, "old"), filepath.Join(root, "new")
			for _, dir := range []string{from, to} {
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(contours.HomeEnv, from+":"+to)
			t.Setenv(contours.RegistryEnv, "")
			t.Setenv("AACP_SESSIONS", "")
			t.Setenv("AACP_PROC", t.TempDir())
			t.Setenv("XDG_RUNTIME_DIR", root)
			t.Setenv(hostcfg.ProjectRootsEnv, root)
			create := func(name, config string) (store.Profile, store.ProfileGroup) {
				p, err := srv.db.CreateProfile(t.Context(), store.ProfileEdit{Name: &name, ConfigDir: &config})
				if err != nil {
					t.Fatal(err)
				}
				g, err := srv.db.CreateGroup(t.Context(), p.ID, store.GroupEdit{Name: &name})
				if err != nil {
					t.Fatal(err)
				}
				return p, g
			}
			_, old := create("source", from)
			_, new := create("destination", to)
			name, path := "shop", filepath.Join(root, "aacpanel")
			p, err := srv.db.CreateProject(t.Context(), old.ID, store.ProjectEdit{Name: &name, Path: &path})
			if err != nil {
				t.Fatal(err)
			}
			rel := filepath.Join("projects", historymove.Slug(path), "12345678-1234-4321-8765-123456789abc.jsonl")
			write := func(config, body string) {
				t.Helper()
				file := filepath.Join(config, rel)
				if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(from, "conversation")
			if clash {
				write(to, "conflict")
			}
			socket := socketPath(t, "move.sock")
			server := action.NewServer(socket, executor.New(nil, ""), time.Minute)
			ln, err := server.Listen()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			ended := make(chan error, 1)
			go func() { ended <- server.Run(ctx, ln) }()
			t.Cleanup(func() {
				cancel()
				if err := <-ended; err != nil {
					t.Error(err)
				}
			})
			srv.exec = action.NewClient(socket, time.Minute)
			body, _ := json.Marshal(map[string]any{"kind": "project.move", "target": fmt.Sprint(p.ID), "params": map[string]any{"group": new.ID, "from": "/untrusted", "to": "/untrusted"}})
			w := post(t, srv, string(body))
			want := http.StatusOK
			if clash {
				want = http.StatusConflict
			}
			if w.Code != want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			pool, _ := srv.db.Pool()
			var actual int
			if err := pool.QueryRow(t.Context(), `SELECT group_id FROM profile_projects WHERE id=$1`, p.ID).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if !clash {
				if actual != new.ID {
					t.Fatal("map did not move")
				}
				if _, err := os.Stat(filepath.Join(from, rel)); !os.IsNotExist(err) {
					t.Fatal("source did not move", err)
				}
				if err := historymove.Pending([]string{from, to}, path); err != nil {
					t.Fatal("commit request was lost to action deduplication:", err)
				}
			} else if actual != old.ID {
				t.Fatal("a conflict changed the map")
			}
			actions, err := srv.db.Actions(t.Context(), store.ActionsReq{Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			result := store.ActionOK
			if clash {
				result = store.ActionFailed
			}
			if len(actions) != 1 || actions[0].Kind != "project.move" || actions[0].Result != result {
				t.Fatalf("journal: %+v", actions)
			}
		})
	}
}
