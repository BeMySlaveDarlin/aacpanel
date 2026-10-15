package check

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

type homeGraph struct {
	Dial struct {
		Now    float64 `json:"now"`
		Hour   float64 `json:"hour"`
		Day    float64 `json:"day"`
		Past   float64 `json:"past"`
		Span   float64 `json:"span"`
		Rising bool    `json:"rising"`
	} `json:"dial"`
	Angles map[string]float64 `json:"angles"`
	DialOf map[string]float64 `json:"dialOf"`
	Rings  []struct {
		Name    string   `json:"name"`
		R       float64  `json:"R"`
		Members []string `json:"members"`
	} `json:"rings"`
	Moon struct {
		Depth    int     `json:"depth"`
		Parent   string  `json:"parent"`
		OffBy    float64 `json:"offBy"`
		FromHost float64 `json:"fromHost"`
		Orbit    float64 `json:"orbit"`
	} `json:"moon"`
	Notes    string   `json:"notes"`
	Head     string   `json:"head"`
	Titles   []string `json:"titles"`
	Jumps    []string `json:"jumps"`
	Sections []string `json:"sections"`
	Canvas   struct {
		Width     int `json:"width"`
		Height    int `json:"height"`
		BoxWidth  int `json:"boxWidth"`
		BoxHeight int `json:"boxHeight"`
	} `json:"canvas"`
	Tip              []string      `json:"tip"`
	TipAway          string        `json:"tipAway"`
	Clicked          []openedTalk  `json:"clicked"`
	AfterDrag        int           `json:"afterDrag"`
	OldSpotAfterDrag []string      `json:"oldSpotAfterDrag"`
	AfterReset       []string      `json:"afterReset"`
	Menu             homeGraphMenu `json:"menu"`
	MenuOpened       openedTalk    `json:"menuOpened"`
	MenuAway         struct {
		Prevented bool `json:"prevented"`
		Menu      bool `json:"menu"`
	} `json:"menuAway"`
	Still       int    `json:"still"`
	AutoAtStart string `json:"autoAtStart"`
	Turning     int    `json:"turning"`
	HiddenDraws int    `json:"hiddenDraws"`
	ShownAgain  int    `json:"shownAgain"`
	OffDraws    int    `json:"offDraws"`
	Help        string `json:"help"`
	Gestures    struct {
		PanDuring     float64 `json:"panDuring"`
		PanAfter      float64 `json:"panAfter"`
		PanTip        string  `json:"panTip"`
		PressAfterPan int     `json:"pressAfterPan"`
		ResetPan      float64 `json:"resetPan"`
		PanEdge       struct {
			Name   string  `json:"name"`
			Off    float64 `json:"off"`
			During float64 `json:"during"`
			After  float64 `json:"after"`
		} `json:"panEdge"`
		WheelPrevent bool    `json:"wheelPrevented"`
		ZoomBy       float64 `json:"zoomBy"`
		ZoomKept     float64 `json:"zoomKept"`
		ZoomOff      float64 `json:"zoomOff"`
		ZoomIn       struct {
			Bound float64 `json:"bound"`
			Tip   string  `json:"tip"`
		} `json:"zoomIn"`
		ZoomOut struct {
			Bound float64 `json:"bound"`
			Tip   string  `json:"tip"`
		} `json:"zoomOut"`
		TurnJumps       []string `json:"turnJumps"`
		TurnApart       float64  `json:"turnApart"`
		MiddlePrevented bool     `json:"middlePrevented"`
		MiddleApart     float64  `json:"middleApart"`
		ResetTips       []string `json:"resetTips"`
		ResetOpened     int      `json:"resetOpened"`
	} `json:"gestures"`
	Keep struct {
		Steps    int      `json:"steps"`
		Changes  int      `json:"changes"`
		Unforced []string `json:"unforced"`
		Unearned []string `json:"unearned"`
		Frozen   int      `json:"frozen"`
		Names    int      `json:"names"`
	} `json:"keep"`
	Period struct {
		Start     string   `json:"start"`
		On        string   `json:"on"`
		Before    []string `json:"before"`
		After     []string `json:"after"`
		OnAfter   string   `json:"onAfter"`
		Hours     string   `json:"hours"`
		Thirty    int      `json:"thirty"`
		Layer     string   `json:"layer"`
		Breakdown string   `json:"breakdown"`
	} `json:"period"`
}

type openedTalk struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type homeGraphMenu struct {
	Prevented bool     `json:"prevented"`
	Label     string   `json:"label"`
	Items     []string `json:"items"`
}

func runHomeGraph(t *testing.T) homeGraph {
	t.Helper()
	var got homeGraph
	runWideFixture(t, "homegraph.html", &got)
	return got
}

