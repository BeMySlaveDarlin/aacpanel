package check

import (
	"strconv"
	"strings"
	"testing"
)

type menuTile struct {
	Title string  `json:"title"`
	N     *string `json:"n"`
	S     *string `json:"s"`
	Fill  *string `json:"fill"`
	Lit   bool    `json:"lit"`
	Stale bool    `json:"stale"`
	Label string  `json:"label"`
	Box   *barBox `json:"box"`
}

type menuStrip struct {
	Text     string  `json:"text"`
	Disabled bool    `json:"disabled"`
	Box      *barBox `json:"box"`
}

type menuQuiet struct {
	Title   string  `json:"title"`
	Sub     *string `json:"sub"`
	Crit    bool    `json:"crit"`
	Opacity string  `json:"opacity"`
	Box     *barBox `json:"box"`
}

// menuSoon is a door of the row that does not open yet.
type menuSoon struct {
	Title    string  `json:"title"`
	Sub      *string `json:"sub"`
	Disabled bool    `json:"disabled"`
	Opacity  string  `json:"opacity"`
	Box      *barBox `json:"box"`
}

type menuRead struct {
	Name     string      `json:"name"`
	Sub      string      `json:"sub"`
	Dot      string      `json:"dot"`
	Tiles    []menuTile  `json:"tiles"`
	Strips   []menuStrip `json:"strips"`
	Accounts []menuSoon  `json:"accounts"`
	Quiet    []menuQuiet `json:"quiet"`
	Theme    []string    `json:"theme"`
	ThemeBox []*barBox   `json:"themeBox"`
	Out      *string     `json:"out"`
	OutBox   *barBox     `json:"outBox"`
	Words    string      `json:"words"`
}

