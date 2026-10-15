package check

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
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
	Angles     map[string]float64 `json:"angles"`
	DialOf     map[string]float64 `json:"dialOf"`
	Systems    []graphSystem      `json:"systems"`
	Moon       graphMoon          `json:"moon"`
	Grandchild graphMoon          `json:"grandchild"`
	MoonSizes  []float64          `json:"moonSizes"`
	OrbitOf    map[string]string  `json:"orbitOf"`
	Stars      []float64          `json:"stars"`
	Order      []string           `json:"order"`
	Galaxy     struct {
		Covered    []string `json:"covered"`
		Outside    []string `json:"outside"`
		Nearest    string   `json:"nearest"`
		Crowded    []string `json:"crowded"`
		FrontShare float64  `json:"frontShare"`
		Alone      float64  `json:"alone"`
		Behind     []struct {
			Name  string  `json:"name"`
			Above bool    `json:"above"`
			Side  string  `json:"side"`
			Share float64 `json:"share"`
		} `json:"behind"`
	} `json:"galaxy"`
	Written  map[string]int `json:"written"`
	Carousel struct {
		Order       []string  `json:"order"`
		First       []string  `json:"first"`
		Prevented   bool      `json:"prevented"`
		Instant     int       `json:"instant"`
		Up          []string  `json:"up"`
		FrontSays   string    `json:"frontSays"`
		Down        []string  `json:"down"`
		Wrap        []string  `json:"wrap"`
		Clicked     []string  `json:"clicked"`
		ClickOpens  int       `json:"clickOpened"`
		Kept        []string  `json:"kept"`
		Reorder     []string  `json:"reorder"`
		Reset       []string  `json:"reset"`
		Midway      string    `json:"midway"`
		Glide       int       `json:"glide"`
		Glided      []string  `json:"glided"`
		Largest     []string  `json:"largest"`
		Grows       []float64 `json:"grows"`
		Shrinks     []float64 `json:"shrinks"`
		Straightens []float64 `json:"straightens"`
		Five        struct {
			First   string         `json:"first"`
			Orbits  map[string]int `json:"orbits"`
			Front   string         `json:"front"`
			Largest string         `json:"largest"`
			Areas   map[string]int `json:"areas"`
		} `json:"five"`
	} `json:"carousel"`
	Phase map[string]json.RawMessage `json:"phase"`
	Quiet struct {
		Behind graphQuiet `json:"behind"`
		Front  graphQuiet `json:"front"`
	} `json:"quiet"`
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

type graphSystem struct {
	Name    string `json:"name"`
	Live    int    `json:"live"`
	Summary string `json:"summary"`
	Orbits  []struct {
		Key     string   `json:"key"`
		Name    string   `json:"name"`
		R       float64  `json:"R"`
		Label   bool     `json:"label"`
		Planets []string `json:"planets"`
	} `json:"orbits"`
}

type graphMoon struct {
	Depth  int     `json:"depth"`
	Parent string  `json:"parent"`
	System string  `json:"system"`
	OffBy  float64 `json:"offBy"`
	Orbit  float64 `json:"orbit"`
}

type graphQuiet struct {
	Hidden  []string `json:"hidden"`
	Shown   int      `json:"shown"`
	Clashes int      `json:"clashes"`
}

