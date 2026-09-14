package notify

import (
	"context"
	"strings"
	"testing"
)

// A session can call the person itself: not a question and not a request for a
// permission, just a line for a phone. It reaches them once, it names the
// session so a tap opens it, and it says nothing more when the panel looks
// again while the call is still standing.
func TestASessionCallingReachesThePhoneOnce(t *testing.T) {
	calm := sess()
	calling := sess()
	calling.Note = &Note{Text: "stuck on the migration, need you", At: "2026-09-08T03:00:00Z"}

	r := Look(
		World{Now: when, Sessions: []Session{calm}},
		World{Now: when, Sessions: []Session{calling}},
	)
	e := only(t, r, "note:4d89ed41:2026-09-08T03:00:00Z")
	if e.Session != "aacpanel" {
		t.Errorf("the call names the session as %q — a tap on the push opens nothing", e.Session)
	}
	if !strings.Contains(e.Body, "stuck on the migration, need you") {
		t.Errorf("the line the session sent is not in the push: %q", e.Body)
	}
	if !strings.Contains(e.Title, "aacpanel") {
		t.Errorf("the title does not say who is calling: %q", e.Title)
	}
	if e.Severity != Critical {
		t.Errorf("a call arrives as %q — a session calls when it needs the person now", e.Severity)
	}
	if len(r.Hold) != 0 {
		t.Errorf("the call is held like a standing condition: %v — it is one line, said once", r.Hold)
	}

	tracker := started(newJournal())
	said := tracker.Step(context.Background(), r)
	if len(said) != 1 {
		t.Fatalf("%d messages went out for one call", len(said))
	}
	again := tracker.Step(context.Background(), Look(
		World{Now: when, Sessions: []Session{calling}},
		World{Now: when, Sessions: []Session{calling}},
	))
	if len(again) != 0 {
		t.Errorf("the same call went out again on the next look: %+v — the phone buzzes every twenty seconds", again)
	}
}

// Two calls from one session are two lines, not one repeated: the panel keys
// them by the moment they were made.
func TestASecondCallIsItsOwnPush(t *testing.T) {
	first := sess()
	first.Note = &Note{Text: "need you", At: "2026-09-08T03:00:00Z"}
	second := sess()
	second.Note = &Note{Text: "still need you", At: "2026-09-08T03:02:00Z"}

	tracker := started(newJournal())
	said := tracker.Step(context.Background(), Look(
		World{Now: when, Sessions: []Session{sess()}},
		World{Now: when, Sessions: []Session{first}},
	))
	said = append(said, tracker.Step(context.Background(), Look(
		World{Now: when, Sessions: []Session{first}},
		World{Now: when, Sessions: []Session{second}},
	))...)

	if len(said) != 2 {
		t.Fatalf("%d messages for two calls: %+v", len(said), said)
	}
	if !strings.Contains(said[1].Body, "still need you") {
		t.Errorf("the second call carried the words of the first: %q", said[1].Body)
	}
	if said[0].Tag == said[1].Tag {
		t.Errorf("both calls wear the tag %q — the phone takes the second for a correction of the "+
			"first and replaces it, so the earlier line is gone before it was read", said[0].Tag)
	}
}
