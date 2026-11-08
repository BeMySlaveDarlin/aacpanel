package check

import (
	"regexp"
	"strings"
	"sync"
	"testing"
)

type deskCardSection struct {
	Name  string   `json:"name"`
	Waits string   `json:"waits"`
	Rows  []string `json:"rows"`
}

type deskCardFacts struct {
	Agent  string   `json:"agent"`
	Line4  string   `json:"line4"`
	More   []string `json:"more"`
	Badges []string `json:"badges"`
	Ctx    string   `json:"ctx"`
}

type deskCardRow struct {
	Name  string `json:"name"`
	Icons int    `json:"icons"`
}

type deskCardHead struct {
	Sub   string `json:"sub"`
	Agent string `json:"agent"`
	Place string `json:"place"`
}

type deskCardAction struct {
	Kind   string         `json:"kind"`
	Target string         `json:"target"`
	Params map[string]any `json:"params"`
}

type deskCard struct {
	Before      []deskCardSection `json:"before"`
	After       []deskCardSection `json:"after"`
	OrderBefore string            `json:"orderBefore"`
	OrderAfter  string            `json:"orderAfter"`
	Ghost       *struct {
		Section string `json:"section"`
		Head    string `json:"head"`
		Say     string `json:"say"`
	} `json:"ghost"`
	Says         map[string]string        `json:"says"`
	Runs         map[string][]string      `json:"runs"`
	SaysAfter    map[string]string        `json:"saysAfter"`
	Tones        map[string]string        `json:"tones"`
	Facts        map[string]deskCardFacts `json:"facts"`
	Bells        map[string]string        `json:"bells"`
	BellsVisited map[string]string        `json:"bellsVisited"`
	ColumnText   string                   `json:"columnText"`
	Rows         []deskCardRow            `json:"rows"`
	Pie          struct {
		Shift                                       struct{ Prevented, Opened bool }
		Prevented, Desk, Gate, ClosedAfter, Escaped bool
		Items                                       []string
		Plate                                       string
		Sent                                        []struct{ Kind, Target string }
	} `json:"pie"`
	Asked []string `json:"asked"`
	Shelf []struct {
		Name    string `json:"name"`
		Contour string `json:"contour"`
		When    string `json:"when"`
		About   string `json:"about"`
		Dot     bool   `json:"dot"`
		Resume  bool   `json:"resume"`
	} `json:"shelf"`
	ArchiveOpened int `json:"archiveOpened"`
	PickedClosed  struct {
		Name     string `json:"name"`
		ID       string `json:"id"`
		Archived bool   `json:"archived"`
	} `json:"pickedClosed"`
	Menu          []string         `json:"menu"`
	Opened        []deskCardAction `json:"opened"`
	ProjectOpened []deskCardAction `json:"projectOpened"`
	OpenClosed    *struct {
		Name string `json:"name"`
	} `json:"openClosed"`
	Resumed           []deskCardAction `json:"resumed"`
	PickedAfterResume int              `json:"pickedAfterResume"`
	WithPanel         deskCardHead     `json:"withPanel"`
	WithoutPanel      deskCardHead     `json:"withoutPanel"`
	PhoneWhen         string           `json:"phoneWhen"`
}

// The page is run once for all the tests below: each of them reads its own
// part of the same answer.
var deskCardRun struct {
	sync.Once
	got deskCard
	ok  bool
}

func runDeskCard(t *testing.T) deskCard {
	t.Helper()
	if chromeBinary() == "" {
		t.Skip("no Chrome on this machine: the fixture runs the components in a real engine")
	}
	// Parallel before the page runs: a test paused inside the run would hold
	// every other one waiting on it.
	parallel(t)
	deskCardRun.Do(func() {
		runWideFixture(t, "deskcard.html", &deskCardRun.got)
		deskCardRun.ok = true
	})
	if !deskCardRun.ok {
		t.Fatal("deskcard.html failed under Chrome — its error is reported by the test that ran it")
	}
	return deskCardRun.got
}

