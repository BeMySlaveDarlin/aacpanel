package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/taskreg"
)

func summaryEvent(t *testing.T, body string) SummaryEvent {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"changes": map[string]any{"body": body}, "from": "open", "to": "open", "status": "open",
	})
	if err != nil {
		t.Fatal(err)
	}
	return SummaryEvent{projectID: 1, Project: "registry", Number: 1, Title: "Verify the inventory",
		Issues: []string{}, Event: taskreg.Event{ID: 1, Kind: "changed", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Precision: "instant", Actor: "conversation", ActorName: "worker", Origin: "panel", Payload: payload}}
}

func summaryAddedText(t *testing.T, row SummaryActivity) (string, bool) {
	t.Helper()
	if _, found := row.Changes["body"]; found {
		t.Fatal("a model record still contains the full body")
	}
	raw, found := row.Changes["appendedText"]
	var text string
	if found {
		if err := json.Unmarshal(raw, &text); err != nil {
			t.Fatal(err)
		}
	}
	return text, found
}

func TestTaskSummaryCompactsAdditionsBeforeIssueGrouping(t *testing.T) {
	parts := []string{"## Notes\n\nFirst addition.", "## Checks\n\nSecond addition.", "## Result\n\nThird addition."}
	events := []SummaryEvent{}
	body := "Existing description."
	for i, part := range parts {
		body += "\n\n" + part
		row := summaryEvent(t, body)
		row.Event.ID = int64(i + 1)
		row.Event.Kind = []string{"created", "status_changed", "closed"}[i]
		row.Issues = []string{"INV-1", "INV-2"}
		events = append(events, row)
	}
	before, _ := json.Marshal(events)
	groups, err := compactSummaryRecords(events, 800)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range []string{"INV-1", "INV-2"} {
		if len(groups[issue]) != 1 || len(groups[issue][0].Events) != len(parts) {
			t.Fatalf("%s has repeated task headers or missing events: %+v", issue, groups[issue])
		}
		for i, row := range groups[issue][0].Events {
			if got, found := summaryAddedText(t, row); !found || got != parts[i] {
				t.Errorf("%s event %d: got %q, want %q", issue, i, got, parts[i])
			}
		}
	}
	raw, _ := json.Marshal(groups)
	if strings.Count(string(raw), `"title":`) != 2 || strings.Count(string(raw), `"project":`) != 2 || strings.Count(string(raw), `"number":`) != 2 {
		t.Fatal("the model input repeats a task header for every event")
	}
	for _, field := range []string{`"issues":`, `"id":`, `"precision":`, `"origin":`, `"actorName":`, `"status":`, `"from":`, `"to":`, `"conversation"`} {
		if strings.Contains(string(raw), field) {
			t.Errorf("the model input retains the redundant field %s", field)
		}
	}
	after, _ := json.Marshal(events)
	if string(after) != string(before) {
		t.Fatal("compaction changed the recorded facts")
	}
}

