package check

import (
	"strings"
	"testing"
)

// One message written once has to leave once, and stand in the feed as one
// bubble in every frame. The screen has two ways out: straight into a session
// that is handling a request, and held back until the session has moved past
// the question it was answering. Both are driven here through the real screen
// in a real engine, with the host counting what it was actually sent.
type sendRun struct {
	BusyOnce  sendScene `json:"busyOnce"`
	BusyTwice sendScene `json:"busyTwice"`
	HeldOnce  sendScene `json:"heldOnce"`
	HeldTwice sendScene `json:"heldTwice"`
	HeldGhost sendScene `json:"heldGhost"`
	BusyChurn sendScene `json:"busyChurn"`
	HeldChurn sendScene `json:"heldChurn"`
}

type sendScene struct {
	Sends     int      `json:"sends"`
	Held      int      `json:"held"`
	HoldState string   `json:"holdState"`
	Bubbles   int      `json:"bubbles"`
	Most      int      `json:"most"`
	State     string   `json:"state"`
	Texts     []string `json:"texts"`
}

func TestAMessageWrittenOnceLeavesOnce(t *testing.T) {
	var got sendRun
	runFixture(t, "sendonce.html", &got)

	one := func(name string, scene sendScene, harm string) {
		t.Helper()
		if scene.Sends != 1 {
			t.Errorf("%s: the host was sent the message %d times instead of once — %s (it was sent %q)",
				name, scene.Sends, harm, strings.Join(scene.Texts, " | "))
		}
		if scene.Bubbles != 1 {
			t.Errorf("%s: the feed ends with %d copies of the message instead of one", name, scene.Bubbles)
		}
		if scene.Most != 1 {
			t.Errorf("%s: %d copies of the message shared one frame — the feed showed the message twice",
				name, scene.Most)
		}
		if scene.State != "queued" {
			t.Errorf("%s: the message settled as %q instead of queued", name, scene.State)
		}
	}

	one("a busy session, one press", got.BusyOnce,
		"a plain send goes out more than once")
	one("a busy session, two presses in one frame", got.BusyTwice,
		"the press is guarded by state written for the next draw, so a phone answering one "+
			"touch with a second click sends the draft again")
	one("a busy session redrawn while the message is in the air", got.BusyChurn,
		"the agent snapshot redrawing the screen makes the send repeat")

	held := func(name string, scene sendScene, harm string) {
		t.Helper()
		if scene.Held != 1 {
			t.Errorf("%s: the session held %d rows instead of one — %s", name, scene.Held, harm)
		}
		if scene.HoldState == "" {
			t.Errorf("%s: a row waiting for the session does not say so", name)
		}
		one(name, scene, harm)
	}

	held("a session still showing the answered question, one press", got.HeldOnce,
		"the held row leaves more than once when the session frees")
	held("a session still showing the answered question, two presses in one frame", got.HeldGhost,
		"the held branch takes the press without a latch of any kind, so the second click of one "+
			"touch adds a second row and a second send")
	held("a session freed while the held row is in the air", got.HeldChurn,
		"the effect that releases held rows fires again on a redraw and sends the row twice")

	if got.HeldTwice.Sends != 2 || got.HeldTwice.Held != 2 || got.HeldTwice.Bubbles != 2 {
		t.Errorf("two messages written one after the other while the session was busy came out as "+
			"%d sends, %d held rows and %d bubbles instead of two of each — holding back a message "+
			"has stopped working", got.HeldTwice.Sends, got.HeldTwice.Held, got.HeldTwice.Bubbles)
	}
	if len(got.HeldTwice.Texts) == 2 && got.HeldTwice.Texts[0] == got.HeldTwice.Texts[1] {
		t.Errorf("both held rows sent the same text (%q) — the queue lost one of the two messages",
			got.HeldTwice.Texts[0])
	}
}
