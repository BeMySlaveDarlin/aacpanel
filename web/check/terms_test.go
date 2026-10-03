package check

import (
	"fmt"
	"strings"
	"testing"
)

type termAction struct {
	Kind   string            `json:"kind"`
	Target string            `json:"target"`
	Params map[string]string `json:"params"`
}

// termCard is a card of a terminal as a fixture reads it; the setting of the
// last line is read only when there is one.
type termCard struct {
	Name     string  `json:"name"`
	Runs     bool    `json:"runs"`
	When     string  `json:"when"`
	Last     *string `json:"last"`
	Lines    int     `json:"lines"`
	Cut      bool    `json:"cut"`
	Ellipsis bool    `json:"ellipsis"`
	Mono     bool    `json:"mono"`
	Under    bool    `json:"under"`
}

// lastReads says what is wrong with the last line of a card that has one, or
// nothing: it stands on one line of the face of the name, under the name and
// across to the end of the card, and a line longer than the card ends in an
// ellipsis rather than running over.
func (c termCard) lastReads(want string, long bool) string {
	switch {
	case c.Last == nil:
		return "the card has no last line"
	case *c.Last != want:
		return fmt.Sprintf("the last line reads %q, expected %q", *c.Last, want)
	case c.Lines != 1:
		return fmt.Sprintf("the last line takes %d lines", c.Lines)
	case !c.Mono:
		return "the last line is not in the face of the name"
	case !c.Under:
		return "the last line does not stand under the name across the card"
	case long && !(c.Cut && c.Ellipsis):
		return fmt.Sprintf("a line longer than the card is cut %v, with an ellipsis %v", c.Cut, c.Ellipsis)
	}
	return ""
}

// tabCell is a tab over an open terminal and the × beside it, as a fixture
// reads them; ink is "faint" or "accent" when the mark wears one of the two.
type tabCell struct {
	Name     string `json:"name"`
	On       bool   `json:"on"`
	Label    string `json:"label"`
	Nested   bool   `json:"nested"`
	Role     string `json:"role"`
	After    bool   `json:"after"`
	SameLine bool   `json:"sameLine"`
	Tall     bool   `json:"tall"`
	Width    int    `json:"width"`
	Icon     int    `json:"icon"`
	Shown    bool   `json:"shown"`
	Ink      string `json:"ink"`
	Cut      bool   `json:"cut"`
	Ellipsis bool   `json:"ellipsis"`
}

// xReads says what is wrong with the × of a tab, or nothing: it is named
// after the tab it closes, stands beside the tab button rather than inside
// it, in a cell that takes no part in the list of tabs, and is drawn without
// a pointer over it, after the name on its line. It is pressed over the whole
// height of the tab and about a finger wide, its mark small and faint, in the
// accent on the open tab.
func (c tabCell) xReads() string {
	ink := "faint"
	if c.On {
		ink = "accent"
	}
	switch {
	case c.Label != "close "+c.Name:
		return fmt.Sprintf("the × is called %q", c.Label)
	case c.Nested:
		return "the × is inside the tab button"
	case c.Role != "presentation":
		return fmt.Sprintf("the cell of the tab and its × has the role %q — the list of tabs would hold it", c.Role)
	case !c.Shown:
		return "the × is not drawn"
	case !c.After || !c.SameLine:
		return fmt.Sprintf("the × stands after the name %v, on its line %v", c.After, c.SameLine)
	case !c.Tall:
		return "the × is lower than the tab"
	case c.Width < 28 || c.Width > 32:
		return fmt.Sprintf("the × is %dpx wide, expected 28–32", c.Width)
	case c.Icon > 16:
		return fmt.Sprintf("the mark of the × is %dpx", c.Icon)
	case c.Ink != ink:
		return fmt.Sprintf("the mark of the × is in %s, expected %s", c.Ink, ink)
	}
	return ""
}

