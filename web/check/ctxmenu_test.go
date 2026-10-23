package check

import (
	"reflect"
	"strings"
	"testing"
)

// The menu of the right click has one host, and only the desktop shell
// mounts it: a component drawn on the phone too asks for its menu all the
// same and, with no host there, leaves the right click to the browser.
func TestTheMenuHostStandsInTheDesktopShellAlone(t *testing.T) {
	files := srcFiles(t)
	var hosts []string
	for _, path := range sortedKeys(files) {
		if strings.Contains(stripComments(files[path]), "<${MenuHost}") {
			hosts = append(hosts, path)
		}
	}
	if !reflect.DeepEqual(hosts, []string{"src/desktop/shell.js"}) {
		t.Errorf("the menu host is mounted in %v — expected the desktop shell alone", hosts)
	}
}

type ctxBox struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

type ctxItem struct {
	Note    string  `json:"note"`
	Tip     string  `json:"tip"`
	Off     bool    `json:"off"`
	Opacity float64 `json:"opacity"`
}

type ctxSent struct {
	Kind   string         `json:"kind"`
	Target string         `json:"target"`
	Params map[string]any `json:"params"`
}

type ctxNative struct {
	Prevented bool `json:"prevented"`
	Menu      bool `json:"menu"`
}

type ctxMenu struct {
	VW      int `json:"vw"`
	VH      int `json:"vh"`
	AtPoint struct {
		Prevented bool   `json:"prevented"`
		Menu      ctxBox `json:"menu"`
		Label     string `json:"label"`
		Focus     string `json:"focus"`
	} `json:"atPoint"`
	Lists       map[string][]string `json:"lists"`
	About       map[string]*ctxItem `json:"about"`
	TipShown    string              `json:"tipShown"`
	DimmedPress struct {
		Open bool `json:"open"`
		Sent int  `json:"sent"`
	} `json:"dimmedPress"`
	Escape struct {
		Open     bool `json:"open"`
		FocusRow bool `json:"focusRow"`
	} `json:"escape"`
	NearBottom ctxBox `json:"nearBottom"`
	Outside    struct {
		Open     bool `json:"open"`
		FocusRow bool `json:"focusRow"`
	} `json:"outside"`
	Corner    ctxBox `json:"corner"`
	Elsewhere struct {
		Prevented bool   `json:"prevented"`
		Menu      ctxBox `json:"menu"`
		Label     string `json:"label"`
		Count     int    `json:"count"`
	} `json:"elsewhere"`
	Chosen struct {
		Open     bool   `json:"open"`
		Picked   string `json:"picked"`
		FocusRow bool   `json:"focusRow"`
	} `json:"chosen"`
	Gone  map[string]bool `json:"gone"`
	Stays struct {
		Open     bool `json:"open"`
		Scrolled int  `json:"scrolled"`
	} `json:"stays"`
	Native   map[string]ctxNative `json:"native"`
	Keyboard struct {
		Prevented bool     `json:"prevented"`
		Menu      ctxBox   `json:"menu"`
		Row       ctxBox   `json:"row"`
		First     string   `json:"first"`
		Walk      []string `json:"walk"`
		Leaked    []string `json:"leaked"`
		Enter     struct {
			Open     bool   `json:"open"`
			Picked   string `json:"picked"`
			FocusRow bool   `json:"focusRow"`
		} `json:"enter"`
		Digit struct {
			Digit int      `json:"digit"`
			Sent  *ctxSent `json:"sent"`
			Open  bool     `json:"open"`
		} `json:"digit"`
	} `json:"keyboard"`
	ScratchAsking struct {
		List []string `json:"list"`
		Move *ctxItem `json:"move"`
	} `json:"scratchAsking"`
	Moved              *ctxSent       `json:"moved"`
	Resumed            *ctxSent       `json:"resumed"`
	ResumedByButton    *ctxSent       `json:"resumedByButton"`
	ArchiveNew         *ctxSent       `json:"archiveNew"`
	ArchiveNewByButton *ctxSent       `json:"archiveNewByButton"`
	ProjectNew         *ctxSent       `json:"projectNew"`
	ProjectNewByButton *ctxSent       `json:"projectNewByButton"`
	Settings           map[string]int `json:"settings"`
	SettingsStart      map[string]int `json:"settingsStart"`
	Terminal           *struct {
		ID    string `json:"id"`
		Place string `json:"place"`
	} `json:"terminal"`
	Copied []string `json:"copied"`
	Rename struct {
		Value    string `json:"value"`
		Menu     bool   `json:"menu"`
		FocusRow bool   `json:"focusRow"`
	} `json:"rename"`
	Restarted   *ctxSent `json:"restarted"`
	Detached    *ctxSent `json:"detached"`
	DetachTitle string   `json:"detachTitle"`
	Stood       []struct {
		Choices []string `json:"choices"`
		Sent    ctxSent  `json:"sent"`
	} `json:"stood"`
	ClosedByMenu    *ctxSent `json:"closedByMenu"`
	ClosedByButton  *ctxSent `json:"closedByButton"`
	Stopped         *ctxSent `json:"stopped"`
	StoppedByButton *ctxSent `json:"stoppedByButton"`
	Started         *ctxSent `json:"started"`
	StartedByButton *ctxSent `json:"startedByButton"`
	PlayIcon        struct {
		Danger bool `json:"danger"`
		Play   bool `json:"play"`
	} `json:"playIcon"`
	Logs           []string `json:"logs"`
	Downed         *ctxSent `json:"downed"`
	DownedByButton *ctxSent `json:"downedByButton"`
	Narrow         struct {
		Width     int  `json:"width"`
		Prevented bool `json:"prevented"`
		Menu      bool `json:"menu"`
	} `json:"narrow"`
	WideFrame struct {
		Width     int  `json:"width"`
		Prevented bool `json:"prevented"`
	} `json:"wideFrame"`
	ArchiveNewOn []struct {
		Name string `json:"name"`
		Menu bool   `json:"menu"`
		Plus bool   `json:"plus"`
	} `json:"archiveNewOn"`
}

