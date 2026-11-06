package check

import (
	"regexp"
	"strings"
	"testing"
)

type deskBriefShot struct {
	Before struct {
		SendDisabled bool   `json:"sendDisabled"`
		Say          string `json:"say"`
	} `json:"before"`
	After struct {
		SendDisabled bool   `json:"sendDisabled"`
		Say          string `json:"say"`
	} `json:"after"`
	Sent string `json:"sent"`
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
