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

func summaryAddedText(t *testing.T, row SummaryEvent) (string, bool) {
	t.Helper()
	var payload struct {
		Changes map[string]json.RawMessage `json:"changes"`
	}
	if err := json.Unmarshal(row.Event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if _, found := payload.Changes["body"]; found {
		t.Fatal("a model record still contains the full body")
	}
	raw, found := payload.Changes["appendedText"]
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
		if len(groups[issue]) != len(parts) {
			t.Fatalf("%s has %d events", issue, len(groups[issue]))
		}
		for i, row := range groups[issue] {
			if got, found := summaryAddedText(t, row); !found || got != parts[i] {
				t.Errorf("%s event %d: got %q, want %q", issue, i, got, parts[i])
			}
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
			if got, found := summaryAddedText(t, groups["Other work"][1]); !found || got != test.want {
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
	for i, want := range []string{"Shared prefix.", "Shared prefix. Replacement in another project.", "Shared prefix. Replacement in another task.", "", "Added."} {
		got, found := summaryAddedText(t, rows[i])
		if got != want || found != (i != 3) {
			t.Errorf("event %d: got %q, present %v, want %q", i, got, found, want)
		}
	}
	if !sameJSON(t, rows[4].Event.Payload, `{"changes":{"appendedText":"Added.","title":{"from":"Old","to":"New"},"status":{"from":"open","to":"done"},"external":"https://example.com/INV-1","blockedBy":[2,3]},"from":"open","to":"done","status":"done"}`) {
		t.Fatalf("compaction lost other changes: %s", rows[4].Event.Payload)
	}
}

func TestTaskSummaryBudgetShrinksExcerptsBeforeRefusing(t *testing.T) {
	for _, test := range []struct {
		count, limit int
	}{
		{1, 800}, {250, 300}, {450, 0}, {1500, -1},
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
				text, found := summaryAddedText(t, row)
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

func TestTaskSummaryBudgetFitsActiveWeek(t *testing.T) {
	tasks := make([]taskreg.Task, 82)
	events := []SummaryEvent{}
	appendEvent := func(index int, kind string, edit taskreg.Edit) {
		t.Helper()
		before := tasks[index].Status
		changes := edit.Apply(&tasks[index])
		payload, err := json.Marshal(map[string]any{"changes": changes, "from": before, "to": tasks[index].Status, "status": tasks[index].Status})
		if err != nil {
			t.Fatal(err)
		}
		row := summaryEvent(t, "")
		row.Number = int64(index + 1)
		row.Title = tasks[index].Title
		row.Event.ID = int64(len(events) + 1)
		row.Event.At = row.Event.At.Add(time.Duration(len(events)) * 10 * time.Minute)
		row.Event.Kind, row.Event.Payload = kind, payload
		events = append(events, row)
	}
	initial := strings.Repeat("Existing inventory details. ", 45)
	note := strings.Repeat("Inventory reconciled with the ledger. ", 80)
	for i := range tasks {
		tasks[i].Status = "open"
		appendEvent(i, "created", taskreg.Edit{Title: taskString("Finish inventory reconciliation"), Markdown: &initial, Replace: true})
	}
	for i := range 438 {
		appendEvent(i%len(tasks), "changed", taskreg.Edit{Markdown: &note})
	}
	for i := range 68 {
		appendEvent(i, "closed", taskreg.Edit{Status: taskString("done"), Markdown: taskString("Inventory verified.")})
	}
	for i := range 12 {
		appendEvent(i, "status_changed", taskreg.Edit{Status: taskString("open"), Markdown: taskString("Verification reopened.")})
	}
	raw, err := json.Marshal(groupSummaryEvents(events))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic week: %d events, raw records %d bytes", len(events), len(raw))
	for _, limit := range []int{800, 300, 0} {
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
	if err != nil || len(source.Records["Other work"]) != len(events) {
		t.Fatalf("the active week does not fit: %v", err)
	}
}