func ctxMenuPage(t *testing.T) ctxMenu {
	t.Helper()
	var got ctxMenu
	runWideFixture(t, "ctxmenu.html", &got)
	return got
}

// near says whether two measures of the page stand within a pixel.
func near(a, b int) bool {
	return a-b <= 1 && b-a <= 1
}

// A right click opens the menu with its corner at the point; near the bottom
// it goes up from the point, and in the corner of the window it is kept
// inside. Escape, a press outside and the item chosen take it down, each
// giving the focus back to the row, and so do a wheel, a scroll that moves
// the row and the window left or resized, while a scroll elsewhere leaves it;
// a right click elsewhere opens the menu of what it was made on, there.
func TestTheRightClickMenuOpensAtThePointAndStaysInside(t *testing.T) {
	got := ctxMenuPage(t)
	const margin = 8

	at := got.AtPoint
	if !at.Prevented || at.Label != "session aacpanel" {
		t.Fatalf("a right click on a session row opened %q (browser menu kept: %v) — the panel's menu is not there", at.Label, !at.Prevented)
	}
	if !near(at.Menu.Left, 300) || !near(at.Menu.Top, 200) {
		t.Errorf("the menu of a click at 300,200 stands at %d,%d — not at the point", at.Menu.Left, at.Menu.Top)
	}
	if at.Focus != "Open" {
		t.Errorf("the menu opens with %q in focus, expected the first item that can be pressed, Open", at.Focus)
	}

	nb := got.NearBottom
	if !near(nb.Bottom, got.VH-40) || !near(nb.Left, 300) {
		t.Errorf("a click 40 px over the bottom opens a menu at %+v: it should go up from the point, its bottom at %d", nb, got.VH-40)
	}
	c := got.Corner
	if c.Right > got.VW-margin || c.Bottom > got.VH-margin || c.Left < margin || c.Top < margin {
		t.Errorf("a click in the corner opens a menu at %+v: it leaves the window %dx%d less its margin", c, got.VW, got.VH)
	}

	if got.Escape.Open || !got.Escape.FocusRow {
		t.Errorf("Escape: menu still open %v, focus back on the row %v", got.Escape.Open, got.Escape.FocusRow)
	}
	if got.Outside.Open || !got.Outside.FocusRow {
		t.Errorf("a press outside: menu still open %v, focus back on the row %v", got.Outside.Open, got.Outside.FocusRow)
	}
	for _, by := range []string{"wheel", "scroll", "blur", "resize"} {
		if !got.Gone[by] {
			t.Errorf("the menu stayed through a %s: the page moved or left from under it", by)
		}
	}
	if got.Stays.Scrolled == 0 {
		t.Fatal("the panel at the right did not scroll: the case of a scroll elsewhere proves nothing")
	}
	if !got.Stays.Open {
		t.Error("a scroll of the panel at the right took down the menu of a row it did not move: " +
			"a feed that follows what the model writes would take every menu down")
	}
	if got.Chosen.Open || got.Chosen.Picked != "helios" || !got.Chosen.FocusRow {
		t.Errorf("choosing Open: menu still open %v, opened %q (expected helios), focus back on the row %v",
			got.Chosen.Open, got.Chosen.Picked, got.Chosen.FocusRow)
	}

	e := got.Elsewhere
	if !e.Prevented || e.Label != "session helios" || e.Count != 1 {
		t.Errorf("a right click on another row while the menu stood: %q, %d menus — expected one, of helios", e.Label, e.Count)
	}
	if !near(e.Menu.Left, 320) || !near(e.Menu.Top, 260) {
		t.Errorf("the menu reopened elsewhere stands at %d,%d, not at the new point 320,260", e.Menu.Left, e.Menu.Top)
	}
}

