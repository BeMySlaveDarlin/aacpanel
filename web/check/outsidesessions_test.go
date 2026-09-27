package check

import (
	"os"
	"strings"
	"testing"
)

// A claude the panel did not start is shown for what it is. The runs a session
// started inside its work fold under it in the desktop column and in the
// phone's list, and the fold says one of them waits for the person; a claude
// typed into a terminal by hand stands on its own, marked, with nothing to
// press. The conversation of either is only read: no terminal, no window, no
// move, no Remote Control, no end, and a note where the composer was.
func TestSessionsThePanelDidNotStartAreOnlyRead(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		DeskRows      []string `json:"deskRows"`
		DeskFold      string   `json:"deskFold"`
		DeskTyped     string   `json:"deskTyped"`
		DeskTypedActs int      `json:"deskTypedActs"`
		DeskOpen      []string `json:"deskOpen"`
		DeskKids      []string `json:"deskKids"`
		PhoneRows     []string `json:"phoneRows"`
		PhoneFold     string   `json:"phoneFold"`
		PhoneTags     []string `json:"phoneTags"`
		PhoneOpen     []string `json:"phoneOpen"`
		Sheet         []struct {
			Text string `json:"text"`
			Off  bool   `json:"off"`
		} `json:"sheet"`
		Tabs     []string `json:"tabs"`
		Place    string   `json:"place"`
		Composer bool     `json:"composer"`
		Note     string   `json:"note"`
		Panel    []string `json:"panel"`
	}
	runWideFixture(t, "outsidesessions.html", &got)

	if strings.Join(got.DeskRows, ",") != "rotation,typed" {
		t.Errorf("the desktop column shows %v folded, expected the session and the one typed by hand, its runs under a fold", got.DeskRows)
	}
	if !strings.Contains(got.DeskFold, "2 runs started by it") || !strings.Contains(got.DeskFold, "1 waiting") {
		t.Errorf("the fold reads %q: it has to count the runs and say one waits", got.DeskFold)
	}
	if !strings.Contains(got.DeskTyped, "outside") || got.DeskTypedActs != 0 {
		t.Errorf("the session typed by hand reads %q with %d actions: it is marked outside and has nothing to press", got.DeskTyped, got.DeskTypedActs)
	}
	if strings.Join(got.DeskKids, ",") != "rotation-25,rotation-e8" || len(got.DeskOpen) != 4 {
		t.Errorf("the open fold shows runs %v among %v", got.DeskKids, got.DeskOpen)
	}

	if strings.Join(got.PhoneRows, ",") != "rotation,typed" || !strings.Contains(got.PhoneFold, "2 runs started by it") {
		t.Errorf("the phone's list shows %v with the fold %q", got.PhoneRows, got.PhoneFold)
	}
	if len(got.PhoneTags) != 2 || got.PhoneTags[1] != "outside" {
		t.Errorf("the phone marks the rows %v — the session typed by hand is outside", got.PhoneTags)
	}
	if len(got.PhoneOpen) != 4 {
		t.Errorf("the open fold on the phone shows %v", got.PhoneOpen)
	}
	var sheet []string
	for _, l := range got.Sheet {
		sheet = append(sheet, l.Text)
	}
	if strings.Join(sheet, ",") != "Open the conversation,Outside the panel" || got.Sheet[0].Off || !got.Sheet[1].Off {
		t.Errorf("the phone's sheet of a run offers %+v — only opening it, and why nothing else", got.Sheet)
	}

	for _, tab := range got.Tabs {
		if strings.HasPrefix(tab, "Terminal") {
			t.Errorf("the conversation offers a terminal (%v) for a session in no pane of tmux", got.Tabs)
		}
	}
	if !strings.Contains(got.Place, "outside") {
		t.Errorf("the session button says %q — the session lives outside the panel", got.Place)
	}
	if got.Composer || !strings.Contains(got.Note, "started by rotation") {
		t.Errorf("composer %v, note %q: a run is only read, and the note names who started it", got.Composer, got.Note)
	}
	if strings.Join(got.Panel, ",") != "Outside the panel,Rename…,Session info,Copy the session ID,Copy the resume command" {
		t.Errorf("the session panel lists %v — no window, move, Remote Control or end for a session the panel did not start", got.Panel)
	}
}
