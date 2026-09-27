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
	All   []string `json:"all"`
	Shown []string `json:"shown"`
	Lines int      `json:"lines"`
	Cut   []string `json:"cut"`
}

type deskCardActs struct {
	Visible string `json:"visible"`
	Icons   int    `json:"icons"`
}

type deskCardHead struct {
	Width    int    `json:"width"`
	Overflow int    `json:"overflow"`
	TwoLines bool   `json:"twoLines"`
	Gap      int    `json:"gap"`
	Sub      string `json:"sub"`
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
		Key     string `json:"key"`
		Last    bool   `json:"last"`
		Say     string `json:"say"`
	} `json:"ghost"`
	Says       map[string]string        `json:"says"`
	SaysAfter  map[string]string        `json:"saysAfter"`
	Tones      map[string]string        `json:"tones"`
	Edge       string                   `json:"edge"`
	NoEdge     string                   `json:"noEdge"`
	Facts      map[string]deskCardFacts `json:"facts"`
	Marks      map[string][]string      `json:"marks"`
	ColumnText string                   `json:"columnText"`
	Acts       map[string]*deskCardActs `json:"acts"`
	HomeAct    string                   `json:"homeAct"`
	Rules      struct {
		Hidden bool `json:"hidden"`
		Shown  bool `json:"shown"`
		Resume bool `json:"resume"`
	} `json:"rules"`
	Asked []string `json:"asked"`
	Shelf []struct {
		Name    string `json:"name"`
		Contour string `json:"contour"`
		When    string `json:"when"`
		About   string `json:"about"`
		Dot     bool   `json:"dot"`
		Resume  string `json:"resume"`
	} `json:"shelf"`
	ArchiveOpened int `json:"archiveOpened"`
	PickedClosed  struct {
		Name     string `json:"name"`
		ID       string `json:"id"`
		Archived bool   `json:"archived"`
	} `json:"pickedClosed"`
	PlusHidden string           `json:"plusHidden"`
	Menu       []string         `json:"menu"`
	Opened     []deskCardAction `json:"opened"`
	OpenClosed *struct {
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
		t.Skip("web/dist/bundle.css is not built: what shows under the pointer and what fits a line is the stylesheet's business — run make front first")
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
		t.Fatal("the fixture runs without a hovering pointer: the rules of the column behind (hover: hover) are measured switched off")
	}
	return deskCardRun.got
}