// The browser keeps its own menu with Shift held, in a field, in an editable
// block, in a terminal, over a selection made inside the object, on an
// object that has no menu, and on a screen narrower than the shell; the same
// object without any of these, and the same frame made wide, are the panel's.
func TestTheBrowserKeepsItsMenuWhereThePanelHasNothingBetter(t *testing.T) {
	got := ctxMenuPage(t)
	for _, name := range []string{"shift", "input", "editable", "terminal", "selection", "noMenu"} {
		n, ok := got.Native[name]
		if !ok {
			t.Fatalf("the page did not try %s", name)
		}
		if n.Prevented || n.Menu {
			t.Errorf("%s: the right click was taken (prevented %v, menu %v) — the browser's own menu belongs there", name, n.Prevented, n.Menu)
		}
	}
	if ctl := got.Native["control"]; !ctl.Prevented || !ctl.Menu {
		t.Errorf("the same object with nothing selected opened no menu (%+v): the cases above prove nothing", ctl)
	}
	if got.Narrow.Width >= 1100 {
		t.Fatalf("the narrow frame is %d px wide — it is not narrower than the shell", got.Narrow.Width)
	}
	if got.Narrow.Prevented || got.Narrow.Menu {
		t.Errorf("a %d px screen took the right click (prevented %v, menu %v)", got.Narrow.Width, got.Narrow.Prevented, got.Narrow.Menu)
	}
	if !got.WideFrame.Prevented {
		t.Errorf("the frame made %d px wide still leaves the right click to the browser: the narrow case proves nothing", got.WideFrame.Width)
	}
}

// The menu key and Shift+F10 come as a contextmenu at the middle of the
// focused row with button -1: the menu stands under the row, not over its
// middle; the arrows go round it, Home and End to its ends, Enter presses the
// item in focus and a digit the item of its number.
func TestTheKeyboardMenuStandsUnderTheRow(t *testing.T) {
	got := ctxMenuPage(t)
	k := got.Keyboard
	if !k.Prevented {
		t.Fatal("the menu key left the browser's menu: the keyboard has no menu of the panel")
	}
	if !near(k.Menu.Top, k.Row.Bottom) || !near(k.Menu.Left, max(k.Row.Left, 8)) {
		t.Errorf("the keyboard's menu stands at %d,%d — expected under the row, at %d,%d", k.Menu.Left, k.Menu.Top, max(k.Row.Left, 8), k.Row.Bottom)
	}
	if k.First != "Open" {
		t.Errorf("the keyboard's menu opens with %q in focus, expected Open", k.First)
	}
	want := []string{"Terminal of the project", "Rename…", "Terminal of the project", "Close the session", "Open", "Open", "Close the session"}
	if !reflect.DeepEqual(k.Walk, want) {
		t.Errorf("Down, Down, Up, End, Down, Home, Up went %q\nexpected %q", k.Walk, want)
	}
	if len(k.Leaked) > 0 {
		t.Errorf("the keys %q went past the open menu to the page: an arrow there is the next session, Escape the panel at the right", k.Leaked)
	}
	if k.Enter.Open || k.Enter.Picked != "aacpanel" || !k.Enter.FocusRow {
		t.Errorf("Enter on Open: menu still open %v, opened %q, focus back on the row %v", k.Enter.Open, k.Enter.Picked, k.Enter.FocusRow)
	}
	if k.Digit.Digit < 1 || k.Digit.Digit > 9 {
		t.Fatalf("Stop the turn is item %d — beyond the digits", k.Digit.Digit)
	}
	if k.Digit.Open || k.Digit.Sent == nil || k.Digit.Sent.Kind != "session.stop" || k.Digit.Sent.Target != "aacpanel" {
		t.Errorf("the digit %d of Stop the turn sent %+v, menu still open %v", k.Digit.Digit, k.Digit.Sent, k.Digit.Open)
	}
}

