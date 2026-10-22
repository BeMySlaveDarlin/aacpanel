package check

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

// waitHue is the colour of waiting, #e3c97a, as the engine computes it.
const waitHue = "rgb(227, 201, 122)"

type groupBlock struct {
	Name string   `json:"name"`
	Rows []string `json:"rows"`
}

type levelRow struct {
	Name                                               string
	X, Width, Top, Bottom, Height, GuideX, GuideHeight float64
}

type levelTrail struct {
	X, Top, Bottom, Height, Line float64
}

type levelGeometry struct {
	Rows   []levelRow
	Trails []levelTrail
	Folds  []struct {
		Height, X float64
		Centered  bool
	}
}

type levelRead struct {
	Rows  []string
	Waits []string
	Dots  [][]string
}

type levelScreen struct {
	Open                              levelGeometry
	ChildClosed, RootClosed, Reopened levelRead
	RunsClosed, RunsHidden            levelRead
	RunsOpen, Stub, Last              levelGeometry
}

// Geometry guards the extra step as well as the cap: a class on a row says
// nothing about where its name or the branch beside it actually stands.
func TestSessionsStopAtTheThirdLevel(t *testing.T) {
	var got struct {
		Levels struct {
			Root                    string
			Order, ChildOrder, Kept []string
			Phone, Desk             levelScreen
		}
	}
	runWideFixture(t, "sessiongroups.html", &got)
	levels := got.Levels
	if levels.Root != "A" || strings.Join(levels.Order, ",") != "A,B,C,D,E" || strings.Join(levels.ChildOrder, ",") != "A,B,E" {
		t.Errorf("root %q, open order %v, child closed order %v: the root keeps its group and the arrows follow visible rows",
			levels.Root, levels.Order, levels.ChildOrder)
	}
	if strings.Join(levels.Kept, ",") != "A,B" {
		t.Errorf("folds are kept as %v, expected both parents by name", levels.Kept)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1.5 }
	byName := func(rows []levelRow) map[string]levelRow {
		out := make(map[string]levelRow)
		for _, row := range rows {
			out[row.Name] = row
		}
		return out
	}
	for name, screen := range map[string]levelScreen{"phone": levels.Phone, "desk": levels.Desk} {
		t.Run(name, func(t *testing.T) {
			rows := byName(screen.Open.Rows)
			if len(rows) != 5 {
				t.Fatalf("the open tree has rows %+v", screen.Open.Rows)
			}
			a, b, c, d, e := rows["A"], rows["B"], rows["C"], rows["D"], rows["E"]
			step := b.X - a.X
			if step < 10 || !near(c.X-b.X, step) || !near(c.X, d.X) || !near(b.X, e.X) {
				t.Errorf("names stand at %v, %v, %v, %v, %v: two equal steps, with D beside C and E beside B",
					a.X, b.X, c.X, d.X, e.X)
			}
			if !near(c.GuideX-b.GuideX, step) || !near(c.GuideX, d.GuideX) || !near(b.GuideHeight, b.Height) ||
				!near(c.GuideHeight, c.Height) || d.GuideHeight <= 0 || d.GuideHeight >= d.Height/2 {
				t.Errorf("branches do not follow the levels or turn at the last child: %+v", screen.Open.Rows)
			}
			if name == "phone" && c.Width < 120 {
				t.Errorf("the third-level name has only %.1f pixels on a 390px phone", c.Width)
			}
			if len(screen.Open.Folds) != 2 {
				t.Fatalf("the first two levels have folds %+v", screen.Open.Folds)
			}
			if !near(screen.Open.Folds[1].X-screen.Open.Folds[0].X, step) {
				t.Errorf("the child's fold does not take its parent's step: %+v", screen.Open.Folds)
			}
			for _, fold := range screen.Open.Folds {
				if !fold.Centered || name == "phone" && (fold.Height < 1.75 || fold.Height >= 2.25) || name == "desk" && fold.Height >= 1.6 {
					t.Errorf("the fold is not a short, centered strip: %+v", fold)
				}
			}
			if len(screen.Open.Trails) != 3 {
				t.Fatalf("the branch to E passes %d parts of B's subtree, expected its fold, C and D", len(screen.Open.Trails))
			}
			bottom := b.Bottom
			for _, trail := range screen.Open.Trails {
				if !near(trail.X, b.GuideX) || !near(trail.Top, bottom) || !near(trail.Height, trail.Bottom-trail.Top) || trail.Line <= 0 {
					t.Errorf("the branch from B to E breaks after %.1f at %+v", bottom, trail)
				}
				bottom = trail.Bottom
			}
			if !near(bottom, e.Top) {
				t.Errorf("B's branch ends at %.1f before E at %.1f", bottom, e.Top)
			}
			childClosed := []string{"A", "fold: opened 2 sessions", "B", "fold: opened 2 sessions", "E"}
			if !reflect.DeepEqual(screen.ChildClosed.Rows, childClosed) || !reflect.DeepEqual(screen.Reopened.Rows, childClosed) {
				t.Errorf("B's fold hides the wrong rows or is lost when A reopens: %+v, %+v", screen.ChildClosed, screen.Reopened)
			}
			if !reflect.DeepEqual(screen.RootClosed.Rows, []string{"A", "fold: opened 2 sessions"}) {
				t.Errorf("A's fold leaves descendants behind: %+v", screen.RootClosed)
			}
			if strings.Join(screen.ChildClosed.Waits, ",") != "· D waits,· D waits" || strings.Join(screen.RootClosed.Waits, ",") != "· D waits" {
				t.Errorf("D's question was hidden: child closed %+v, root closed %+v", screen.ChildClosed, screen.RootClosed)
			}
			if !reflect.DeepEqual(screen.ChildClosed.Dots, [][]string{{"idle", "wait"}}) ||
				!reflect.DeepEqual(screen.RootClosed.Dots, [][]string{{"idle", "idle"}}) {
				t.Errorf("fold dots must describe their direct children: child %+v, root %+v", screen.ChildClosed.Dots, screen.RootClosed.Dots)
			}
			runs := byName(screen.RunsOpen.Rows)
			for _, run := range []string{"RB", "RC", "RD"} {
				if row, ok := runs[run]; !ok || !near(row.X, c.X) {
					t.Errorf("run %s stands at %+v, expected the third-level ancestor's fold at %.1f", run, row, c.X)
				}
			}
			if row, ok := runs["RA"]; !ok || !near(row.X, b.X) {
				t.Errorf("root run stands at %+v, expected the second level at %.1f", row, b.X)
			}
			if !reflect.DeepEqual(screen.RunsClosed.Rows, []string{"A", "fold: opened 2 sessions", "B", "fold: opened 2 sessions",
				"C", "D", "runs: 3 runs started by it", "E", "runs: 1 run started by it"}) {
				t.Errorf("runs do not follow their parent's subtree: %v", screen.RunsClosed.Rows)
			}
			if !reflect.DeepEqual(screen.RunsHidden.Rows, []string{"A", "fold: opened 2 sessions", "runs: 1 run started by it", "RA"}) {
				t.Errorf("the root's fold leaves a child's runs behind: %v", screen.RunsHidden.Rows)
			}
			stub := byName(screen.Stub.Rows)
			if len(stub) != 4 || !near(stub["B"].X, b.X) || !near(stub["C"].X, c.X) || !near(stub["D"].X, c.X) || len(screen.Stub.Trails) != 3 {
				t.Errorf("a closed parent does not hold the root's place: %+v", screen.Stub)
			}
			if len(screen.Last.Trails) != 0 {
				t.Errorf("the last second-level child carries a branch to no sister: %+v", screen.Last.Trails)
			}
		})
	}
}

