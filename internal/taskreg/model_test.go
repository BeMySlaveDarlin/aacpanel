package taskreg

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBodySectionsKeepTheirOrderAndMixedValues(t *testing.T) {
	raw := json.RawMessage(`{"files":["a.go"],"notes":{"B":"second","A":"first"},"result":"ready","measure":3,"verify":"run it","plan":["one",{"step":"two"}],"precondition":"available","context":"scope","description":"purpose","extra":"kept","":"unnamed"}`)
	got := Markdown(raw)
	previous := -1
	for _, heading := range []string{"## Description", "## Context", "## Precondition", "## Plan", "## Verify", "## Measure", "## Result", "## Notes", "## Files", "## \n", "## extra"} {
		at := strings.Index(got, heading)
		if at <= previous {
			t.Fatalf("section %q lost or out of order in %s", heading, got)
		}
		previous = at
	}
	for _, text := range []string{"- one", "**step**", "two", "**A**\n\nfirst", "**B**\n\nsecond", "unnamed", "kept"} {
		if !strings.Contains(got, text) {
			t.Fatalf("missing %q in %s", text, got)
		}
	}
	if Markdown(json.RawMessage(`null`)) != "" {
		t.Fatal("null became prose")
	}
}

func TestExternalNumbersComeFromIssueReferencesAndKeepNativeBindings(t *testing.T) {
	for _, tc := range []struct {
		title, external string
		refs            []string
		url             string
	}{
		{"LMS-42 and SER-11 then LMS-42", `{"session":"native","id":"999"}`, []string{"LMS-42", "SER-11"}, ""},
		{"Read it", `"https://issues.example/LMS-42"`, []string{"LMS-42"}, "https://issues.example/LMS-42"},
		{"Read it", `{"key":"SER-11","url":"https://issues.example/SER-11"}`, []string{"SER-11"}, "https://issues.example/SER-11"},
		{"Read it", `{"session":null,"id":"T-1"}`, []string{}, ""},
	} {
		refs, url := external(tc.title, json.RawMessage(tc.external))
		if !reflect.DeepEqual(refs, tc.refs) || url != tc.url {
			t.Fatalf("external %s: %v, %q", tc.external, refs, url)
		}
	}
}

func TestSourceDatesAndLegacyClosedState(t *testing.T) {
	task, err := ParseTask(`{"id":1,"title":"Work","status":"closed","created":"2025-01-01","updated":"2025-01-02T10:00:00Z","body":null,"unknown":{"keep":true}}`)
	if err != nil || task.Status != "done" || !strings.Contains(string(task.Source), `"status":"closed"`) || task.ClosedAt != nil {
		t.Fatalf("legacy task: %+v, %v", task, err)
	}
	if _, precision, err := Date("2025-01-02"); err != nil || precision != "date" {
		t.Fatalf("date precision: %s, %v", precision, err)
	}
	if _, precision, err := Date("2025-01-02T10:00:00Z"); err != nil || precision != "instant" {
		t.Fatalf("timestamp precision: %s, %v", precision, err)
	}
	if _, _, err := Date("yesterday"); err == nil {
		t.Fatal("invalid date accepted")
	}
}

func TestExplicitJiraReferenceIsIndexedWithoutScanningTheBody(t *testing.T) {
	task, err := ParseTask(`{"id":1,"title":"Work","status":"open","refs":{"jira":"LMS-42","mr":5},"body":{"description":"Related to SER-11."}}`)
	if err != nil || !reflect.DeepEqual(task.ExternalRefs, []string{"LMS-42"}) {
		t.Fatalf("explicit issue reference: %+v, %v", task, err)
	}
}