type graphPhase struct {
	Share float64 `json:"share"`
	RGB   []int   `json:"rgb"`
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

// A session stands on the orbit of its project at the angle of its last
// request: the meridian of now at zero, a log scale to a day at 300 degrees,
// anything older at the end of it. A session that has not asked yet counts
// from its start, one that names neither stands at the end. Two that asked at
// once are pulled apart toward older only, and a session another one opened
// is its moon, on the small orbit round it at the angle of its own request,
// whatever the project of its directory; a grandchild is a moon of the moon.
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

	for name, at := range map[string]string{"blog": "blog", "draft": "draft", "shop": "shop", "bench-1": "bench"} {
		if math.Abs(got.Angles[name]-got.DialOf[at]) > 0.11 {
			t.Errorf("%s stands at %v°, while its last request — or its start, having asked nothing — puts it at %v°",
				name, got.Angles[name], got.DialOf[at])
		}
	}
	if got.Angles["stray"] != 300 {
		t.Errorf("a session with neither a request nor a start stands at %v°, not at the end of the scale", got.Angles["stray"])
	}
	if got.Angles["notes"] <= got.Angles["blog"] {
		t.Errorf("notes asked when blog did on the same orbit and stands at %v° against %v°: neighbours are pulled "+
			"apart toward older", got.Angles["notes"], got.Angles["blog"])
	}

	m := got.Moon
	if m.Parent != "blog" || m.Depth != 1 || m.System != "client" || m.OffBy > 0.001 || m.Orbit != int64ish(got.MoonSizes[0]) {
		t.Errorf("the thread blog opened in the directory of drafts is a moon of %q at depth %d in %q, %v off the angle "+
			"of its request, %v from blog: a child stands on the orbit of its parent at the angle of its own last request",
			m.Parent, m.Depth, m.System, m.OffBy, m.Orbit)
	}
	g := got.Grandchild
	if g.Parent != "codex-1" || g.Depth != 2 || g.OffBy > 0.001 || g.Orbit != int64ish(got.MoonSizes[1]) {
		t.Errorf("the session the thread opened is a moon of %q at depth %d, %v off its angle, %v from it: "+
			"a grandchild is a moon of the moon, on a smaller orbit", g.Parent, g.Depth, g.OffBy, g.Orbit)
	}
	if got.OrbitOf["codex-1"] != "blog" || got.OrbitOf["deep"] != "blog" {
		t.Errorf("the moons tell of the orbits %q and %q: a moon belongs where its planet does", got.OrbitOf["codex-1"], got.OrbitOf["deep"])
	}
	if got.Notes != "waiting for an answer, 1 h" {
		t.Errorf("the waiting session says %q beside its mark: the words of its row and how long it has waited", got.Notes)
	}
}

func int64ish(v float64) float64 {
	return math.Round(v)
}

