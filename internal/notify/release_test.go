package notify

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// releaseOut is a look of the host once GitHub answered, with latest out
// and the panel on running; nothing newer when the two are the same.
func releaseOut(running, latest string) World {
	w := snapshotWorld()
	w.ReleaseSeen = true
	if latest != running {
		w.Release = &PanelRelease{Running: running, Latest: latest, Update: "./install.sh update"}
	}
	return w
}

func ofRelease(said []Message) []Message {
	var out []Message
	for _, m := range said {
		if m.Kind == KindRelease {
			out = append(out, m)
		}
	}
	return out
}

// A release of the panel newer than the one it runs is said once: the looks
// after it say nothing, nor a restart of the service while GitHub has not
// answered yet, a release further on is said once more, and an update onto
// the latest is forgotten without a word.
func TestANewerReleaseOfThePanelIsSaidOnce(t *testing.T) {
	ctx := context.Background()
	journal := newJournal()
	tr := started(journal)
	step := func(prev, cur World) []Message { return ofRelease(tr.Step(ctx, Look(prev, cur))) }

	level, out, further := releaseOut("v1.2.0", "v1.2.0"), releaseOut("v1.2.0", "v1.3.0"), releaseOut("v1.2.0", "v1.4.0")
	if said := step(snapshotWorld(), level); len(said) > 0 {
		t.Fatalf("a panel on the latest release was told of it: %+v", said)
	}
	said := step(level, out)
	if len(said) != 1 || !strings.Contains(said[0].Title, "v1.3.0 is out") ||
		!strings.Contains(said[0].Body, "runs v1.2.0") || !strings.Contains(said[0].Body, "./install.sh update") {
		t.Fatalf("a newer release was said as %+v, meant one push naming both releases and the update", said)
	}
	for range 3 {
		if again := step(out, out); len(again) > 0 {
			t.Fatalf("the same release was said again on the next look: %+v", again)
		}
	}

	// A service started again has not asked GitHub yet: until it has, the
	// reason it told stays told.
	tr = NewTracker(journal)
	if again := step(out, snapshotWorld()); len(again) > 0 {
		t.Errorf("the release was said on the first look after a restart: %+v", again)
	}
	if again := step(snapshotWorld(), out); len(again) > 0 {
		t.Errorf("the release was said again once GitHub answered after a restart: %+v", again)
	}

	if said := step(out, further); len(said) != 1 || !strings.Contains(said[0].Title, "v1.4.0") {
		t.Errorf("a release further on was said as %+v, meant one push", said)
	}
	updated := releaseOut("v1.4.0", "v1.4.0")
	if said := step(further, updated); len(said) > 0 {
		t.Errorf("an update onto the latest release was said: %+v", said)
	}
	if _, told := tr.active["release:v1.4.0"]; told {
		t.Error("the reason of v1.4.0 is kept after the update onto it: the next release of the same number would pass in silence")
	}
}

// Before GitHub answered, the look judges nothing of releases: a held reason
// is neither raised nor let go.
func TestNoAnswerFromGitHubIsNoReason(t *testing.T) {
	unseen := releaseOut("v1.2.0", "v1.3.0")
	unseen.ReleaseSeen = false
	if r := Look(snapshotWorld(), unseen); len(r.Raise) > 0 || has(r.Seen, domainRelease) {
		t.Errorf("a look before GitHub answered was judged: raised %v, seen %v", keys(r.Raise), r.Seen)
	}
}

// The person may turn the news of releases off, and its push carries the
// button that does it.
func TestTheNewsOfReleasesCanBeTurnedOff(t *testing.T) {
	e := only(t, Look(snapshotWorld(), releaseOut("v1.2.0", "v1.3.0")), "release:v1.3.0").raised()
	if !(Prefs{}).Allows(e) {
		t.Error("the news of a release was held back by an empty choice")
	}
	off := Prefs{Off: []string{KindRelease}}.Clean()
	if !reflect.DeepEqual(off.Off, []string{KindRelease}) || off.Allows(e) {
		t.Errorf("the news of releases was not turned off: the choice keeps %v off", off.Off)
	}
	want := &Quiet{What: "off", Key: KindRelease, Label: "Quiet: panel releases"}
	if got := quietOf(e.Kind, e.Source); !reflect.DeepEqual(got, want) {
		t.Errorf("the push carries %+v, want %+v", got, want)
	}
	if muted, ok := (Prefs{}).Mute(*want); !ok || !reflect.DeepEqual(muted.Off, []string{KindRelease}) {
		t.Errorf("the button turned off %v (%v), want the releases", muted.Off, ok)
	}
}