// Every object has its menu: a live session of claude, the home one, one in
// tmux, one the panel did not start, a thread of codex, a closed conversation
// on the shelf and in the archive, a project's heading, a container running
// and stopped, a stack. A dimmed item is marked ~.
func TestEveryObjectHasItsMenu(t *testing.T) {
	got := ctxMenuPage(t)
	want := map[string]string{
		"aacpanel": "Open · Terminal of the project · | · Rename… · ~Role… · ~Session messages… · | · Stop the turn · ~Move to tmux · ~Turn Remote Control on · ~Stand under… · ~Pin the group · | · " +
			"Copy the session ID · Copy the resume command · | · Restart the session · Close the session",
		"helios": "Open · Terminal of the project · | · Rename… · ~Role… · ~Session messages… · | · ~Turn Remote Control on · ~Stand under… · ~Pin the group · | · " +
			"Copy the session ID · Copy the resume command · | · Restart the session",
		"scratch": "Open · Terminal of the project · | · ~Rename… · ~Role… · ~Session messages… · | · Move to the stream · Open a window on this machine · " +
			"~Turn Remote Control on · ~Stand under… · ~Pin the group · | · Copy the session ID · Copy the resume command · | · Restart the session · Close the session",
		"child": "Open · Terminal of the project · | · Rename… · ~Role… · ~Session messages… · | · ~Turn Remote Control on · Detach from parent · Stand under… · ~Pin the group of aacpanel · | · " +
			"Copy the session ID · Copy the resume command · | · Restart the session · Close the session",
		"ext":          "Open · | · Copy the session ID · Copy the resume command",
		"codex":        "Open · | · ~Stand under… · ~Pin the group · | · Copy the thread ID · | · Stop the turn · Close the session",
		"codexChild":   "Open · | · Detach from parent · Stand under… · ~Pin the group of aacpanel · | · Copy the thread ID · | · Close the session",
		"closed":       "Open · Resume · New session in the project · | · Copy the session ID · Copy the resume command",
		"archive":      "Open · Resume · New session in the project · | · Copy the session ID · Copy the resume command",
		"archiveCodex": "Open · Resume · New session in the project · | · Copy the thread ID",
		"project":      "New session · Terminal of the project · | · Settings of the project",
		"projectHome":  "Terminal of the project · | · Settings of the project",
		"running":      "~Restart · Stop · | · Logs · Copy the name",
		"exited":       "~Restart · Start · | · Logs · Copy the name",
		"stack":        "Bring up · Bring down",
	}
	for name, list := range want {
		have := strings.Join(got.Lists[name], " · ")
		if have != list {
			t.Errorf("the menu of %s:\n  %s\nexpected\n  %s", name, have, list)
		}
	}
	// The move waits on the panel's answer, dimmed, and is pressed once it
	// has come.
	asking := got.ScratchAsking.Move
	if asking == nil || !asking.Off || asking.Note != "asking…" {
		t.Errorf("before the panel answers where scratch can move, the move stands as %+v — expected dimmed, asking…", asking)
	}
	if m := got.About["move"]; m == nil || !m.Off || m.Tip != "the session is answering — switch once it finishes" || m.Note != "stops 2 agents" {
		t.Errorf("the move of a working session stands as %+v — expected dimmed with why it waits, and what it stops", m)
	}
	for _, name := range []string{"restart", "close"} {
		if m := got.About[name]; m == nil || m.Note != "stops 2 agents" {
			t.Errorf("%s of a session with two agents at work says %+v — it should name what it stops", name, m)
		}
	}
	if m := got.About["codexClose"]; m == nil || !strings.Contains(m.Tip, "the daemon unloads it") {
		t.Errorf("the close of a codex thread says %+v — it should carry what a close does to a thread", m)
	}
	// Only a session standing under another is taken from under it: the item
	// names the parent and says the session stands on its own in the list.
	if m := got.About["detach"]; m == nil || m.Off || m.Note != "under aacpanel" ||
		!strings.Contains(m.Tip, "stands on its own in the list") {
		t.Errorf("the detach of a session aacpanel opened stands as %+v — expected naming aacpanel and what it does", m)
	}
	if m := got.About["parent"]; m == nil || m.Off || m.Opacity != 1 || !strings.Contains(m.Tip, "same project") {
		t.Errorf("the parent choice must be visible without hover and explain its scope: %+v", m)
	}
}

