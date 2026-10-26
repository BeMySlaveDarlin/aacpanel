package check

import (
	"math"
	"os"
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
		t.Error("src/css/base.css: the root font size does not read --ui-scale — the two buttons in the header move nothing")
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
		t.Error("the desktop shell does not take the scale — the header has nothing to move")
	}
	if !strings.Contains(shell, `class="dkzoom"`) {
		t.Error("the header has no scale control — the size of the interface is settled by whoever wrote the styles")
	}
	if !strings.Contains(cssWithoutComments(cssSrc(t)), ".dkzoom") {
		t.Error("the scale control has no styles — two bare buttons stand in the header")
	}
}

// TestArchiveRowKeepsItsButtonsOffTheText measures a row of the archive in an
// engine. The buttons of a row used to be laid over it: they covered the model
// and the age of the conversation exactly when the pointer arrived, and the
// row answered a click with something other than what was read under it.
func TestArchiveRowKeepsItsButtonsOffTheText(t *testing.T) {
	var got struct {
		Plain struct {
			Overlap int     `json:"overlap"`
			Act     int     `json:"act"`
			Panel   int     `json:"panel"`
			Text    float64 `json:"text"`
			Shown   float64 `json:"shown"`
		} `json:"plain"`
		Scaled struct {
			Overlap int     `json:"overlap"`
			Act     int     `json:"act"`
			Panel   int     `json:"panel"`
			Text    float64 `json:"text"`
		} `json:"scaled"`
		Picked   string   `json:"picked"`
		PickRows []string `json:"pickRows"`
	}
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: the geometry of the panel is the stylesheet's — run make front first")
	}
	runWideFixture(t, "deskpanel.html", &got)

	if got.Plain.Act == 0 {
		t.Fatal("the archive drew no rows with buttons at all — the fixture measured nothing")
	}
	if got.Plain.Shown < 1 {
		t.Errorf("the buttons of a row are drawn at opacity %.2f before anything is hovered — a row that shows "+
			"what it can do only once it is touched says nothing from across the desk", got.Plain.Shown)
	}
	if got.Plain.Overlap > 0 {
		t.Errorf("the buttons of a row cover %d px² of its text — what is read under the pointer is not what the row says",
			got.Plain.Overlap)
	}
	if got.Scaled.Overlap > 0 {
		t.Errorf("at a larger scale the buttons cover %d px² of the text — the row comes apart as soon as the interface grows",
			got.Scaled.Overlap)
	}

	// A fifth of the scale is the least a person asks for when they say the
	// interface is too small; everything of a row has to answer it at once.
	for _, part := range []struct {
		what       string
		plain, big float64
	}{
		{"the button of a row", float64(got.Plain.Act), float64(got.Scaled.Act)},
		{"the panel", float64(got.Plain.Panel), float64(got.Scaled.Panel)},
		{"the text of a row", got.Plain.Text, got.Scaled.Text},
	} {
		if part.big < part.plain*1.2 {
			t.Errorf("at a scale of 1.4 %s went from %.0f to %.0f — it does not follow the scale, and grows apart from its neighbours",
				part.what, part.plain, part.big)
		}
	}

	if want := []string{"Acme Labs", "personal", "design", "ops", "docs", "research", "a contour with a name longer than its button"}; !equalStrings(got.PickRows, want) {
		t.Errorf("the contour filter of the archive offers %v, expected %v — the panel filters by something other than the map",
			got.PickRows, want)
	}
	if !strings.Contains(got.Picked, "contour=7") {
		t.Errorf("a contour picked in the archive went out as %q — the filter of the panel changes nothing", got.Picked)
	}
}

// The contour buttons keep their columns in both panels, including a short
// last row. Each button filters by its name and releases that filter again.
func TestDesktopContoursKeepEvenColumnsAndToggleTheirFilter(t *testing.T) {
	type layout struct {
		Display  string    `json:"display"`
		Wrap     string    `json:"wrap"`
		Rows     []int     `json:"rows"`
		Widths   []float64 `json:"widths"`
		Title    string    `json:"title"`
		Ellipsis string    `json:"ellipsis"`
		Clipped  bool      `json:"clipped"`
	}
	type selection struct {
		Picks   []string `json:"picks"`
		Pressed []string `json:"pressed"`
		Shown   []string `json:"shown"`
	}
	var got struct {
		PickRows []string               `json:"pickRows"`
		Seven    map[string]layout      `json:"seven"`
		Fifteen  map[string]layout      `json:"fifteen"`
		Toggles  map[string][]selection `json:"toggles"`
	}
	runWideFixture(t, "deskpanel.html", &got)
	if len(got.PickRows) != 7 {
		t.Fatalf("the contour group drew %v, expected seven buttons", got.PickRows)
	}
	for _, side := range []string{"left", "right"} {
		for _, sample := range []struct {
			count int
			rows  []int
			got   layout
		}{
			{7, []int{5, 2}, got.Seven[side]},
			{15, []int{5, 5, 5}, got.Fifteen[side]},
		} {
			l := sample.got
			if l.Display != "flex" || l.Wrap != "wrap" {
				t.Errorf("%s with %d contours: display %q, wrap %q, expected wrapping flex", side, sample.count, l.Display, l.Wrap)
			}
			if len(l.Rows) != len(sample.rows) {
				t.Errorf("%s with %d contours: rows %v, expected %v", side, sample.count, l.Rows, sample.rows)
			} else {
				for i, count := range sample.rows {
					if l.Rows[i] != count {
						t.Errorf("%s with %d contours: row %d has %d buttons, expected %d", side, sample.count, i, l.Rows[i], count)
					}
				}
			}
			if len(l.Widths) != sample.count {
				t.Errorf("%s: measured %d buttons, expected %d", side, len(l.Widths), sample.count)
			} else {
				for i, width := range l.Widths {
					if width <= 0 || math.Abs(width-l.Widths[0]) > .1 {
						t.Errorf("%s with %d contours: button %d is %.2f wide, first is %.2f", side, sample.count, i, width, l.Widths[0])
					}
				}
			}
			if l.Title != got.PickRows[6] || l.Ellipsis != "ellipsis" || !l.Clipped {
				t.Errorf("%s with %d contours: the long name has title %q, overflow %q, clipped %v", side, sample.count, l.Title, l.Ellipsis, l.Clipped)
			}
		}

		states := got.Toggles[side]
		wantPicks := [][]string{nil, {got.PickRows[0]}, {got.PickRows[0], got.PickRows[6]}, {got.PickRows[6]}, nil}
		if len(states) != len(wantPicks) {
			t.Errorf("%s: read %d toggle states, expected %d", side, len(states), len(wantPicks))
			continue
		}
		for step, picks := range wantPicks {
			state := states[step]
			if !equalStrings(state.Picks, picks) {
				t.Errorf("%s step %d: picked %v, expected %v", side, step, state.Picks, picks)
			}
			shown := picks
			if len(shown) == 0 {
				shown = got.PickRows
			}
			if !equalStrings(state.Shown, shown) {
				t.Errorf("%s step %d: visible contours %v, expected %v", side, step, state.Shown, shown)
			}
			if len(state.Pressed) != len(got.PickRows) {
				t.Errorf("%s step %d: read %d pressed states for %d buttons", side, step, len(state.Pressed), len(got.PickRows))
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
					t.Errorf("%s step %d: %q has aria-pressed %q, expected %q", side, step, name, state.Pressed[i], want)
				}
			}
		}
	}
}
