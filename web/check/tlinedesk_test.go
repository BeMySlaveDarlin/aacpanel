package check

import (
	"regexp"
	"strings"
	"testing"
)

// At a desk the timeline is a column beside the prose: an entry says when the
// work began, what it was and how long it took, three of its calls by name
// with the rest counted, and the files it touched, the written ones told from
// the read.
func TestTheDeskColumnSaysTheWorkOfEachRun(t *testing.T) {
	var got struct {
		Entries []struct {
			Time, Sum string
			Took      string
			Calls     []string
			More      string
			Files     []string
		}
	}
	runWideFixture(t, "tlinedesk.html", &got)
	if len(got.Entries) != 4 {
		t.Fatalf("entries drawn: %+v", got.Entries)
	}
	e := got.Entries[0]
	if e.Sum != "4 commands · 2 files" || e.Took != "8s" || !regexp.MustCompile(`^\d{2}:\d{2}$`).MatchString(e.Time) {
		t.Errorf("the first entry reads %q %q %q", e.Time, e.Sum, e.Took)
	}
	if strings.Join(e.Calls, " | ") != "make step0 | git status --short | go vet ./..." || e.More != "+1 more call" {
		t.Errorf("the calls of the entry: %v, %q", e.Calls, e.More)
	}
	if strings.Join(e.Files, " ") != "md.js* hl.js" {
		t.Errorf("the files of the entry: %v — the written one marked", e.Files)
	}
}