// cardX is a card of a terminal and the × beside it, as a fixture reads them:
// right and top are how far the × stands from the right edge and the top of
// the card, under the parts of the card's text it stands over.
type cardX struct {
	Name   string   `json:"name"`
	Label  string   `json:"label"`
	Nested bool     `json:"nested"`
	Role   string   `json:"role"`
	Right  int      `json:"right"`
	Top    int      `json:"top"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Icon   int      `json:"icon"`
	Shown  bool     `json:"shown"`
	Ink    string   `json:"ink"`
	Under  []string `json:"under"`
}

// xReads says what is wrong with the × of a card, or nothing: it is named
// after the terminal it closes, stands beside the card button rather than
// inside it, in a cell that takes no part in the list, and is drawn without a
// pointer over it at the top right corner of the card. It is pressed over a
// square about a finger wide, its mark small and faint, and no text of the
// card runs under it.
func (c cardX) xReads() string {
	switch {
	case c.Label != "close "+c.Name:
		return fmt.Sprintf("the × is called %q", c.Label)
	case c.Nested:
		return "the × is inside the card button — pressing it would open the terminal"
	case c.Role != "presentation":
		return fmt.Sprintf("the cell of the card and its × has the role %q — the list would hold it", c.Role)
	case !c.Shown:
		return "the × is not drawn"
	case c.Right < 0 || c.Right > 8 || c.Top < 0 || c.Top > 8:
		return fmt.Sprintf("the × stands %dpx from the right edge of the card and %dpx from its top, expected its top right corner", c.Right, c.Top)
	case c.Width < 28 || c.Width > 36 || c.Height < 28 || c.Height > 36:
		return fmt.Sprintf("the × is %d×%dpx, expected about 32×32", c.Width, c.Height)
	case c.Icon > 16:
		return fmt.Sprintf("the mark of the × is %dpx", c.Icon)
	case c.Ink != "faint":
		return fmt.Sprintf("the mark of the × is in %s, expected the faint ink", c.Ink)
	case len(c.Under) > 0:
		return fmt.Sprintf("the text of the card runs under the ×: %v", c.Under)
	}
	return ""
}

// cardClose is the × of the cards walked by a fixture: how every card stands
// with its ×, a press on the × of an idle card and of a running one, and the
// × of every card under an executor that cannot close a terminal.
type cardClose struct {
	X    []cardX `json:"x"`
	Idle struct {
		Closes    int  `json:"closes"`
		Down      bool `json:"down"`
		SentTwice bool `json:"sentTwice"`
		Sheet     bool `json:"sheet"`
		Opened    bool `json:"opened"`
		Gone      bool `json:"gone"`
		Before    int  `json:"before"`
		After     int  `json:"after"`
	} `json:"idle"`
	Running struct {
		Title        string `json:"title"`
		Sub          string `json:"sub"`
		ClosedBefore int    `json:"closedBefore"`
		Closes       int    `json:"closes"`
		Gone         bool   `json:"gone"`
		Opened       bool   `json:"opened"`
	} `json:"running"`
	Unknown []struct {
		Name     string `json:"name"`
		Disabled bool   `json:"disabled"`
		Title    string `json:"title"`
	} `json:"unknown"`
}

// cardsClose demands of the × of the cards what the × of a tab does: a card
// where nothing runs but the shell closes at once, one where something runs
// asks first, naming the terminal and what runs, and neither opens a
// terminal; the × stays down while its close is on its way, and every × is
// down with the reason when the executor cannot close a terminal. cards is
// how many cards the walk measured, title and runs what the question says of
// the running one.
func cardsClose(t *testing.T, c cardClose, cards int, title, runs string) {
	t.Helper()
	t.Run("every card has its own × at its top right", func(t *testing.T) {
		if len(c.X) != cards {
			t.Fatalf("%d cards read, expected %d", len(c.X), cards)
		}
		for _, x := range c.X {
			if trouble := x.xReads(); trouble != "" {
				t.Errorf("%s: %s", x.Name, trouble)
			}
		}
	})

	t.Run("× on an idle card closes it at once", func(t *testing.T) {
		i := c.Idle
		if i.Closes != 1 || i.Sheet {
			t.Errorf("× on an idle card sent %d closes of it and opened a sheet %v, expected one close and no sheet", i.Closes, i.Sheet)
		}
		if !i.Down || i.SentTwice {
			t.Errorf("while the close was on its way the × was down %v, and a second press sent another %v", i.Down, i.SentTwice)
		}
		if i.Opened {
			t.Error("× on an idle card opened a terminal")
		}
		if !i.Gone || i.After != i.Before-1 {
			t.Errorf("after the close the card is gone %v, and %d cards of %d stand — expected only that card gone", i.Gone, i.After, i.Before)
		}
	})

	t.Run("× on a running card asks first", func(t *testing.T) {
		r := c.Running
		if r.ClosedBefore != 0 {
			t.Fatal("the terminal was closed before the question was answered")
		}
		if r.Title != title || !strings.Contains(r.Sub, runs) {
			t.Errorf("the question reads %q / %q — expected %q, saying %q", r.Title, r.Sub, title, runs)
		}
		if r.Closes != 1 || !r.Gone {
			t.Errorf("the answer sent %d closes and the card is gone %v, expected one close and the card gone", r.Closes, r.Gone)
		}
		if r.Opened {
			t.Error("× on a running card opened a terminal")
		}
	})

	t.Run("× on a card is down where the executor cannot close a terminal", func(t *testing.T) {
		if len(c.Unknown) == 0 {
			t.Fatal("no card read under an executor without term.close")
		}
		for _, x := range c.Unknown {
			if !x.Disabled || !strings.Contains(x.Title, "does not know the action “term.close”") {
				t.Errorf("%s: down %v, its title %q — expected down, saying the host does not know term.close", x.Name, x.Disabled, x.Title)
			}
		}
	})
}

type termsPhoneShot struct {
	Error    string   `json:"error"`
	Nav      []string `json:"nav"`
	LogoMenu []string `json:"logoMenu"`
	Tabs     []string `json:"tabs"`
	Current  string   `json:"current"`
	Pages    []struct {
		Title  string `json:"title"`
		Places []struct {
			Head  string   `json:"head"`
			Cards []string `json:"cards"`
		} `json:"places"`
		Empty     string `json:"empty"`
		NewButton bool   `json:"newButton"`
	} `json:"pages"`
	Picked   string       `json:"picked"`
	HomeCard []termCard   `json:"homeCards"`
	BareCard termCard     `json:"bareCard"`
	Picker   []string     `json:"picker"`
	Rechosen []string     `json:"rechosen"`
	Started  []termAction `json:"started"`
	Opened   struct {
		Head   string   `json:"head"`
		Tabs   []string `json:"tabs"`
		On     string   `json:"on"`
		Stream string   `json:"stream"`
		Nav    bool     `json:"nav"`
	} `json:"opened"`
	Keys     []string `json:"keys"`
	Switched struct {
		On     string `json:"on"`
		Stream string `json:"stream"`
	} `json:"switched"`
	Plus struct {
		Params map[string]string `json:"params"`
		On     string            `json:"on"`
	} `json:"plus"`
	Menu    []string   `json:"menu"`
	Window  termAction `json:"window"`
	Renamed struct {
		Action termAction `json:"action"`
		Tabs   []string   `json:"tabs"`
	} `json:"renamed"`
	Ask struct {
		Title        string `json:"title"`
		Sub          string `json:"sub"`
		Says         string `json:"says"`
		ClosedBefore int    `json:"closedBefore"`
	} `json:"ask"`
	Kept struct {
		Closes int      `json:"closes"`
		Tabs   []string `json:"tabs"`
	} `json:"kept"`
	Closed struct {
		Action termAction `json:"action"`
		Tabs   []string   `json:"tabs"`
		On     string     `json:"on"`
	} `json:"closed"`
	Back struct {
		Current string   `json:"current"`
		Nav     []string `json:"nav"`
	} `json:"back"`
	X struct {
		Before []string  `json:"before"`
		Cells  []tabCell `json:"cells"`
		Idle   struct {
			Down      bool     `json:"down"`
			SentTwice bool     `json:"sentTwice"`
			Closes    int      `json:"closes"`
			Sheet     bool     `json:"sheet"`
			Row       []string `json:"row"`
		} `json:"idle"`
		Running struct {
			Title        string   `json:"title"`
			Sub          string   `json:"sub"`
			ClosedBefore int      `json:"closedBefore"`
			Closes       int      `json:"closes"`
			Row          []string `json:"row"`
		} `json:"running"`
		Open struct {
			Closes int      `json:"closes"`
			Sheet  bool     `json:"sheet"`
			Row    []string `json:"row"`
			Stream string   `json:"stream"`
		} `json:"open"`
		Last struct {
			Closes int  `json:"closes"`
			Layer  bool `json:"layer"`
			Pager  bool `json:"pager"`
		} `json:"last"`
	} `json:"x"`
	Cards cardClose `json:"cards"`
}

// The terminals of places on a phone, walked once in the whole shell; every
// part of the walk is a behaviour of its own.
func TestTerminalsOnThePhone(t *testing.T) {
	var got termsPhoneShot
	runFixture(t, "termsphone.html", &got)
	if got.Error != "" {
		t.Fatalf("the walk broke off: %s", got.Error)
	}

	// Sessions and profiles stand left of the home button, the host and the
	// terminals right of it; usage is among the live tiles that lead the menu
	// under the logo.
	t.Run("the bottom menu and the menu under the logo", func(t *testing.T) {
		if want := []string{"Sessions", "Profiles", "(home)", "Host", "Terminals"}; !equalStrings(got.Nav, want) {
			t.Errorf("the bottom menu reads %v, expected %v", got.Nav, want)
		}
		if len(got.LogoMenu) < 3 || !equalStrings(got.LogoMenu[:3], []string{"Machine", "Usage", "Briefs"}) {
			t.Errorf("the menu under the logo reads %v — the live tiles lead it, usage among them: it left the bottom menu for there", got.LogoMenu)
		}
	})

	// A page per contour of the map, in its order, counting its terminals; a
	// contour without any keeps its page.
	t.Run("the pager pages the contours of the map", func(t *testing.T) {
		if want := []string{"personal 7", "work 1", "ops"}; !equalStrings(got.Tabs, want) {
			t.Errorf("the tabs read %v, expected %v — the contours in the order of the map", got.Tabs, want)
		}
		if got.Current != "personal 7" || got.Picked != "work 1" {
			t.Errorf("the pager opened on %q and a tap turned it to %q, expected personal and then work", got.Current, got.Picked)
		}
	})

	// A card stands under its place on the page of the contour that lists it,
	// the projects in the order of the map rather than of the host. The first
	// contour takes home ahead of its projects and a place no contour lists
	// after them, though another contour lists the home directory. A card says
	// what the terminal is called, whether something runs in it and when it was
	// typed into.
	t.Run("the cards stand under their places on the page of their contour", func(t *testing.T) {
		want := [][]string{
			{"# personal", "Home ~: zsh, htop", "shop /srv/proj/shop: make check, zsh, git log",
				"api /srv/proj/api: npm run dev", "misc /opt/misc: zsh"},
			{"# work", "lab /data/lab: zsh"},
			{"# ops", "No terminals here yet: New starts a shell in a place."},
		}
		if len(got.Pages) != len(want) {
			t.Fatalf("%d pages, expected %d: %+v", len(got.Pages), len(want), got.Pages)
		}
		for i, p := range got.Pages {
			lines := []string{"# " + p.Title}
			for _, place := range p.Places {
				lines = append(lines, place.Head+": "+strings.Join(place.Cards, ", "))
			}
			if len(p.Places) == 0 {
				lines = append(lines, p.Empty)
			}
			if !equalStrings(lines, want[i]) {
				t.Errorf("page %d reads %v, expected %v", i, lines, want[i])
			}
			if !p.NewButton {
				t.Errorf("page %q has no New", p.Title)
			}
		}
		if len(got.HomeCard) != 2 {
			t.Fatalf("home shows %d cards, expected 2: %+v", len(got.HomeCard), got.HomeCard)
		}
		if c := got.HomeCard[0]; c.Name != "zsh" || c.Runs || c.When != "typed 3m" {
			t.Errorf("an idle shell reads %+v, expected zsh, no dot, typed 3m", c)
		}
		if c := got.HomeCard[1]; c.Name != "htop" || !c.Runs || c.When != "typed 1h" {
			t.Errorf("a running command reads %+v, expected htop with its dot, typed 1h", c)
		}
	})

	// Under the name a card shows the last line on the screen of its terminal,
	// in the face of the name, on one line cut with an ellipsis; a terminal
	// whose screen the host did not read has no such line at all.
	t.Run("a card shows the last line of its screen", func(t *testing.T) {
		if len(got.HomeCard) != 2 {
			t.Fatalf("home shows %d cards, expected 2", len(got.HomeCard))
		}
		if trouble := got.HomeCard[0].lastReads("u@helios ~ $", false); trouble != "" {
			t.Errorf("the idle shell: %s", trouble)
		}
		htop := "F1Help  F2Setup  F3Search  F4Filter  F5Tree  F6SortBy  F7Nice -  F8Nice +  F9Kill  F10Quit"
		if trouble := got.HomeCard[1].lastReads(htop, true); trouble != "" {
			t.Errorf("htop: %s", trouble)
		}
		if c := got.BareCard; c.Name != "zsh" || c.Last != nil {
			t.Errorf("a terminal listed without a last line reads %+v — expected no line under its name", c)
		}
	})

	// New asks where: home at the top, then the projects of the contour whose
	// page it was pressed on, a project in the home directory not offered
	// twice; the chips of the contours over them switch to another.
	t.Run("the picker offers home and the projects of the contour", func(t *testing.T) {
		want := []string{"Home ~", "(personal)", "(work*)", "(ops)", "lab /data/lab"}
		if !equalStrings(got.Picker, want) {
			t.Errorf("New on the page of work offers %v, expected %v", got.Picker, want)
		}
		want = []string{"Home ~", "(personal*)", "(work)", "(ops)", "shop /srv/proj/shop", "api /srv/proj/api"}
		if !equalStrings(got.Rechosen, want) {
			t.Errorf("the chip of personal left %v, expected %v", got.Rechosen, want)
		}
	})

	// A pick starts a shell there and opens it: the terminal attaches by its
	// id, the tabs of the place stand over it and the menu goes away.
	t.Run("a pick starts and opens the terminal", func(t *testing.T) {
		if len(got.Started) != 1 || got.Started[0].Target != "/srv/proj/shop" || got.Started[0].Params["place"] != "/srv/proj/shop" {
			t.Fatalf("the pick asked %+v, expected term.start of /srv/proj/shop", got.Started)
		}
		o := got.Opened
		if o.Head != "shop" || o.On != "zsh" || o.Nav {
			t.Errorf("the opened terminal: head %q, open tab %q, menu drawn %v — expected shop, the new zsh, no menu", o.Head, o.On, o.Nav)
		}
		if want := []string{"make check", "zsh", "git log", "zsh"}; !equalStrings(o.Tabs, want) {
			t.Errorf("the tabs of the place read %v, expected %v", o.Tabs, want)
		}
		if o.Stream != "/api/term/stream?term=t-new1" {
			t.Errorf("the terminal attached with %q, expected the stream of the new id", o.Stream)
		}
		if want := []string{"Esc", "Ctrl", "Tab", "←", "↑", "↓", "→", "Enter"}; !equalStrings(got.Keys, want) {
			t.Errorf("the keys over the keyboard read %v, expected the row of the session terminal %v", got.Keys, want)
		}
	})

	t.Run("the tabs switch and add", func(t *testing.T) {
		if got.Switched.On != "make check" || got.Switched.Stream != "/api/term/stream?term=t-s1" {
			t.Errorf("a tap on a tab left %+v, expected make check attached", got.Switched)
		}
		if got.Plus.Params["place"] != "/srv/proj/shop" || got.Plus.On != "/api/term/stream?term=t-new2" {
			t.Errorf("the plus of the tabs gave %+v, expected a new terminal in the same place, opened", got.Plus)
		}
	})

	// ⋯ holds the window, the name, a new tab and the closing, and each
	// reaches the host with the id of the open tab.
	t.Run("the menu acts on the open tab", func(t *testing.T) {
		if want := []string{"Open in a window", "Rename tab", "New tab here", "Close tab"}; !equalStrings(got.Menu, want) {
			t.Errorf("⋯ reads %v, expected %v", got.Menu, want)
		}
		if got.Window.Kind != "term.console" || got.Window.Params["id"] != "t-s1" {
			t.Errorf("the window asked %+v, expected term.console of t-s1", got.Window)
		}
		r := got.Renamed.Action
		if r.Kind != "term.rename" || r.Params["id"] != "t-s1" || r.Params["name"] != "build" || !contains("build", got.Renamed.Tabs) {
			t.Errorf("the rename asked %+v and left %v, expected term.rename of t-s1 to build", r, got.Renamed.Tabs)
		}
	})

	// Closing asks first and says the shell and what runs in it end; Cancel
	// keeps the tab, the button closes it and the tab typed into last takes
	// its place.
	t.Run("closing a tab asks first", func(t *testing.T) {
		a := got.Ask
		if a.ClosedBefore != 0 {
			t.Fatal("the tab was closed before the question was answered")
		}
		if a.Title != "Close build?" || !strings.Contains(a.Sub, "make runs in it") ||
			!strings.Contains(a.Says, "The shell of the tab ends, and whatever runs in it ends with it") {
			t.Errorf("the question reads %q / %q / %q — it has to name the tab, what runs and that both end", a.Title, a.Sub, a.Says)
		}
		if got.Kept.Closes != 0 || !contains("build", got.Kept.Tabs) {
			t.Errorf("Cancel closed the tab: %+v", got.Kept)
		}
		c := got.Closed
		if c.Action.Kind != "term.close" || c.Action.Params["id"] != "t-s1" || contains("build", c.Tabs) || c.On != "zsh" {
			t.Errorf("the closing left %+v, expected term.close of t-s1 and the tab typed into last open", c)
		}
		if got.Back.Current != "personal 8" || !contains("Terminals", got.Back.Nav) {
			t.Errorf("back lands on %q with the menu %v, expected the page of personal, the contour of shop, under the menu",
				got.Back.Current, got.Back.Nav)
		}
	})

	// Home holds zsh, htop running, two shells started after them — the last
	// with a name too long for its tab — and zsh open again.
	long := "tail -f /var/log/aacpanel/collector.log"
	x := got.X

	t.Run("every tab has its own × after its name", func(t *testing.T) {
		if want := []string{"zsh*", "htop", "zsh", long}; !equalStrings(x.Before, want) {
			t.Fatalf("the tabs of home read %v, expected %v", x.Before, want)
		}
		if len(x.Cells) != 4 {
			t.Fatalf("%d tabs read, expected 4", len(x.Cells))
		}
		for _, c := range x.Cells {
			if trouble := c.xReads(); trouble != "" {
				t.Errorf("%s: %s", c.Name, trouble)
			}
		}
		if c := x.Cells[3]; !c.Cut || !c.Ellipsis {
			t.Errorf("a name too long for its tab is cut %v, with an ellipsis %v — it has to give way before the ×", c.Cut, c.Ellipsis)
		}
	})

	// × on a tab where only the shell runs closes it with no question, and
	// the tab whose close is on its way keeps its × down.
	t.Run("× on an idle tab closes it at once", func(t *testing.T) {
		i := x.Idle
		if i.Closes != 1 || i.Sheet {
			t.Errorf("× on an idle tab sent %d closes of it and opened a sheet %v, expected one close and no sheet", i.Closes, i.Sheet)
		}
		if !i.Down || i.SentTwice {
			t.Errorf("while the close was on its way the × was down %v, and a second press sent another %v", i.Down, i.SentTwice)
		}
	})

	t.Run("× on a running tab asks first", func(t *testing.T) {
		r := x.Running
		if r.ClosedBefore != 0 {
			t.Fatal("the tab was closed before the question was answered")
		}
		if r.Title != "Close htop?" || !strings.Contains(r.Sub, "htop runs in it") {
			t.Errorf("the question reads %q / %q — it has to name the tab whose × was pressed and what runs in it", r.Title, r.Sub)
		}
		if r.Closes != 1 {
			t.Errorf("the answer sent %d closes of htop, expected one", r.Closes)
		}
	})

	// Closing a tab that is not the open one, at once or after the question,
	// leaves the open tab open, though the tab typed into last stands by.
	t.Run("× on another tab keeps the open one", func(t *testing.T) {
		if want := []string{"zsh*", "htop", "zsh"}; !equalStrings(x.Idle.Row, want) {
			t.Errorf("the idle tab closed, the tabs read %v, expected %v", x.Idle.Row, want)
		}
		if want := []string{"zsh*", "zsh"}; !equalStrings(x.Running.Row, want) {
			t.Errorf("the running tab closed, the tabs read %v, expected %v", x.Running.Row, want)
		}
	})

	t.Run("× on the open tab gives way, and the last puts the place down", func(t *testing.T) {
		o := x.Open
		if o.Closes != 1 || o.Sheet || !equalStrings(o.Row, []string{"zsh*"}) || o.Stream != "/api/term/stream?term=t-new3" {
			t.Errorf("× on the open tab left %+v, expected one close, no sheet, and the shell started third open", o)
		}
		l := x.Last
		if l.Closes != 1 || l.Layer || !l.Pager {
			t.Errorf("× on the last tab left %+v, expected one close and the places again", l)
		}
	})

	// The page of personal holds seven cards, htop's last line longer than
	// the card; back on it after the tabs, misc holds an idle zsh and api
	// runs npm.
	cardsClose(t, got.Cards, 7, "Close npm run dev?", "npm runs in it")
}

// The terminal button of a conversation goes to the terminal of the session's
// project typed into last, and starts one at the root of the project when it
// has none — the session works in a directory inside it.
func TestTheSessionTerminalButton(t *testing.T) {
	var got struct {
		Error    string `json:"error"`
		Button   bool   `json:"button"`
		Existing struct {
			Went  map[string]string `json:"went"`
			Asked []string          `json:"asked"`
		} `json:"existing"`
		None struct {
			Went  map[string]string `json:"went"`
			Asked []struct {
				Kind   string `json:"kind"`
				Target string `json:"target"`
				Place  string `json:"place"`
			} `json:"asked"`
		} `json:"none"`
	}
	runFixture(t, "termsjump.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke off: %s", got.Error)
	}
	if !got.Button {
		t.Fatal("the row under the composer has no terminal button")
	}
	t.Run("a project with terminals", func(t *testing.T) {
		if got.Existing.Went["id"] != "t-last" || len(got.Existing.Asked) != 0 {
			t.Errorf("the button went to %v and asked %v, expected the terminal of the project typed into last, "+
				"nothing started", got.Existing.Went, got.Existing.Asked)
		}
	})
	t.Run("a project without terminals", func(t *testing.T) {
		a := got.None.Asked
		if len(a) != 1 || a[0].Kind != "term.start" || a[0].Place != "/srv/proj/shop" || a[0].Target != "/srv/proj/shop" {
			t.Fatalf("the button asked %+v, expected term.start at the root of the project", a)
		}
		if got.None.Went["id"] != "t-new" || got.None.Went["place"] != "/srv/proj/shop" {
			t.Errorf("the button went to %v, expected the new terminal", got.None.Went)
		}
	})
}

// A listener without the terminal route draws nothing of the terminals: no
// item in the bottom menu, no section on the wide screen, no button in a
// conversation, and a tab kept from elsewhere goes back to the host.
func TestNoTerminalRouteNoTerminals(t *testing.T) {
	var got struct {
		Error       string   `json:"error"`
		Nav         []string `json:"nav"`
		Tab         string   `json:"tab"`
		Sections    []string `json:"sections"`
		DeskAt      string   `json:"deskAt"`
		Deck        bool     `json:"deck"`
		Button      bool     `json:"button"`
		EmptyScreen bool     `json:"emptyScreen"`
	}
	runFixture(t, "termsabsent.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke off: %s", got.Error)
	}
	if contains("Terminals", got.Nav) || len(got.Nav) != 3 {
		t.Errorf("the bottom menu reads %v, expected Sessions, Profiles, Host", got.Nav)
	}
	if got.Tab != "host" {
		t.Errorf("a kept terminals tab stays %q, expected the host", got.Tab)
	}
	if contains("Terminals", got.Sections) || len(got.Sections) == 0 {
		t.Errorf("the sections of the wide screen read %v, expected no terminals", got.Sections)
	}
	if got.DeskAt != "home" {
		t.Errorf("a kept terminals section stays %q, expected home", got.DeskAt)
	}
	if !got.Deck || got.Button {
		t.Errorf("the row under the composer: drawn %v, terminal button %v — expected the row without the button", got.Deck, got.Button)
	}
	if got.EmptyScreen {
		t.Error("a screen of terminals is drawn where there are none")
	}
}

// The wide screen: the section after the sessions, the places side by side
// with home first, and a card opening E2 — the tabs of its place where a head
// would be, each with its ×, the window at the end, the terminal filling the
// rest.
func TestTerminalsOnTheDesk(t *testing.T) {
	type column struct {
		Head  string     `json:"head"`
		Count string     `json:"count"`
		Path  string     `json:"path"`
		Cards []string   `json:"cards"`
		Shots []termCard `json:"shots"`
		Left  int        `json:"left"`
		Top   int        `json:"top"`
	}
	var got struct {
		Error     string   `json:"error"`
		Sections  []string `json:"sections"`
		Columns   []column `json:"columns"`
		Chooser   string   `json:"chooser"`
		NewButton string   `json:"newButton"`
		Opened    struct {
			Back    string    `json:"back"`
			Tabs    []string  `json:"tabs"`
			On      string    `json:"on"`
			Buttons []string  `json:"buttons"`
			Stream  string    `json:"stream"`
			Columns bool      `json:"columns"`
			Cells   []tabCell `json:"cells"`
		} `json:"opened"`
		Fills struct {
			Height int  `json:"height"`
			Window int  `json:"window"`
			Keys   bool `json:"keys"`
		} `json:"fills"`
		Asked    []string `json:"asked"`
		Question string   `json:"question"`
		Closed   struct {
			Action termAction `json:"action"`
			On     string     `json:"on"`
		} `json:"closed"`
		Other struct {
			Action termAction `json:"action"`
			Sheet  bool       `json:"sheet"`
			Row    []string   `json:"row"`
			Stream string     `json:"stream"`
		} `json:"other"`
		Unknown []struct {
			Disabled bool   `json:"disabled"`
			Title    string `json:"title"`
		} `json:"unknown"`
		Back  []string  `json:"back"`
		Cards cardClose `json:"cards"`
	}
	runWideFixture(t, "termsdesk.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke off: %s", got.Error)
	}

	t.Run("the places stand side by side", func(t *testing.T) {
		if len(got.Sections) < 2 || got.Sections[0] != "Sessions" || got.Sections[1] != "Terminals" {
			t.Errorf("the sections read %v, expected terminals right after the sessions", got.Sections)
		}
		heads := []string{}
		for _, c := range got.Columns {
			heads = append(heads, c.Head+" "+c.Count+" "+c.Path)
		}
		if want := []string{"Home 2 ~", "lab 1 /data/lab", "shop 3 /srv/proj/shop"}; !equalStrings(heads, want) {
			t.Errorf("the columns read %v, expected %v", heads, want)
		}
		for i := 1; i < len(got.Columns); i++ {
			if got.Columns[i].Top != got.Columns[0].Top || got.Columns[i].Left <= got.Columns[i-1].Left {
				t.Errorf("the places stand one under another rather than side by side: %+v", got.Columns)
				break
			}
		}
		if got.Chooser != "places" || got.NewButton != "New" {
			t.Errorf("above the columns: chooser %q, button %q — expected the choice of places and New", got.Chooser, got.NewButton)
		}
	})

	// A card in a column wears the dot the host gives it, whatever the
	// command, and the last line of its screen under its name.
	t.Run("a card in a column tells what runs and its last line", func(t *testing.T) {
		if len(got.Columns) != 3 || len(got.Columns[2].Shots) != 3 {
			t.Fatalf("the columns are %+v, expected shop third with three cards", got.Columns)
		}
		shop := got.Columns[2].Shots
		for i, want := range []struct {
			name string
			runs bool
		}{{"make check", true}, {"zsh", false}, {"git log", false}} {
			if shop[i].Name != want.name || shop[i].Runs != want.runs {
				t.Errorf("card %d reads %q with a dot %v, expected %q with %v — the dot is the host's word",
					i, shop[i].Name, shop[i].Runs, want.name, want.runs)
			}
		}
		long := "ok   shop/internal/store   15.90s   coverage: 81.4% of statements in shop/internal/store/..."
		if trouble := shop[0].lastReads(long, true); trouble != "" {
			t.Errorf("make check: %s", trouble)
		}
		if trouble := shop[1].lastReads("u@helios shop main $", false); trouble != "" {
			t.Errorf("zsh: %s", trouble)
		}
		if shop[2].Last != nil {
			t.Errorf("git log shows the last line %q — the host gave it none", *shop[2].Last)
		}
	})

	t.Run("a card opens its terminal under the tabs of its place", func(t *testing.T) {
		o := got.Opened
		if o.Back != "shop" || o.On != "make check" || o.Columns {
			t.Errorf("E2: back %q, open tab %q, columns still drawn %v — expected shop, make check, no columns", o.Back, o.On, o.Columns)
		}
		if want := []string{"make check", "zsh", "git log"}; !equalStrings(o.Tabs, want) {
			t.Errorf("the tabs read %v, expected %v", o.Tabs, want)
		}
		if want := []string{"Open in a window"}; !equalStrings(o.Buttons, want) {
			t.Errorf("the end of the line reads %v, expected %v — a tab is closed by its ×", o.Buttons, want)
		}
		if o.Stream != "/api/term/stream?term=t-s1" {
			t.Errorf("the terminal attached with %q, expected t-s1", o.Stream)
		}
		if got.Fills.Keys || got.Fills.Height < got.Fills.Window*2/3 {
			t.Errorf("the terminal is %dpx of %dpx, keys row drawn %v — it has to fill the section, with no phone keys",
				got.Fills.Height, got.Fills.Window, got.Fills.Keys)
		}
		if want := []string{"Home", "lab", "shop"}; !equalStrings(got.Back, want) {
			t.Errorf("back shows %v, expected the columns again", got.Back)
		}
	})

	t.Run("every tab has its own × after its name", func(t *testing.T) {
		if len(got.Opened.Cells) != 3 {
			t.Fatalf("%d tabs read, expected 3", len(got.Opened.Cells))
		}
		for _, c := range got.Opened.Cells {
			if trouble := c.xReads(); trouble != "" {
				t.Errorf("%s: %s", c.Name, trouble)
			}
		}
	})

	// × on the open tab, where make runs, asks first; the answer closes it
	// and the tab typed into last takes its place.
	t.Run("× on a running tab asks first", func(t *testing.T) {
		if contains("term.close", got.Asked) || !contains("term.console", got.Asked) {
			t.Errorf("before the question was answered the host was asked %v", got.Asked)
		}
		if got.Question != "Close make check?" {
			t.Errorf("the question reads %q, expected it to name make check", got.Question)
		}
		if got.Closed.Action.Kind != "term.close" || got.Closed.Action.Params["id"] != "t-s1" || got.Closed.On != "zsh" {
			t.Errorf("the closing left %+v, expected t-s1 closed and zsh open", got.Closed)
		}
	})

	t.Run("× on another idle tab closes it at once and keeps the open one", func(t *testing.T) {
		o := got.Other
		if o.Action.Kind != "term.close" || o.Action.Params["id"] != "t-s3" || o.Sheet {
			t.Errorf("× on the third tab sent %+v and opened a sheet %v, expected term.close of t-s3 and no sheet", o.Action, o.Sheet)
		}
		if !equalStrings(o.Row, []string{"zsh*"}) || o.Stream != "/api/term/stream?term=t-s2" {
			t.Errorf("the tabs read %v and the terminal shows %q, expected zsh still open", o.Row, o.Stream)
		}
	})

	t.Run("× is down where the executor cannot close a tab", func(t *testing.T) {
		if len(got.Unknown) == 0 {
			t.Fatal("no tab read under an executor without term.close")
		}
		for i, x := range got.Unknown {
			if !x.Disabled || !strings.Contains(x.Title, "does not know the action “term.close”") {
				t.Errorf("tab %d: the × is down %v, its title %q — expected down, saying the host does not know term.close", i, x.Disabled, x.Title)
			}
		}
	})

	// The columns hold six cards, make check's last line longer than the
	// card; back on them after the tabs, home holds an idle zsh and htop
	// running.
	cardsClose(t, got.Cards, 6, "Close htop?", "htop runs in it")
}

type personalLayout struct {
	Pages []string `json:"pages"`
	Home  string   `json:"home"`
	Loose string   `json:"loose"`
}

// The places no contour of the map lists — home and a directory of no project
// — stand on the page of the personal contour, the one the map marks default
// by its config directory, and a terminal started in one opens that page, even
// where the map puts the personal contour second. A map that marks none gives
// them to its first contour.
func TestTerminalsOutsideTheMapStandOnThePersonalContour(t *testing.T) {
	var got struct {
		Marked   personalLayout `json:"marked"`
		Unmarked personalLayout `json:"unmarked"`
	}
	runFixture(t, "termspersonal.html", &got)

	if want := []string{"work: lab", "personal: Home, tmp"}; !equalStrings(got.Marked.Pages, want) {
		t.Errorf("with personal marked default the pages read %v, expected %v", got.Marked.Pages, want)
	}
	if got.Marked.Home != "personal" || got.Marked.Loose != "personal" {
		t.Errorf("a terminal at home opens the page %q, one elsewhere %q — expected personal for both",
			got.Marked.Home, got.Marked.Loose)
	}
	if want := []string{"work: Home, lab, tmp", "personal: "}; !equalStrings(got.Unmarked.Pages, want) {
		t.Errorf("with no contour marked the pages read %v, expected %v", got.Unmarked.Pages, want)
	}
	if got.Unmarked.Home != "work" || got.Unmarked.Loose != "work" {
		t.Errorf("with no contour marked a terminal at home opens %q, one elsewhere %q — expected the first, work",
			got.Unmarked.Home, got.Unmarked.Loose)
	}
}