type menuFit struct {
	Half   bool `json:"half"`
	Screen struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"screen"`
	Sheet  *barBox `json:"sheet"`
	Body   *barBox `json:"body"`
	Scroll float64 `json:"scroll"`
	Client float64 `json:"client"`
	Foot   *barBox `json:"foot"`
}

type menuAsked struct {
	Briefs  int `json:"briefs"`
	Actions int `json:"actions"`
	Devices int `json:"devices"`
}

type hostMenu struct {
	Before          menuRead  `json:"before"`
	Full            menuRead  `json:"full"`
	Fit             menuFit   `json:"fit"`
	Pages           []string  `json:"pages"`
	SoonPressed     int       `json:"soonPressed"`
	SoonPages       []string  `json:"soonPages"`
	SoonClosed      int       `json:"soonClosed"`
	DarkOnDark      int       `json:"darkOnDark"`
	LightOnDark     int       `json:"lightOnDark"`
	ThemeAfter      []string  `json:"themeAfter"`
	LightOnLight    int       `json:"lightOnLight"`
	AskedOpen       menuAsked `json:"askedOpen"`
	Installs        int       `json:"installs"`
	ClosedOnInstall int       `json:"closedOnInstall"`
	Reopened        struct {
		Asked menuAsked `json:"asked"`
		Usage menuTile  `json:"usage"`
	} `json:"reopened"`
	ReopenFirst menuTile    `json:"reopenFirst"`
	Update      menuRead    `json:"update"`
	Updates     int         `json:"updates"`
	Updating    []menuStrip `json:"updating"`
	UpdateFit   menuFit     `json:"updateFit"`
	Installed   []menuStrip `json:"installed"`
	Partial     menuRead    `json:"partial"`
	Bare        menuRead    `json:"bare"`
	Other       []string    `json:"other"`
}

func shown(p *string) string {
	if p == nil {
		return "<none>"
	}
	return *p
}

// tileOf returns the live tile of that title, failing the part when the menu
// has none: a tile that is not drawn does not pass for one that says nothing.
func tileOf(t *testing.T, tiles []menuTile, title string) menuTile {
	t.Helper()
	for _, tile := range tiles {
		if tile.Title == title {
			return tile
		}
	}
	t.Fatalf("there is no %s tile among %q", title, tileTitles(tiles))
	return menuTile{}
}

func tileTitles(tiles []menuTile) string {
	names := make([]string, 0, len(tiles))
	for _, t := range tiles {
		names = append(names, t.Title)
	}
	return strings.Join(names, "|")
}

// TestTheHostMenu runs the menu once in a real engine and reads it part by
// part: the fixture walks every state, each part below holds one promise.
func TestTheHostMenu(t *testing.T) {
	var got hostMenu
	runFixture(t, "hostmenu.html", &got)
	for _, part := range []struct {
		name  string
		check func(*testing.T, hostMenu)
	}{
		{"opensOnWhatTheMachineIsDoing", menuOpensOnWhatTheMachineIsDoing},
		{"opensAtOnceAndAsksWhenItOpens", menuOpensAtOnceAndAsksWhenItOpens},
		{"doorsOpenTheirPages", menuDoorsOpenTheirPages},
		{"accountsDoNotOpenYet", menuAccountsDoNotOpenYet},
		{"themeSwitchFlipsTheTheme", menuThemeSwitchFlipsTheTheme},
		{"stripIsTheInstallOrTheUpdate", menuStripIsTheInstallOrTheUpdate},
		{"leavesOutWhatItDoesNotHave", menuLeavesOutWhatItDoesNotHave},
		{"fitsTheSheetAsItOpens", menuFitsTheSheetAsItOpens},
	} {
		t.Run(part.name, func(t *testing.T) { part.check(t, got) })
	}
}

// The host menu opens on what the machine is doing: the host with the way the
// app reaches it, three live tiles that are doors to their pages, the install
// while it waits, three quiet doors with a line each, and a footer with the
// theme and the way out.
func menuOpensOnWhatTheMachineIsDoing(t *testing.T, got hostMenu) {
	m := got.Full

	if m.Name != "HELIOS" {
		t.Errorf("the head names %q, not the host", m.Name)
	}
	if m.Sub != "control panel · via LAN · up 12 days 3 h" || m.Dot != "on" {
		t.Errorf("the line under the host reads %q with the dot %q", m.Sub, m.Dot)
	}
	if strings.Contains(m.Words, "monitoring panel") {
		t.Errorf("the menu still calls itself the monitoring panel: %q", m.Words)
	}

	if tileTitles(m.Tiles) != "Machine|Usage|Briefs" {
		t.Fatalf("the live tiles are %q", tileTitles(m.Tiles))
	}
	machine, usage, briefs := tileOf(t, m.Tiles, "Machine"), tileOf(t, m.Tiles, "Usage"), tileOf(t, m.Tiles, "Briefs")
	if shown(machine.N) != "18%cpu" || shown(machine.S) != "mem 32%" || shown(machine.Fill) != "18.4%" {
		t.Errorf("Machine reads %s / %s with a bar of %s", shown(machine.N), shown(machine.S), shown(machine.Fill))
	}
	if shown(usage.N) != "7%5 h" || shown(usage.S) != "week 21%" || shown(usage.Fill) != "7.2%" {
		t.Errorf("Usage reads %s / %s with a bar of %s — not the contour the sessions page stands on",
			shown(usage.N), shown(usage.S), shown(usage.Fill))
	}
	if !strings.Contains(usage.Label, "personal") {
		t.Errorf("Usage does not say whose limits it shows: %q", usage.Label)
	}
	if shown(briefs.N) != "2" || shown(briefs.S) != "to answer" || !briefs.Lit {
		t.Errorf("Briefs reads %s / %s, lit %v — two of three wait for an answer", shown(briefs.N), shown(briefs.S), briefs.Lit)
	}
	if machine.Lit || usage.Lit {
		t.Error("a tile other than Briefs is lit")
	}
	for _, tile := range m.Tiles {
		if tile.Box == nil || tile.Box.Height < 44 {
			t.Errorf("the tile %s is %+v — narrower than a finger", tile.Title, tile.Box)
		}
	}

	if len(m.Strips) != 1 || m.Strips[0].Text != "Install the appnot installed yet" {
		t.Errorf("with the install waiting the strip reads %+v", m.Strips)
	}

	quiet := map[string]string{}
	for _, q := range m.Quiet {
		quiet[q.Title] = shown(q.Sub)
	}
	if len(m.Quiet) != 3 || quiet["Journal"] != "1 failed today" || quiet["Devices"] != "3 with access" ||
		quiet["Settings"] != "host · device" {
		t.Errorf("the quiet doors read %+v", m.Quiet)
	}
	if len(m.Quiet) == 0 || !m.Quiet[0].Crit {
		t.Error("a failure today is not in the colour of a failure")
	}

	if strings.Join(m.Theme, ",") != "Dark:true,Light:false" {
		t.Errorf("the theme reads %v on the dark theme", m.Theme)
	}
	for _, b := range append(append([]*barBox{}, m.ThemeBox...), m.OutBox) {
		if b == nil || b.Height < 44 {
			t.Errorf("a button of the footer is %+v — narrower than a finger", b)
		}
	}
	if shown(m.Out) != "Sign out" {
		t.Errorf("the footer has no Sign out: %q", shown(m.Out))
	}
	if len(m.ThemeBox) == 2 && m.OutBox != nil && m.ThemeBox[1] != nil && m.OutBox.Left <= m.ThemeBox[1].Right {
		t.Errorf("Sign out stands at %+v, not at the right of the theme %+v", m.OutBox, m.ThemeBox[1])
	}
	if m.OutBox != nil && m.OutBox.Width > got.Fit.Screen.Width/2 {
		t.Errorf("Sign out is %.0f px wide — a full-width row again, not a word at the edge", m.OutBox.Width)
	}
	if len(got.Other) > 0 {
		t.Errorf("the menu asked the service for something it was not given: %v", got.Other)
	}
}

// The menu opens at once and fills in: the counts it asks for are asked when
// it opens, not on every render, and the shelf that has not answered yet keeps
// its line out instead of a guess.
func menuOpensAtOnceAndAsksWhenItOpens(t *testing.T, got hostMenu) {
	if tileTitles(got.Before.Tiles) != "Machine|Usage|Briefs" {
		t.Fatalf("before the shelf answers the tiles are %q, not the three live ones", tileTitles(got.Before.Tiles))
	}
	if b := tileOf(t, got.Before.Tiles, "Briefs"); b.N != nil || b.S != nil || b.Lit {
		t.Errorf("before the shelf answers Briefs reads %s / %s, lit %v", shown(b.N), shown(b.S), b.Lit)
	}
	if got.AskedOpen != (menuAsked{1, 1, 1}) {
		t.Errorf("one opening and two themes later the service was asked %+v times — once per opening", got.AskedOpen)
	}
	if got.Reopened.Asked != (menuAsked{2, 2, 2}) {
		t.Errorf("opened a second time the service was asked %+v times in all", got.Reopened.Asked)
	}
	if b := got.ReopenFirst; b.N != nil || b.Lit {
		t.Errorf("opened again Briefs shows %s, lit %v, before the shelf answers — the count of the last opening", shown(b.N), b.Lit)
	}
	u := got.Reopened.Usage
	if shown(u.N) != "64%5 h" || shown(u.S) != "week 40%" || !strings.Contains(u.Label, "algo") {
		t.Errorf("with the sessions page on algo the usage reads %s / %s (%q)", shown(u.N), shown(u.S), u.Label)
	}
}

// Every door opens its page through the shell, in the order the menu reads.
func menuDoorsOpenTheirPages(t *testing.T, got hostMenu) {
	if strings.Join(got.Pages, ",") != "machine,usage,briefs,journal,devices,settings" {
		t.Errorf("the doors opened %v", got.Pages)
	}
}

// Over the quiet doors stands a row of the same doors that do not open yet:
// the accounts of Claude and Codex and the balancer, a word on top and what it
// is with soon under it, drawn quieter than a door that opens, and pressing
// them opens no page.
func menuAccountsDoNotOpenYet(t *testing.T, got hostMenu) {
	m := got.Full
	doors := make([]string, 0, len(m.Accounts))
	for _, a := range m.Accounts {
		doors = append(doors, a.Title+": "+shown(a.Sub))
	}
	if strings.Join(doors, "|") != "Claude: accounts · soon|Codex: accounts · soon|Balancer: settings · soon" {
		t.Fatalf("the row of doors that do not open yet reads %q", strings.Join(doors, "|"))
	}
	if len(m.Quiet) == 0 || len(m.Strips) == 0 {
		t.Fatalf("the menu has no quiet doors (%+v) or no strip (%+v) to stand between", m.Quiet, m.Strips)
	}
	journal := m.Quiet[0]
	open, err := strconv.ParseFloat(journal.Opacity, 64)
	if err != nil {
		t.Fatalf("the opacity of %s reads %q", journal.Title, journal.Opacity)
	}
	for _, a := range m.Accounts {
		if !a.Disabled {
			t.Errorf("%s can be pressed: it is not disabled", a.Title)
		}
		if quiet, err := strconv.ParseFloat(a.Opacity, 64); err != nil || quiet >= open {
			t.Errorf("%s is drawn at the opacity %q next to %s at %q — as loud as a door that opens",
				a.Title, a.Opacity, journal.Title, journal.Opacity)
		}
		if a.Box == nil || journal.Box == nil || m.Strips[0].Box == nil {
			t.Fatalf("the row was not measured: %+v, %+v, %+v", a.Box, journal.Box, m.Strips[0].Box)
		}
		if a.Box.Top != m.Accounts[0].Box.Top || a.Box.Height != journal.Box.Height {
			t.Errorf("%s stands at %+v, not in one row of the height of %s %+v", a.Title, a.Box, journal.Title, journal.Box)
		}
		if a.Box.Top < m.Strips[0].Box.Bottom || a.Box.Bottom > journal.Box.Top {
			t.Errorf("%s stands at %+v, not between the strip %+v and the quiet doors %+v",
				a.Title, a.Box, m.Strips[0].Box, journal.Box)
		}
	}
	if got.SoonPressed != 3 || len(got.SoonPages) != 0 || got.SoonClosed != 0 {
		t.Errorf("pressing %d of the three opened %v and closed the menu %d times", got.SoonPressed, got.SoonPages, got.SoonClosed)
	}
}

// The theme is two words with the one in use pressed: pressing it changes
// nothing, pressing the other flips the theme.
func menuThemeSwitchFlipsTheTheme(t *testing.T, got hostMenu) {
	if got.DarkOnDark != 0 || got.LightOnDark != 1 || got.LightOnLight != 1 {
		t.Errorf("Dark on dark switched %d times, Light on dark %d, Light on light %d",
			got.DarkOnDark, got.LightOnDark-got.DarkOnDark, got.LightOnLight-got.LightOnDark)
	}
	if strings.Join(got.ThemeAfter, ",") != "Dark:false,Light:true" {
		t.Errorf("after the flip the theme reads %v", got.ThemeAfter)
	}
}

// One strip under the tiles, only while it applies: the install closes the
// menu and runs, an update takes the strip and its press runs the update, and
// an installed app that is up to date has no strip at all.
func menuStripIsTheInstallOrTheUpdate(t *testing.T, got hostMenu) {
	if got.Installs != 1 || got.ClosedOnInstall != 1 {
		t.Errorf("Install ran %d times and closed the menu %d times", got.Installs, got.ClosedOnInstall)
	}
	if s := got.Update.Strips; len(s) != 1 || s[0].Text != "Update the appa new version is ready" || s[0].Disabled {
		t.Errorf("with an update waiting the strip reads %+v", s)
	}
	if got.Updates != 1 {
		t.Errorf("the update strip ran the update %d times", got.Updates)
	}
	if s := got.Updating; len(s) != 1 || !strings.HasPrefix(s[0].Text, "Updating…") || !s[0].Disabled {
		t.Errorf("while the update runs the strip reads %+v", s)
	}
	if len(got.Installed) != 0 {
		t.Errorf("an installed app with nothing to update still has a strip: %+v", got.Installed)
	}
	if b := tileOf(t, got.Update.Tiles, "Briefs"); shown(b.N) != "0" || b.Lit {
		t.Errorf("with every brief sent Briefs reads %s and is lit %v", shown(b.N), b.Lit)
	}
}

// A value the app does not have leaves its line out: no zero, no dash, no
// undefined in its place.
func menuLeavesOutWhatItDoesNotHave(t *testing.T, got hostMenu) {

	p := got.Partial
	if p.Sub != "control panel" {
		t.Errorf("with no map and no uptime the line reads %q", p.Sub)
	}
	machine, usage := tileOf(t, p.Tiles, "Machine"), tileOf(t, p.Tiles, "Usage")
	if shown(machine.N) != "40%cpu" || machine.S != nil {
		t.Errorf("with the cpu and no memory Machine reads %s / %s", shown(machine.N), shown(machine.S))
	}
	if shown(usage.N) != "50%5 h" || usage.S != nil {
		t.Errorf("with five hours and no week Usage reads %s / %s", shown(usage.N), shown(usage.S))
	}
	for _, q := range p.Quiet {
		if q.Title != "Settings" && q.Sub != nil {
			t.Errorf("with the service down %s still reads %q", q.Title, *q.Sub)
		}
	}

	b := got.Bare
	for _, tile := range []menuTile{tileOf(t, b.Tiles, "Machine"), tileOf(t, b.Tiles, "Usage")} {
		if tile.N != nil || tile.S != nil || tile.Fill != nil {
			t.Errorf("with no snapshot %s reads %s / %s with a bar of %s", tile.Title, shown(tile.N), shown(tile.S), shown(tile.Fill))
		}
	}
	for _, read := range []menuRead{p, b} {
		for _, junk := range []string{"undefined", "NaN", "null", "—"} {
			if strings.Contains(read.Words, junk) {
				t.Errorf("the menu says %q where it has no value: %q", junk, read.Words)
			}
		}
	}
	if strings.Contains(b.Words, "%") {
		t.Errorf("with no snapshot the menu still shows a share: %q", b.Words)
	}
}

// The whole menu fits the height the sheet opens to on a phone: nothing in it
// needs a drag or a scroll to be found, with the install or the update in it.
func menuFitsTheSheetAsItOpens(t *testing.T, got hostMenu) {
	for name, f := range map[string]menuFit{"install": got.Fit, "update": got.UpdateFit} {
		if !f.Half {
			t.Errorf("%s: the sheet did not open at its first stop", name)
		}
		if f.Sheet == nil || f.Body == nil || f.Foot == nil {
			t.Fatalf("%s: the sheet was not measured: %+v", name, f)
		}
		if f.Scroll > f.Client+1 {
			t.Errorf("%s: the menu is %.0f px tall in a sheet that opens to %.0f px — the rest needs a drag",
				name, f.Scroll, f.Client)
		}
		if f.Foot.Bottom > f.Body.Bottom+1 || f.Sheet.Bottom > f.Screen.Height+1 || f.Sheet.Top < 0 {
			t.Errorf("%s: the footer ends at %.0f, the sheet at %.0f of %.0f px", name, f.Foot.Bottom, f.Sheet.Bottom, f.Screen.Height)
		}
	}
}
