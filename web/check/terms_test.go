package check

import (
	"strings"
	"testing"
)

type termAction struct {
	Kind   string            `json:"kind"`
	Target string            `json:"target"`
	Params map[string]string `json:"params"`
}

type termsPhoneShot struct {
	Error    string   `json:"error"`
	Nav      []string `json:"nav"`
	LogoMenu []string `json:"logoMenu"`
	Places   []string `json:"places"`
	Current  string   `json:"current"`
	Picked   string   `json:"picked"`
	HomeCard []struct {
		Name string `json:"name"`
		Runs bool   `json:"runs"`
		When string `json:"when"`
	} `json:"homeCards"`
	Picker  []string     `json:"picker"`
	Started []termAction `json:"started"`
	Opened  struct {
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
}

// The terminals of places on a phone, walked once in the whole shell; every
// part of the walk is a behaviour of its own.
func TestTerminalsOnThePhone(t *testing.T) {
	var got termsPhoneShot
	runFixture(t, "termsphone.html", &got)
	if got.Error != "" {
		t.Fatalf("the walk broke off: %s", got.Error)
	}

	// Sessions and terminals stand left of the home button, what runs and the
	// map right of it; usage leads the menu under the logo.
	t.Run("the bottom menu and the menu under the logo", func(t *testing.T) {
		if want := []string{"Sessions", "Terminals", "(home)", "Containers", "Profiles"}; !equalStrings(got.Nav, want) {
			t.Errorf("the bottom menu reads %v, expected %v", got.Nav, want)
		}
		if len(got.LogoMenu) == 0 || got.LogoMenu[0] != "Usage" {
			t.Errorf("the menu under the logo reads %v — usage leads it, it left the bottom menu for there", got.LogoMenu)
		}
	})

	// A page per place, home first even where the host lists another place
	// before it; a card says what the terminal is called, whether something
	// runs in it and when it was typed into.
	t.Run("the pager puts home first", func(t *testing.T) {
		if want := []string{"Home 2", "lab 1", "shop 3"}; !equalStrings(got.Places, want) {
			t.Errorf("the places read %v, expected %v — home first, then the places with terminals", got.Places, want)
		}
		if got.Current != "Home 2" || got.Picked != "shop 3" {
			t.Errorf("the pager opened on %q and a tap turned it to %q, expected home and then shop", got.Current, got.Picked)
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

	// New asks where: home at the top, the projects of the map under their
	// contours, a project in the home directory not offered twice.
	t.Run("the picker puts home first", func(t *testing.T) {
		want := []string{"Home ~", "[personal]", "shop /srv/proj/shop", "api /srv/proj/api", "[work]", "lab /data/lab"}
		if !equalStrings(got.Picker, want) {
			t.Errorf("the choice of a place reads %v, expected %v", got.Picker, want)
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
		if got.Back.Current != "shop 4" || !contains("Terminals", got.Back.Nav) {
			t.Errorf("back lands on %q with the menu %v, expected the page of shop under the menu", got.Back.Current, got.Back.Nav)
		}
	})
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
// conversation, and a tab kept from elsewhere goes back to the containers.
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
		t.Errorf("the bottom menu reads %v, expected Sessions, Containers, Profiles", got.Nav)
	}
	if got.Tab != "containers" {
		t.Errorf("a kept terminals tab stays %q, expected the containers", got.Tab)
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
// would be, the window and the closing at the end, the terminal filling the
// rest.
func TestTerminalsOnTheDesk(t *testing.T) {
	type column struct {
		Head  string   `json:"head"`
		Count string   `json:"count"`
		Path  string   `json:"path"`
		Cards []string `json:"cards"`
		Left  int      `json:"left"`
		Top   int      `json:"top"`
	}
	var got struct {
		Error     string   `json:"error"`
		Sections  []string `json:"sections"`
		Columns   []column `json:"columns"`
		Chooser   string   `json:"chooser"`
		NewButton string   `json:"newButton"`
		Opened    struct {
			Back    string   `json:"back"`
			Tabs    []string `json:"tabs"`
			On      string   `json:"on"`
			Buttons []string `json:"buttons"`
			Stream  string   `json:"stream"`
			Columns bool     `json:"columns"`
		} `json:"opened"`
		Fills struct {
			Height int  `json:"height"`
			Window int  `json:"window"`
			Keys   bool `json:"keys"`
		} `json:"fills"`
		Asked  []string `json:"asked"`
		Closed struct {
			Action termAction `json:"action"`
			On     string     `json:"on"`
		} `json:"closed"`
		Back []string `json:"back"`
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

	t.Run("a card opens its terminal under the tabs of its place", func(t *testing.T) {
		o := got.Opened
		if o.Back != "shop" || o.On != "make check" || o.Columns {
			t.Errorf("E2: back %q, open tab %q, columns still drawn %v — expected shop, make check, no columns", o.Back, o.On, o.Columns)
		}
		if want := []string{"make check", "zsh", "git log"}; !equalStrings(o.Tabs, want) {
			t.Errorf("the tabs read %v, expected %v", o.Tabs, want)
		}
		if want := []string{"Open in a window", "Close tab"}; !equalStrings(o.Buttons, want) {
			t.Errorf("the end of the line reads %v, expected %v", o.Buttons, want)
		}
		if o.Stream != "/api/term/stream?term=t-s1" {
			t.Errorf("the terminal attached with %q, expected t-s1", o.Stream)
		}
		if got.Fills.Keys || got.Fills.Height < got.Fills.Window*2/3 {
			t.Errorf("the terminal is %dpx of %dpx, keys row drawn %v — it has to fill the section, with no phone keys",
				got.Fills.Height, got.Fills.Window, got.Fills.Keys)
		}
		if contains("term.close", got.Asked) || !contains("term.console", got.Asked) {
			t.Errorf("before the question was answered the host was asked %v", got.Asked)
		}
		if got.Closed.Action.Kind != "term.close" || got.Closed.Action.Params["id"] != "t-s1" || got.Closed.On != "zsh" {
			t.Errorf("the closing left %+v, expected t-s1 closed and zsh open", got.Closed)
		}
		if want := []string{"Home", "lab", "shop"}; !equalStrings(got.Back, want) {
			t.Errorf("back shows %v, expected the columns again", got.Back)
		}
	})
}