// A session stands on the ring of its contour at the angle of its last
// request: the meridian of now at zero, a log scale to a day at 300 degrees,
// anything older at the end of it. A session that has not asked yet counts
// from its start, one that names neither stands at the end. Two that asked at
// once are pulled apart toward older only, and a session another one opened
// is its moon, on the small orbit round it at the angle of its own request.
func TestTheGraphPlacesASessionByItsLastRequest(t *testing.T) {
	got := runHomeGraph(t)

	d := got.Dial
	if d.Now != 0 || d.Day != 300 || d.Past != 300 || d.Span != 300 {
		t.Errorf("the dial reads now %v°, a day %v°, five days %v° (span %v°): the scale is now at 0 and a day at 300, older at the end",
			d.Now, d.Day, d.Past, d.Span)
	}
	if math.Abs(d.Hour-156.5) > 0.5 {
		t.Errorf("an hour stands at %v° instead of 156.5°: the scale is not log(1 + min/2) over a day", d.Hour)
	}
	if !d.Rising {
		t.Error("an older request does not stand further round the dial")
	}

	for _, name := range []string{"blog", "draft", "shop"} {
		if math.Abs(got.Angles[name]-got.DialOf[name]) > 0.11 {
			t.Errorf("%s stands at %v°, while its last request — or its start, having asked nothing — puts it at %v°",
				name, got.Angles[name], got.DialOf[name])
		}
	}
	if got.Angles["stray"] != 300 {
		t.Errorf("a session with neither a request nor a start stands at %v°, not at the end of the scale", got.Angles["stray"])
	}
	if got.Angles["notes"] <= got.Angles["blog"] {
		t.Errorf("notes asked when blog did and stands at %v° against %v°: neighbours are pulled apart toward older",
			got.Angles["notes"], got.Angles["blog"])
	}

	rings := map[string][]string{}
	order := []string{}
	for _, r := range got.Rings {
		rings[r.Name] = r.Members
		order = append(order, r.Name)
	}
	if !reflect.DeepEqual(order, []string{"personal", "client"}) {
		t.Errorf("the rings go %v from the host out, not in the order of the map", order)
	}
	if !contains("codex-1", rings["client"]) || !contains("shop", rings["personal"]) {
		t.Errorf("the rings hold %v: a session belongs to the ring of its contour, a codex thread included", rings)
	}

	m := got.Moon
	if m.Parent != "blog" || m.Depth != 1 || m.OffBy > 0.001 || m.Orbit != 46 {
		t.Errorf("the thread blog opened is a moon of %q at depth %d, %v off the angle of its request, %v from blog: "+
			"a child stands on the orbit of its parent at the angle of its own last request", m.Parent, m.Depth, m.OffBy, m.Orbit)
	}
	if got.Notes != "waiting for an answer, 1 h" {
		t.Errorf("the waiting session says %q beside its mark: the words of its row and how long it has waited", got.Notes)
	}
}

// The tile of the sessions is gone from the home: its numbers are the header
// of the graph, and the rows beside the graph lead to the containers and the
// machine.
func TestTheHomeHasNoTileOfTheSessions(t *testing.T) {
	got := runHomeGraph(t)

	if got.Head != "6 live in 2 contours · 2 busy · 1 waiting for an answer" {
		t.Errorf("the header of the graph says %q", got.Head)
	}
	sessions := 0
	for _, title := range got.Titles {
		if title == "Sessions" {
			sessions++
		}
	}
	if sessions != 1 || !reflect.DeepEqual(got.Jumps, []string{"Containers", "Machine"}) {
		t.Errorf("the home holds the titles %v and the rows %v: Sessions is the graph alone, beside it Containers and Machine",
			got.Titles, got.Jumps)
	}
	if !reflect.DeepEqual(got.Sections, []string{"machine"}) {
		t.Errorf("the row of the machine opened %v", got.Sections)
	}
	for _, title := range []string{"Usage today", "Cache", "Usage by hour", "Top projects", "Models and subagents", "Tools"} {
		if !contains(title, got.Titles) {
			t.Errorf("the home has no tile %q: %v", title, got.Titles)
		}
	}
	if got.Canvas.Width != got.Canvas.BoxWidth || got.Canvas.Height != got.Canvas.BoxHeight || got.Canvas.Width == 0 {
		t.Errorf("the canvas is %dx%d over a box of %dx%d device pixels: it is drawn blurred or cut",
			got.Canvas.Width, got.Canvas.Height, got.Canvas.BoxWidth, got.Canvas.BoxHeight)
	}
}

