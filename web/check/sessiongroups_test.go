package check

import (
	"reflect"
	"strings"
	"testing"
)

type groupBlock struct {
	Name string   `json:"name"`
	Rows []string `json:"rows"`
}

type levelRow struct {
	Name  string
	Depth int
}

type levelTree struct {
	Rows  []levelRow
	Folds []int
}

type levelRead struct {
	Rows  []string
	Waits []string
}

type levelScreen struct {
	Open                              levelTree
	ChildClosed, RootClosed, Reopened levelRead
	RunsClosed, RunsHidden            levelRead
	RunsOpen, Stub                    levelTree
}

// A chain of sessions, each opened by the one before, stands two steps deep at
// most: D, opened by C at the third level, stays at C's level beside it. G,
// opened last with nothing under it, stands first among A's children.
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
	if levels.Root != "A" || strings.Join(levels.Order, ",") != "A,G,B,C,D,E,F" || strings.Join(levels.ChildOrder, ",") != "A,G,B,E,F" {
		t.Errorf("root %q, open order %v, child closed order %v: the root keeps its group and the arrows follow visible rows",
			levels.Root, levels.Order, levels.ChildOrder)
	}
	if strings.Join(levels.Kept, ",") != "A,B" {
		t.Errorf("folds are kept as %v, expected both parents by name", levels.Kept)
	}
	depths := func(rows []levelRow) map[string]int {
		out := make(map[string]int)
		for _, row := range rows {
			out[row.Name] = row.Depth
		}
		return out
	}
	for name, screen := range map[string]levelScreen{"phone": levels.Phone, "desk": levels.Desk} {
		t.Run(name, func(t *testing.T) {
			rows := depths(screen.Open.Rows)
			if len(rows) != 7 {
				t.Fatalf("the open tree has rows %+v", screen.Open.Rows)
			}
			if want := map[string]int{"A": 0, "B": 1, "C": 2, "D": 2, "E": 1, "F": 2, "G": 1}; !reflect.DeepEqual(rows, want) {
				t.Errorf("the levels are %v, want %v: two steps, with D and F beside C, E and G beside B", rows, want)
			}
			if len(screen.Open.Folds) != 3 {
				t.Fatalf("the first two levels have folds %+v", screen.Open.Folds)
			}
			if screen.Open.Folds[1] != screen.Open.Folds[0]+1 {
				t.Errorf("the child's fold does not take its parent's step: %v", screen.Open.Folds)
			}
			childClosed := []string{"A", "fold: 3 opened", "G", "B", "fold: 2 opened", "E", "fold: 1 opened", "F"}
			if !reflect.DeepEqual(screen.ChildClosed.Rows, childClosed) || !reflect.DeepEqual(screen.Reopened.Rows, childClosed) {
				t.Errorf("B's fold hides the wrong rows or is lost when A reopens: %+v, %+v", screen.ChildClosed, screen.Reopened)
			}
			if !reflect.DeepEqual(screen.RootClosed.Rows, []string{"A", "fold: 3 opened"}) {
				t.Errorf("A's fold leaves descendants behind: %+v", screen.RootClosed)
			}
			if strings.Join(screen.ChildClosed.Waits, ",") != "D,D" || strings.Join(screen.RootClosed.Waits, ",") != "D" {
				t.Errorf("D's question was hidden: child closed %+v, root closed %+v", screen.ChildClosed, screen.RootClosed)
			}
			runs := depths(screen.RunsOpen.Rows)
			for _, run := range []string{"RB", "RC", "RD"} {
				if depth, ok := runs[run]; !ok || depth != rows["C"] {
					t.Errorf("run %s stands at level %d (drawn: %v), expected the third-level ancestor's fold at %d", run, depth, ok, rows["C"])
				}
			}
			if depth, ok := runs["RA"]; !ok || depth != rows["B"] {
				t.Errorf("root run stands at level %d (drawn: %v), expected the second level, %d", depth, ok, rows["B"])
			}
			if !reflect.DeepEqual(screen.RunsClosed.Rows, []string{"A", "fold: 3 opened", "G", "B", "fold: 2 opened",
				"C", "D", "runs: 3 runs started by it", "E", "fold: 1 opened", "F", "runs: 1 run started by it"}) {
				t.Errorf("runs do not follow their parent's subtree: %v", screen.RunsClosed.Rows)
			}
			if !reflect.DeepEqual(screen.RunsHidden.Rows, []string{"A", "fold: 3 opened", "runs: 1 run started by it", "RA"}) {
				t.Errorf("the root's fold leaves a child's runs behind: %v", screen.RunsHidden.Rows)
			}
			stub := depths(screen.Stub.Rows)
			if len(stub) != 6 || stub["G"] != rows["B"] || stub["B"] != rows["B"] || stub["C"] != rows["C"] || stub["D"] != rows["C"] ||
				stub["F"] != rows["C"] {
				t.Errorf("a closed parent does not hold the root's place: %+v", screen.Stub)
			}
		})
	}
}

// The sessions a session had the panel open stand under it in both lists.
//
// On the phone every project stands under its heading, and a child stands in
// the block of its parent whatever its own project: person, of a project of
// its own, is under lead and nowhere else,
// and the block of lead stands first, where its waiting child puts it. The
// fold over the children is open, says how many there are and who of them
// waits; the children stand a step in, in the order they were opened. The child of a parent that closed
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
	var got struct {
		Phone      []groupBlock `json:"phone"`
		PhoneAlgo  []groupBlock `json:"phoneAlgo"`
		Plain      []groupBlock `json:"plain"`
		PlainMarks int          `json:"plainMarks"`
		Desk       []string     `json:"desk"`
		Order      []string     `json:"order"`
		StubOpens  struct {
			Phone []struct{ Name, ID string } `json:"phone"`
			Desk  []struct {
				Name, ID string
				Archived bool
			} `json:"desk"`
		} `json:"stubOpens"`
		PhoneFolded []groupBlock `json:"phoneFolded"`
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
		{"lead", []string{"lead", "fold: 2 opened · person", "› person", "› review the protocol"}},
		{"wiki", []string{"stub: docsclosed 40 min ago", "› wiki"}},
		{"shop", []string{"shop"}},
	}
	if !reflect.DeepEqual(got.Phone, phone) {
		t.Errorf("the phone lays the contour out as\n%v\nexpected\n%v", got.Phone, phone)
	}
	if algo := []groupBlock{{"rotation", []string{"rotation"}}, {"quotes", []string{"quotes (opened by lead)"}}}; !reflect.DeepEqual(got.PhoneAlgo, algo) {
		t.Errorf("the page of the other contour reads %v, expected the child of lead on its own, naming lead", got.PhoneAlgo)
	}

	desk := []string{
		"head: lead · work +", "lead", "fold: 2 opened · person", "› person", "› review the protocol",
		"head: wiki · work +", "stub: docsclosed 40 min ago", "› wiki",
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

	folded := []string{"lead", "fold: 2 opened · person"}
	if len(got.PhoneFolded) == 0 || !reflect.DeepEqual(got.PhoneFolded[0].Rows, folded) {
		t.Errorf("the closed fold on the phone leaves %v, expected the parent and the fold that still says who waits",
			got.PhoneFolded)
	}
	deskFolded := []string{"head: lead · work +", "lead", "fold: 2 opened · person",
		"head: wiki · work +", "stub: docsclosed 40 min ago", "› wiki", "head: shop · side +", "shop",
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
