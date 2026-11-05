package check

import (
	"os"
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
	Row    int      `json:"row"`
}

type deskCardRow struct {
	Name  string   `json:"name"`
	Icons int      `json:"icons"`
	Shown bool     `json:"shown"`
	Bad   []string `json:"bad"`
}

type deskCardHead struct {
	Width    int    `json:"width"`
	Overflow int    `json:"overflow"`
	TwoLines bool   `json:"twoLines"`
	Gap      int    `json:"gap"`
	Sub      string `json:"sub"`
	Agent    string `json:"agent"`
	Place    string `json:"place"`
}

type deskCardAction struct {
	Kind   string         `json:"kind"`
	Target string         `json:"target"`
	Params map[string]any `json:"params"`
}

type deskCard struct {
	Pointer     bool              `json:"pointer"`
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
	Edge         string                   `json:"edge"`
	NoEdge       string                   `json:"noEdge"`
	Facts        map[string]deskCardFacts `json:"facts"`
	Bells        map[string]string        `json:"bells"`
	BellsVisited map[string]string        `json:"bellsVisited"`
	ColumnText   string                   `json:"columnText"`
	Rows         []deskCardRow            `json:"rows"`
	HomeAct      string                   `json:"homeAct"`
	Hiding       []string                 `json:"hiding"`
	Asked        []string                 `json:"asked"`
	Shelf        []struct {
		Name    string   `json:"name"`
		Contour string   `json:"contour"`
		When    string   `json:"when"`
		About   string   `json:"about"`
		Dot     bool     `json:"dot"`
		Resume  string   `json:"resume"`
		Bad     []string `json:"bad"`
	} `json:"shelf"`
	ArchiveOpened int `json:"archiveOpened"`
	PickedClosed  struct {
		Name     string `json:"name"`
		ID       string `json:"id"`
		Archived bool   `json:"archived"`
	} `json:"pickedClosed"`
	Plus          string           `json:"plus"`
	Menu          []string         `json:"menu"`
	Opened        []deskCardAction `json:"opened"`
	ProjectPlus   string           `json:"projectPlus"`
	ProjectOpened []deskCardAction `json:"projectOpened"`
	OpenClosed    *struct {
		Name   string `json:"name"`
		Resume string `json:"resume"`
		When   string `json:"when"`
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
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: what stands on a row and what fits a line is the stylesheet's business — run make front first")
	}
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
	if !deskCardRun.got.Pointer {
		t.Fatal("the fixture runs without a hovering pointer: a rule behind (hover: hover) that hid the actions until the pointer came would go unseen")
	}
	return deskCardRun.got
}

