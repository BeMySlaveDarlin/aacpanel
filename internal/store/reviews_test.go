package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"aacpanel/internal/testdb"
)

func openStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := testdb.DSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

func note(id, text string) ReviewNote {
	return ReviewNote{ID: id, Path: "pkg/env.go", Line: 74, Quote: "\tif warn != \"\" {", Text: text, At: time.Now().UTC()}
}

// A reading survives the tab it was written in: the phone locks, the browser
// drops the page, the reading moves to the desk, and what was written down is
// still there.
func TestAReadingOutlivesTheScreenItWasWrittenOnPG(t *testing.T) {
	s, ctx := openStore(t)
	id := "r-outlives-" + testdb.RunTag()

	want := Review{ID: id, Session: "lab", Cwd: "/srv/proj", Base: "main",
		Notes: []ReviewNote{note("n1", "why not the other branch"), note("n2", "this reads twice")}}
	if err := s.SaveReview(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.ReviewOf(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 2 || got.Notes[1].Text != "this reads twice" {
		t.Fatalf("the notes came back as %+v", got.Notes)
	}
	if got.Notes[0].Quote == "" {
		t.Error("the quote of the line is gone — without it a note cannot be found again when the file moves")
	}
	if got.SentAt != nil {
		t.Error("a reading nobody sent came back as sent")
	}
}

// A reading nobody has started is empty, not missing: the screen opens it the
// same way whether there is anything in it or not.
func TestAReadingNobodyStartedIsEmptyNotMissingPG(t *testing.T) {
	s, ctx := openStore(t)

	got, err := s.ReviewOf(ctx, "r-never-touched-"+testdb.RunTag())
	if err != nil {
		t.Fatalf("an unstarted reading answered with an error: %v", err)
	}
	if got.Notes == nil {
		t.Error("the notes came back as nothing at all — a screen reading their length dies on it")
	}
	if len(got.Notes) != 0 {
		t.Errorf("an unstarted reading has %d notes", len(got.Notes))
	}
}

// What was sent cannot be changed. The session holds a file; a note edited
// afterwards would leave the shelf and the panel telling different stories
// with no way to say which is right.
func TestASentReadingRefusesEveryChangePG(t *testing.T) {
	s, ctx := openStore(t)
	id := "r-sent-" + testdb.RunTag()

	if err := s.SaveReview(ctx, Review{ID: id, Session: "lab", Cwd: "/srv/proj",
		Notes: []ReviewNote{note("n1", "first")}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReviewSent(ctx, id, "/var/lib/aacpanel/reviews/"+id+".json"); err != nil {
		t.Fatal(err)
	}

	err := s.SaveReview(ctx, Review{ID: id, Session: "lab", Cwd: "/srv/proj",
		Notes: []ReviewNote{note("n1", "second thoughts")}})
	if !errors.Is(err, ErrReviewSent) {
		t.Errorf("a sent reading took an edit: %v", err)
	}
	if err := s.MarkReviewSent(ctx, id, "/elsewhere.json"); !errors.Is(err, ErrReviewSent) {
		t.Errorf("a sent reading was sent a second time: %v", err)
	}
	if err := s.DropReview(ctx, id); !errors.Is(err, ErrReviewSent) {
		t.Errorf("a sent reading was deleted: %v", err)
	}

	got, err := s.ReviewOf(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.SentAt == nil {
		t.Fatal("the reading does not remember being sent")
	}
	if got.Notes[0].Text != "first" {
		t.Errorf("the note now reads %q — what was sent has to stay what it was", got.Notes[0].Text)
	}
	if got.Path == "" {
		t.Error("the index does not say where the file landed")
	}
}

// The index answers for every reading at once, newest first — that is what it
// is for, and opening them one at a time is what it saves.
func TestTheIndexCarriesEveryReadingWithItsStatusPG(t *testing.T) {
	s, ctx := openStore(t)
	tag := testdb.RunTag()
	session := "idx-" + tag

	older := "r-older-" + tag
	newer := "r-newer-" + tag
	if err := s.SaveReview(ctx, Review{ID: older, Session: session, Cwd: "/srv/proj",
		Notes: []ReviewNote{note("n1", "older")}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReviewSent(ctx, older, "/var/lib/aacpanel/reviews/"+older+".json"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReview(ctx, Review{ID: newer, Session: session, Cwd: "/srv/proj",
		Notes: []ReviewNote{note("n1", "newer")}}); err != nil {
		t.Fatal(err)
	}

	list, err := s.Reviews(ctx, session, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("the index of %s holds %d readings", session, len(list))
	}
	if list[0].ID != newer {
		t.Errorf("the index opens with %s — the newest reading comes first", list[0].ID)
	}
	if list[0].SentAt != nil || list[1].SentAt == nil {
		t.Error("the index does not tell a draft from a reading that has gone")
	}
	for _, r := range list {
		if r.Notes == nil {
			t.Errorf("the notes of %s travel as nothing at all", r.ID)
		}
	}
}

// A draft is deleted by whoever wrote it; that is the only reading that can be.
func TestADraftIsDroppedAndTheIndexForgetsItPG(t *testing.T) {
	s, ctx := openStore(t)
	id := "r-drop-" + testdb.RunTag()
	session := "drop-" + testdb.RunTag()

	if err := s.SaveReview(ctx, Review{ID: id, Session: session, Cwd: "/srv/proj",
		Notes: []ReviewNote{note("n1", "never mind")}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DropReview(ctx, id); err != nil {
		t.Fatal(err)
	}

	list, err := s.Reviews(ctx, session, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("the index still holds %d readings after the draft was dropped", len(list))
	}
}