// The live sessions of a contour keep their places whatever they do: they
// stand in blocks of their projects in the order of the project map, each
// under the heading of its project, a project of one session as well; the
// sessions no project holds after them with no heading, and the arrow keys
// go down the column in that order. A session being raised stands in the
// block of its project, under its heading, at the place the project has in
// the map. The collector lists the
// sessions by how full they are, which changes by the minute; a place that
// followed it would move every time the person looked, and the eye and the
// arrow keys would lose the session they were on.
func TestDeskColumnKeepsItsPlacesWhateverTheSessionsDo(t *testing.T) {
	got := runDeskCard(t)

	want := []deskCardSection{
		{Name: "personal", Rows: []string{"# aacpanel · Side", "aacpanel", "# person · Side", "person", "# helios · Host", "helios", "scratch"}},
		{Name: "Globex Group", Rows: []string{"# shop · SHOP", "shop", "# shop-admin · SHOP", "# api · Platform", "api", "acme-fingerprint-rotation-review"}},
	}
	check := func(when string, sections []deskCardSection) {
		if len(sections) != len(want) {
			t.Fatalf("%s: the column shows %d contours: %+v", when, len(sections), sections)
		}
		for i, sec := range sections {
			if sec.Name != want[i].Name || strings.Join(sec.Rows, ",") != strings.Join(want[i].Rows, ",") {
				t.Errorf("%s: contour %q stands as %v, expected %q as %v — the places follow the map, not the state",
					when, sec.Name, sec.Rows, want[i].Name, want[i].Rows)
			}
		}
	}
	check("at first", got.Before)
	check("a minute later, every state changed", got.After)
	if got.OrderBefore != "aacpanel,person,helios,scratch,shop,api,acme-fingerprint-rotation-review" || got.OrderAfter != got.OrderBefore {
		t.Errorf("the arrow keys of the shell go %q, then %q — the same places the column shows, before and after", got.OrderBefore, got.OrderAfter)
	}
	if got.Ghost == nil || got.Ghost.Section != "Globex Group" || got.Ghost.Head != "shop-admin" || got.Ghost.Say != "starting" {
		t.Errorf("the console being raised stands as %+v: in the contour of its project, saying it starts", got.Ghost)
	}
}

// A card says how its session stands in the words of the phone, with what
// runs right after the state's word; a call the person has not come for
// makes the card wait as a question does. A card that waits for the person
// is counted in the heading of its contour.
func TestDeskRowSaysTheStateInThePhonesWords(t *testing.T) {
	got := runDeskCard(t)

	for name, want := range map[string]string{
		"person":   "asks Palette · 2 questions",
		"aacpanel": "working",
		"api":      "allow a tool",
		"shop":     "called you",
		"helios":   "idle",
	} {
		if got.Says[name] != want {
			t.Errorf("%s says %q, expected %q", name, got.Says[name], want)
		}
	}
	for name, want := range map[string]string{
		"aacpanel": "2 subagents running",
		"shop":     "1 subagents running,1 observers watching",
		"helios":   "",
	} {
		if strings.Join(got.Runs[name], ",") != want {
			t.Errorf("%s counts %v running, expected %q", name, got.Runs[name], want)
		}
	}
	if got.SaysAfter["aacpanel"] != "asks Scope" || got.SaysAfter["person"] != "idle" {
		t.Errorf("after the change the cards say %v", got.SaysAfter)
	}
	for _, dead := range []string{"handling the request", "waiting for a message", "waiting for an answer to a question", "answering"} {
		if strings.Contains(got.ColumnText, dead) {
			t.Errorf("the column still says %q — the state is said in the words of the phone", dead)
		}
	}
	if got.Tones["person"] != "wait" || got.Tones["aacpanel"] != "busy" || got.Tones["helios"] != "idle" || got.Tones["shop"] != "wait" {
		t.Errorf("the cards carry tones %v", got.Tones)
	}
	if got.Before[0].Waits != "1" || got.Before[1].Waits != "2" {
		t.Errorf("the headings count %q and %q waiting, expected one and two", got.Before[0].Waits, got.Before[1].Waits)
	}
}

// Under the state a card says who runs the session, in the hue of its agent,
// on which model at which effort, and how often it was compacted, in words;
// its settings stand in their places on every card — the mode, where it
// lives, its cap, Remote Control, a worktree, an off one dimmed in its place —
// and the share of its context closes them.
func TestDeskRowTellsWhatTheSessionRunsOn(t *testing.T) {
	got := runDeskCard(t)

	for name, want := range map[string]struct{ line4, more, badges, ctx string }{
		"aacpanel": {"Claude Opus 5.5 Extra", "2 compactions", "Auto|stream|no cap|RC|worktree off", "42%"},
		"api":      {"Claude Opus 5.5 Extra", "", "Plan|stream|no cap|RC off|worktree off", "37%"},
		"shop":     {"Claude Opus 5.5 Extra", "", "Bypass|stream|no cap|RC off|worktree off", "26%"},
		"helios":   {"Claude Opus 5.5 Extra", "", "Auto|stream|no cap|RC|worktree off", "10%"},
		"scratch":  {"Claude Opus 5.5 Extra", "", "Auto|tmux|no cap|RC off|worktree off", "5%"},
	} {
		f := got.Facts[name]
		if f.Line4 != want.line4 || strings.Join(f.More, ",") != want.more || strings.Join(f.Badges, "|") != want.badges || f.Ctx != want.ctx {
			t.Errorf("%s reads %q %v %v %q, expected %q %q %q %q", name, f.Line4, f.More, f.Badges, f.Ctx,
				want.line4, want.more, want.badges, want.ctx)
		}
		if f.Agent != "claude" {
			t.Errorf("%s: the line opens with a word painted as %q's, expected the word of its agent, claude", name, f.Agent)
		}
	}
}