// The live sessions of a contour keep their places whatever they do: they
// stand in blocks of their projects in the order of the project map — a
// project of one session said by its card alone, with no heading over it —
// the sessions no project holds after them, and the arrow keys go down the
// column in that order. A session being raised stands in the block of its
// project, at the place the project has in the map. The collector lists the
// sessions by how full they are, which changes by the minute; a place that
// followed it would move every time the person looked, and the eye and the
// arrow keys would lose the session they were on.
func TestDeskColumnKeepsItsPlacesWhateverTheSessionsDo(t *testing.T) {
	got := runDeskCard(t)

	want := []deskCardSection{
		{Name: "personal", Rows: []string{"aacpanel", "person", "helios", "scratch"}},
		{Name: "Globex Group", Rows: []string{"shop", "api", "acme-fingerprint-rotation-review"}},
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
// is the warm one, counted in the heading of its contour.
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
	if !strings.Contains(got.Edge, "gradient") || got.NoEdge != "none" {
		t.Errorf("the waiting card has the ground %q and a quiet one %q — only the one waiting for the person is warm", got.Edge, got.NoEdge)
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

// A card offers its controls without the pointer — a new session in its
// project where the card stands for the project alone, pause, restart, pin
// and close; the home session has no close and no plus. The controls stand
// in the top right corner of the card, on the line of the name at the right
// edge, and nothing of the card runs into them: not a long name, not the
// state or the lines under it. The column stands as narrow as it gets, with
// the right panel open. One the panel did not start has no controls, which
// the fixture of such sessions holds.
func TestDeskRowActionStandsOnEveryRow(t *testing.T) {
	got := runDeskCard(t)

	if len(got.Rows) < 7 {
		t.Fatalf("the column shows %d live cards: %+v", len(got.Rows), got.Rows)
	}
	for _, row := range got.Rows {
		want := 5
		switch row.Name {
		case "helios":
			want = 3
		case "scratch", "acme-fingerprint-rotation-review":
			want = 4
		}
		if row.Icons != want || !row.Shown {
			t.Errorf("%s has %d controls, shown %v — every card the panel started shows its controls without the pointer", row.Name, row.Icons, row.Shown)
		}
		if len(row.Bad) > 0 {
			t.Errorf("%s: %v — the controls keep the corner of their own", row.Name, row.Bad)
		}
	}
	if got.HomeAct != "pause session helios,restart session helios,pin session group helios" {
		t.Errorf("the home session offers %q, expected pause, restart and pin", got.HomeAct)
	}
	if len(got.Hiding) > 0 {
		t.Errorf("the stylesheet takes off the screen %v — the controls of the column stand without the pointer", got.Hiding)
	}
}

// The plus in the heading of a contour, there without the pointer, starts a
// new session in a project of its map: the projects drop from it grouped as the map groups them, the home
// project left out while the home session lives, and a press sends the same
// action the projects panel does, addressed by the id of the map entry. The
// plus of a card that stands for its project alone, there without the
// pointer too, sends the same action for its own project.
func TestDeskContourHeadingStartsASession(t *testing.T) {
	got := runDeskCard(t)

	if got.Plus != "visible" {
		t.Errorf("the plus of a heading stands %q — it is there without the pointer", got.Plus)
	}
	if want := "[Side],aacpanel live,person live,blog"; strings.Join(got.Menu, ",") != want {
		t.Errorf("the plus of personal offers %v, expected %s", got.Menu, want)
	}
	if len(got.Opened) != 1 || got.Opened[0].Kind != "session.open" || got.Opened[0].Target != "blog" {
		t.Fatalf("a press on blog sent %+v, expected one session.open", got.Opened)
	}
	if id, ok := got.Opened[0].Params["project"].(float64); !ok || int(id) != 13 {
		t.Errorf("the new session is addressed as %+v, expected the project of the map by its id", got.Opened[0].Params)
	}
	if got.ProjectPlus != "visible" {
		t.Errorf("the plus of a project's card stands %q — it is there without the pointer", got.ProjectPlus)
	}
	if len(got.ProjectOpened) != 1 || got.ProjectOpened[0].Kind != "session.open" || got.ProjectOpened[0].Target != "person" {
		t.Fatalf("a press on the plus of person sent %+v, expected one session.open", got.ProjectOpened)
	}
	if id, ok := got.ProjectOpened[0].Params["project"].(float64); !ok || int(id) != 12 {
		t.Errorf("the session of the project's card is addressed as %+v, expected project 12 by its id", got.ProjectOpened[0].Params)
	}
}

// The closed conversations of every contour shown stand on a shelf below all
// of them: one request for all the contours, newest first, one per project
// and none where nothing was said; each a card of the live skeleton, dimmed,
// with its name, its contour, what it was about and how long ago, and the bar
// of its edge drawn as the peak it reached rather than a fill. The time and
// Resume stand on every card, and nothing runs into them. The archive is a
// press away.
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
			if row.Resume == "none" || row.When == "" {
				t.Errorf("%s on the shelf shows Resume %q and the time %q — both stand without the pointer", row.Name, row.Resume, row.When)
			}
			if len(row.Bad) > 0 {
				t.Errorf("%s on the shelf: %v", row.Name, row.Bad)
			}
		}
	}
	if got.ArchiveOpened != 1 {
		t.Errorf("the way to the archive opened it %d times", got.ArchiveOpened)
	}
	if got.PickedClosed.Name != "shop-admin" || got.PickedClosed.ID != "c-shop-admin" || !got.PickedClosed.Archived {
		t.Errorf("a press on a closed conversation opened %+v", got.PickedClosed)
	}
	if got.OpenClosed == nil || got.OpenClosed.Name != "shop-admin" || got.OpenClosed.Resume == "none" || got.OpenClosed.When == "none" {
		t.Errorf("the open closed conversation stands as %+v: with its time and Resume both", got.OpenClosed)
	}
	if len(got.Resumed) != 1 || got.Resumed[0].Kind != "session.resume" || got.Resumed[0].Params["session"] != "c-shop-admin" {
		t.Errorf("Resume sent %+v", got.Resumed)
	}
	if got.PickedAfterResume != 1 {
		t.Errorf("Resume also opened the conversation: %d picks", got.PickedAfterResume)
	}
}

// The header of a conversation on the wide screen stands in two lines on the
// left — the name, and under it who runs the session on which model, the
// state, the share and the path — with the tools in one line on the right. The tools never leave the header, with the
// right panel open or without it, and a short name leaves no hole after it.
func TestDeskChatHeaderStandsInTwoLines(t *testing.T) {
	got := runDeskCard(t)

	for label, head := range map[string]deskCardHead{"with the right panel": got.WithPanel, "without it": got.WithoutPanel} {
		if head.Overflow > 0 {
			t.Errorf("%s the tools run %d px past the header of %d px", label, head.Overflow, head.Width)
		}
		if !head.TwoLines {
			t.Errorf("%s the state and the path do not stand under the name", label)
		}
		if head.Gap > 1 {
			t.Errorf("%s the name leaves a hole of %d px after it", label, head.Gap)
		}
		if !strings.HasPrefix(head.Sub, "Claude · Opus 5.5 · working · 26% ·") || head.Agent != "claude" {
			t.Errorf("%s the line under the name reads %q, opened by a word painted as %q's: the agent, the model, the state, the share, the path",
				label, head.Sub, head.Agent)
		}
		if !strings.Contains(head.Place, "RC") {
			t.Errorf("%s the session button reads %q", label, head.Place)
		}
	}
	if got.WithPanel.Width >= got.WithoutPanel.Width {
		t.Errorf("the header is %d px with the panel and %d px without — the panel was not open", got.WithPanel.Width, got.WithoutPanel.Width)
	}
	if !strings.Contains(got.WithoutPanel.Sub, "~/Projects/Globex/shop") {
		t.Errorf("with room the path reads whole: %q", got.WithoutPanel.Sub)
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
