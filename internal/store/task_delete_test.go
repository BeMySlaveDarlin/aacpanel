package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"aacpanel/internal/taskreg"
)

func TestPanelTaskRegistrySurvivesMapDeletionPG(t *testing.T) {
	for _, removal := range []string{"project", "group", "profile", "sql"} {
		for _, content := range []string{"tasks", "handoffs", "both", "empty", "files"} {
			t.Run(removal+"/"+content, func(t *testing.T) {
				s, root := profileStore(t)
				profile, project := taskProject(t, s, root, "kept-registry")
				snapshot := taskreg.Snapshot{Root: project.Path, Present: true, TasksComplete: true, HandoffsComplete: true}
				if content == "tasks" || content == "both" || content == "files" {
					snapshot.Files = append(snapshot.Files, taskreg.File{Name: "tasks/1.json", Raw: `{"id":1,"title":"Keep this task","status":"closed","created":"2026-01-01"}`})
				}
				if content == "handoffs" || content == "both" || content == "files" {
					snapshot.Files = append(snapshot.Files, taskreg.File{Name: "handoff/progress.json", Raw: `{"key":"progress","title":"Keep this handoff"}`})
				}
				if content == "files" {
					if _, err := s.ImportTasks(t.Context(), project.ID, project.Path, snapshot); err != nil {
						t.Fatal(err)
					}
				} else if _, err := s.SwitchTaskWriter(t.Context(), project.ID, project.Path, snapshot); err != nil {
					t.Fatal(err)
				}
				before, err := s.Profiles(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				journalBefore, err := s.Journal(t.Context(), 100)
				if err != nil {
					t.Fatal(err)
				}
				pool, _ := s.Pool()
				counts := func() [4]int {
					t.Helper()
					var out [4]int
					for i, table := range []string{"project_tasks", "task_events", "project_handoffs", "task_stores"} {
						if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE project_id=$1", project.ID).Scan(&out[i]); err != nil {
							t.Fatal(err)
						}
					}
					return out
				}
				kept := counts()
				switch removal {
				case "project":
					_, err = s.DeleteProject(t.Context(), project.ID)
				case "group":
					err = s.DeleteGroup(t.Context(), project.GroupID, true)
				case "profile":
					err = s.DeleteProfile(t.Context(), profile.ID, true)
				case "sql":
					_, err = pool.Exec(t.Context(), `DELETE FROM profile_projects WHERE id=$1`, project.ID)
				}
				if content == "empty" || content == "files" {
					if err != nil {
						t.Fatal(err)
					}
					if got := counts(); got != [4]int{} {
						t.Fatalf("deleted map entry left registry copies: %v", got)
					}
					return
				}
				if removal == "sql" {
					var pg *pgconn.PgError
					if !errors.As(err, &pg) || pg.ConstraintName != "panel_task_registry_not_empty" {
						t.Fatalf("raw deletion bypassed the registry: %v", err)
					}
				} else if !errors.Is(err, ErrNotEmpty) {
					t.Fatalf("deletion did not report a nonempty registry: %v", err)
				}
				for _, text := range []string{project.Name, fmt.Sprintf("%d tasks", kept[0]), fmt.Sprintf("%d handoffs", kept[2])} {
					if !strings.Contains(err.Error(), text) {
						t.Errorf("refusal lost %q: %v", text, err)
					}
				}
				after, err := s.Profiles(t.Context())
				if err != nil || !reflect.DeepEqual(before, after) || counts() != kept {
					t.Fatalf("refusal changed the map or registry: %+v, %v", after, err)
				}
				journalAfter, err := s.Journal(t.Context(), 100)
				if err != nil || !reflect.DeepEqual(journalBefore, journalAfter) {
					t.Fatalf("refusal changed the map journal: %+v, %v", journalAfter, err)
				}
			})
		}
	}
}
