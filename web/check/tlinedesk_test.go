package check

import (
	"regexp"
	"strings"
	"testing"
)

// At a desk the timeline is a column beside the prose: an entry says when the
// work began, what it was and how long it took, three of its calls by name
// with the rest counted, and the files it touched, the written ones told from
// the read. An entry stands at the height of the reply its work led to, or,
// when the entry above is still in the way, right under it — never over it.
func TestTheDeskColumnSaysTheWorkBesideItsReply(t *testing.T) {
	var got struct {
		Entries []struct {
			Top, Bottom float64
			Time, Sum   string
			Took        string
			Calls       []string
			More        string
			Files       []string
		}
		Rows     []float64
		ColRight float64
		LineLeft float64
	}
	runWideFixture(t, "tlinedesk.html", &got)
	if len(got.Entries) != 4 {
		t.Fatalf("entries drawn: %+v", got.Entries)
	}
	if got.LineLeft < got.ColRight {
		t.Errorf("the column of the timeline starts at %.1f, inside the prose that ends at %.1f", got.LineLeft, got.ColRight)
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
	// The replies after the runs are rows 1, 2, 3 and 4 of the column.
	if d := e.Top - got.Rows[1]; d < -1 || d > 1 {
		t.Errorf("the first entry stands %.1fpx off its reply", d)
	}
	pushed := 0
	for i := 1; i < len(got.Entries); i++ {
		prev, cur := got.Entries[i-1], got.Entries[i]
		if cur.Top < prev.Bottom {
			t.Errorf("entry %d starts at %.1f, over the entry above that ends at %.1f", i, cur.Top, prev.Bottom)
		}
		if cur.Top < got.Rows[i+1]-1 {
			t.Errorf("entry %d stands at %.1f, above its reply at %.1f", i, cur.Top, got.Rows[i+1])
		}
		if cur.Top > got.Rows[i+1]+1 {
			pushed++
		}
	}
	if pushed == 0 {
		t.Error("no entry had to stand under another: the fixture does not crowd the column")
	}
}
