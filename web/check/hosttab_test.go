package check

import (
	"strings"
	"testing"
)

// hostPage is what stands on the screen with the host tab on one of its pages.
type hostPage struct {
	Page       string   `json:"page"`
	Kept       string   `json:"kept"`
	Back       bool     `json:"back"`
	Head       []string `json:"head"`
	RowTop     int      `json:"rowTop"`
	RowBottom  int      `json:"rowBottom"`
	Strip      bool     `json:"strip"`
	StripTop   int      `json:"stripTop"`
	Filters    []string `json:"filters"`
	FiltersTop int      `json:"filtersTop"`
	Outside    []string `json:"outside"`
	Stacks     int      `json:"stacks"`
	Grid       bool     `json:"grid"`
	Entries    int      `json:"entries"`
}

// hostLayer is a page the host menu opened over the screen.
type hostLayer struct {
	Title string `json:"title"`
	Back  bool   `json:"back"`
	Pager bool   `json:"pager"`
	Nav   bool   `json:"nav"`
}

type hostTurn struct {
	Box  string `json:"box"`
	Page string `json:"page"`
}

type hostReturn struct {
	Pager bool   `json:"pager"`
	Page  string `json:"page"`
	Title string `json:"title"`
}

// The host tab of the phone: the containers, the machine and the journal as
// pages of one tab, turned by a swipe the way the sessions turn their
// contours, while the host menu keeps opening the machine and the journal
// over the screen.
func TestTheHostTabPagesTheContainersTheMachineAndTheJournal(t *testing.T) {
	var got struct {
		Error    string   `json:"error"`
		Nav      []string `json:"nav"`
		NavOn    string   `json:"navOn"`
		Pages    []string `json:"pages"`
		First    hostPage `json:"first"`
		Forward  hostTurn `json:"forward"`
		Machine  hostPage `json:"machine"`
		Further  hostTurn `json:"further"`
		Journal  hostPage `json:"journal"`
		Backward hostTurn `json:"backward"`
		Again    string   `json:"again"`
		Reopened struct {
			Page  string `json:"page"`
			NavOn string `json:"navOn"`
			Tab   string `json:"tab"`
		} `json:"reopened"`
		Tapped  hostPage `json:"tapped"`
		Details struct {
			hostLayer
			Page string `json:"page"`
			Kept string `json:"kept"`
		} `json:"details"`
		MenuMachine     hostLayer  `json:"menuMachine"`
		MenuMachineBack hostReturn `json:"menuMachineBack"`
		MenuJournal     hostLayer  `json:"menuJournal"`
		MenuJournalBack hostReturn `json:"menuJournalBack"`
	}
	runFixture(t, "hosttab.html", &got)

	// The menu is read before anything is pressed: a walk that lost its way
	// for want of the Host button still says what the menu read.
	t.Run("the tab is called Host and a kept containers tab opens it", func(t *testing.T) {
		if want := []string{"Sessions", "Profiles", "Host"}; !equalStrings(got.Nav, want) {
			t.Errorf("the bottom menu reads %v, expected %v", got.Nav, want)
		}
		if got.NavOn != "Host" {
			t.Errorf("a phone that kept the containers tab opened on %q, expected the host tab", got.NavOn)
		}
	})
	if got.Error != "" {
		t.Fatalf("the walk broke off: %s", got.Error)
	}

	t.Run("three pages in order, the containers first", func(t *testing.T) {
		if want := []string{"Containers", "Machine", "Journal"}; !equalStrings(got.Pages, want) {
			t.Fatalf("the pager reads %v, expected %v", got.Pages, want)
		}
		f := got.First
		if f.Page != "Containers" || f.Stacks != 3 {
			t.Errorf("the tab opened on %q with %d stacks, expected the containers with all three", f.Page, f.Stacks)
		}
		if !f.Strip || strings.Join(f.Filters, "|") != "All|Running|Down|Unhealthy" {
			t.Errorf("the containers page has the strip of the machine %v and the filters %v, expected both as before",
				f.Strip, f.Filters)
		}
	})

	// The row of pages is the first thing under the header, as on the
	// sessions: what belongs to the containers turns with their page and
	// never pushes the row about.
	t.Run("the row of pages stands still and the strip turns with the containers", func(t *testing.T) {
		f, m, j := got.First, got.Machine, got.Journal
		if f.RowTop <= 0 || f.RowTop != m.RowTop || f.RowTop != j.RowTop {
			t.Errorf("the row of pages stands at %d on the containers, %d on the machine, %d on the journal — "+
				"expected one place on all three", f.RowTop, m.RowTop, j.RowTop)
		}
		for _, p := range []hostPage{f, m, j} {
			if len(p.Outside) != 0 {
				t.Errorf("on the %s page %v stand outside the pages — the strip and the filters belong to the "+
					"containers page", p.Page, p.Outside)
			}
		}
		if f.StripTop < f.RowBottom || f.FiltersTop < f.StripTop {
			t.Errorf("on the containers page the row ends at %d, the strip starts at %d and the filters at %d — "+
				"expected the strip under the row and the filters under the strip", f.RowBottom, f.StripTop, f.FiltersTop)
		}
	})

	t.Run("a swipe turns the pages both ways", func(t *testing.T) {
		if got.Forward.Box != "pfpages" || got.Forward.Page != "Machine" {
			t.Errorf("a swipe on the containers moved %q and left the tab on %q, expected the pages turned to the machine",
				got.Forward.Box, got.Forward.Page)
		}
		if got.Further.Page != "Journal" {
			t.Errorf("a second swipe left the tab on %q, expected the journal", got.Further.Page)
		}
		if got.Backward.Page != "Machine" {
			t.Errorf("a swipe back left the tab on %q, expected the machine", got.Backward.Page)
		}
	})

	t.Run("the machine and the journal are pages of the tab, not layers", func(t *testing.T) {
		m := got.Machine
		if m.Back || len(m.Head) != 0 {
			t.Errorf("the machine page of the tab has a way back %v and a head %v — it is a page, its pager names it",
				m.Back, m.Head)
		}
		if !m.Grid {
			t.Error("the machine page of the tab draws no tiles of the machine")
		}
		if m.Strip {
			t.Error("the machine page carries the strip of the machine meant for the containers")
		}
		j := got.Journal
		if j.Back || len(j.Head) != 0 {
			t.Errorf("the journal page of the tab has a way back %v and a head %v", j.Back, j.Head)
		}
		if j.Entries != 3 {
			t.Errorf("the journal page shows %d entries, expected 3", j.Entries)
		}
	})

	t.Run("the page chosen is kept", func(t *testing.T) {
		if got.Machine.Kept != "machine" {
			t.Errorf("the page chosen is kept as %q, expected machine", got.Machine.Kept)
		}
		if got.Again != "Machine" {
			t.Errorf("back from the sessions the tab stands on %q, expected the machine it was left on", got.Again)
		}
		r := got.Reopened
		if r.Page != "Machine" || r.NavOn != "Host" {
			t.Errorf("a new start opened tab %q on page %q, expected the machine of the host", r.NavOn, r.Page)
		}
	})

	t.Run("details on the strip turns the pager to the machine", func(t *testing.T) {
		if got.Tapped.Page != "Containers" || !got.Tapped.Strip {
			t.Fatalf("a tap on the name left the tab on %q with the strip %v, expected the containers with it",
				got.Tapped.Page, got.Tapped.Strip)
		}
		d := got.Details
		if d.Title != "" || !d.Pager || !d.Nav {
			t.Errorf("details opened a layer %q (pager %v, menu %v), expected the pages of the tab to stay",
				d.Title, d.Pager, d.Nav)
		}
		if d.Page != "Machine" || d.Kept != "machine" {
			t.Errorf("details left the tab on %q, kept %q, expected the machine page", d.Page, d.Kept)
		}
	})

	t.Run("the host menu opens the machine and the journal as layers", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			layer hostLayer
			back  hostReturn
		}{
			{"Machine", got.MenuMachine, got.MenuMachineBack},
			{"Journal", got.MenuJournal, got.MenuJournalBack},
		} {
			if c.layer.Title != c.name || !c.layer.Back || c.layer.Pager || c.layer.Nav {
				t.Errorf("the menu opened %s as %+v, expected a layer with its head and the way back, "+
					"over the pages and the bottom menu", c.name, c.layer)
			}
			if !c.back.Pager || c.back.Title != "" || c.back.Page != "Machine" {
				t.Errorf("back from %s the screen reads %+v, expected the host tab on the page it stood on", c.name, c.back)
			}
		}
	})
}
