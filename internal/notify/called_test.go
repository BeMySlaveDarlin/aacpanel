package notify

import (
	"context"
	"strings"
	"testing"
	"time"
)

func call(text, at string) Note {
	return Note{Session: sess().ID, Text: text, At: at}
}

// A session can call the person itself: not a question and not a request for a
// permission, just a line for a phone. It reaches them once, it names the
// session so a tap opens it, and it says nothing more while the collector
// still lists it.
func TestASessionCallingReachesThePhoneOnce(t *testing.T) {
	var calls Calls
	board := []Note{call("stuck on the migration, need you", "2026-09-08T03:00:00Z")}

	news, waiting := calls.Fresh(when, board, []Session{sess()})
	if waiting {
		t.Error("a call of a session the snapshot shows is taken for one waiting for its session")
	}
	if len(news) != 1 {
		t.Fatalf("%d pushes for one call: %+v", len(news), news)
	}
	e := news[0]
	if e.Key != "note:4d89ed41:2026-09-08T03:00:00Z" {
		t.Errorf("the call is keyed %q — the phone and the journal tell calls apart by the key", e.Key)
	}
	if e.Session != "aacpanel" {
		t.Errorf("the call names the session as %q — a tap on the push opens nothing", e.Session)
	}
	if !strings.Contains(e.Body, "stuck on the migration, need you") {
		t.Errorf("the line the session sent is not in the push: %q", e.Body)
	}
	if !strings.Contains(e.Body, "personal · aacpanel") {
		t.Errorf("the push does not say where the session works: %q", e.Body)
	}
	if !strings.Contains(e.Title, "aacpanel") {
		t.Errorf("the title does not say who is calling: %q", e.Title)
	}
	if e.Severity != Critical {
		t.Errorf("a call arrives as %q — a session calls when it needs the person now", e.Severity)
	}
	if kindOf(e.Key) != KindCall {
		t.Errorf("the call is of the kind %q — the choice of pushes would take it for something else", kindOf(e.Key))
	}

	tracker := started(newJournal())
	said := tracker.Step(context.Background(), Report{Raise: news})
	if len(said) != 1 {
		t.Fatalf("%d messages went out for one call", len(said))
	}
	again, _ := calls.Fresh(when.Add(20*time.Second), board, []Session{sess()})
	if len(again) != 0 {
		t.Errorf("the same call went out again while the collector lists it: %+v — the phone buzzes on every answer", again)
	}
}

// Two calls from one session are two lines, not one repeated: the panel keys
// them by the moment they were made.
func TestASecondCallIsItsOwnPush(t *testing.T) {
	var calls Calls
	first, _ := calls.Fresh(when, []Note{call("need you", "2026-09-08T03:00:00Z")}, []Session{sess()})
	second, _ := calls.Fresh(when.Add(2*time.Minute),
		[]Note{call("still need you", "2026-09-08T03:02:00Z")}, []Session{sess()})

	tracker := started(newJournal())
	said := tracker.Step(context.Background(), Report{Raise: first})
	said = append(said, tracker.Step(context.Background(), Report{Raise: second})...)

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

// A session the snapshot does not show yet has no name to title the push
// with and nothing for a tap to open: its call waits for it rather than going
// out anonymous, and goes out once it shows.
func TestACallWaitsForItsSessionToShow(t *testing.T) {
	var calls Calls
	board := []Note{call("need you", "2026-09-08T03:00:00Z")}

	news, waiting := calls.Fresh(when, board, nil)
	if len(news) != 0 {
		t.Fatalf("a call of a session nobody sees went out: %+v", news)
	}
	if !waiting {
		t.Fatal("the call of a session not in the snapshot is not said to wait — nobody would ask for it again soon")
	}

	news, waiting = calls.Fresh(when.Add(5*time.Second), board, []Session{sess()})
	if len(news) != 1 || news[0].Session != "aacpanel" {
		t.Fatalf("the call did not go out once its session showed: %+v", news)
	}
	if waiting {
		t.Error("the call still waits after it went out")
	}
}

// A carried call is remembered while the collector may still list it, and not
// for ever: the memory would grow with every call of every session.
func TestACarriedCallIsForgottenAfterTheCollectorDropsIt(t *testing.T) {
	var calls Calls
	calls.Fresh(when, []Note{call("need you", "2026-09-08T03:00:00Z")}, []Session{sess()})

	calls.Fresh(when.Add(callMemory+time.Second), nil, []Session{sess()})
	if len(calls.told) != 0 {
		t.Errorf("a call gone from the board for longer than the collector keeps one is still remembered: %v", calls.told)
	}
}

// A call goes out between two looks of the host. The report it comes in
// carries nothing seen, so what the tracker holds from the last look stays
// held: a container that is down is not declared up because a session called.
func TestACallBetweenLooksLeavesTheHeldAlone(t *testing.T) {
	ctx := context.Background()
	tracker := started(newJournal())
	down := alertEventFixture()
	tracker.Step(ctx, holding(down))

	var calls Calls
	news, _ := calls.Fresh(when, []Note{call("need you", "2026-09-08T03:00:00Z")}, []Session{sess()})
	said := tracker.Step(ctx, Report{Raise: news})
	if len(said) != 1 || said[0].Back {
		t.Fatalf("a call between two looks said %+v — only the call is news", said)
	}
	if again := tracker.Step(ctx, holding(down)); len(again) != 0 {
		t.Errorf("the alert held before the call was announced again after it: %+v", again)
	}
}

// A codex thread is named by the tail of its id, and its card reads by the
// name the panel gave it or the session of its project: every push of the
// thread is titled the way the card it opens reads, and the tap still opens
// the session by its name.
func TestAPushOfACodexThreadIsTitledAsItsCardReads(t *testing.T) {
	thread := Session{ID: "019a1f00-0000-7000-8000-00000000abcd", Name: "codex-0000abcd", Shown: "cart review",
		Profile: "personal", CWD: "/srv/proj/shop", Status: "waiting", StatusAt: 1788813894909,
		WaitingFor: "input needed", Ask: &Ask{Header: "Which way", Text: "A or B?", Count: 1, At: "2026-09-08T03:00:00Z"}}
	var calls Calls
	news, _ := calls.Fresh(when, []Note{{Session: thread.ID, Text: "stuck", At: "2026-09-08T03:00:00Z"}}, []Session{thread})
	if len(news) != 1 {
		t.Fatalf("the call of the thread made %d pushes", len(news))
	}
	for _, e := range []Event{news[0], asked(thread), waiting(thread), freed(thread, time.Minute), closed(thread)} {
		if !strings.Contains(e.Title, "cart review") || strings.Contains(e.Title, "codex-0000abcd") {
			t.Errorf("the push %s is titled %q", e.Key, e.Title)
		}
		if e.Session != "codex-0000abcd" {
			t.Errorf("the push %s opens %q", e.Key, e.Session)
		}
	}
	if e := Called(sess(), call("need you", "2026-09-08T03:00:00Z")); e.Title != "aacpanel is calling" {
		t.Errorf("a session with no other name is titled %q", e.Title)
	}
}
