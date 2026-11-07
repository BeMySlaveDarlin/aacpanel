package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/taskreg"
)

func taskSample(t *testing.T) taskreg.Snapshot {
	t.Helper()
	out := taskreg.Snapshot{Root: "/srv/proj", Present: true, TasksComplete: true, HandoffsComplete: true, Files: []taskreg.File{}}
	meta, err := os.ReadFile("testdata/taskstore/meta.json")
	if err != nil {
		t.Fatal(err)
	}
	out.Meta = meta
	for _, dir := range []string{"tasks", "handoff"} {
		files, err := os.ReadDir("testdata/taskstore/" + dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			raw, err := os.ReadFile("testdata/taskstore/" + dir + "/" + file.Name())
			if err != nil {
				t.Fatal(err)
			}
			out.Files = append(out.Files, taskreg.File{Name: dir + "/" + file.Name(), Raw: string(raw)})
		}
	}
	return out
}

func taskProject(t *testing.T, s *Store, root, name string) (Profile, ProfileProject) {
	t.Helper()
	p := mustProfile(t, s, name, root, 0)
	g := mustGroup(t, s, p.ID, "work", 0)
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return p, mustProject(t, s, g.ID, name, dir, 0)
}

func TestTaskImportPreservesSourceAndIsIdempotentPG(t *testing.T) {
	s, root := profileStore(t)
	_, p := taskProject(t, s, root, "registry")
	sample := taskSample(t)
	result, err := s.ImportTasks(t.Context(), p.ID, p.Path, sample)
	if err != nil || len(result.Errors) > 0 || result.Tasks != 4 || result.Handoffs != 1 || result.Changed != 5 {
		t.Fatalf("import: %+v, %v", result, err)
	}
	before, err := s.Task(t.Context(), p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Events) != 2 || before.Events[0].Actor != "creator" || before.Events[1].Actor != "editor" || before.Events[0].Precision != "date" {
		t.Fatalf("source events: %+v", before.Events)
	}
	if before.Task.CreatedBy != "creator" || before.Task.Session != "editor" || len(before.Task.TouchedBy) != 2 || len(before.Handoffs) != 1 || !sameJSON(t, before.Task.External, `{"session":"native-session","id":"16"}`) {
		t.Fatalf("lost metadata: %+v", before)
	}
	if !strings.Contains(before.Task.Markdown, "## Plan\n\n- Read files\n- Import rows") || !strings.Contains(before.Task.Markdown, "**Decision**") || !strings.Contains(string(before.Task.Source), `"extra"`) {
		t.Fatalf("lost body or unknown fields: %+v", before.Task)
	}
	pool, _ := s.Pool()
	var taskVersion, eventVersion, handoffVersion string
	if err = pool.QueryRow(t.Context(), `SELECT xmin::text FROM project_tasks WHERE project_id=$1 AND number=1`, p.ID).Scan(&taskVersion); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(t.Context(), `SELECT string_agg(xmin::text,',' ORDER BY id) FROM task_events WHERE project_id=$1`, p.ID).Scan(&eventVersion); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(t.Context(), `SELECT xmin::text FROM project_handoffs WHERE project_id=$1`, p.ID).Scan(&handoffVersion); err != nil {
		t.Fatal(err)
	}
	result, err = s.ImportTasks(t.Context(), p.ID, p.Path, sample)
	if err != nil || result.Changed != 0 || result.Deleted != 0 || len(result.Errors) > 0 {
		t.Fatalf("repeat: %+v, %v", result, err)
	}
	after, err := s.Task(t.Context(), p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("repeat changed detail: before %+v, after %+v", before, after)
	}
	var a, b, c string
	_ = pool.QueryRow(t.Context(), `SELECT xmin::text FROM project_tasks WHERE project_id=$1 AND number=1`, p.ID).Scan(&a)
	_ = pool.QueryRow(t.Context(), `SELECT string_agg(xmin::text,',' ORDER BY id) FROM task_events WHERE project_id=$1`, p.ID).Scan(&b)
	_ = pool.QueryRow(t.Context(), `SELECT xmin::text FROM project_handoffs WHERE project_id=$1`, p.ID).Scan(&c)
	if a != taskVersion || b != eventVersion || c != handoffVersion {
		t.Fatal("repeat rewrote database rows")
	}
	legacy, err := s.Task(t.Context(), p.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(legacy.Task.Markdown, "A legacy Markdown body.") || legacy.Task.ClosedBy != "closer" || len(legacy.Events) != 3 {
		t.Fatalf("legacy task: %+v", legacy)
	}
	list, err := s.Task(t.Context(), p.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.Task.Markdown, "- A legacy list") || len(list.Events) != 1 || !reflect.DeepEqual(list.Task.ExternalRefs, []string{"SER-12"}) {
		t.Fatalf("legacy list or invented history: %+v", list)
	}
}

func TestTaskImportUpdatesDeletesAndKeepsBrokenFilesPG(t *testing.T) {
	s, root := profileStore(t)
	_, p := taskProject(t, s, root, "registry")
	sample := taskSample(t)
	if _, err := s.ImportTasks(t.Context(), p.ID, p.Path, sample); err != nil {
		t.Fatal(err)
	}
	for i, file := range sample.Files {
		if file.Name == "tasks/1.json" {
			sample.Files[i].Raw = strings.ReplaceAll(strings.ReplaceAll(file.Raw, `"status": "open"`, `"status": "done"`), `"updated": "2025-01-04"`, `"updated": "2025-01-07"`)
		}
		if file.Name == "tasks/2.json" {
			sample.Files[i].Raw = "{broken"
		}
	}
	kept := []taskreg.File{}
	for _, file := range sample.Files {
		if file.Name != "tasks/3.json" {
			kept = append(kept, file)
		}
	}
	sample.Files = kept
	result, err := s.ImportTasks(t.Context(), p.ID, p.Path, sample)
	if err != nil || len(result.Errors) != 1 || result.Deleted != 1 || result.Changed != 1 {
		t.Fatalf("update: %+v, %v", result, err)
	}
	detail, err := s.Task(t.Context(), p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.Status != "done" || detail.Events[len(detail.Events)-1].Kind != "status_changed" {
		t.Fatalf("status transition: %+v", detail)
	}
	observed := []string{}
	for _, event := range detail.Events {
		if event.Kind == "changed" {
			observed = append(observed, event.At.Format(time.DateOnly))
		}
	}
	if !reflect.DeepEqual(observed, []string{"2025-01-04", "2025-01-07"}) {
		t.Fatalf("a later import erased observed activity: %v", observed)
	}
	broken, err := s.Task(t.Context(), p.ID, 2)
	if err != nil || broken.Task.DeletedAt != nil {
		t.Fatalf("broken source was removed: %+v, %v", broken, err)
	}
	deleted, err := s.Task(t.Context(), p.ID, 3)
	if err != nil || deleted.Task.DeletedAt == nil {
		t.Fatalf("missing source not marked: %+v, %v", deleted, err)
	}
	result, err = s.ImportTasks(t.Context(), p.ID, p.Path, sample)
	if err != nil || result.Changed != 0 || result.Deleted != 0 {
		t.Fatalf("repeat deletion: %+v, %v", result, err)
	}
	if _, err = s.ImportTasks(t.Context(), p.ID, p.Path, taskSample(t)); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Task(t.Context(), p.ID, 3)
	if err != nil || restored.Task.DeletedAt != nil {
		t.Fatalf("restored source: %+v, %v", restored, err)
	}
	partial := taskreg.Snapshot{Present: true, Root: "/srv/proj"}
	if _, err = s.ImportTasks(t.Context(), p.ID, p.Path, partial); err != nil {
		t.Fatal(err)
	}
	page, err := s.Tasks(t.Context(), TaskFilter{ProjectID: p.ID})
	if err != nil || page.Total != 4 {
		t.Fatalf("incomplete listing deleted rows: %+v, %v", page, err)
	}
	// A syntactically valid JSON value unsupported by jsonb must not abort
	// the neighbouring imports either.
	sample = taskSample(t)
	sample.Files[0].Raw = strings.Replace(sample.Files[0].Raw, "Keep every source field.", `bad \u0000 value`, 1)
	result, err = s.ImportTasks(t.Context(), p.ID, p.Path, sample)
	if err != nil || len(result.Errors) != 1 || result.Tasks != 3 {
		t.Fatalf("jsonb error escaped the file: %+v, %v", result, err)
	}
}

func TestTaskImportRejectsUnmappedAndMovedProjectsPG(t *testing.T) {
	s, root := profileStore(t)
	_, p := taskProject(t, s, root, "registry")
	if _, err := s.ImportTasks(t.Context(), p.ID+10000, p.Path, taskSample(t)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unmapped import: %v", err)
	}
	if _, err := s.ImportTasks(t.Context(), p.ID, p.Path+"/other", taskSample(t)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale path import: %v", err)
	}
	page, err := s.Tasks(t.Context(), TaskFilter{})
	if err != nil || page.Total != 0 {
		t.Fatalf("unmapped source leaked into registry: %+v, %v", page, err)
	}
}

func TestTaskQueriesAndPeriodPG(t *testing.T) {
	s, root := profileStore(t)
	contour, p := taskProject(t, s, root, "registry")
	other, second := taskProject(t, s, root, "other")
	for _, project := range []ProfileProject{p, second} {
		if _, err := s.ImportTasks(t.Context(), project.ID, project.Path, taskSample(t)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		filter TaskFilter
		want   int
	}{
		{"contour", TaskFilter{ContourID: contour.ID}, 4},
		{"project", TaskFilter{ProjectID: p.ID}, 4},
		{"other contour", TaskFilter{ContourID: other.ID}, 4},
		{"status", TaskFilter{ProjectID: p.ID, Statuses: []string{"open", "deferred"}}, 2},
		{"tag", TaskFilter{ProjectID: p.ID, Tag: "backend"}, 1},
		{"body text", TaskFilter{ProjectID: p.ID, Text: "every source FIELD"}, 1},
		{"literal text", TaskFilter{ProjectID: p.ID, Text: "%"}, 0},
		{"external", TaskFilter{ProjectID: p.ID, External: "lms-42"}, 1},
		{"native binding", TaskFilter{ProjectID: p.ID, External: "16"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.Tasks(t.Context(), tc.filter)
			if err != nil || page.Total != tc.want || len(page.Tasks) != tc.want {
				t.Fatalf("query: %+v, %v", page, err)
			}
		})
	}
	page, err := s.Tasks(t.Context(), TaskFilter{ProjectID: p.ID, Limit: 1, Offset: 1})
	if err != nil || page.Total != 4 || len(page.Tasks) != 1 || page.Tasks[0].Number != 2 {
		t.Fatalf("pagination/order: %+v, %v", page, err)
	}
	from := time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC)
	until := from.AddDate(0, 0, 2)
	period, err := s.TasksPeriod(t.Context(), TaskFilter{ContourID: contour.ID, From: &from, Until: &until})
	if err != nil || period.TotalTasks != 2 || len(period.Groups) != 2 {
		t.Fatalf("period: %+v, %v", period, err)
	}
	if period.Groups[0].External != "LMS-42" || period.Groups[0].Tasks[0].Task.Status != "open" || len(period.Groups[0].Tasks[0].Events) != 1 {
		t.Fatalf("open activity missing: %+v", period)
	}
	if period.Groups[1].External != "SER-11" || len(period.Groups[1].Tasks[0].Events) != 2 {
		t.Fatalf("closure activity missing: %+v", period)
	}
	for _, group := range period.Groups {
		for _, activity := range group.Tasks {
			for _, event := range activity.Events {
				if event.At.Before(from) || !event.At.Before(until) {
					t.Fatalf("event outside interval: %+v", event)
				}
			}
		}
	}
	bad := []TaskFilter{{Statuses: []string{"unknown"}}, {Offset: -1}, {From: &from}, {From: &until, Until: &from}}
	for _, filter := range bad {
		if _, err := s.Tasks(t.Context(), filter); err == nil {
			t.Fatalf("invalid filter accepted: %+v", filter)
		}
	}
	if _, err := s.Task(t.Context(), p.ID, 1000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
	if _, err := s.TasksPeriod(t.Context(), TaskFilter{From: &from, Until: &until}); err == nil {
		t.Fatal("unscoped period accepted")
	}
	var source map[string]any
	detail, err := s.Task(t.Context(), p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(detail.Handoffs[0].Source, &source); err != nil || source["extra"] != "keep this" {
		t.Fatalf("handoff source lost: %v", source)
	}
}

func TestTaskImportRefreshesDerivedIssueKeysWithoutChangingSourcePG(t *testing.T) {
	s, root := profileStore(t)
	_, p := taskProject(t, s, root, "derived")
	sample := taskSample(t)
	if _, err := s.ImportTasks(t.Context(), p.ID, p.Path, sample); err != nil {
		t.Fatal(err)
	}
	pool, _ := s.Pool()
	if _, err := pool.Exec(t.Context(), `UPDATE project_tasks SET external_refs='{}' WHERE project_id=$1 AND number=1`, p.ID); err != nil {
		t.Fatal(err)
	}
	result, err := s.ImportTasks(t.Context(), p.ID, p.Path, sample)
	if err != nil || result.Changed != 1 {
		t.Fatalf("derived refresh: %+v %v", result, err)
	}
	task, err := s.Task(t.Context(), p.ID, 1)
	if err != nil || !reflect.DeepEqual(task.Task.ExternalRefs, []string{"LMS-42"}) || task.Task.LastEventAt == nil || task.Task.LastActor != "editor" {
		t.Fatalf("derived facts: %+v %v", task, err)
	}
}