// The sessions a session had the panel open stand under it in both lists.
//
// On the phone a child stands in the block of its parent whatever its own
// project: person, of a project of its own, is under lead and nowhere else,
// and the block of lead stands first, where its waiting child puts it. The
// fold over the children is open, says how many there are and who of them
// waits, in the colour of waiting; the children stand a step in, in the order
// they were opened, on the line of a branch. The child of a parent that closed
// stands under a stub of it that says when it closed and opens its
// conversation; a child whose parent lives in another contour stands on its
// own page and says which session opened it.
//
// The desktop column lays a contour out in blocks of its projects as the
// phone does, in the order of the map: a heading over every project, the
// only session of one included, with its name, the group of the map and the
// way to a new session in it; under it the rows, the children of a parent in
// its block. The rows keep their places whatever the sessions do, so the
// arrow keys of the shell go down them as they stand, block after block, the
// children among them while the fold is open; closed, the fold keeps who
// waits and the dots of the states, the arrow keys pass its children by, and
// the fold stays closed for its parent until it is opened again. A digit
// picks nothing.
//
// A list where nobody opened anybody shows none of it.
func TestSessionsAnotherOpenedStandUnderIt(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		Phone      []groupBlock `json:"phone"`
		PhoneAlgo  []groupBlock `json:"phoneAlgo"`
		Plain      []groupBlock `json:"plain"`
		PlainMarks int          `json:"plainMarks"`
		Desk       []string     `json:"desk"`
		Order      []string     `json:"order"`
		WaitHue    string       `json:"waitHue"`
		Guide      string       `json:"guide"`
		StubOpens  struct {
			Phone []struct{ Name, ID string } `json:"phone"`
			Desk  []struct {
				Name, ID string
				Archived bool
			} `json:"desk"`
		} `json:"stubOpens"`
		PhoneFolded []groupBlock `json:"phoneFolded"`
		PhoneDots   []string     `json:"phoneDots"`
		DeskFolded  []string     `json:"deskFolded"`
		OrderFolded []string     `json:"orderFolded"`
		Kept        []string     `json:"kept"`
		OrderOpen   []string     `json:"orderOpen"`
		Walk        []string     `json:"walk"`
		Digit       string       `json:"digit"`
		Back        string       `json:"back"`
	}
	runWideFixture(t, "sessiongroups.html", &got)

	phone := []groupBlock{
		{"lead", []string{"lead", "fold: opened 2 sessions· person asks you", "├ person", "└ review the protocol"}},
		{"wiki", []string{"stub: docsclosed 40 min ago", "└ wiki"}},
		{"shop", []string{"shop"}},
	}
	if !reflect.DeepEqual(got.Phone, phone) {
		t.Errorf("the phone lays the contour out as\n%v\nexpected\n%v", got.Phone, phone)
	}
	if algo := []groupBlock{{"rotation", []string{"rotation"}}, {"quotes", []string{"quotes (opened by lead)"}}}; !reflect.DeepEqual(got.PhoneAlgo, algo) {
		t.Errorf("the page of the other contour reads %v, expected the child of lead on its own, naming lead", got.PhoneAlgo)
	}
	if got.WaitHue != waitHue {
		t.Errorf("who of the children waits is said in %s, not in the colour of waiting %s", got.WaitHue, waitHue)
	}
	if got.Guide == "" || got.Guide == "0px" {
		t.Errorf("a child stands with no line of the branch beside it (%q)", got.Guide)
	}

	desk := []string{
		"head: lead · work +", "lead", "fold: opened 2 sessions· person asks you", "├ person", "└ review the protocol",
		"head: wiki · work +", "stub: docsclosed 40 min ago", "└ wiki",
		"head: shop · side +", "shop",
		"head: rotation · trading +", "rotation",
		"head: quotes · trading +", "quotes (opened by lead)",
	}
	if !reflect.DeepEqual(got.Desk, desk) {
		t.Errorf("the desktop column reads\n%v\nexpected\n%v", got.Desk, desk)
	}
	order := []string{"lead", "person", "codex-5afc361b", "wiki", "shop", "rotation", "quotes"}
	if !reflect.DeepEqual(got.Order, order) || !reflect.DeepEqual(got.OrderOpen, order) {
		t.Errorf("the arrow keys go down %v, and %v once the fold is opened again; expected the rows in the order they stand, %v",
			got.Order, got.OrderOpen, order)
	}
	if walk := []string{"shop", "rotation"}; !reflect.DeepEqual(got.Walk, walk) {
		t.Errorf("ArrowDown from shop lit %v, expected the row under it as the column stands, in the next contour: %v",
			got.Walk, walk)
	}
	if got.Digit != "rotation" {
		t.Errorf("a press of 1 on rotation left %q open — a digit picks no session", got.Digit)
	}
	if got.Back != "shop" {
		t.Errorf("ArrowUp from rotation opened %q, expected the row above it, shop", got.Back)
	}

	if len(got.StubOpens.Phone) != 1 || got.StubOpens.Phone[0].Name != "docs" || got.StubOpens.Phone[0].ID != "d0" {
		t.Errorf("the stub on the phone opens %+v, expected the closed conversation of docs", got.StubOpens.Phone)
	}
	if len(got.StubOpens.Desk) != 1 || got.StubOpens.Desk[0].Name != "docs" || got.StubOpens.Desk[0].ID != "d0" ||
		!got.StubOpens.Desk[0].Archived {
		t.Errorf("the stub in the column opens %+v, expected the closed conversation of docs", got.StubOpens.Desk)
	}

	folded := []string{"lead", "fold: opened 2 sessions· person asks you"}
	if len(got.PhoneFolded) == 0 || !reflect.DeepEqual(got.PhoneFolded[0].Rows, folded) {
		t.Errorf("the closed fold on the phone leaves %v, expected the parent and the fold that still says who waits",
			got.PhoneFolded)
	}
	if strings.Join(got.PhoneDots, ",") != "wait,busy" {
		t.Errorf("the closed fold shows the states %v, expected a dot for each child, in the order they were opened", got.PhoneDots)
	}
	deskFolded := []string{"head: lead · work +", "lead", "fold: opened 2 sessions· person asks you",
		"head: wiki · work +", "stub: docsclosed 40 min ago", "└ wiki", "head: shop · side +", "shop",
		"head: rotation · trading +", "rotation", "head: quotes · trading +", "quotes (opened by lead)"}
	if !reflect.DeepEqual(got.DeskFolded, deskFolded) {
		t.Errorf("the column with the fold closed reads\n%v\nexpected\n%v", got.DeskFolded, deskFolded)
	}
	if strings.Join(got.OrderFolded, ",") != "lead,wiki,shop,rotation,quotes" {
		t.Errorf("with the fold closed the arrow keys go down %v: a hidden row is passed by", got.OrderFolded)
	}
	if strings.Join(got.Kept, ",") != "lead" {
		t.Errorf("the closed fold is kept as %v, expected by the name of its parent", got.Kept)
	}

	plain := []groupBlock{{"person", []string{"person"}}, {"lead", []string{"review the protocol", "lead"}},
		{"wiki", []string{"wiki"}}, {"shop", []string{"shop"}}}
	if !reflect.DeepEqual(got.Plain, plain) || got.PlainMarks != 0 {
		t.Errorf("a list where nobody opened anybody reads %v with %d marks of a group", got.Plain, got.PlainMarks)
	}
}