// A contour is a system: an orbit for every project with a live session, in
// the order of the map rather than by what is busy, the equal sessions of a
// project as planets of one orbit, the sessions of no project on an orbit of
// their own outside the rest. An orbit names its project only where its
// planets do not.
func TestAContourIsASystemOfItsProjects(t *testing.T) {
	got := runHomeGraph(t)

	type orbit struct {
		key     string
		planets string
		label   bool
	}
	want := map[string][]orbit{
		"personal": {{"p11", "shop", false}, {"outside", "stray", true}},
		"client":   {{"p21", "draft", true}, {"p22", "blog,notes", false}},
		"lab":      {{"p31", "bench-1", false}},
	}
	names := []string{}
	for _, sys := range got.Systems {
		names = append(names, sys.Name)
		have := []orbit{}
		last := 0.0
		for _, o := range sys.Orbits {
			have = append(have, orbit{o.Key, strings.Join(o.Planets, ","), o.Label})
			if o.R <= last {
				t.Errorf("in %s the orbit %s stands at %v, inside the orbit before it", sys.Name, o.Key, o.R)
			}
			last = o.R
		}
		if !reflect.DeepEqual(have, want[sys.Name]) {
			t.Errorf("%s holds the orbits %v, want %v: an orbit a project in the order of the map, its sessions its "+
				"planets, the moons off it, outside the map last", sys.Name, have, want[sys.Name])
		}
	}
	if !reflect.DeepEqual(names, []string{"personal", "client", "lab"}) {
		t.Errorf("the systems are %v, not the contours in the order of the map", names)
	}
	w := got.Written
	if w["blog"] != 1 || w["drafts"] != 1 || w["outside"] != 1 || w["shop"] != 1 || w["client"] != 1 {
		t.Errorf("the frame writes %v: blog and shop once, as their planets, drafts and outside the map once, as their "+
			"orbits, client once, as its star", w)
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
	if !(got.Stars[0] < got.Stars[1] && got.Stars[1] < got.Stars[2]) {
		t.Errorf("a star is %v at a liveness of 0, 1 and 4: the livelier the contour, the larger its star", got.Stars)
	}
}

// The galaxy opens with the liveliest contour in front as a near plan, most
// of the height of the box, and every system in the box, none over another —
// and its places stand clear of each other for any number of contours.
func TestTheGalaxyOpensOnTheLiveliest(t *testing.T) {
	got := runHomeGraph(t)
	if !reflect.DeepEqual(got.Order, []string{"client", "personal", "lab"}) || got.Galaxy.Nearest != "client" {
		t.Errorf("the galaxy opens in the order %v with %q nearest: the liveliest in front", got.Order, got.Galaxy.Nearest)
	}
	if got.Galaxy.FrontShare < 0.55 || got.Galaxy.FrontShare > 0.72 {
		t.Errorf("the system in front takes %.2f of the height of the box: the near plan is most of it", got.Galaxy.FrontShare)
	}
	if got.Galaxy.Alone < 0.6 || got.Galaxy.Alone > 0.7 {
		t.Errorf("a lone contour takes %.2f of the height of the box: it is the same near plan, not the whole box", got.Galaxy.Alone)
	}
	sides := map[string]bool{}
	for _, b := range got.Galaxy.Behind {
		sides[b.Side] = true
		if !b.Above || b.Side == "middle" || b.Share > 0.25 {
			t.Errorf("%s behind the front stands above it %v, to its %s, at %.2f of its area: the row behind is "+
				"markedly smaller, above it and to its sides", b.Name, b.Above, b.Side, b.Share)
		}
	}
	if len(got.Galaxy.Behind) != 2 || !sides["left"] || !sides["right"] {
		t.Errorf("the two contours behind stand to one side of the front: %+v", got.Galaxy.Behind)
	}
	if len(got.Galaxy.Crowded) != 0 {
		t.Errorf("with more contours the places of the galaxy cover one another: %v", got.Galaxy.Crowded)
	}
	if len(got.Galaxy.Covered) != 0 || len(got.Galaxy.Outside) != 0 {
		t.Errorf("on the first view systems cover one another (%v) or stand outside the box (%v)",
			got.Galaxy.Covered, got.Galaxy.Outside)
	}
}

// The wheel with Shift brings the next contour forward, round and round both
// ways, and zooms nothing; a click on a star brings its contour forward and
// opens nothing. The one in front is the largest on the screen, whatever its
// size: a contour of one orbit in front of one of five is drawn up to it. Who is in front changes only by the person: a contour that
// wakes up does not jump the queue until Reset view orders them anew. Where
// less motion is asked for a system stands in its place at once; elsewhere
// it glides there over some frames.
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
	if want := []string{"personal: the largest", "client: the largest", "lab: the largest"}; !reflect.DeepEqual(c.Largest, want) {
		t.Errorf("after each turn of the wheel with Shift the one in front is the largest on the screen: %v", c.Largest)
	}
	five := c.Five
	if five.First != "big" || five.Orbits["big"] != 5 || five.Orbits["small"] != 1 || five.Front != "small" {
		t.Fatalf("the fixture of one orbit against five ran short: %+v", five)
	}
	if five.Largest != "small: the largest" || five.Areas["small"] <= five.Areas["big"] {
		t.Errorf("a contour of one orbit brought in front of one of five says %q and takes %v of the screen: the "+
			"one in front is drawn up to the largest, the one behind smaller", five.Largest, five.Areas)
	}
	if len(c.Grows) != 3 || !(c.Grows[0] < c.Grows[1] && c.Grows[1] < c.Grows[2]) ||
		len(c.Shrinks) != 3 || !(c.Shrinks[0] > c.Shrinks[1] && c.Shrinks[1] > c.Shrinks[2]) {
		t.Errorf("midway through a glide the one coming forward is to be midway in size %v, the one going back %v", c.Grows, c.Shrinks)
	}
	if s := c.Straightens; len(s) != 3 || !(s[0] > s[1] && s[1] > s[2] && s[2] == 0) {
		t.Errorf("the one coming forward is to straighten as it comes, square to the view in front: it leans %v", s)
	}
	if c.Glide < 6 || c.Midway != "" || !reflect.DeepEqual(c.Glided, all) {
		t.Errorf("outside less motion the contour came forward over %d frames, was already found in place midway (%q), "+
			"and the stars at the end were found as %v", c.Glide, c.Midway, c.Glided)
	}
}

