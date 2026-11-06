package check

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type homeGraph struct {
	NotesOf map[string]string `json:"notesOf"`
	Spins   struct {
		Turn      float64 `json:"turn"`
		InnerTurn float64 `json:"innerTurn"`
		Orbits    []struct {
			Key      string  `json:"key"`
			R        float64 `json:"R"`
			Spin     float64 `json:"spin"`
			IdleSpin float64 `json:"idleSpin"`
			LiveSpin float64 `json:"liveSpin"`
		} `json:"orbits"`
		Moons   map[string]float64 `json:"moons"`
		Stirred float64            `json:"stirred"`
		OffBy   float64            `json:"offBy"`
		Kept    int                `json:"kept"`
	} `json:"spins"`
	Motion struct {
		AtStart    string  `json:"atStart"`
		OffDraws   int     `json:"offDraws"`
		Kept       string  `json:"kept"`
		FPS        float64 `json:"fps"`
		Moved      float64 `json:"moved"`
		Hidden     int     `json:"hiddenDraws"`
		ShownAgain int     `json:"shownAgain"`
		Away       int     `json:"awayDraws"`
		Back       int     `json:"backDraws"`
		Reopened   string  `json:"reopened"`
		KeptOff    string  `json:"keptOff"`
		OffAgain   int     `json:"offAgain"`
		PlainStart string  `json:"plainStart"`
		Made       int     `json:"made"`
	} `json:"motion"`
	Systems    []graphSystem     `json:"systems"`
	Moon       graphMoon         `json:"moon"`
	Grandchild graphMoon         `json:"grandchild"`
	OrbitOf    map[string]string `json:"orbitOf"`
	Weights    struct {
		Order     []string       `json:"order"`
		Weights   map[string]int `json:"weights"`
		TieFresh  []string       `json:"tieFresh"`
		TieKept   []string       `json:"tieKept"`
		Later     []string       `json:"later"`
		Reordered []string       `json:"reordered"`
	} `json:"weights"`
	Order  []string `json:"order"`
	Galaxy struct {
		Nearest string `json:"nearest"`
	} `json:"galaxy"`
	Written  map[string]int `json:"written"`
	Carousel struct {
		Order       []string `json:"order"`
		First       []string `json:"first"`
		Prevented   bool     `json:"prevented"`
		Instant     int      `json:"instant"`
		Up          []string `json:"up"`
		FrontSays   string   `json:"frontSays"`
		Down        []string `json:"down"`
		Wrap        []string `json:"wrap"`
		Clicked     []string `json:"clicked"`
		ClickOpens  int      `json:"clickOpened"`
		OrbitClick  []string `json:"orbitClicked"`
		OrbitOpens  int      `json:"orbitOpened"`
		OrbitStar   int      `json:"orbitFromStar"`
		OrbitMarks  int      `json:"orbitFromMarks"`
		BehindTip   string   `json:"behindTip"`
		PlanetClick []string `json:"planetClicked"`
		PlanetOpens int      `json:"planetOpened"`
		Kept        []string `json:"kept"`
		Reorder     []string `json:"reorder"`
		Reset       []string `json:"reset"`
		Midway      string   `json:"midway"`
		Glide       int      `json:"glide"`
		Glided      []string `json:"glided"`
		Five        struct {
			First  string         `json:"first"`
			Orbits map[string]int `json:"orbits"`
			Front  string         `json:"front"`
		} `json:"five"`
		OrbitTie struct {
			Apart int    `json:"apart"`
			Kept  string `json:"kept"`
			Fresh string `json:"fresh"`
		} `json:"orbitTie"`
		OrbitGlide struct {
			Moved     int    `json:"moved"`
			Midway    string `json:"midway"`
			Frames    int    `json:"frames"`
			Arrived   string `json:"arrived"`
			Back      string `json:"back"`
			BackDraws int    `json:"backDraws"`
		} `json:"orbitGlide"`
	} `json:"carousel"`
	Quiet struct {
		Behind graphQuiet `json:"behind"`
		Front  graphQuiet `json:"front"`
	} `json:"quiet"`
	Notes            string        `json:"notes"`
	Head             string        `json:"head"`
	Titles           []string      `json:"titles"`
	Jumps            []string      `json:"jumps"`
	Sections         []string      `json:"sections"`
	Tip              []string      `json:"tip"`
	TipAway          string        `json:"tipAway"`
	Clicked          []openedTalk  `json:"clicked"`
	AfterDrag        int           `json:"afterDrag"`
	OldSpotAfterDrag []string      `json:"oldSpotAfterDrag"`
	AfterReset       []string      `json:"afterReset"`
	Menu             homeGraphMenu `json:"menu"`
	MenuOpened       openedTalk    `json:"menuOpened"`
	MenuBehind       bool          `json:"menuBehind"`
	MenuAway         struct {
		Prevented bool `json:"prevented"`
		Menu      bool `json:"menu"`
	} `json:"menuAway"`
	Still    int      `json:"still"`
	Help     string   `json:"help"`
	Legend   []string `json:"legend"`
	Gestures struct {
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
		ZoomOutBy    float64 `json:"zoomOutBy"`
		ZoomOutOff   float64 `json:"zoomOutOff"`
		ZoomIn       struct {
			Bound float64 `json:"bound"`
			Tip   string  `json:"tip"`
		} `json:"zoomIn"`
		ZoomOut struct {
			Bound float64 `json:"bound"`
			Tip   string  `json:"tip"`
		} `json:"zoomOut"`
		TurnApart       float64  `json:"turnApart"`
		MiddlePrevented bool     `json:"middlePrevented"`
		MiddleApart     float64  `json:"middleApart"`
		ResetTips       []string `json:"resetTips"`
		ResetOpened     int      `json:"resetOpened"`
	} `json:"gestures"`
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

type graphSystem struct {
	Name    string `json:"name"`
	Live    int    `json:"live"`
	Summary string `json:"summary"`
	Orbits  []struct {
		Key     string   `json:"key"`
		Name    string   `json:"name"`
		Label   bool     `json:"label"`
		Planets []string `json:"planets"`
	} `json:"orbits"`
}

type graphMoon struct {
	Depth  int    `json:"depth"`
	Parent string `json:"parent"`
	System string `json:"system"`
}

type graphQuiet struct {
	Hidden    []string `json:"hidden"`
	Shown     int      `json:"shown"`
	ShownKeys []string `json:"shownKeys"`
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

// A session stands on the orbit of its project; a session another one opened
// is its moon, on the small orbit round it, whatever the project of its
// directory, and a grandchild is a moon of the moon. How long ago it last
// asked is told beside its mark and in its tip.
func TestAPlanetStandsOnTheOrbitOfItsProject(t *testing.T) {
	got := runHomeGraph(t)

	if got.NotesOf["draft"] != "idle, 5 min" || got.NotesOf["stray"] != "idle, 1 d" {
		t.Errorf("draft says %q and stray %q beside their marks: a session that has asked nothing counts from its "+
			"start, one that names neither a day", got.NotesOf["draft"], got.NotesOf["stray"])
	}

	m := got.Moon
	if m.Parent != "blog" || m.Depth != 1 || m.System != "client" {
		t.Errorf("the thread blog opened in the directory of drafts is a moon of %q at depth %d in %q: "+
			"a child stands on the orbit round its parent", m.Parent, m.Depth, m.System)
	}
	g := got.Grandchild
	if g.Parent != "codex-1" || g.Depth != 2 {
		t.Errorf("the session the thread opened is a moon of %q at depth %d: a grandchild is a moon of the moon",
			g.Parent, g.Depth)
	}
	if got.OrbitOf["codex-1"] != "blog" || got.OrbitOf["deep"] != "blog" {
		t.Errorf("the moons tell of the orbits %q and %q: a moon belongs where its planet does", got.OrbitOf["codex-1"], got.OrbitOf["deep"])
	}
	if got.Notes != "waits for an answer, 1 h" {
		t.Errorf("the waiting session says %q beside its mark: the words of its row and how long it has waited", got.Notes)
	}
}

// A contour is a system: an orbit for every project with a live session, the
// equal sessions of a project as planets of one orbit, the sessions of no
// project on an orbit of their own — here stray alone weighs less than shop at
// work and stands inside it. An orbit names its project only where its planets
// do not, and only in the system in front: personal behind writes neither.
func TestAContourIsASystemOfItsProjects(t *testing.T) {
	got := runHomeGraph(t)

	type orbit struct {
		key     string
		planets string
		label   bool
	}
	want := map[string][]orbit{
		"personal": {{"outside", "stray", true}, {"p11", "shop", false}},
		"client":   {{"p21", "draft", true}, {"p22", "blog,notes", false}},
		"lab":      {{"p31", "bench-1", false}},
	}
	names := []string{}
	for _, sys := range got.Systems {
		names = append(names, sys.Name)
		have := []orbit{}
		for _, o := range sys.Orbits {
			planets := append([]string(nil), o.Planets...)
			sort.Strings(planets)
			have = append(have, orbit{o.Key, strings.Join(planets, ","), o.Label})
		}
		if !reflect.DeepEqual(have, want[sys.Name]) {
			t.Errorf("%s holds the orbits %v, want %v: an orbit a project, the lighter inside, its sessions its "+
				"planets, the moons off it", sys.Name, have, want[sys.Name])
		}
	}
	if !reflect.DeepEqual(names, []string{"personal", "client", "lab"}) {
		t.Errorf("the systems are %v, not the contours in the order of the map", names)
	}
	w := got.Written
	if w["counts"] != 0 {
		t.Errorf("the frame writes the counts of %d stars under their names: a star says its name, its counts are in its tip", w["counts"])
	}
	if w["blog"] != 1 || w["drafts"] != 1 || w["client"] != 1 || w["outside"] != 0 || w["shop"] != 0 {
		t.Errorf("the frame writes %v: blog once, as its planet, drafts once, as its orbit, client once, as its star, "+
			"and of personal behind neither shop nor its orbit outside the map", w)
	}
	if w["helper"] != 1 || w["thread"] != 0 {
		t.Errorf("the thread of codex blog opened is written %d times as \"codex · blog\" and %d as its own name: "+
			"a helper of a session is written as that session's helper", w["helper"], w["thread"])
	}
	summary := map[string]string{}
	for _, sys := range got.Systems {
		summary[sys.Name] = fmt.Sprintf("%d %s", sys.Live, sys.Summary)
	}
	if summary["client"] != "3 5 sessions · 1 busy · 1 waiting" || summary["personal"] != "1 2 sessions · 1 busy" ||
		summary["lab"] != "0 1 session" {
		t.Errorf("the stars say %v: a star counts its sessions, the busy and the waiting, and is as alive as the busy "+
			"and twice the waiting", summary)
	}
}

// The galaxy opens with the liveliest contour in front, nearest the eye.
func TestTheGalaxyOpensOnTheLiveliest(t *testing.T) {
	got := runHomeGraph(t)
	if !reflect.DeepEqual(got.Order, []string{"client", "personal", "lab"}) || got.Galaxy.Nearest != "client" {
		t.Errorf("the galaxy opens in the order %v with %q nearest: the liveliest in front", got.Order, got.Galaxy.Nearest)
	}
}

// The wheel with Shift brings the next contour forward, round and round both
// ways, and zooms nothing; a click on a star, or among the orbits of a system
// behind, brings its contour forward and opens nothing, and so does a contour
// of one orbit against one of five. Who is in front changes only by the
// person: a contour that wakes up does not jump the queue until Reset view
// orders them anew. Where less motion is asked for a system stands in its
// place at once; elsewhere it glides there over some frames.
func TestTheWheelWithShiftBringsAContourForward(t *testing.T) {
	got := runHomeGraph(t)
	c := got.Carousel
	all := []string{"client", "personal", "lab"}

	if !reflect.DeepEqual(c.First, all) {
		t.Fatalf("the stars are not where the galaxy puts them on the first view: %v", c.First)
	}
	if !c.Prevented {
		t.Error("the wheel with Shift scrolls the page under the graph")
	}
	for _, step := range []struct {
		what string
		got  []string
	}{
		{"up from client — personal in front", c.Up},
		{"down from personal — client in front", c.Down},
		{"down from client — round to lab", c.Wrap},
		{"a click on personal's star", c.Clicked},
		{"a click among client's orbits", c.OrbitClick},
		{"a click on a planet of lab behind", c.PlanetClick},
	} {
		if !reflect.DeepEqual(step.got, all) {
			t.Errorf("after %s the stars are found as %v: the contour comes forward, and nothing is zoomed", step.what, step.got)
		}
	}
	if c.FrontSays != "the contour in front" {
		t.Errorf("the star in front says %q", c.FrontSays)
	}
	if c.ClickOpens != 0 {
		t.Errorf("a click on a star opened %d sessions", c.ClickOpens)
	}
	if c.OrbitMarks < 30 {
		t.Errorf("the click among client's orbits fell %d px from its star and %d px from its nearest mark: "+
			"it asks the star or a planet, not the orbits", c.OrbitStar, c.OrbitMarks)
	}
	if c.PlanetOpens != 0 {
		t.Errorf("a click on a planet of a system behind opened %d sessions: only the system in front opens its bodies", c.PlanetOpens)
	}
	if c.BehindTip != "system lab" {
		t.Errorf("the pointer over a planet of lab behind shows %q: the system taken whole", c.BehindTip)
	}
	if c.OrbitOpens != 0 {
		t.Errorf("a click among the orbits of a system behind opened %d sessions", c.OrbitOpens)
	}
	if c.Instant > 2 {
		t.Errorf("where less motion is asked for, a contour came forward over %d frames", c.Instant)
	}
	if !reflect.DeepEqual(c.Kept, all) {
		t.Errorf("once lab woke up the stars were found as %v where they stood: the order moved by itself", c.Kept)
	}
	if len(c.Reorder) == 0 || c.Reorder[0] != "lab" || !reflect.DeepEqual(c.Reset, all) {
		t.Errorf("Reset view ordered the contours %v and the stars were found as %v: it puts the liveliest in front",
			c.Reorder, c.Reset)
	}
	five := c.Five
	if five.First != "big" || five.Orbits["big"] != 5 || five.Orbits["small"] != 1 || five.Front != "small" {
		t.Errorf("a contour of one orbit against one of five, with %q in front at first and orbits %v, brings %q "+
			"forward: the one of one orbit comes forward as any other", five.First, five.Orbits, five.Front)
	}
	if c.Glide < 6 || c.Midway != "" || !reflect.DeepEqual(c.Glided, all) {
		t.Errorf("outside less motion the contour came forward over %d frames, was already found in place midway (%q), "+
			"and the stars at the end were found as %v", c.Glide, c.Midway, c.Glided)
	}
}

// The picture of a planet is made once: while the planets go round not one
// picture is made anew.
func TestAPlanetsPictureIsMadeOnce(t *testing.T) {
	got := runHomeGraph(t)
	if got.Motion.Made != 0 {
		t.Errorf("%d pictures of planets were made while they went round for a second and a half: they are made once", got.Motion.Made)
	}
}

// The tile of the sessions is gone from the home: its numbers are the header
// of the graph, and the rows beside the graph lead to the containers and the
// machine.
func TestTheHomeHasNoTileOfTheSessions(t *testing.T) {
	got := runHomeGraph(t)

	if got.Head != "8 live in 3 contours · 2 busy · 1 waiting for an answer" {
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
}

// A session of the graph is the session of its row: the pointer over its mark
// names it, a click opens it as the row does, a right click opens the row's
// menu. A drag turns the scene and opens nothing; Reset view brings it back.
func TestTheGraphOpensASessionAsItsRowDoes(t *testing.T) {
	got := runHomeGraph(t)

	tip := []string{"blog", "client · blog · context 52% · last reply 1 h ago", "waits for an answer",
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
	if got.MenuBehind {
		t.Error("a right click on a planet of a system behind opened a session's menu: only the system in front answers for its bodies")
	}
	if got.MenuAway.Prevented || got.MenuAway.Menu {
		t.Error("a right click on the empty dial takes the browser's menu away with nothing in its place")
	}
}

// With Motion off the graph is drawn when what it shows changes, not on every
// frame. Motion is off where less motion is asked for and nothing is kept;
// switched on, the planets go round, no more than thirty frames a second and
// none while the page is hidden or the graph is off the screen, and the
// choice is kept for the next time the page opens; with nothing kept and no
// wish for less motion it is on.
func TestTheGraphDrawsOnlyWhenSomethingMoves(t *testing.T) {
	got := runHomeGraph(t)

	if got.Still != 0 {
		t.Errorf("the graph drew %d frames while nothing changed", got.Still)
	}
	m := got.Motion
	if m.AtStart != "false" || m.OffDraws != 0 {
		t.Errorf("where less motion is asked for, Motion starts %q and the graph draws %d frames of twenty", m.AtStart, m.OffDraws)
	}
	if m.Kept != "on" || m.Moved < 0.1 {
		t.Errorf("switched on, Motion is kept as %q and the names move %v px in a second and a half: the planets go round, "+
			"slowly", m.Kept, m.Moved)
	}
	if m.FPS > 31 || m.FPS < 15 {
		t.Errorf("the planets go round at %v frames a second: at most thirty", m.FPS)
	}
	if m.Hidden != 0 || m.ShownAgain < 4 {
		t.Errorf("the graph drew %d frames while the page was hidden, %d of twelve once seen again", m.Hidden, m.ShownAgain)
	}
	if m.Away != 0 || m.Back < 4 {
		t.Errorf("the graph drew %d frames while off the screen, %d of twelve once back on it", m.Away, m.Back)
	}
	if m.Reopened != "true" || m.KeptOff != "off" || m.OffAgain != 0 {
		t.Errorf("opened again Motion is %q, switched off it is kept as %q and the graph draws %d frames: the person's "+
			"choice outlives the page", m.Reopened, m.KeptOff, m.OffAgain)
	}
	if m.PlainStart != "true" {
		t.Errorf("with nothing kept and no wish for less motion, Motion starts %q", m.PlainStart)
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
		t.Errorf("Reset view left the names %.2f px off the way back from where the moves took them", g.ResetPan)
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
	if math.Abs(g.ZoomOutBy-math.Exp(-0.45)) > 0.01 || g.ZoomOutOff > 1 {
		t.Errorf("the wheel out scales the picture by %.3f (want %.3f) with the names up to %.2f px off their marks: "+
			"the names crowd as the picture shrinks, and still every one rides with its mark", g.ZoomOutBy, math.Exp(-0.45), g.ZoomOutOff)
	}
	if math.Abs(g.ZoomBy-math.Exp(0.45)) > 0.01 || g.ZoomKept > 1 || g.ZoomOff > 1 {
		t.Errorf("300 px of the wheel scaled the picture by %.3f (want %.3f), moved the mark under the pointer by %.2f px "+
			"and the others up to %.2f px off the scale round it: the zoom keeps the point under the pointer, "+
			"and every name rides with its mark", g.ZoomBy, math.Exp(0.45), g.ZoomKept, g.ZoomOff)
	}
	if g.ZoomOut.Bound >= 1 || g.ZoomIn.Bound <= 1 || g.ZoomIn.Tip != "cx-thread" || g.ZoomOut.Tip != "draft" {
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
	if !reflect.DeepEqual(g.ResetTips, []string{"blog", "notes", "draft", "cx-thread", "deep"}) || g.ResetOpened != 0 {
		t.Errorf("after a move, a zoom and Reset view the marks are found as %v, %d opened on the way",
			g.ResetTips, g.ResetOpened)
	}
	if !strings.Contains(got.Help, "drag to move") || !strings.Contains(got.Help, "Shift-drag to turn") ||
		!strings.Contains(got.Help, "wheel to zoom") {
		t.Errorf("the legend says %q of the gestures", got.Help)
	}
}

// A system behind the front one is one target and small with distance: it
// says its own name and that of a session waiting for the person, and no
// other — the names of its planets on leaders round a small system are a
// tangle. The system in front writes every name.
func TestABehindSaysItsNameAndTheWait(t *testing.T) {
	q := runHomeGraph(t).Quiet
	shown := append([]string(nil), q.Behind.ShownKeys...)
	sort.Strings(shown)
	if !reflect.DeepEqual(shown, []string{"*personal", "node-f"}) {
		t.Errorf("a system behind the front writes %v: only its own name and the session waiting for the person", shown)
	}
	if contains("node-f", q.Behind.Hidden) {
		t.Error("a session waiting for the person lost its name behind the front")
	}
	if len(q.Front.Hidden) != 0 {
		t.Errorf("the system in front hides the names %v", q.Front.Hidden)
	}
}

// The orbits of a contour go out by weight: a session of the project apiece,
// its moons of every depth among them, a busy one once more and a waiting one
// twice. The small and quiet stand inside, the large and lively outside.
// Orbits of equal weight keep the order they were drawn in; a snapshot that
// leaves the order of the weights as it was moves no orbit, one that changes
// it does.
func TestTheOrbitsGoOutByWeight(t *testing.T) {
	w := runHomeGraph(t).Weights
	if want := []string{"p43", "p42", "p44", "p41"}; !reflect.DeepEqual(w.Order, want) {
		t.Errorf("solo (one idle), pair (two), late (one waiting) and big (four and two moons) go out as %v, want %v: "+
			"by weight, not by the map; weights %v", w.Order, want, w.Weights)
	}
	if want := map[string]int{"p43": 1, "p42": 2, "p44": 3, "p41": 6}; !reflect.DeepEqual(w.Weights, want) {
		t.Errorf("the orbits weigh %v, want %v: a session apiece with its moons, a waiting one twice more", w.Weights, want)
	}
	if want := []string{"p42", "p43", "p44", "p41"}; !reflect.DeepEqual(w.TieFresh, want) {
		t.Errorf("with nothing drawn before, pair and solo of equal weight stand %v: the order of the map, want %v", w.TieFresh, want)
	}
	if !reflect.DeepEqual(w.TieKept, w.Order) {
		t.Errorf("solo grows to the weight of pair and the orbits go %v after %v: equal weights keep their places", w.TieKept, w.Order)
	}
	if !reflect.DeepEqual(w.Later, w.Order) {
		t.Errorf("a later snapshot with the same order of weights draws the orbits %v after %v", w.Later, w.Order)
	}
	if want := []string{"p42", "p44", "p41", "p43"}; !reflect.DeepEqual(w.Reordered, want) {
		t.Errorf("solo with three sessions at work outweighs big and the orbits go %v, want %v", w.Reordered, want)
	}
}

// The legend names what the marks of the graph mean, and nothing of a place
// on an orbit: a place there means nothing to name.
func TestTheLegendSaysWhatTheMarksMean(t *testing.T) {
	got := runHomeGraph(t)
	if want := []string{"busy", "idle", "waiting for an answer", "lit part — context", "background work", "moons — sessions it opened"}; !reflect.DeepEqual(got.Legend, want) {
		t.Errorf("the legend says %v, want %v: a place on an orbit means nothing to name", got.Legend, want)
	}
}

// A snapshot that moves an orbit in or out carries it there over some frames,
// at once where less motion is asked for; one that leaves two orbits of equal
// weight moves neither.
func TestAnOrbitGlidesToItsNewPlace(t *testing.T) {
	c := runHomeGraph(t).Carousel
	g := c.OrbitGlide
	if g.Moved < 40 {
		t.Fatalf("shop moves %d px as its orbit goes in: the fixture is to move it further than a mark", g.Moved)
	}
	if g.Midway == "shop" || g.Arrived != "shop" || g.Frames < 6 {
		t.Errorf("a few frames in, the tip at shop's new place says %q, %d frames later %q: shop glides in, not jumps", g.Midway, g.Frames, g.Arrived)
	}
	if g.Back != "shop" || g.BackDraws > 3 {
		t.Errorf("where less motion is asked for, the snapshot back draws %d frames and shop's old place says %q: "+
			"it is there at once", g.BackDraws, g.Back)
	}
	tie := c.OrbitTie
	if tie.Apart < 40 {
		t.Fatalf("shop's place by the order of the map stands %d px from its place kept: the fixture is to tell them apart", tie.Apart)
	}
	if tie.Kept != "shop" || tie.Fresh == "shop" {
		t.Errorf("shop stops working and weighs as stray: its kept place says %q, its place by the map %q — "+
			"orbits of equal weight stay where they were drawn", tie.Kept, tie.Fresh)
	}
}

// The orbits turn as the planets of a sun go round: the innermost orbit of a
// system once in three minutes, the orbits further out slower, the time of a
// turn as the radius to the power of one and a half, and an orbit where every
// session idles at a quarter of that. Moons go round their planet faster
// than the planet round its star, and theirs faster still. A turn moves every
// planet and every moon by its own spin.
func TestTheOrbitsTurnAsPlanetsDo(t *testing.T) {
	sp := runHomeGraph(t).Spins
	if sp.InnerTurn < 120 || sp.InnerTurn > 240 {
		t.Errorf("the innermost orbit, live, turns once in %v s: two to four minutes", sp.InnerTurn)
	}
	if len(sp.Orbits) != 4 {
		t.Fatalf("the spins of %d orbits came back, not four", len(sp.Orbits))
	}
	idle := map[string]bool{"p43": true, "p42": true, "p41": true, "p44": false}
	for i, o := range sp.Orbits {
		want := o.LiveSpin
		if idle[o.Key] {
			want = o.IdleSpin
		}
		if math.Abs(o.Spin-want) > 1e-12 {
			t.Errorf("the orbit %s turns at %v rad/s, want %v: an orbit of idle sessions turns at a quarter", o.Key, o.Spin, want)
		}
		if math.Abs(o.LiveSpin/o.IdleSpin-4) > 1e-9 {
			t.Errorf("at the radius of %s an idle orbit turns at %v of a live one: a quarter", o.Key, o.IdleSpin/o.LiveSpin)
		}
		if i > 0 {
			in := sp.Orbits[i-1]
			if ratio, kepler := in.LiveSpin/o.LiveSpin, math.Pow(o.R/in.R, 1.5); math.Abs(ratio-kepler) > 1e-9 || ratio <= 1 {
				t.Errorf("the orbit at %v turns %v times as fast as the one at %v, want %v: the inner ones faster, as R^1.5",
					in.R, ratio, o.R, kepler)
			}
		}
	}
	if math.Abs(sp.Stirred-1) > 1e-9 {
		t.Errorf("big idles but for a moon at work, and turns at %v of a live orbit: a family at work keeps its orbit live", sp.Stirred)
	}
	m := sp.Moons
	if !(m["b1"] > m["big"] && m["m1"] > m["b1"]) {
		t.Errorf("the orbit of big turns at %v, the moons of b1 round it at %v, those of m1 at %v: moons go round faster "+
			"than their planet", m["big"], m["b1"], m["m1"])
	}
	if sp.OffBy > 1e-9 || sp.Kept != 6 {
		t.Errorf("ten seconds of turning leave a node %v off where its spin puts it, and keep %d turns: four orbits and two "+
			"planets with moons", sp.OffBy, sp.Kept)
	}
}