// An item that cannot be pressed now stays in its place, dimmed, says why on
// its tip in the words of the buttons, and takes a press without doing
// anything.
func TestAnItemThatCannotBePressedSaysWhy(t *testing.T) {
	got := ctxMenuPage(t)
	for name, why := range map[string]string{
		"remote":           "the host does not know the action “session.remote”",
		"containerRestart": "the host does not know the action “container.restart”",
		"rename":           "a session in tmux is renamed on its own screen",
		"parentEmpty":      "no other live session in this project can be its parent",
	} {
		m := got.About[name]
		if m == nil {
			t.Errorf("%s: there is no such item in its menu", name)
			continue
		}
		if !m.Off || m.Opacity >= 1 {
			t.Errorf("%s stands as %+v — expected dimmed", name, m)
		}
		if !strings.Contains(m.Tip, why) {
			t.Errorf("%s says %q — expected %q", name, m.Tip, why)
		}
	}
	if !strings.Contains(got.TipShown, "session.remote") {
		t.Errorf("the pointer on a dimmed item showed the tip %q — its reason is not read", got.TipShown)
	}
	if !got.DimmedPress.Open || got.DimmedPress.Sent != 0 {
		t.Errorf("a press on a dimmed item closed the menu (%v) or sent %d actions", !got.DimmedPress.Open, got.DimmedPress.Sent)
	}
}

