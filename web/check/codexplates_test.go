package check

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const phone360 = `{"width":360,"height":800,"deviceScaleFactor":3,"mobile":true}`

// On the phone a contour page opens with its limits: claude's five hours and
// seven days, and beside them, the third window of their row, codex's week
// where codex has spent in the contour. Every window is a plate of one shape
// in a row of two and of three: the word of its agent, Claude or Codex in the
// colour of its mark, and the share over the bar, the window and the time to
// its reset under it, each line in its place in every window of the row; the
// window says it all in its title. Codex's bar is in codex's hue, as its ring
// at a desk is, and claude's in the accent. A contour without codex has
// claude's two; a contour codex alone spent in has codex's alone rather than
// the word that nobody worked there; an account that told no week, ran into
// its limit or left its numbers old says so, and beside claude's the week not
// told stands short where their last lines do. On the narrowest phone and on
// a common one no word is cut and no window runs into another.
func TestThePhoneContourPageShowsCodexsWeekBesideClaudesLimits(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type plate struct {
		Value     string `json:"value"`
		Span      string `json:"span"`
		Sub       string `json:"sub"`
		Title     string `json:"title"`
		Agent     string `json:"agent"`
		Word      string `json:"word"`
		WordAgent string `json:"wordAgent"`
		WordHue   string `json:"wordHue"`
		FillHue   string `json:"fillHue"`
		FillShare int    `json:"fillShare"`
		Dim       string `json:"dim"`
		X         int    `json:"x"`
		Y         int    `json:"y"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		HeadAt    int    `json:"headAt"`
		FootAt    int    `json:"footAt"`
		Lines     []int  `json:"lines"`
		Inside    bool   `json:"inside"`
	}
	type page struct {
		Name     string   `json:"name"`
		Width    int      `json:"width"`
		Dim      string   `json:"dim"`
		Plates   []plate  `json:"plates"`
		Notes    []string `json:"notes"`
		None     string   `json:"none"`
		Cut      []string `json:"cut"`
		Overlaps []string `json:"overlaps"`
	}
	for _, screen := range []struct{ name, metrics string }{{"360px", phone360}, {"390px", phone390}} {
		var got struct {
			Width    int      `json:"width"`
			CodexHue string   `json:"codexHue"`
			Accent   string   `json:"accent"`
			Pages    []page   `json:"pages"`
			Sky      []string `json:"sky"`
		}
		runFixtureOn(t, "codexplates.html", screen.metrics, phonePointer, &got)
		at := screen.name
		if len(got.Pages) != 5 {
			t.Fatalf("%s: the fixture measured %d contour pages: %+v", at, len(got.Pages), got.Pages)
		}
		for _, p := range got.Pages {
			if len(p.Cut) > 0 {
				t.Errorf("%s, %s: the limits cut words off: %v", at, p.Name, p.Cut)
			}
			if len(p.Overlaps) > 0 {
				t.Errorf("%s, %s: windows run into one another: %v", at, p.Name, p.Overlaps)
			}
			for _, l := range p.Plates {
				name := l.Word + " " + l.Span
				if !l.Inside {
					t.Errorf("%s, %s: the window %q stands out of the limits", at, p.Name, name)
				}
				// The word, the share, the window and the reset read on a line
				// each; the line of no week has no share and no window.
				for i, n := range l.Lines {
					if n > 1 {
						t.Errorf("%s, %s: a line of the window %q breaks in %d (%v)", at, p.Name, name, n, l.Lines)
						break
					}
					if n == 0 && !((i == 1 || i == 2) && l.Value == "") {
						t.Errorf("%s, %s: a line of the window %q is missing (%v)", at, p.Name, name, l.Lines)
						break
					}
				}
				hue := map[string]string{"Claude": claudeWordHue, "Codex": codexWordHue}[l.Word]
				if hue == "" || l.WordHue != hue || l.WordAgent != strings.ToLower(l.Word) {
					t.Errorf("%s, %s: the window %q is signed %q by %q in %s, expected Claude or Codex in the colour of its mark",
						at, p.Name, name, l.Word, l.WordAgent, l.WordHue)
				}
			}
		}
		// oneRow says whether the windows of a page fill one row from left to
		// right, as tall and as wide as one another, with the word of each and
		// its last line in the same places.
		oneRow := func(p page) bool {
			first, last := p.Plates[0], p.Plates[len(p.Plates)-1]
			if first.X > 1 || last.X+last.Width < p.Width-1 {
				return false
			}
			for i, l := range p.Plates[1:] {
				if l.Y != first.Y || l.Height != first.Height || l.X <= p.Plates[i].X ||
					l.Width < first.Width-2 || l.Width > first.Width+2 ||
					l.HeadAt < first.HeadAt-1 || l.HeadAt > first.HeadAt+1 || l.FootAt < first.FootAt-1 || l.FootAt > first.FootAt+1 {
					return false
				}
			}
			return true
		}
		rowOf := func(p page) string {
			var out []string
			for _, l := range p.Plates {
				out = append(out, fmt.Sprintf("%q at %d,%d %dx%d, the word at %d, the last line to %d",
					l.Word+" "+l.Span, l.X, l.Y, l.Width, l.Height, l.HeadAt, l.FootAt))
			}
			return fmt.Sprintf("%s in %dpx", strings.Join(out, "; "), p.Width)
		}

		acme := got.Pages[0]
		if acme.Name != "Acme" || len(acme.Plates) != 3 {
			t.Fatalf("%s: Acme reads %+v, expected claude's two windows and codex's week", at, acme)
		}
		if !oneRow(acme) {
			t.Errorf("%s: Acme's windows stand %s, expected three equal windows in one row", at, rowOf(acme))
		}
		five, week, cx := acme.Plates[0], acme.Plates[1], acme.Plates[2]
		if five.Word != "Claude" || five.Value != "7%" || five.Span != "5h" || five.Sub != "3 h" ||
			five.Title != "Claude · 5 hours: 7%, resets in 3 h" ||
			week.Word != "Claude" || week.Value != "41%" || week.Span != "7d" || week.Sub != "98 h" ||
			week.Title != "Claude · 7 days: 41%, resets in 98 h" || five.Agent != "" || week.Agent != "" {
			t.Errorf("%s: claude's windows of Acme read %+v and %+v, expected them signed Claude, short, "+
				"with the whole in the title", at, five, week)
		}
		if cx.Agent != "codex" || cx.Word != "Codex" || cx.Value != "38%" || cx.FillShare != 38 || cx.Span != "7d" ||
			cx.Sub != "52 h" || cx.Title != "Codex · 7 days: 38%, resets in 52 h" {
			t.Errorf("%s: codex's week of Acme reads %+v, expected it signed Codex with 38%% and its reset in 52 h, "+
				"its week and reset whole in the title", at, cx)
		}
		if cx.FillHue != got.CodexHue || five.FillHue != got.Accent || week.FillHue != got.Accent {
			t.Errorf("%s: codex's bar is %s and claude's %s and %s, expected codex's hue %s and the accent %s",
				at, cx.FillHue, five.FillHue, week.FillHue, got.CodexHue, got.Accent)
		}
		if acme.Dim != "1" || len(acme.Notes) != 0 {
			t.Errorf("%s: fresh numbers of Acme are dimmed (%s) or noted %v", at, acme.Dim, acme.Notes)
		}
		if sky := strings.Join(got.Sky, ", "); sky != fmt.Sprintf("Claude %s, Claude %[1]s, Codex %s", claudeWordSkyHue, codexWordSkyHue) {
			t.Errorf("%s: on the sky the words of Acme's windows read %s, expected the darker pair of the marks", at, sky)
		}

		personal := got.Pages[1]
		if personal.Name != "personal" || len(personal.Plates) != 2 || len(personal.Notes) != 0 {
			t.Fatalf("%s: a contour without codex reads %+v, expected claude's two windows", at, personal)
		}
		if l, r := personal.Plates[0], personal.Plates[1]; l.Word != "Claude" || l.Span != "5h" || l.Sub != "2 h" ||
			l.Title != "Claude · 5 hours: 12%, resets in 2 h" || r.Word != "Claude" || r.Span != "7d" || r.Sub != "100 h" ||
			r.Title != "Claude · 7 days: 24%, resets in 100 h" || !oneRow(personal) {
			t.Errorf("%s: a contour without codex reads %s, %+v and %+v, expected claude's two windows half the row "+
				"each, read as the windows of a row of three", at, rowOf(personal), l, r)
		}

		lab := got.Pages[2]
		if lab.Name != "lab" || lab.None != "" || len(lab.Plates) != 1 {
			t.Fatalf("%s: a contour codex alone spent in reads %+v, expected codex's window alone", at, lab)
		}
		if l := lab.Plates[0]; l.Agent != "codex" || l.Word != "Codex" || l.Sub != "codex has told no weekly window yet" ||
			l.Value != "" || l.Span != "" || l.FillShare != -1 || l.Title != "" {
			t.Errorf("%s: the week codex has not told reads %+v, expected the line that says so under the word Codex", at, l)
		}
		if lab.Dim != "1" || strings.Join(lab.Notes, " | ") != "codex says the limit is reached" {
			t.Errorf("%s: a codex account at its limit reads dimmed %s with %v", at, lab.Dim, lab.Notes)
		}

		globex := got.Pages[3]
		if globex.Name != "globex" || len(globex.Plates) != 3 {
			t.Fatalf("%s: globex reads %+v, expected claude's two windows and codex's week", at, globex)
		}
		if !oneRow(globex) {
			t.Errorf("%s: globex's windows stand %s, expected three equal windows in one row", at, rowOf(globex))
		}
		old := globex.Plates[2]
		if old.Value != "92%" || old.Sub != "102 h" || old.Dim == "1" || globex.Plates[0].Dim != "1" || globex.Dim != "1" {
			t.Errorf("%s: codex's old week beside claude's fresh windows reads %+v, the block dimmed %s and claude's %s; "+
				"expected codex's window alone dimmed", at, old, globex.Dim, globex.Plates[0].Dim)
		}
		if len(globex.Notes) != 2 || globex.Notes[0] != "codex says the limit is reached" ||
			!strings.HasPrefix(globex.Notes[1], "codex's numbers are from 1 h ago") {
			t.Errorf("%s: globex notes %v, expected the limit reached and how old codex's numbers are", at, globex.Notes)
		}

		initech := got.Pages[4]
		if initech.Name != "initech" || len(initech.Plates) != 3 {
			t.Fatalf("%s: initech reads %+v, expected claude's two windows and the week codex has not told", at, initech)
		}
		if !oneRow(initech) {
			t.Errorf("%s: initech's windows stand %s, expected three equal windows in one row, the line of no week "+
				"where the others have their last line", at, rowOf(initech))
		}
		spent, soon, none := initech.Plates[0], initech.Plates[1], initech.Plates[2]
		if spent.Value != "99.5%" || spent.Span != "5h" || spent.Sub != "any moment" ||
			spent.Title != "Claude · 5 hours: 99.5%, resets any moment" ||
			soon.Span != "7d" || soon.Sub != "30 min" || soon.Title != "Claude · 7 days: 64%, resets in 30 min" {
			t.Errorf("%s: claude's windows of initech read %+v and %+v", at, spent, soon)
		}
		if none.Agent != "codex" || none.Word != "Codex" || none.Value != "" || none.Span != "" || none.FillShare != -1 ||
			none.Sub != "no week yet" || none.Title != "codex has told no weekly window yet" {
			t.Errorf("%s: the week codex has not told beside claude's reads %+v, expected it said short under the word "+
				"Codex and whole in the title", at, none)
		}
		if initech.Dim != "1" || len(initech.Notes) != 0 {
			t.Errorf("%s: fresh numbers of initech are dimmed (%s) or noted %v", at, initech.Dim, initech.Notes)
		}
	}
}