// The live sessions of a contour keep their places whatever they do: the order
// is the order of the project map, the sessions no project holds after them,
// and the keys 1–9 go down the column in that order. The collector lists the
// sessions by how full they are, which changes by the minute; a key that
// followed it would name another session every time the person looked.
func TestDeskColumnKeepsItsPlacesWhateverTheSessionsDo(t *testing.T) {
	got := runDeskCard(t)

	want := []deskCardSection{
		{Name: "personal", Rows: []string{"1:aacpanel", "2:person", "3:atlas", "4:scratch"}},
		{Name: "Algorithmics", Rows: []string{"5:lms", "6:ai-platform", ":lms-admin"}},
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
	if got.OrderBefore != "aacpanel,person,atlas,scratch,lms,ai-platform" || got.OrderAfter != got.OrderBefore {
		t.Errorf("the keys of the shell go %q, then %q — the same places the column shows, before and after", got.OrderBefore, got.OrderAfter)
	}
	if got.Ghost == nil || got.Ghost.Section != "Algorithmics" || got.Ghost.Key != "" || !got.Ghost.Last ||
		!strings.HasPrefix(got.Ghost.Say, "starting on the host") {
		t.Errorf("the console being raised stands as %+v: in the contour of its project, after the live ones, with no key", got.Ghost)
	}
}

// A row says how its session stands in the words of the phone, and only what
// is unusual about it is marked: Remote Control, the home session, a console,
// a session the panel did not start — the feed is where a session lives and
// goes unmarked. A session that waits for the person is the bright row, with
// an edge of its own, counted in the heading of its contour.
func TestDeskRowSaysTheStateInThePhonesWords(t *testing.T) {
	got := runDeskCard(t)

	for name, want := range map[string]string{
		"person":      "asks you · Palette · 2 questions",
		"aacpanel":    "working · 2 agents",
		"ai-platform": "waiting: a dialog is open",
		"lms":         "working · 1 agent",
	} {
		if got.Says[name] != want {
			t.Errorf("%s says %q, expected %q", name, got.Says[name], want)
		}
	}
	if !regexp.MustCompile(`^idle · \d+ min ago$`).MatchString(got.Says["atlas"]) {
		t.Errorf("an idle session says %q — idle, and since when", got.Says["atlas"])
	}
	if got.SaysAfter["aacpanel"] != "asks you · Scope" || !strings.HasPrefix(got.SaysAfter["person"], "idle") {
		t.Errorf("after the change the rows say %v", got.SaysAfter)
	}
	for _, dead := range []string{"handling the request", "waiting for a message", "waiting for an answer to a question"} {
		if strings.Contains(got.ColumnText, dead) {
			t.Errorf("the column still says %q — the state is said in the words of the phone", dead)
		}
	}
	if got.Tones["person"] != "wait" || got.Tones["aacpanel"] != "busy" || got.Tones["atlas"] != "idle" {
		t.Errorf("the rows carry tones %v", got.Tones)
	}
	if !strings.Contains(got.Edge, "inset") || got.NoEdge != "none" {
		t.Errorf("the waiting row has the edge %q and a quiet one %q — only the one waiting for the person gets it", got.Edge, got.NoEdge)
	}
	if got.Before[0].Waits != "1" || got.Before[1].Waits != "1" {
		t.Errorf("the headings count %q and %q waiting, expected one each", got.Before[0].Waits, got.Before[1].Waits)
	}

	for name, want := range map[string]string{
		"aacpanel": "RC",
		"atlas":    "home,RC",
		"scratch":  "console",
		"lms":      "",
	} {
		if strings.Join(got.Marks[name], ",") != want {
			t.Errorf("%s is marked %v, expected %q", name, got.Marks[name], want)
		}
	}
	if regexp.MustCompile(`\bfeed\b`).MatchString(got.ColumnText) {
		t.Error("the column marks a session as living on the feed — that is where every session lives unless marked")
	}
}

// Under the state stands a quiet line of what the session runs on, in the
// words the panel uses elsewhere and in the order of what tells most: the
// group of its project unless it is the name, the model and its effort, the
// mode where it is not the one the account starts in (a mode that stops the
// session asking is said always), the context in tokens, how long it has
// been up, and its compactions. One line: what does not fit drops from the
// end, a whole fact at a time.
func TestDeskRowTellsWhatTheSessionRunsOn(t *testing.T) {
	got := runDeskCard(t)

	for name, want := range map[string]string{
		"aacpanel":    "Pets|Opus 5.5 · Extra|418k of 1m|up 3 h|2 compactions",
		"ai-platform": "Platform|Opus 5.5 · Extra|Plan|370k of 1m|up 3 h",
		"lms":         "Opus 5.5 · Extra|Bypass|260k of 1m|up 3 h",
		"atlas":       "Host|Opus 5.5 · Extra|100k of 1m|up 3 h",
		"scratch":     "Opus 5.5 · Extra|50k of 1m|up 3 h",
	} {
		f := got.Facts[name]
		if strings.Join(f.All, "|") != want {
			t.Errorf("%s runs on %q, expected %q", name, strings.Join(f.All, "|"), want)
		}
		if f.Lines != 1 {
			t.Errorf("%s: the line of what it runs on takes %d lines", name, f.Lines)
		}
		if len(f.Shown) == 0 || strings.Join(f.Shown, "|") != strings.Join(f.All[:len(f.Shown)], "|") {
			t.Errorf("%s shows %v of %v — what does not fit drops from the end", name, f.Shown, f.All)
		}
		if len(f.Cut) > 0 {
			t.Errorf("%s shows %v cut at the edge of the column — a fact that does not fit is dropped whole", name, f.Cut)
		}
	}
	if f := got.Facts["aacpanel"]; len(f.Shown) >= len(f.All) {
		t.Errorf("the longest line shows all of %v — the fixture no longer checks what happens to a line too long for the column", f.All)
	}
}

// A row does one thing besides opening, and says so under the pointer and on
// the open row, not all the time: a cross to close a session, a restart for
// the home one. One the panel did not start has nothing, which the fixture of
// such sessions holds.
func TestDeskRowShowsItsActionUnderThePointer(t *testing.T) {
	got := runDeskCard(t)

	if a := got.Acts["open"]; a == nil || a.Visible != "visible" || a.Icons != 1 {
		t.Errorf("the open row's action is %+v, expected one, shown", a)
	}
	if a := got.Acts["other"]; a == nil || a.Visible != "hidden" || a.Icons != 1 {
		t.Errorf("a row neither open nor under the pointer has its action %+v, expected one, hidden until the pointer comes", a)
	}
	if a := got.Acts["home"]; a == nil || a.Icons != 1 || got.HomeAct != "restart session atlas" {
		t.Errorf("the home session offers %+v (%q), expected its restart alone", a, got.HomeAct)
	}
	if a := got.Acts["console"]; a == nil || a.Icons != 1 {
		t.Errorf("a session in a console offers %+v: it is closed like any other the panel started", a)
	}
	if !got.Rules.Hidden || !got.Rules.Shown {
		t.Errorf("the stylesheet hides the action %v and brings it out under the pointer and on the open row %v", got.Rules.Hidden, got.Rules.Shown)
	}
}

// The plus in the heading of a contour starts a new session in a project of
// its map: the projects drop from it grouped as the map groups them, the home
// project left out while the home session lives, and a press sends the same
// action the projects panel does, addressed by the id of the map entry.
func TestDeskContourHeadingStartsASession(t *testing.T) {
	got := runDeskCard(t)

	if got.PlusHidden != "hidden" {
		t.Errorf("the plus of a heading stands %q without the pointer — it comes under the pointer", got.PlusHidden)
	}
	if want := "[Pets],aacpanel live,person live,blog"; strings.Join(got.Menu, ",") != want {
		t.Errorf("the plus of personal offers %v, expected %s", got.Menu, want)
	}
	if len(got.Opened) != 1 || got.Opened[0].Kind != "session.open" || got.Opened[0].Target != "blog" {
		t.Fatalf("a press on blog sent %+v, expected one session.open", got.Opened)
	}
	if id, ok := got.Opened[0].Params["project"].(float64); !ok || int(id) != 13 {
		t.Errorf("the new session is addressed as %+v, expected the project of the map by its id", got.Opened[0].Params)
	}
}

// The closed conversations of every contour shown stand on a shelf below all
// of them: one request for all the contours, newest first, one per project
// and none where nothing was said; each with its name, its contour, what it
// was about and when, and no dot, key or bar. Resume stands in place of the
// time under the pointer and on the open conversation; the archive is a press
// away.
func TestDeskShelfHoldsTheClosedConversations(t *testing.T) {
	got := runDeskCard(t)

	if len(got.Asked) != 1 || !strings.Contains(got.Asked[0], "contour=3") || !strings.Contains(got.Asked[0], "contour=1") {
		t.Fatalf("the shelf asked %v — once, for both contours", got.Asked)
	}
	var names []string
	for _, row := range got.Shelf {
		names = append(names, row.Name)
	}
	if strings.Join(names, ",") != "person-e8,career,lms-admin" {
		t.Errorf("the shelf holds %v, expected one conversation per project, the empty one dropped", names)
	}
	if len(got.Shelf) == 3 {
		first, last := got.Shelf[0], got.Shelf[2]
		if first.Contour != "personal" || got.Shelf[1].Contour != "personal" || last.Contour != "Algorithmics" {
			t.Errorf("the shelf names the contours %q, %q, %q", first.Contour, got.Shelf[1].Contour, last.Contour)
		}
		if first.When != "1 h ago" || last.When != "1 d ago" || !strings.Contains(last.About, "migrate the admin filters") {
			t.Errorf("the shelf says %q and %q, %q", first.When, last.When, last.About)
		}
		for _, row := range got.Shelf {
			if row.Dot || row.Resume != "none" {
				t.Errorf("%s on the shelf has a dot, key or bar %v, Resume %q — a closed conversation is quiet until the pointer comes", row.Name, row.Dot, row.Resume)
			}
		}
	}
	if !got.Rules.Resume {
		t.Error("the stylesheet never brings Resume out under the pointer")
	}
	if got.ArchiveOpened != 1 {
		t.Errorf("the way to the archive opened it %d times", got.ArchiveOpened)
	}
	if got.PickedClosed.Name != "lms-admin" || got.PickedClosed.ID != "c-lms-admin" || !got.PickedClosed.Archived {
		t.Errorf("a press on a closed conversation opened %+v", got.PickedClosed)
	}
	if got.OpenClosed == nil || got.OpenClosed.Name != "lms-admin" || got.OpenClosed.Resume == "none" || got.OpenClosed.When != "none" {
		t.Errorf("the open closed conversation stands as %+v: Resume in place of its time", got.OpenClosed)
	}
	if len(got.Resumed) != 1 || got.Resumed[0].Kind != "session.resume" || got.Resumed[0].Params["session"] != "c-lms-admin" {
		t.Errorf("Resume sent %+v", got.Resumed)
	}
	if got.PickedAfterResume != 1 {
		t.Errorf("Resume also opened the conversation: %d picks", got.PickedAfterResume)
	}
}

// The header of a conversation on the wide screen stands in two lines on the
// left — the name, and under it the state, the share and the path — with the
// tools in one line on the right. The tools never leave the header, with the
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
		if !strings.HasPrefix(head.Sub, "answering · 26% ·") {
			t.Errorf("%s the line under the name reads %q: the state, the share, the path", label, head.Sub)
		}
		if !strings.Contains(head.Place, "RC") {
			t.Errorf("%s the session button reads %q", label, head.Place)
		}
	}
	if got.WithPanel.Width >= got.WithoutPanel.Width {
		t.Errorf("the header is %d px with the panel and %d px without — the panel was not open", got.WithPanel.Width, got.WithoutPanel.Width)
	}
	if !strings.Contains(got.WithoutPanel.Sub, "~/Projects/Algo/lms") {
		t.Errorf("with room the path reads whole: %q", got.WithoutPanel.Sub)
	}
}

// The time of a closed conversation is said in the words of the screen: how
// long ago, in English, not a date in the locale of the browser.
func TestClosedConversationTimeIsRelative(t *testing.T) {
	got := runDeskCard(t)
	if !strings.HasPrefix(got.PhoneWhen, "2 h ago") || regexp.MustCompile(`\p{Cyrillic}`).MatchString(got.PhoneWhen) {
		t.Errorf("a closed conversation on the phone reads %q, expected how long ago in English", got.PhoneWhen)
	}
}
