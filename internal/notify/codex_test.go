package notify

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func codexOn(release string) World {
	w := snapshotWorld()
	w.CodexSeen = true
	w.Codex = []Codex{{Contour: "personal", Release: release, Reference: "0.162.1"}}
	return w
}

// A daemon of codex that moved past the release its protocol was checked
// against is said once for that release: the looks after it say nothing, a
// release further on is said once more, and a daemon on the release of the
// reference is never said — nor when it is brought level.
func TestACodexReleasePastTheReferenceIsSaidOnce(t *testing.T) {
	ctx := context.Background()
	journal := newJournal()
	tr := started(journal)
	// The blind snapshot below has news of its own: only what codex says counts.
	step := func(prev, cur World) []Message { return ofCodex(tr.Step(ctx, Look(prev, cur))) }

	level, moved, further := codexOn("0.162.1"), codexOn("0.163.0"), codexOn("0.164.0")
	if said := step(snapshotWorld(), level); len(said) > 0 {
		t.Fatalf("a daemon on the release of the reference was said: %+v", said)
	}
	said := step(level, moved)
	if len(said) != 1 || said[0].Kind != KindCodex || !strings.Contains(said[0].Body, "moved to 0.163.0") ||
		!strings.Contains(said[0].Body, "checked against 0.162.1") || !strings.Contains(said[0].Body, "make codex-contract") {
		t.Fatalf("a daemon past the reference was said as %+v, meant one push naming both releases", said)
	}
	for range 3 {
		if again := step(moved, moved); len(again) > 0 {
			t.Fatalf("the same release was said again on the next look: %+v", again)
		}
	}

	blind := snapshotWorld()
	blind.AgentErr = "the snapshot cannot be parsed"
	step(moved, blind)
	if again := step(blind, moved); len(again) > 0 {
		t.Errorf("the release was said again after the snapshot went blind for a look: %+v", again)
	}
	if again := ofCodex(NewTracker(journal).Step(ctx, Look(moved, moved))); len(again) > 0 {
		t.Errorf("the release was said again after the service started again: %+v", again)
	}

	if said := step(moved, further); len(said) != 1 || !strings.Contains(said[0].Body, "moved to 0.164.0") {
		t.Errorf("a release further on was said as %+v, meant one push", said)
	}
	if said := step(further, level); len(said) > 0 {
		t.Errorf("a daemon brought level to the reference was said: %+v", said)
	}
}

func ofCodex(said []Message) []Message {
	var out []Message
	for _, m := range said {
		if m.Kind == KindCodex {
			out = append(out, m)
		}
	}
	return out
}

// A release the snapshot does not name, on either side, is no reason: the
// handshake may name none, and a collector that does not carry the releases
// says nothing of them.
func TestAnUnknownReleaseIsNoReason(t *testing.T) {
	for _, c := range []Codex{
		{Contour: "personal", Release: "", Reference: "0.162.1"},
		{Contour: "personal", Release: "0.163.0", Reference: ""},
	} {
		w := snapshotWorld()
		w.CodexSeen = true
		w.Codex = []Codex{c}
		if r := Look(snapshotWorld(), w); len(r.Raise) > 0 {
			t.Errorf("%+v raised %v", c, keys(r.Raise))
		}
	}
	unseen := codexOn("0.163.0")
	unseen.CodexSeen = false
	if r := Look(snapshotWorld(), unseen); len(r.Raise) > 0 || has(r.Seen, domainCodex) {
		t.Errorf("a snapshot without the releases was judged: raised %v, seen %v", keys(r.Raise), r.Seen)
	}
}

// The person may turn the news of codex off, and its push carries the button
// that does it.
func TestTheNewsOfCodexCanBeTurnedOff(t *testing.T) {
	m := codexOn("0.163.0")
	e := only(t, Look(snapshotWorld(), m), "codex:personal:0.163.0").raised()
	if !(Prefs{}).Allows(e) {
		t.Error("the news of codex was held back by an empty choice")
	}
	off := Prefs{Off: []string{KindCodex}}.Clean()
	if !reflect.DeepEqual(off.Off, []string{KindCodex}) || off.Allows(e) {
		t.Errorf("the news of codex was not turned off: the choice keeps %v off", off.Off)
	}
	want := &Quiet{What: "off", Key: KindCodex, Label: "Quiet: codex updates"}
	if got := quietOf(e.Kind, e.Source); !reflect.DeepEqual(got, want) {
		t.Errorf("the push carries %+v, want %+v", got, want)
	}
}
