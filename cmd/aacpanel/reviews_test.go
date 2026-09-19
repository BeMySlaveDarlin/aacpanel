package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/store"
)

// A note has to be findable again after the branch moves, and the only thing
// that finds it is the text of its line. A note without one would quietly sit
// on whatever ended up at that number later.
func TestANoteWithoutItsPlaceIsRefused(t *testing.T) {
	good := store.ReviewNote{ID: "n1", Path: "pkg/env.go", Line: 74, Quote: "\tif warn != \"\" {", Text: "reads twice"}

	cases := map[string]store.ReviewNote{
		"no name of its own": {Path: good.Path, Line: good.Line, Quote: good.Quote, Text: good.Text},
		"no file":            {ID: "n1", Line: good.Line, Quote: good.Quote, Text: good.Text},
		"no line":            {ID: "n1", Path: good.Path, Quote: good.Quote, Text: good.Text},
		"nothing written":    {ID: "n1", Path: good.Path, Line: good.Line, Quote: good.Quote},
	}
	for name, note := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := cleanNotes([]store.ReviewNote{note}); err == nil {
				t.Error("taken into a reading")
			}
		})
	}

	if _, err := cleanNotes([]store.ReviewNote{good}); err != nil {
		t.Errorf("a whole note was refused: %v", err)
	}
}

// Two notes under one name is a reading where deleting one deletes the other.
func TestTwoNotesCannotShareAName(t *testing.T) {
	note := store.ReviewNote{ID: "n1", Path: "pkg/env.go", Line: 74, Quote: "x", Text: "first"}
	other := note
	other.Text = "second"

	if _, err := cleanNotes([]store.ReviewNote{note, other}); err == nil {
		t.Error("two notes went in under one name")
	}
}

// The ceilings are held here because a request is where a number too large
// arrives: the database would take it and the screen would have to draw it.
func TestAReadingIsHeldToWhatAScreenCanDraw(t *testing.T) {
	long := store.ReviewNote{ID: "n1", Path: "pkg/env.go", Line: 1,
		Quote: strings.Repeat("q", maxNoteQuote+50), Text: strings.Repeat("t", maxNoteText+50)}

	out, err := cleanNotes([]store.ReviewNote{long})
	if err != nil {
		t.Fatalf("a long note was refused outright: %v", err)
	}
	if len(out[0].Text) != maxNoteText || len(out[0].Quote) != maxNoteQuote {
		t.Errorf("the note came back %d and %d long", len(out[0].Text), len(out[0].Quote))
	}

	many := make([]store.ReviewNote, maxNotes+1)
	for i := range many {
		many[i] = store.ReviewNote{ID: string(rune('a'+i%26)) + strings.Repeat("x", i), Path: "f", Line: 1, Quote: "q", Text: "t"}
	}
	if _, err := cleanNotes(many); err == nil {
		t.Error("a reading of more notes than the ceiling was taken")
	}
}

// The name of a reading becomes the name of a file on a shelf, so it is made
// here and never taken from what arrives.
func TestTheNameOfAReadingIsMadeHere(t *testing.T) {
	at := time.Date(2026, 9, 19, 8, 15, 0, 0, time.UTC)

	if got := reviewName("aacpanel", at); got != "aacpanel-20260919-081500" {
		t.Errorf("the reading is named %q", got)
	}
	if got := reviewName("../../etc/passwd", at); strings.ContainsAny(got, "./") {
		t.Errorf("a path got into the name of a reading: %q", got)
	}
	if got := reviewName("!!! ???", at); !strings.HasPrefix(got, "reading-") {
		t.Errorf("a name with nothing to keep became %q, expected a plain one", got)
	}
}

// A name that arrives in a path is checked before it is used, for the same
// reason.
func TestANameThatIsNotAReadingIsRefused(t *testing.T) {
	for _, name := range []string{"../etc", "a/b", "a.json", "", strings.Repeat("x", 200)} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/reviews/x", nil)
		r.SetPathValue("id", name)
		if _, ok := reviewID(w, r); ok {
			t.Errorf("%q was taken for the name of a reading", name)
		}
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/reviews/x", nil)
	r.SetPathValue("id", "aacpanel-20260919-081500")
	if _, ok := reviewID(w, r); !ok {
		t.Error("a plain name was refused")
	}
}

// Without a database the panel says so rather than answering as if a reading
// had been kept.
func TestReviewsWithoutADatabaseSayWhy(t *testing.T) {
	srv := &Server{}
	calls := []struct {
		name   string
		method string
		run    func(http.ResponseWriter, *http.Request)
	}{
		{"index", http.MethodGet, srv.apiReviews},
		{"one reading", http.MethodGet, srv.apiReview},
		{"draft", http.MethodPut, srv.apiReviewDraft},
		{"sent", http.MethodPost, srv.apiReviewSent},
		{"drop", http.MethodDelete, srv.apiReviewDrop},
	}
	for _, call := range calls {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(call.method, "/api/reviews/x",
			strings.NewReader(`{"session":"lab","path":"/var/lib/aacpanel/reviews/x.json","notes":[]}`))
		r.SetPathValue("id", "lab-20260919-081500")
		call.run(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with no database gave %d, expected 503", call.name, w.Code)
		}
	}
}
