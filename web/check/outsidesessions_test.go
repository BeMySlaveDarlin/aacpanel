package check

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The note on a session out of the panel's reach says why it is only read: a
// run names the session it runs inside, a claude under a tmux of its own names
// that server, and one typed into a terminal by hand says so.
func TestTheOutsideNoteSaysWhyTheSessionIsOnlyRead(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the note is made by the engine, not read out of the source")
	}
	kin := bundleFront(t, filepath.Join(webDir, "src", "screens", "sessions", "kin.js"))
	script := `
import { outsideNote, placeOf } from ` + jsString("file://"+kin) + `;
const rows = [
    { outside: true, parent: { session: "rotation" } },
    { outside: true, tmuxServer: "-L work" },
    { outside: true },
];
process.stdout.write(JSON.stringify(rows.map((s) => [placeOf(s), outsideNote(s)])));
`
	out, err := exec.Command(node, "--input-type=module", "-e", script).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var got [][2]string
	if err := json.Unmarshal(out, &got); err != nil || len(got) != 3 {
		t.Fatalf("the notes came back as %s (%v)", out, err)
	}
	for i, says := range []string{"started by rotation", "tmux -L work", "in a terminal of its own"} {
		if got[i][0] != "outside" || !strings.Contains(got[i][1], says) {
			t.Errorf("row %d is marked %q with the note %q, expected outside and a note saying %q", i, got[i][0], got[i][1], says)
		}
	}
	if strings.Contains(got[1][1], "terminal of its own") {
		t.Errorf("a claude under a tmux of its own is said to run in a terminal of its own: %q", got[1][1])
	}
}

// A claude the panel did not start is shown for what it is. The runs a session
// started inside its work stand under it: on the phone in a fold that says one
// of them waits for the person, in the desktop column with the waiting one
// outside the fold and the rest in it; a claude typed into a terminal by hand
// stands on its own, marked, with nothing to press. The conversation of either
// is only read: no terminal, no window, no move, no Remote Control, no end,
// and a note where the composer was.
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

	if strings.Join(got.DeskRows, ",") != "rotation,rotation-e8,typed" {
		t.Errorf("the desktop column shows %v folded, expected the session, its run that waits for the person "+
			"outside the fold, and the one typed by hand", got.DeskRows)
	}
	if got.DeskFold != "1 more run" {
		t.Errorf("the fold reads %q: with the waiting run standing outside it, it counts the runs it still holds", got.DeskFold)
	}
	if !strings.Contains(got.DeskTyped, "outside") || got.DeskTypedActs != 0 {
		t.Errorf("the session typed by hand reads %q with %d actions: it is marked outside and has nothing to press", got.DeskTyped, got.DeskTypedActs)
	}
	if strings.Join(got.DeskKids, ",") != "rotation-e8,rotation-25" || len(got.DeskOpen) != 4 {
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

// A live session with Remote Control up says so on its row of the desktop
// column, the way the session's own tools say it; the others say nothing.
func TestADeskRowSaysRemoteControlIsUp(t *testing.T) {
	var got struct {
		DeskRemote []string `json:"deskRemote"`
	}
	runWideFixture(t, "outsidesessions.html", &got)
	if strings.Join(got.DeskRemote, ",") != "rotation:RC" {
		t.Errorf("the rows saying Remote Control is up are %v, expected only the session that has it", got.DeskRemote)
	}
}