// A planet is lit as the moon in its phase: the lit share of its disc is the
// share of its context, in its own tone, and in the tone of the cap near the
// cap.
func TestAPlanetIsLitByItsContext(t *testing.T) {
	got := runHomeGraph(t)
	read := func(key string) graphPhase {
		var p graphPhase
		if err := json.Unmarshal(got.Phase[key], &p); err != nil {
			t.Fatalf("the phase at %s did not parse: %v", key, err)
		}
		return p
	}
	var ink, capTone []int
	_ = json.Unmarshal(got.Phase["ink"], &ink)
	_ = json.Unmarshal(got.Phase["cap"], &capTone)
	for _, pct := range []string{"0", "25", "50", "75"} {
		want, _ := strconv.ParseFloat(pct, 64)
		if have := read(pct).Share; math.Abs(have-want/100) > 0.04 {
			t.Errorf("a planet at %s%% of its context is lit over %.3f of its disc", pct, have)
		}
	}
	near := func(a, b []int) bool {
		if len(a) != 3 || len(b) != 3 {
			return false
		}
		for i := range a {
			if math.Abs(float64(a[i]-b[i])) > 12 {
				return false
			}
		}
		return true
	}
	if !near(read("40").RGB, ink) || !near(read("70").RGB, capTone) {
		t.Errorf("the lit part is %v at 40%% and %v at 70%%: its own tone %v, and the tone of the cap %v near the cap",
			read("40").RGB, read("70").RGB, ink, capTone)
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

	tip := []string{"blog", "client · blog · context 52% · last request 1 h ago", "waiting for an answer",
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
	// A slow turn of the galaxy brings names onto each other now and then —
	// the row behind sweeps over the names of the system in front — and those
	// move; the rule itself is held above, on the drawing. Here the count says
	// the graph keeps the sides at all: without them the same turn makes half
	// as many jumps again.
	if len(got.Gestures.TurnJumps) > 7 {
		t.Errorf("names jumped across their marks in a slow turn of the home graph: %v", got.Gestures.TurnJumps)
	}
}

// A system behind the front one is smaller with distance while its names are
// not: there a name is written only where it stands clear, but for a session
// waiting for the person, which is written wherever it stands. The system in
// front writes every name.
func TestNamesBehindTheFrontStandClear(t *testing.T) {
	q := runHomeGraph(t).Quiet
	if q.Behind.Clashes != 0 || len(q.Behind.Hidden) == 0 {
		t.Errorf("a crowded system behind the front writes names over each other %d times, hiding %v", q.Behind.Clashes, q.Behind.Hidden)
	}
	if contains("node-f", q.Behind.Hidden) {
		t.Error("a session waiting for the person lost its name behind the front")
	}
	if len(q.Front.Hidden) != 0 || q.Front.Clashes == 0 {
		t.Errorf("the system in front hides the names %v (and writes %d over each other — the fixture is to be crowded)",
			q.Front.Hidden, q.Front.Clashes)
	}
}