// A session of the graph is the session of its row: the pointer over its mark
// names it, a click opens it as the row does, a right click opens the row's
// menu. A drag turns the scene and opens nothing; Reset view brings it back.
func TestTheGraphOpensASessionAsItsRowDoes(t *testing.T) {
	got := runHomeGraph(t)

	tip := []string{"blog", "client · context 52% · last request 1 h ago", "waiting for an answer",
		"click — open the session · right-click — its menu"}
	if !reflect.DeepEqual(got.Tip, tip) {
		t.Errorf("the tip over blog reads %q", got.Tip)
	}
	if got.TipAway != "" {
		t.Errorf("a tip stands over the empty dial: %q", got.TipAway)
	}
	if !reflect.DeepEqual(got.Clicked, []openedTalk{{"blog", "id-blog"}}) {
		t.Errorf("a click on blog opened %v, not the session by its name and id", got.Clicked)
	}
	if got.AfterDrag != 1 || contains("notes", got.OldSpotAfterDrag) {
		t.Errorf("a drag opened something (%d opened) or left notes where it stood (%v)", got.AfterDrag, got.OldSpotAfterDrag)
	}
	if !reflect.DeepEqual(got.AfterReset, []string{"notes"}) {
		t.Errorf("after Reset view a click where notes stands opened %v", got.AfterReset)
	}

	m := got.Menu
	if !m.Prevented || m.Label != "session shop" {
		t.Errorf("a right click on shop opened %q (taken from the browser: %v)", m.Label, m.Prevented)
	}
	for _, item := range []string{"Open", "Rename…", "Copy the session ID", "Stop the turn", "Restart the session", "Close the session"} {
		if !contains(item, m.Items) {
			t.Errorf("the menu of shop has no %q: %v", item, m.Items)
		}
	}
	if got.MenuOpened != (openedTalk{"shop", "id-shop"}) {
		t.Errorf("Open in the menu opened %v", got.MenuOpened)
	}
	if got.MenuAway.Prevented || got.MenuAway.Menu {
		t.Error("a right click on the empty dial takes the browser's menu away with nothing in its place")
	}
}

// The graph is drawn when what it shows changes, not on every frame. Where
// less motion is asked for it does not turn by itself; switched on, it turns
// frame after frame while the page is seen and stops while it is hidden.
func TestTheGraphDrawsOnlyWhenSomethingMoves(t *testing.T) {
	got := runHomeGraph(t)

	if got.Still != 0 {
		t.Errorf("the graph drew %d frames while nothing changed", got.Still)
	}
	if got.AutoAtStart != "false" {
		t.Errorf("Auto-rotate starts %q where the system asks for less motion", got.AutoAtStart)
	}
	if got.Turning < 6 || got.ShownAgain < 6 {
		t.Errorf("switched on, the graph drew %d frames of twelve, and %d once the page was seen again", got.Turning, got.ShownAgain)
	}
	if got.HiddenDraws != 0 || got.OffDraws != 0 {
		t.Errorf("the graph drew %d frames while the page was hidden and %d once switched off", got.HiddenDraws, got.OffDraws)
	}
}

// One switch sets the period of the four tiles of the home — the spend by
// hour, the projects, the models, the tools — and the layer a tile opens
// takes the period it stands on. It starts on two weeks.
func TestOneSwitchSetsThePeriodOfTheFourTiles(t *testing.T) {
	got := runHomeGraph(t)
	p := got.Period

	four := []string{"breakdown", "models", "series", "summary", "tools"}
	if p.Start != "14d" || p.On != "14 days" || !reflect.DeepEqual(p.Before, four) {
		t.Errorf("the home starts on %q (%q lit) and asks %v for it: the four tiles start on two weeks", p.Start, p.On, p.Before)
	}
	if p.OnAfter != "7 days" || !reflect.DeepEqual(p.After, four) {
		t.Errorf("after 7 days was pressed, %q is lit and %v are asked for the week: every tile follows the one switch",
			p.OnAfter, p.After)
	}
	if p.Thirty != 0 {
		t.Errorf("%d requests went out for thirty days: a tile still keeps a period of its own", p.Thirty)
	}
	if p.Hours != "Usage by hour" {
		t.Errorf("the spend of a week is titled %q", p.Hours)
	}
	if p.Layer != "7 days" || p.Breakdown != "7 days" {
		t.Errorf("the layers of the models and the projects open on %q and %q, not on the period of the switch", p.Layer, p.Breakdown)
	}
}

