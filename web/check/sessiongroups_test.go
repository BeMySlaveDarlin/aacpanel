package check

import (
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
// In the desktop column the children keep keys of their own while the fold is
// open, in the order the rows stand; closed, the fold keeps who waits and the
// dots of the states, the keys close up behind it, and the fold stays closed
// for its parent until it is opened again.
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

	desk := []string{"1 lead", "fold: opened 2 sessions· person asks you", "2 ├ person", "3 └ review the protocol",
		"stub: docsclosed 40 min ago", "4 └ wiki", "5 shop", "6 rotation", "7 quotes (opened by lead)"}
	if !reflect.DeepEqual(got.Desk, desk) {
		t.Errorf("the desktop column reads\n%v\nexpected\n%v", got.Desk, desk)
	}
	order := []string{"lead", "person", "codex-5afc361b", "wiki", "shop", "rotation", "quotes"}
	if !reflect.DeepEqual(got.Order, order) || !reflect.DeepEqual(got.OrderOpen, order) {
		t.Errorf("the keys 1–9 name %v, and %v once the fold is opened again; expected the rows in the order they stand, %v",
			got.Order, got.OrderOpen, order)
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
	deskFolded := []string{"1 lead", "fold: opened 2 sessions· person asks you", "stub: docsclosed 40 min ago",
		"2 └ wiki", "3 shop", "4 rotation", "5 quotes (opened by lead)"}
	if !reflect.DeepEqual(got.DeskFolded, deskFolded) {
		t.Errorf("the column with the fold closed reads\n%v\nexpected\n%v", got.DeskFolded, deskFolded)
	}
	if strings.Join(got.OrderFolded, ",") != "lead,wiki,shop,rotation,quotes" {
		t.Errorf("with the fold closed the keys name %v: a hidden row keeps no key", got.OrderFolded)
	}
	if strings.Join(got.Kept, ",") != "lead" {
		t.Errorf("the closed fold is kept as %v, expected by the name of its parent", got.Kept)
	}

	plain := []groupBlock{{"person", []string{"person"}}, {"lead", []string{"lead", "review the protocol"}},
		{"wiki", []string{"wiki"}}, {"shop", []string{"shop"}}}
	if !reflect.DeepEqual(got.Plain, plain) || got.PlainMarks != 0 {
		t.Errorf("a list where nobody opened anybody reads %v with %d marks of a group", got.Plain, got.PlainMarks)
	}
}
