package check

import (
	"regexp"
	"strings"
	"testing"
)

type deskBriefShot struct {
	Before struct {
		Root         float64 `json:"root"`
		Column       int     `json:"column"`
		Prose        float64 `json:"prose"`
		Line         int     `json:"line"`
		Room         int     `json:"room"`
		Heading      float64 `json:"heading"`
		SendDisabled bool    `json:"sendDisabled"`
		Say          string  `json:"say"`
	} `json:"before"`
	After struct {
		SendDisabled bool   `json:"sendDisabled"`
		Say          string `json:"say"`
	} `json:"after"`
	Sent string `json:"sent"`
}

// A brief is a document read for the better part of an hour, and a desk is not
// a hand. The column and the type of it follow the scale of the shell, so the
// person who draws the interface larger draws the document larger with it.
func TestBriefAtADeskIsReadAtDeskSize(t *testing.T) {
	var desk deskBriefShot
	runWideFixture(t, "briefdesk.html", &desk)

	// The desktop page holds every screen on it to a reading width of its own.
	// A document that takes that width instead of its own is narrower than
	// either of them was drawn to be.
	// The column takes the width the page has for it, up to the measure the
	// document sets itself. Held to anything narrower — a reading width meant
	// for another screen — it stands as a strip with the room empty around it.
	want := 82.0 * desk.Before.Root
	if room := float64(desk.Before.Room); want > room {
		want = room
	}
	if float64(desk.Before.Column) < want-2 {
		t.Errorf("the reading column is %d px of the %d px the page offers, on a %.0f px root — the document is being held to somebody else's width (wanted about %.0f)",
			desk.Before.Column, desk.Before.Room, desk.Before.Root, want)
	}
	if desk.Before.Prose <= desk.Before.Root {
		t.Errorf("the prose is %.2f px on a %.2f px root: at a desk the document reads one step above the panel around it",
			desk.Before.Prose, desk.Before.Root)
	}
	// The column is only half the answer: the line inside it is held to a
	// measure of its own, and a wide column with a narrow measure reads as the
	// phone's strip with room wasted around it.
	if desk.Before.Line < 70 {
		t.Errorf("a line of prose runs %d characters — at a desk the measure is wider than a hand's", desk.Before.Line)
	}
	if desk.Before.Heading <= desk.Before.Prose {
		t.Errorf("a question heading (%.2f px) does not stand above the prose under it (%.2f px)", desk.Before.Heading, desk.Before.Prose)
	}

	var phone deskBriefShot
	runFixture(t, "briefdesk.html", &phone)
	if deskStep, phoneStep := desk.Before.Prose/desk.Before.Root, phone.Before.Prose/phone.Before.Root; deskStep <= phoneStep {
		t.Errorf("the document reads at the phone's step on a monitor: %.3f of the root against %.3f", deskStep, phoneStep)
	}
}

// The answers of a brief go into a session, and the session is found in the
// snapshot the panel hands down. Without it the document says the session that
// wrote the brief is gone and bars the send — on a screen where it is running.
func TestBriefAtADeskCanSendItsAnswers(t *testing.T) {
	var got deskBriefShot
	runWideFixture(t, "briefdesk.html", &got)

	if !got.Before.SendDisabled {
		t.Error("a brief with nothing answered offers to send: there is nothing to send yet")
	}
	if got.After.SendDisabled {
		t.Errorf("an answered brief still cannot be sent, and the dock says: %q", got.After.Say)
	}
	if got.Sent != "lab" {
		t.Errorf("the answers went to %q, not into the session that wrote the brief", got.Sent)
	}
}

// Every screen that opens a conversation hands it the snapshot. A brief opens
// as a layer of the run, and it finds the session its answers go into there
// and nowhere else: a screen that leaves the prop out bars the send with a
// line about a session that is in fact running.
func TestEveryConversationGetsTheSnapshot(t *testing.T) {
	calls := regexp.MustCompile(`(?s)<\$\{Chat\}(.*?)/>`)
	for _, path := range sortedKeys(srcFiles(t)) {
		src := srcFiles(t)[path]
		for _, call := range calls.FindAllStringSubmatch(src, -1) {
			if !strings.Contains(call[1], "snapshot=") {
				t.Errorf("%s opens a conversation without the snapshot — a brief opened in it cannot find the session to answer", path)
			}
		}
	}
}

// A conversation opens its own brief as a layer over the run, the way it
// opens a file or a page: the document sits on top of the feed and the feed
// is exactly where it was once the sheet closes. There is no case where the
// reader is meant to leave the conversation for it, so Chat has no prop for a
// caller to hand a brief through — one that still passes onBrief is feeding
// a channel nothing on the other end reads.
func TestNoConversationIsHandedABriefChannel(t *testing.T) {
	calls := regexp.MustCompile(`(?s)<\$\{Chat\}(.*?)/>`)
	for _, path := range sortedKeys(srcFiles(t)) {
		src := srcFiles(t)[path]
		for _, call := range calls.FindAllStringSubmatch(src, -1) {
			if strings.Contains(call[1], "onBrief=") {
				t.Errorf("%s hands the conversation an onBrief prop — a brief opens as a layer over the run and Chat reads no such prop", path)
			}
		}
	}
}

// The shelf is one screen with two states: the cards, and the document that a
// card opens. Which one it draws is told from outside, so a shell that names
// the prop something of its own gets the cards and nothing else — and the card
// it clicks calls a handler that was never passed.
func TestEveryShelfIsToldWhichBriefIsOpen(t *testing.T) {
	calls := regexp.MustCompile(`(?s)<\$\{Briefs\}(.*?)/>`)
	files := srcFiles(t)
	for _, path := range sortedKeys(files) {
		for _, call := range calls.FindAllStringSubmatch(files[path], -1) {
			for _, prop := range []string{"open=", "onOpen="} {
				if !strings.Contains(call[1], prop) {
					t.Errorf("%s draws the shelf without %s — the documents on it do not open", path, prop)
				}
			}
		}
	}
}