// A live card carries no controls: at a desk a right click opens them as its
// pie, the desk's own, and a sector does what it says through the gate. Shift
// keeps the browser's menu, Escape takes the pie down, and a new session in a
// project is the plus of its heading.
func TestDeskCardsOpenTheirPieOnARightClick(t *testing.T) {
	got := runDeskCard(t)

	if len(got.Rows) < 7 {
		t.Fatalf("the column shows %d live cards: %+v", len(got.Rows), got.Rows)
	}
	for _, row := range got.Rows {
		if row.Icons != 0 {
			t.Errorf("%s has %d controls on the card, expected none: they are its pie", row.Name, row.Icons)
		}
	}
	p := got.Pie
	if p.Shift.Prevented || p.Shift.Opened {
		t.Errorf("a right click with Shift took the browser's menu away (%v) or opened the pie (%v)", p.Shift.Prevented, p.Shift.Opened)
	}
	if !p.Prevented || !p.Desk {
		t.Fatalf("a right click on the card opened no pie of the desk: prevented %v, desk %v", p.Prevented, p.Desk)
	}
	want := "Close the session,Restart the session,Pause…,Pin the group,More,Stop the turn"
	if strings.Join(p.Items, ",") != want {
		t.Errorf("the pie of aacpanel offers %v, expected %s", p.Items, want)
	}
	if !strings.Contains(p.Plate, "click an action") {
		t.Errorf("the plate of the desk's pie reads %q, expected it to ask for a click", p.Plate)
	}
	if !p.Gate || len(p.Sent) != 1 || p.Sent[0].Kind != "session.restart" || p.Sent[0].Target != "aacpanel" || !p.ClosedAfter {
		t.Errorf("Restart in the pie went through the gate %v, sent %+v, closed the pie %v: expected one confirmed session.restart of aacpanel",
			p.Gate, p.Sent, p.ClosedAfter)
	}
	if !p.Escaped {
		t.Error("Escape left the pie of the desk open")
	}
}

// The plus in the heading of a contour starts a new session in a project of
// its map: the projects drop from it grouped as the map groups them, the home
// project left out while the home session lives, and a press sends the same
// action the projects panel does, addressed by the id of the map entry. The
// plus in the heading of a project of one session sends the same action for
// that project.
func TestDeskContourHeadingStartsASession(t *testing.T) {
	got := runDeskCard(t)

	if want := "[Side],aacpanel live,person live,blog"; strings.Join(got.Menu, ",") != want {
		t.Errorf("the plus of personal offers %v, expected %s", got.Menu, want)
	}
	if len(got.Opened) != 1 || got.Opened[0].Kind != "session.open" || got.Opened[0].Target != "blog" {
		t.Fatalf("a press on blog sent %+v, expected one session.open", got.Opened)
	}
	if id, ok := got.Opened[0].Params["project"].(float64); !ok || int(id) != 13 {
		t.Errorf("the new session is addressed as %+v, expected the project of the map by its id", got.Opened[0].Params)
	}
	if len(got.ProjectOpened) != 1 || got.ProjectOpened[0].Kind != "session.open" || got.ProjectOpened[0].Target != "person" {
		t.Fatalf("a press on the plus of person sent %+v, expected one session.open", got.ProjectOpened)
	}
	if id, ok := got.ProjectOpened[0].Params["project"].(float64); !ok || int(id) != 12 {
		t.Errorf("the session of the project's heading is addressed as %+v, expected project 12 by its id", got.ProjectOpened[0].Params)
	}
}