// An item sends what the button of the same name sends, through the gate: the
// close of the row, Resume of the shelf, the new session of the archive and
// of a project's plus, the stop and the start of a container, the stack
// brought down. A restart reaches any session the panel started, not only
// the home one. The rest does what its button does: the terminal of the
// project, the settings of the map on the project, the copies, the rename,
// the logs, the move.
func TestMenuItemsSendWhatTheButtonsSend(t *testing.T) {
	got := ctxMenuPage(t)
	pairs := []struct {
		what         string
		menu, button *ctxSent
		kind, target string
	}{
		{"close of a session", got.ClosedByMenu, got.ClosedByButton, "session.close", "aacpanel"},
		{"Resume of a closed conversation", got.Resumed, got.ResumedByButton, "session.resume", "blog"},
		{"new session from the archive", got.ArchiveNew, got.ArchiveNewByButton, "session.open", "blog"},
		{"new session of a project", got.ProjectNew, got.ProjectNewByButton, "session.open", "aacpanel"},
		{"stop of a running container", got.Stopped, got.StoppedByButton, "container.stop", "shop-web"},
		{"start of a stopped container", got.Started, got.StartedByButton, "container.start", "shop-db"},
		{"stack brought down", got.Downed, got.DownedByButton, "stack.down", "shop"},
	}
	for _, p := range pairs {
		if p.menu == nil || p.menu.Kind != p.kind || p.menu.Target != p.target {
			t.Errorf("%s: the menu sent %+v, expected %s of %s", p.what, p.menu, p.kind, p.target)
			continue
		}
		if !reflect.DeepEqual(p.menu, p.button) {
			t.Errorf("%s: the menu sent %+v and the button %+v", p.what, p.menu, p.button)
		}
	}
	if r := got.Restarted; r == nil || r.Kind != "session.restart" || r.Target != "aacpanel" || len(r.Params) != 0 {
		t.Errorf("Restart of a session that is not the home one sent %+v — expected session.restart of aacpanel", r)
	}
	if d := got.Detached; d == nil || d.Kind != "session.detach" || d.Target != "review" || len(d.Params) != 0 {
		t.Errorf("the detach of review sent %+v — expected session.detach of review", d)
	}
	if got.DetachTitle != "Detach review from aacpanel?" {
		t.Errorf("the sheet before the detach asks %q — it should name the session and its parent", got.DetachTitle)
	}
	wantParents := []struct{ target, parent, label string }{
		{"codex-2", "review", "review"}, {"review", "codex-2", "cx-review"},
	}
	if len(got.Stood) != len(wantParents) {
		t.Errorf("the parent choices sent %+v, want %d requests", got.Stood, len(wantParents))
	} else {
		for i, want := range wantParents {
			pick := got.Stood[i]
			if !reflect.DeepEqual(pick.Choices, []string{want.label}) {
				t.Errorf("the parent picker for %s offers %v, want only %s", want.target, pick.Choices, want.label)
			}
			if a := pick.Sent; a.Kind != "session.parent" || a.Target != want.target || len(a.Params) != 1 || a.Params["parent"] != want.parent {
				t.Errorf("the parent choice for %s sent %+v, want parent %s", want.target, a, want.parent)
			}
		}
	}
	if m := got.Moved; m == nil || m.Kind != "session.switch" || m.Target != "scratch" || m.Params["to"] != "stream" {
		t.Errorf("the move of scratch sent %+v — expected session.switch to the stream", m)
	}
	if got.Terminal == nil || got.Terminal.ID != "t-aac" || got.Terminal.Place != "/srv/aacpanel" {
		t.Errorf("the terminal of the project opened %+v — expected the one typed into last there, t-aac", got.Terminal)
	}
	if !reflect.DeepEqual(got.Settings, map[string]int{"contour": 3, "project": 11}) {
		t.Errorf("the settings of the project were asked at %v — expected contour 3, project 11", got.Settings)
	}
	if !reflect.DeepEqual(got.SettingsStart, map[string]int{"contour": 3, "group": 7, "project": 11}) {
		t.Errorf("the settings of the map open at %v — expected the group that holds the project, 7, and the project", got.SettingsStart)
	}
	if want := []string{"id-aacpanel", "cd /srv/aacpanel && claude --resume id-aacpanel"}; !reflect.DeepEqual(got.Copied, want) {
		t.Errorf("the copies put %q into the clipboard, expected %q", got.Copied, want)
	}
	if got.Rename.Value != "aacpanel" || got.Rename.Menu || !got.Rename.FocusRow {
		t.Errorf("Rename… opened a sheet with %q (menu still open %v), focus back on the row after it %v",
			got.Rename.Value, got.Rename.Menu, got.Rename.FocusRow)
	}
	if !reflect.DeepEqual(got.Logs, []string{"shop-db"}) {
		t.Errorf("Logs opened %q, expected the logs of shop-db", got.Logs)
	}
}

// A row of the archive offers a new session by its plus where its menu offers
// one and nowhere else: a conversation off the map whose directory names no
// project has neither, and a plus there would ask the host for a session of no
// name.
func TestTheArchiveOffersANewSessionWhereItsMenuDoes(t *testing.T) {
	got := ctxMenuPage(t)
	var names []string
	for _, r := range got.ArchiveNewOn {
		names = append(names, r.Name)
		if r.Plus != r.Menu {
			t.Errorf("the row of %s in the archive has a plus %v, while its menu offers a new session %v", r.Name, r.Plus, r.Menu)
		}
		if r.Name == "loose" && (r.Plus || r.Menu) {
			t.Errorf("the row of loose, off the map in a directory that names no project, offers a new session: plus %v, menu %v",
				r.Plus, r.Menu)
		}
	}
	if want := []string{"blog", "drafts", "loose"}; !reflect.DeepEqual(names, want) {
		t.Errorf("the archive was read as %q, expected %q", names, want)
	}
}

// A stopped container starts from the play icon, in the quiet colour: the
// red is for what stops.
func TestAStoppedContainerStartsFromAPlayIcon(t *testing.T) {
	got := ctxMenuPage(t)
	if !got.PlayIcon.Play {
		t.Error("the start of a stopped container is not the play icon")
	}
	if got.PlayIcon.Danger {
		t.Error("the start of a stopped container is painted as a danger")
	}
}
