package check

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestDesktopSizesRideOnTheRootFontSize holds the desktop styles to sizes the
// scale can move. A height or a width written in pixels stays where it was
// while the type beside it grows, so a row keeps its old height around larger
// letters and the buttons on it stop fitting.
func TestDesktopSizesRideOnTheRootFontSize(t *testing.T) {
	// Hairlines, bars and scrollbars are drawn, not read: they are the same
	// thickness at any scale, and a thin line in pixels is the point of it.
	const drawn = 12

	files := cssFiles(t)
	// A media query is a question about the window, not a size of anything
	// drawn: its width is in pixels on purpose and no scale may move it.
	query := regexp.MustCompile(`@media[^{]*`)
	size := regexp.MustCompile(`\b(?:min-)?(?:width|height):\s*(-?\d*\.?\d+)px`)
	seen := 0
	for _, path := range sortedKeys(files) {
		if !strings.HasPrefix(path, "src/css/desktop") {
			continue
		}
		css := query.ReplaceAllStringFunc(files[path], blankOut)
		seen++
		for _, m := range size.FindAllStringSubmatchIndex(css, -1) {
			value, err := strconv.ParseFloat(css[m[2]:m[3]], 64)
			if err != nil || math.Abs(value) <= drawn {
				continue
			}
			t.Errorf("%s:%d: a size of %spx — the scale of the shell moves rem, and this one stays behind "+
				"while the text around it grows", path, lineAt(css, m[0]), css[m[2]:m[3]])
		}
	}
	if seen == 0 {
		t.Fatal("no desktop styles found — the test is useless, check the path")
	}

	root := files["src/css/base.css"]
	if !strings.Contains(root, "var(--ui-scale") {
		t.Error("src/css/base.css: the root font size does not read --ui-scale — the two scale buttons move nothing")
	}
}

// blankOut keeps the length and the lines of what it replaces, so a position
// found afterwards still names the line it came from.
func blankOut(text string) string {
	out := []byte(text)
	for i := range out {
		if out[i] != '\n' {
			out[i] = ' '
		}
	}
	return string(out)
}

// TestDesktopScaleLivesOnTheRootAndLeavesWithTheShell holds the one place the
// scale is written down. Left on the root after the desktop shell is gone it
// follows the same browser to the narrow screen, where nothing can undo it.
func TestDesktopScaleLivesOnTheRootAndLeavesWithTheShell(t *testing.T) {
	src, ok := srcFiles(t)["src/desktop/scale.js"]
	if !ok {
		t.Fatal("src/desktop/scale.js not found — the test is looking in the wrong place")
	}
	body := withoutComments(src)

	if !strings.Contains(body, "document.documentElement") {
		t.Error("src/desktop/scale.js does not touch the root — the rem of the shell are measured against it and see nothing else")
	}
	for _, want := range []struct{ code, harm string }{
		{`setProperty("--ui-scale"`, "the scale is put somewhere other than the root, and the rem of the shell do not see it"},
		{`removeProperty("--ui-scale")`, "the scale stays on the root after the desktop shell is gone and follows the browser to the phone"},
		{"localStorage.setItem", "the scale is forgotten with the page, and it is set again at every visit"},
	} {
		if !strings.Contains(body, want.code) {
			t.Errorf("src/desktop/scale.js does not %s — %s", want.code, want.harm)
		}
	}

	shell := srcFiles(t)["src/desktop/shell.js"]
	if !strings.Contains(shell, "useScale()") {
		t.Error("the desktop shell does not take the scale — its controls have nothing to move")
	}
	if !strings.Contains(shell, `class="dkzoom"`) {
		t.Error("the shell has no scale control — the size of the interface is settled by whoever wrote the styles")
	}
	if !strings.Contains(cssWithoutComments(cssSrc(t)), ".dkzoom") {
		t.Error("the scale control has no styles — two bare buttons stand in the rail")
	}
}

// The contour filter of the archive offers the contours of the map, and a
// contour picked there goes out with the request.
func TestTheArchiveFiltersByTheContoursOfTheMap(t *testing.T) {
	var got struct {
		Picked   string   `json:"picked"`
		PickRows []string `json:"pickRows"`
	}
	runWideFixture(t, "deskpanel.html", &got)
	if want := []string{"Acme Labs", "personal", "design", "ops", "docs", "research", "a contour with a name longer than its button"}; !equalStrings(got.PickRows, want) {
		t.Errorf("the contour filter of the archive offers %v, expected %v — the panel filters by something other than the map",
			got.PickRows, want)
	}
	if !strings.Contains(got.Picked, "contour=7") {
		t.Errorf("a contour picked in the archive went out as %q — the filter of the panel changes nothing", got.Picked)
	}
}