// The closed conversations of every contour shown stand on a shelf below all
// of them: one request for all the contours, newest first, one per project
// and none where nothing was said; each a card of the live skeleton, dimmed,
// with its name, its contour, what it was about and how long ago, and the bar
// of its edge drawn as the peak it reached rather than a fill. The time and
// Resume are on every card. The archive is a press away.
func TestDeskShelfHoldsTheClosedConversations(t *testing.T) {
	got := runDeskCard(t)

	if len(got.Asked) != 1 || !strings.Contains(got.Asked[0], "contour=3") || !strings.Contains(got.Asked[0], "contour=1") {
		t.Fatalf("the shelf asked %v — once, for both contours", got.Asked)
	}
	var names []string
	for _, row := range got.Shelf {
		names = append(names, row.Name)
	}
	if strings.Join(names, ",") != "person-e8,career,shop-admin" {
		t.Errorf("the shelf holds %v, expected one conversation per project, the empty one dropped", names)
	}
	if len(got.Shelf) == 3 {
		first, last := got.Shelf[0], got.Shelf[2]
		if first.Contour != "personal" || got.Shelf[1].Contour != "personal" || last.Contour != "Globex Group" {
			t.Errorf("the shelf names the contours %q, %q, %q", first.Contour, got.Shelf[1].Contour, last.Contour)
		}
		if first.When != "1h10" || last.When != "26h" || !strings.Contains(last.About, "move the report exports") {
			t.Errorf("the shelf says %q and %q, %q", first.When, last.When, last.About)
		}
		for _, row := range got.Shelf {
			if row.Dot {
				t.Errorf("%s on the shelf has a bar of a fill — a closed conversation shows the peak it reached", row.Name)
			}
			if !row.Resume || row.When == "" {
				t.Errorf("%s on the shelf has Resume %v and the time %q", row.Name, row.Resume, row.When)
			}
		}
	}
	if got.ArchiveOpened != 1 {
		t.Errorf("the way to the archive opened it %d times", got.ArchiveOpened)
	}
	if got.PickedClosed.Name != "shop-admin" || got.PickedClosed.ID != "c-shop-admin" || !got.PickedClosed.Archived {
		t.Errorf("a press on a closed conversation opened %+v", got.PickedClosed)
	}
	if got.OpenClosed == nil || got.OpenClosed.Name != "shop-admin" {
		t.Errorf("the open closed conversation stands as %+v", got.OpenClosed)
	}
	if len(got.Resumed) != 1 || got.Resumed[0].Kind != "session.resume" || got.Resumed[0].Params["session"] != "c-shop-admin" {
		t.Errorf("Resume sent %+v", got.Resumed)
	}
	if got.PickedAfterResume != 1 {
		t.Errorf("Resume also opened the conversation: %d picks", got.PickedAfterResume)
	}
}

// The header of a conversation on the wide screen says, under the name, who
// runs the session on which model, the state, the share and the path, with
// the right panel open or without it.
func TestDeskChatHeaderSaysWhatTheSessionRunsOn(t *testing.T) {
	got := runDeskCard(t)

	for label, head := range map[string]deskCardHead{"with the right panel": got.WithPanel, "without it": got.WithoutPanel} {
		if !strings.HasPrefix(head.Sub, "Claude · Opus 5.5 · working · 26% ·") || head.Agent != "claude" {
			t.Errorf("%s the line under the name reads %q, opened by a word painted as %q's: the agent, the model, the state, the share, the path",
				label, head.Sub, head.Agent)
		}
		if !strings.Contains(head.Place, "RC") {
			t.Errorf("%s the session button reads %q", label, head.Place)
		}
	}
	if !strings.Contains(got.WithoutPanel.Sub, "~/Projects/Globex/shop") {
		t.Errorf("the path reads %q", got.WithoutPanel.Sub)
	}
}

// The time of a closed conversation is said in the words of the screen: how
// long ago, in the card's own short span, not a date in the locale of the
// browser.
func TestClosedConversationTimeIsRelative(t *testing.T) {
	got := runDeskCard(t)
	if got.PhoneWhen != "2h" || regexp.MustCompile(`\p{Cyrillic}`).MatchString(got.PhoneWhen) {
		t.Errorf("a closed conversation on the phone reads %q, expected how long ago, 2h", got.PhoneWhen)
	}
}

// A session that called the person waits on its card until its conversation
// has been on screen: the state's word says the call and the corner how long
// ago, on no other card, and the card is quiet again once the person has been
// there and come back, before the host says so.
func TestDeskRowRingsUntilTheSessionIsOpened(t *testing.T) {
	got := runDeskCard(t)
	said, ok := got.Bells["shop"]
	if len(got.Bells) != 1 || !ok {
		t.Fatalf("the cards saying a call are %v, wanted the one session that called", got.Bells)
	}
	if said != "called you · 3m" {
		t.Errorf("the card says %q, wanted the call and how long ago it came", said)
	}
	if len(got.BellsVisited) != 0 {
		t.Errorf("the conversation of the session was on screen, and the column still says the call: %v", got.BellsVisited)
	}
}