func TestTaskSummaryBodyFallbacksAndRuneLimits(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
	}{
		{"last section", "Description.\n\n## Plan\n\nOld plan.\n\n## Result\n\nVerified.", "## Result\n\nVerified."},
		{"section at start", "## Notes\n\nVerified.", "## Notes\n\nVerified."},
		{"replacement", strings.Repeat("é", 801), strings.Repeat("é", 800) + "…"},
		{"exact limit", strings.Repeat("é", 800), strings.Repeat("é", 800)},
		{"inline heading", "Description with ## inline text.", "Description with ## inline text."},
		{"empty replacement", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := summaryEvent(t, "Unrelated previous body.")
			row := summaryEvent(t, test.body)
			groups, err := compactSummaryRecords([]SummaryEvent{first, row}, 800)
			if err != nil {
				t.Fatal(err)
			}
			if got, found := summaryAddedText(t, groups["Other work"][0].Events[1]); !found || got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestTaskSummaryKeepsTaskBodiesSeparateAndOtherChangesIntact(t *testing.T) {
	first := summaryEvent(t, "Shared prefix.")
	otherProject := summaryEvent(t, "Shared prefix. Replacement in another project.")
	otherProject.projectID = 2
	otherTask := summaryEvent(t, "Shared prefix. Replacement in another task.")
	otherTask.Number = 2
	noBody := summaryEvent(t, "")
	noBody.Event.Payload = json.RawMessage(`{"status":"open"}`)
	last := summaryEvent(t, "")
	last.Event.Payload = json.RawMessage(`{"changes":{"body":"Shared prefix. Added.","title":{"from":"Old","to":"New"},"status":{"from":"open","to":"done"},"external":"https://example.com/INV-1","blockedBy":[2,3]},"from":"open","to":"done","status":"done"}`)
	groups, err := compactSummaryRecords([]SummaryEvent{first, otherProject, otherTask, noBody, last}, 800)
	if err != nil {
		t.Fatal(err)
	}
	rows := groups["Other work"]
	if len(rows) != 3 || len(rows[0].Events) != 3 || len(rows[1].Events) != 1 || len(rows[2].Events) != 1 {
		t.Fatalf("tasks were merged by name or number alone: %+v", rows)
	}
	activities := []SummaryActivity{rows[0].Events[0], rows[1].Events[0], rows[2].Events[0], rows[0].Events[1], rows[0].Events[2]}
	for i, want := range []string{"Shared prefix.", "Shared prefix. Replacement in another project.", "Shared prefix. Replacement in another task.", "", "Added."} {
		got, found := summaryAddedText(t, activities[i])
		if got != want || found != (i != 3) {
			t.Errorf("event %d: got %q, present %v, want %q", i, got, found, want)
		}
	}
	raw, _ := json.Marshal(activities[4])
	if !sameJSON(t, raw, `{"kind":"changed","at":"2026-01-01T00:00:00Z","actor":"worker","changes":{"appendedText":"Added.","title":{"from":"Old","to":"New"},"external":"https://example.com/INV-1","blockedBy":[2,3]},"from":"open","to":"done"}`) {
		t.Fatalf("compaction lost changes or duplicated the status: %s", raw)
	}
}

func TestTaskSummaryKeepsImportedStatusTransitions(t *testing.T) {
	for _, test := range []struct {
		kind, payload, from, to string
	}{
		{"closed", `{"status":"cancelled"}`, "", "cancelled"},
		{"status_changed", `{"from":"done","to":"open"}`, "done", "open"},
		{"changed", `{"from":"open","to":"open","status":"open"}`, "", ""},
	} {
		t.Run(test.kind, func(t *testing.T) {
			row := summaryEvent(t, "")
			row.Event.Kind, row.Event.Payload = test.kind, json.RawMessage(test.payload)
			groups, err := compactSummaryRecords([]SummaryEvent{row}, 800)
			if err != nil {
				t.Fatal(err)
			}
			event := groups["Other work"][0].Events[0]
			if event.From != test.from || event.To != test.to {
				t.Fatalf("status transition: %+v", event)
			}
		})
	}
}

func TestTaskSummaryBudgetShrinksExcerptsBeforeRefusing(t *testing.T) {
	for _, test := range []struct {
		count, limit int
	}{
		{1, 800}, {300, 300}, {600, 100}, {1000, 0}, {2000, -1},
	} {
		t.Run(fmt.Sprint(test.count), func(t *testing.T) {
			events := make([]SummaryEvent, test.count)
			for i := range events {
				events[i] = summaryEvent(t, strings.Repeat("x", 1000))
				events[i].Number = int64(i + 1)
			}
			source, err := taskSummarySource(events)
			if test.limit < 0 {
				var bad ErrBadRequest
				if !errors.As(err, &bad) || err.Error() != "the period has too much activity for a summary; choose a shorter period" {
					t.Fatalf("overflow without text: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if source.Empty || len(source.Records["Other work"]) != len(events) {
				t.Fatal("the budget dropped events")
			}
			for _, row := range source.Records["Other work"] {
				if len(row.Events) != 1 {
					t.Fatal("the budget dropped a task's events")
				}
				text, found := summaryAddedText(t, row.Events[0])
				want := ""
				if test.limit > 0 {
					want = strings.Repeat("x", test.limit) + "…"
				}
				if text != want || found != (test.limit > 0) {
					t.Fatalf("budget %d: got %d runes, present %v", test.limit, len([]rune(text)), found)
				}
			}
			raw, _ := json.Marshal(source.Records)
			if len(raw) > summaryRecordsBudget {
				t.Fatalf("records exceed the budget: %d bytes", len(raw))
			}
		})
	}
}

func TestTaskSummaryFingerprintUsesUnchangedRawRecords(t *testing.T) {
	first := summaryEvent(t, strings.Repeat("x", 1000))
	first.Event.Payload = append([]byte(" \n"), first.Event.Payload...)
	second := summaryEvent(t, "## Notes\n\nVerified.")
	second.Number = 2
	second.Issues = []string{"INV-1", "INV-2"}
	events := []SummaryEvent{first, second}
	// This is the complete grouping and encoding used to identify cached periods.
	raw, err := json.Marshal(map[string][]SummaryEvent{"Other work": {first}, "INV-1": {second}, "INV-2": {second}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	source, err := taskSummarySource(events)
	if err != nil {
		t.Fatal(err)
	}
	if source.Fingerprint != hex.EncodeToString(sum[:]) || !reflect.DeepEqual(source.Actors, []string{"conversation"}) {
		t.Fatalf("identity or authors changed: %+v", source)
	}
	events[0] = summaryEvent(t, strings.Repeat("x", 1000)+"outside the excerpt")
	changed, err := taskSummarySource(events)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Fingerprint == source.Fingerprint || !reflect.DeepEqual(changed.Records, source.Records) {
		t.Fatal("cache identity depends on the excerpt instead of the recorded body")
	}
	empty, err := taskSummarySource(nil)
	emptySum := sha256.Sum256([]byte(`{}`))
	if err != nil || !empty.Empty || empty.Fingerprint != hex.EncodeToString(emptySum[:]) || len(empty.Records) != 0 {
		t.Fatalf("empty period: %+v, %v", empty, err)
	}
}

func TestTaskSummaryBudgetFitsMeasuredWeek(t *testing.T) {
	tasks := make([]taskreg.Task, 111)
	events := []SummaryEvent{}
	payloads := []map[string]any{}
	initial := strings.Repeat("Initial inventory details. ", 40)
	appendEvent := func(index int, kind string, edit taskreg.Edit) {
		t.Helper()
		before := tasks[index].Status
		changes := edit.Apply(&tasks[index])
		payload := map[string]any{"changes": changes, "from": before, "to": tasks[index].Status, "status": tasks[index].Status}
		row := summaryEvent(t, "")
		row.Project = "inventory"
		row.Number = int64(index + 1)
		row.Issues = []string{fmt.Sprintf("INV-%05d", index+1)}
		if index == 20 {
			row.Issues = append(row.Issues, "INV-SHARED")
		}
		row.URL = "https://tracker.example.com/" + strings.Repeat("p", 46)
		headerBytes := len(row.Project) + len(row.URL)
		for _, ref := range row.Issues {
			headerBytes += len(ref)
		}
		row.Title = "Reconcile inventory " + strings.Repeat("x", 191-headerBytes-len("Reconcile inventory "))
		row.Event.ID = int64(10000 + len(events))
		row.Event.At = time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC).Add(time.Duration(len(events)) * 10 * time.Minute)
		row.Event.Actor = fmt.Sprintf("11111111-2222-4333-8444-%012d", index)
		row.Event.ActorName = "inventory-worker-" + strings.Repeat("x", 24)
		row.Event.Kind = kind
		events = append(events, row)
		payloads = append(payloads, payload)
	}
	for i := range tasks {
		tasks[i].Status = "open"
		if i >= 82 {
			tasks[i].Markdown = initial
		}
	}
	for i := range 82 {
		appendEvent(i, "created", taskreg.Edit{Title: taskString("Reconcile inventory records"), Markdown: &initial, Replace: true})
	}
	// Multibyte notes keep the byte budget distinct from the rune limit.
	note := strings.Repeat("é", 1600)
	for i := range 438 {
		appendEvent(i%len(tasks), "changed", taskreg.Edit{Markdown: &note})
	}
	for i := range 68 {
		appendEvent(i, "closed", taskreg.Edit{Status: taskString("done"), Markdown: taskString("Inventory verified.")})
	}
	for i := range 14 {
		appendEvent(i, "status_changed", taskreg.Edit{Status: taskString("open"), Markdown: taskString("Verification reopened.")})
	}
	withoutBody := func(payload map[string]any) []byte {
		t.Helper()
		changes := payload["changes"].(map[string]any)
		body, found := changes["body"]
		delete(changes, "body")
		raw, err := json.Marshal(payload)
		if found {
			changes["body"] = body
		}
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	payloadBytes := 0
	for _, payload := range payloads {
		payloadBytes += len(withoutBody(payload))
	}
	for i, payload := range payloads {
		if payloadBytes >= 103*len(events) {
			break
		}
		before := len(withoutBody(payload))
		payload["changes"].(map[string]any)["external"] = fmt.Sprintf("https://example.com/INV-%04d", i)
		payloadBytes += len(withoutBody(payload)) - before
	}
	headerBytes, eventBytes, bodyBytes := 0, 0, 0
	bodyless := make([]SummaryEvent, len(events))
	for i := range events {
		row := &events[i]
		headerBytes += len(row.Project) + len(row.Title) + len(row.URL)
		for _, ref := range row.Issues {
			headerBytes += len(ref)
		}
		event := row.Event
		eventBytes += len(fmt.Sprint(event.ID)) + len(event.Kind) + len(event.At.Format(time.RFC3339Nano)) + len(event.Precision) + len(event.Actor) + len(event.ActorName) + len(event.Origin)
		bodyBytes += len(payloads[i]["changes"].(map[string]any)["body"].(string))
		var err error
		row.Event.Payload, err = json.Marshal(payloads[i])
		if err != nil {
			t.Fatal(err)
		}
		bodyless[i] = *row
		bodyless[i].Event.Payload = withoutBody(payloads[i])
	}
	t.Logf("measured-shape week: %d tasks, %d events, %d body bytes; average header/event/payload bytes: %.1f / %.1f / %.1f",
		len(tasks), len(events), bodyBytes, float64(headerBytes)/float64(len(events)), float64(eventBytes)/float64(len(events)), float64(payloadBytes)/float64(len(events)))
	if len(events) != 602 || headerBytes != 191*len(events) || eventBytes < 127*len(events) || eventBytes > 129*len(events) || payloadBytes < 102*len(events) || payloadBytes > 104*len(events) || bodyBytes < 4.5*(1<<20) || bodyBytes > 5.5*(1<<20) {
		t.Fatal("the fixture does not match the measured week")
	}
	groups := groupSummaryEvents(events)
	count := 0
	for _, rows := range groups {
		count += len(rows)
	}
	if count != 608 {
		t.Fatalf("issue grouping has %d records, want 608", count)
	}
	raw, _ := json.Marshal(groups)
	oldBodyless, _ := json.Marshal(groupSummaryEvents(bodyless))
	t.Logf("raw records: %d bytes; raw records without bodies: %d bytes", len(raw), len(oldBodyless))
	if len(oldBodyless) <= summaryRecordsBudget {
		t.Fatal("the fixture does not reproduce the overflow without body text")
	}
	for _, limit := range []int{800, 300, 100, 0} {
		groups, err := compactSummaryRecords(events, limit)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(groups)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%d-rune excerpts: %d bytes", limit, len(raw))
	}
	source, err := taskSummarySource(events)
	if err != nil {
		t.Fatal(err)
	}
	count, taskCount, hasText := 0, 0, false
	for _, tasks := range source.Records {
		taskCount += len(tasks)
		for _, task := range tasks {
			count += len(task.Events)
			for _, event := range task.Events {
				_, found := summaryAddedText(t, event)
				hasText = hasText || found
			}
		}
	}
	if count != 608 || taskCount != 112 || !hasText {
		t.Fatalf("the measured week lost tasks, events or all note excerpts: tasks=%d events=%d text=%v", taskCount, count, hasText)
	}
}