// Each contour button of the archive filters by its name and releases that
// filter again, several at once or none; a long name is whole in its tip.
func TestDesktopArchiveContoursToggleTheirFilter(t *testing.T) {
	type selection struct {
		Picks   []string `json:"picks"`
		Pressed []string `json:"pressed"`
		Shown   []string `json:"shown"`
	}
	var got struct {
		PickRows []string    `json:"pickRows"`
		Title    string      `json:"title"`
		Toggles  []selection `json:"toggles"`
	}
	runWideFixture(t, "deskpanel.html", &got)
	if len(got.PickRows) != 7 {
		t.Fatalf("the contour group drew %v, expected seven buttons", got.PickRows)
	}
	if got.Title != got.PickRows[6] {
		t.Errorf("the long name has the tip %q", got.Title)
	}
	wantPicks := [][]string{nil, {got.PickRows[0]}, {got.PickRows[0], got.PickRows[6]}, {got.PickRows[6]}, nil}
	if len(got.Toggles) != len(wantPicks) {
		t.Fatalf("read %d toggle states, expected %d", len(got.Toggles), len(wantPicks))
	}
	for step, picks := range wantPicks {
		state := got.Toggles[step]
		if !equalStrings(state.Picks, picks) {
			t.Errorf("step %d: picked %v, expected %v", step, state.Picks, picks)
		}
		shown := picks
		if len(shown) == 0 {
			shown = got.PickRows
		}
		if !equalStrings(state.Shown, shown) {
			t.Errorf("step %d: visible contours %v, expected %v", step, state.Shown, shown)
		}
		if len(state.Pressed) != len(got.PickRows) {
			t.Errorf("step %d: read %d pressed states for %d buttons", step, len(state.Pressed), len(got.PickRows))
			continue
		}
		for i, name := range got.PickRows {
			want := "false"
			for _, pick := range picks {
				if name == pick {
					want = "true"
				}
			}
			if state.Pressed[i] != want {
				t.Errorf("step %d: %q has aria-pressed %q, expected %q", step, name, state.Pressed[i], want)
			}
		}
	}
}

// The contours of the sessions column at a desk are a switch, as the tabs of
// the phone are: the column shows one contour, a press on a tab shows that
// one and marks it alone selected, and a press on the selected tab keeps it.
// The choice is kept where the phone keeps the page of its sessions tab, so
// the column drawn again stands where it was left, and on the first contour
// once the one kept is gone from the map.
func TestDesktopColumnContoursAreASwitch(t *testing.T) {
	type state struct {
		Shown    string   `json:"shown"`
		Roles    []string `json:"roles"`
		Selected []string `json:"selected"`
		Pressed  int      `json:"pressed"`
		Sections []string `json:"sections"`
		Panel    string   `json:"panel"`
		Kept     string   `json:"kept"`
	}
	var got struct {
		TabRows []string `json:"tabRows"`
		Group   string   `json:"group"`
		Tabs    []state  `json:"tabs"`
	}
	runWideFixture(t, "deskpanel.html", &got)
	if len(got.TabRows) != 7 {
		t.Fatalf("the switch drew %v, expected a tab for each of the seven contours", got.TabRows)
	}
	if got.Group != "tablist" {
		t.Errorf("the switch is a %q, expected a tablist: one of its tabs is chosen, not several", got.Group)
	}
	names := got.TabRows
	// What the column stands on after each step: the contour kept before the
	// first draw, two presses, a press on the selected tab, the column drawn
	// again, and drawn again over a kept contour that is not on the map.
	want := []struct{ shown, why string }{
		{names[4], "the first draw does not start on the contour kept"},
		{names[0], "a press on a tab does not show its contour"},
		{names[6], "a press on another tab does not move the column to it"},
		{names[6], "a press on the selected tab lets it go"},
		{names[6], "the column drawn again forgets the contour it stood on"},
		{names[0], "a kept contour gone from the map does not fall back to the first"},
	}
	if len(got.Tabs) != len(want) {
		t.Fatalf("read %d states of the switch, expected %d: %+v", len(got.Tabs), len(want), got.Tabs)
	}
	for step, w := range want {
		s := got.Tabs[step]
		if s.Shown != w.shown || !equalStrings(s.Sections, []string{w.shown}) || s.Panel != w.shown {
			t.Errorf("step %d: %s — the column stands on %q and shows sections %v in a panel named %q, expected %q alone",
				step, w.why, s.Shown, s.Sections, s.Panel, w.shown)
		}
		if s.Pressed != 0 {
			t.Errorf("step %d: %d tabs say aria-pressed — a tab of a switch is selected, not pressed", step, s.Pressed)
		}
		if len(s.Roles) != len(names) || len(s.Selected) != len(names) {
			t.Errorf("step %d: read %d roles and %d selected states for %d tabs", step, len(s.Roles), len(s.Selected), len(names))
			continue
		}
		for i, name := range names {
			if s.Roles[i] != "tab" {
				t.Errorf("step %d: %q has the role %q, expected tab", step, name, s.Roles[i])
			}
			mark := "false"
			if name == w.shown {
				mark = "true"
			}
			if s.Selected[i] != mark {
				t.Errorf("step %d: %q has aria-selected %q, expected %q", step, name, s.Selected[i], mark)
			}
		}
	}
	if got.Tabs[2].Kept != names[6] {
		t.Errorf("after a press on %q the device keeps %q — the next draw would not find the contour pressed", names[6], got.Tabs[2].Kept)
	}
}