// The picture of the graph is handled as a map: a drag moves it, the wheel
// scales it round the pointer within its bounds and does not scroll the page,
// a drag with Shift or the middle button turns it, Reset view brings back the
// turn, the place and the scale. A press that travelled opens nothing.
func TestTheGraphIsHandledAsAMap(t *testing.T) {
	got := runHomeGraph(t)
	g := got.Gestures

	if g.PanDuring > 0.01 || g.PanAfter > 0.01 {
		t.Errorf("a drag of 60×25 moved the names by up to %.2f px off it while dragged and %.2f px once let go: "+
			"the picture moves whole, every name with its mark", g.PanDuring, g.PanAfter)
	}
	if g.PanTip != "blog" {
		t.Errorf("after the move the pointer over blog's new place finds %q", g.PanTip)
	}
	if g.PressAfterPan != 0 {
		t.Errorf("a press that travelled opened %d sessions", g.PressAfterPan)
	}
	if g.ResetPan > 0.01 {
		t.Errorf("Reset view left the names %.2f px from where they stood", g.ResetPan)
	}
	if g.PanEdge.Name == "" || g.PanEdge.Off > 0.01 || g.PanEdge.During >= 0 {
		t.Errorf("a move that took %q to the edge (its name at x %.1f) moved the names up to %.2f px off the move "+
			"while it went on: a name keeps its side through a move", g.PanEdge.Name, g.PanEdge.During, g.PanEdge.Off)
	}
	if g.PanEdge.After < 0 {
		t.Errorf("once the move ended the name of %q still starts over the edge, at x %.1f: the end of a move "+
			"places again a name the move took off the picture", g.PanEdge.Name, g.PanEdge.After)
	}

	if !g.WheelPrevent {
		t.Error("the wheel over the graph scrolls the page as it zooms")
	}
	if math.Abs(g.ZoomBy-math.Exp(0.45)) > 0.01 || g.ZoomKept > 0.5 || g.ZoomOff > 0.5 {
		t.Errorf("300 px of the wheel scaled the picture by %.3f (want %.3f), moved the mark under the pointer by %.2f px "+
			"and the others up to %.2f px off the scale round it: the zoom keeps the point under the pointer, "+
			"and every name rides with its mark", g.ZoomBy, math.Exp(0.45), g.ZoomKept, g.ZoomOff)
	}
	if g.ZoomOut.Bound >= 1 || g.ZoomIn.Bound <= 1 || g.ZoomIn.Tip != "cx-thread" || g.ZoomOut.Tip != "stray" {
		t.Errorf("the zoom, driven past its bounds %v and %v, is not found there (%q in, %q out)",
			g.ZoomOut.Bound, g.ZoomIn.Bound, g.ZoomIn.Tip, g.ZoomOut.Tip)
	}

	if g.TurnApart < 10 || g.MiddleApart < 5 {
		t.Errorf("a drag with Shift parted the names by %.1f px and with the middle button by %.1f px: both turn the scene",
			g.TurnApart, g.MiddleApart)
	}
	if !g.MiddlePrevented {
		t.Error("the middle button on the graph starts the browser's scrolling as it turns the scene")
	}
	if !reflect.DeepEqual(g.ResetTips, []string{"shop", "blog", "notes", "draft", "stray", "cx-thread"}) || g.ResetOpened != 0 {
		t.Errorf("after a move, a zoom and Reset view the marks are found as %v, %d opened on the way",
			g.ResetTips, g.ResetOpened)
	}
	if !strings.Contains(got.Help, "drag to move") || !strings.Contains(got.Help, "Shift-drag to turn") ||
		!strings.Contains(got.Help, "wheel to zoom") {
		t.Errorf("the legend says %q of the gestures", got.Help)
	}
}

// A name keeps its side while the scene turns: it leaves it only when the
// side covers something — another name, a caption, a mark, the edge — and
// only for a side that covers less than half of that. A frame of a move or a
// zoom alone moves no name off its side. Turned a little at a time, names do
// not jump across their marks.
func TestTheNamesKeepTheirSides(t *testing.T) {
	got := runHomeGraph(t)
	k := got.Keep

	if k.Steps != 61 || k.Names != 14 {
		t.Fatalf("the crowded ring drew %d steps with %d names — the fixture ran short", k.Steps, k.Names)
	}
	if len(k.Unforced) != 0 {
		t.Errorf("names left a side that covered nothing: %v", k.Unforced)
	}
	if len(k.Unearned) != 0 {
		t.Errorf("names crossed their marks for a side not markedly clearer than the one they left: %v", k.Unearned)
	}
	if k.Changes > k.Steps {
		t.Errorf("fourteen names changed sides %d times over %d small steps", k.Changes, k.Steps)
	}
	if k.Frozen != 0 {
		t.Errorf("%d names changed sides in frames of a move or a zoom", k.Frozen)
	}
	if len(got.Gestures.TurnJumps) > 2 {
		t.Errorf("names jumped across their marks in a slow turn of the home graph: %v", got.Gestures.TurnJumps)
	}
}
