package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/taskreg"
)

func taskString(value string) *string { return &value }

func panelTaskProject(t *testing.T, s *Store, root, name string) ProfileProject {
	t.Helper()
	_, p := taskProject(t, s, root, name)
	if _, err := s.SwitchTaskWriter(t.Context(), p.ID, p.Path, taskreg.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTaskWriterSwitchImportsAtomicallyAndBlocksFileUpdatesPG(t *testing.T) {
	s, root := profileStore(t)
	_, p := taskProject(t, s, root, "writer-switch")
	actor := taskreg.Actor{Conversation: "conversation", Name: "worker"}
	if _, err := s.WriteTask(t.Context(), p.ID, 0, actor, taskreg.Edit{Title: taskString("Work")}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "cctm") {
		t.Fatalf("file writer accepted edit: %v", err)
	}
	sample := taskSample(t)
	broken := sample
	broken.Files = append(slices.Clone(sample.Files), taskreg.File{Name: "tasks/8.json", Raw: `invalid`})
	if _, err := s.SwitchTaskWriter(t.Context(), p.ID, p.Path, broken); err == nil {
		t.Fatal("switched with a broken final import")
	}
	writer, err := s.TaskWriter(t.Context(), p.ID)
	if err != nil || writer != taskreg.FilesWriter {
		t.Fatalf("failed switch changed writer: %q %v", writer, err)
	}
	page, err := s.Tasks(t.Context(), TaskFilter{ProjectID: p.ID})
	if err != nil || page.Total != 0 {
		t.Fatalf("failed final import was not rolled back: %+v %v", page, err)
	}
	partial := sample
	partial.TasksComplete = false
	if _, err = s.SwitchTaskWriter(t.Context(), p.ID, p.Path, partial); err == nil {
		t.Fatal("switched with an incomplete final import")
	}
	if _, err = s.SwitchTaskWriter(t.Context(), p.ID, "/srv/moved", sample); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale map switch: %v", err)
	}
	result, err := s.SwitchTaskWriter(t.Context(), p.ID, p.Path, sample)
	if err != nil || result.Tasks != 4 {
		t.Fatalf("final import: %+v %v", result, err)
	}
	writer, err = s.TaskWriter(t.Context(), p.ID)
	if err != nil || writer != taskreg.PanelWriter {
		t.Fatalf("writer: %q %v", writer, err)
	}
	task, err := s.WriteTask(t.Context(), p.ID, 1, actor, taskreg.Edit{Title: taskString("Panel title")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ImportTasks(t.Context(), p.ID, p.Path, sample); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Task(t.Context(), p.ID, 1)
	if err != nil || detail.Task.Title != task.Title {
		t.Fatalf("file import overwrote panel task: %+v %v", detail.Task, err)
	}
	empty := sample
	empty.Files = nil
	if _, err = s.ImportTasks(t.Context(), p.ID, p.Path, empty); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Task(t.Context(), p.ID, 1)
	if err != nil || detail.Task.DeletedAt != nil {
		t.Fatalf("file import deleted panel task: %+v %v", detail.Task, err)
	}
	next, err := s.WriteTask(t.Context(), p.ID, 0, actor, taskreg.Edit{Title: taskString("Next work")})
	if err != nil || next.Number != 5 {
		t.Fatalf("number after import: %d %v", next.Number, err)
	}
}

func TestTaskCreationNumbersAreConsecutiveUnderConcurrencyPG(t *testing.T) {
	s, root := profileStore(t)
	p := panelTaskProject(t, s, root, "concurrent-tasks")
	pool, _ := s.Pool()
	// Keeping inserts overlapped makes a missing project lock fail reliably,
	// even on a machine whose workers would otherwise run one after another.
	if _, err := pool.Exec(t.Context(), `CREATE FUNCTION slow_task_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.03); RETURN NEW; END $$;
        CREATE TRIGGER slow_task_insert BEFORE INSERT ON project_tasks FOR EACH ROW EXECUTE FUNCTION slow_task_insert()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(t.Context(), `DROP TRIGGER slow_task_insert ON project_tasks; DROP FUNCTION slow_task_insert()`)
	})
	const count = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	numbers := make(chan int64, count)
	failures := make(chan error, count)
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			task, err := s.WriteTask(t.Context(), p.ID, 0, taskreg.Actor{Conversation: fmt.Sprint(i), Name: "worker"}, taskreg.Edit{Title: taskString("Work")})
			if err != nil {
				failures <- err
				return
			}
			numbers <- task.Number
		}()
	}
	close(start)
	wg.Wait()
	close(numbers)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	got := []int64{}
	for n := range numbers {
		got = append(got, n)
	}
	slices.Sort(got)
	want := []int64{}
	for n := int64(1); n <= count; n++ {
		want = append(want, n)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parallel numbers: %v, want %v", got, want)
	}
}

func TestTaskWritesKeepBodyEventsAttributionAndHandoffBoundariesPG(t *testing.T) {
	s, root := profileStore(t)
	p := panelTaskProject(t, s, root, "native-tasks")
	actor := taskreg.Actor{Conversation: "thread-one", Name: "worker-one"}
	other := taskreg.Actor{Conversation: "thread-two", Name: "worker-two"}
	before := time.Now().UTC().Add(-time.Second)
	task, err := s.WriteTask(t.Context(), p.ID, 0, actor, taskreg.Edit{Title: taskString("Work"), Markdown: taskString("First paragraph"), Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	tags := []string{"backend", "urgent"}
	blockers := []int64{task.Number}
	blocked, err := s.WriteTask(t.Context(), p.ID, 0, other, taskreg.Edit{Title: taskString("Follow-up"), Priority: taskString("HIGH"), BlockedBy: &blockers})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := s.Tasks(t.Context(), TaskFilter{ProjectID: p.ID, Next: true})
	if err != nil || len(queue.Tasks) != 1 || queue.Tasks[0].Number != task.Number {
		t.Fatalf("unfinished blocker ignored: %+v %v", queue, err)
	}
	edit := taskreg.Edit{Title: taskString("LMS-42: finish"), Priority: taskString("LOW"), Tags: &tags, External: taskString("https://issues.example/LMS-42"), Markdown: taskString("Run the check"), Section: "Verify"}
	task, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, edit)
	if err != nil || task.Markdown != "First paragraph\n\n## Verify\n\nRun the check" {
		t.Fatalf("append: %+v %v", task, err)
	}
	task, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{Status: taskString("done")})
	if err != nil || task.ClosedAt == nil || task.ClosedBy != actor.Conversation {
		t.Fatalf("closure: %+v %v", task, err)
	}
	queue, err = s.Tasks(t.Context(), TaskFilter{ProjectID: p.ID, Next: true})
	if err != nil || len(queue.Tasks) != 1 || queue.Tasks[0].Number != blocked.Number {
		t.Fatalf("finished blocker still blocks: %+v %v", queue, err)
	}
	h, err := s.WriteHandoff(t.Context(), p.ID, actor, p.Path, taskreg.HandoffInput{Title: "Saved", Markdown: "Progress", Next: "Start the follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	var groups map[string][]int64
	_ = json.Unmarshal(h.Tasks, &groups)
	if !reflect.DeepEqual(groups, map[string][]int64{"created": {task.Number}, "closed": {task.Number}, "touched": {task.Number}}) || !slices.Equal(h.TaskNumbers, []int64{task.Number}) {
		t.Fatalf("handoff includes another actor or loses events: %s", h.Tasks)
	}
	byKey, err := s.Handoff(t.Context(), p.ID, h.Key)
	if err != nil || byKey.Title != h.Title || byKey.Next != h.Next || byKey.SessionName != actor.Name {
		t.Fatalf("read handoff: %+v %v", byKey, err)
	}
	latest, err := s.Handoff(t.Context(), p.ID, "")
	if err != nil || latest.Key != h.Key {
		t.Fatalf("latest handoff: %+v %v", latest, err)
	}
	if _, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{Status: taskString("open"), Markdown: taskString("Replacement"), Replace: true}); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Task(t.Context(), p.ID, task.Number)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.ClosedAt != nil || detail.Task.ClosedBy != "" || detail.Task.Markdown != "Replacement" || len(detail.Events) != 4 {
		t.Fatalf("reopen/replace: %+v events %+v", detail.Task, detail.Events)
	}
	for i, e := range detail.Events {
		if e.Precision != "instant" || e.Actor != actor.Conversation || e.ActorName != actor.Name || e.Origin != "panel" || e.At.Before(before) || e.At.After(time.Now().Add(time.Second)) {
			t.Errorf("event %d: %+v", i, e)
		}
	}
	if detail.Events[0].Kind != "created" || detail.Events[1].Kind != "changed" || detail.Events[2].Kind != "closed" || detail.Events[3].Kind != "status_changed" {
		t.Fatalf("event kinds: %+v", detail.Events)
	}
	if !strings.Contains(string(detail.Events[1].Payload), "external") || !strings.Contains(string(detail.Events[1].Payload), "body") {
		t.Fatalf("edit payload: %s", detail.Events[1].Payload)
	}
	h, err = s.WriteHandoff(t.Context(), p.ID, actor, p.Path, taskreg.HandoffInput{Title: "Resumed", Markdown: "More work"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(h.Tasks, &groups)
	if len(groups["created"]) != 0 || len(groups["closed"]) != 0 || !slices.Equal(groups["touched"], []int64{task.Number}) {
		t.Fatalf("handoff repeated previous events: %s", h.Tasks)
	}
	if _, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{Markdown: taskString(""), Replace: true}); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Task(t.Context(), p.ID, task.Number)
	if err != nil || detail.Task.Markdown != "" {
		t.Fatalf("empty replacement lost: %+v %v", detail.Task, err)
	}
}

func TestTaskFieldsCanBeClearedWithoutLosingOmittedValuesPG(t *testing.T) {
	s, root := profileStore(t)
	p := panelTaskProject(t, s, root, "task-fields")
	actor := taskreg.Actor{Conversation: "worker", Name: "worker"}
	first, err := s.WriteTask(t.Context(), p.ID, 0, actor, taskreg.Edit{Title: taskString("First")})
	if err != nil {
		t.Fatal(err)
	}
	tags := []string{"backend", "urgent"}
	blockers := []int64{first.Number}
	order := 3.5
	task, err := s.WriteTask(t.Context(), p.ID, 0, actor, taskreg.Edit{Title: taskString("Second"), Tags: &tags, BlockedBy: &blockers, Order: &order, Status: taskString("deferred"), Priority: taskString("HIGH"), External: taskString("https://issues.example/LMS-42")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{Title: taskString("Renamed")}); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Task(t.Context(), p.ID, task.Number)
	if err != nil || detail.Task.Order == nil || *detail.Task.Order != order || !slices.Equal(detail.Task.Tags, tags) || !slices.Equal(detail.Task.BlockedBy, blockers) || detail.Task.ExternalURL == "" || detail.Task.Status != "deferred" {
		t.Fatalf("omitted fields changed: %+v %v", detail.Task, err)
	}
	emptyTags := []string{}
	emptyBlockers := []int64{}
	if _, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{Tags: &emptyTags, BlockedBy: &emptyBlockers, External: taskString(""), Priority: taskString(""), Status: taskString("cancelled")}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{}); err != nil {
		t.Fatal(err)
	}
	detail, err = s.Task(t.Context(), p.ID, task.Number)
	if err != nil || len(detail.Events) != 3 || len(detail.Task.Tags) != 0 || len(detail.Task.BlockedBy) != 0 || detail.Task.ExternalURL != "" || detail.Task.Priority != "" {
		t.Fatalf("clear/no-op: %+v %+v %v", detail.Task, detail.Events, err)
	}
	var payload struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(detail.Events[2].Payload, &payload)
	if payload.Status != "cancelled" {
		t.Fatalf("closure payload lost cancellation: %s", detail.Events[2].Payload)
	}
	self := []int64{task.Number}
	if _, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{BlockedBy: &self}); err == nil {
		t.Fatal("self blocker accepted")
	}
	missing := []int64{99999}
	if _, err = s.WriteTask(t.Context(), p.ID, task.Number, actor, taskreg.Edit{BlockedBy: &missing}); err == nil {
		t.Fatal("missing blocker accepted")
	}
}
