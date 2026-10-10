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
// where codex has spent in the contour, signed Codex in codex's hue, its bar
// in that hue as its ring at a desk is. Three to a row, the windows are equal
// and short: the time to a reset without the word, codex's week by the word
// Codex alone, the line of no week in three words, and each window says it
// all in its title. A contour without codex keeps claude's two as they were;
// a contour codex alone spent in has codex's alone rather than the word that
// nobody worked there; an account that told no week, ran into its limit or
// left its numbers old says so. On the narrowest phone and on a common one no
// word is cut and no window runs into another.
func TestThePhoneContourPageShowsCodexsWeekBesideClaudesLimits(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type plate struct {
		Name      string `json:"name"`
		Value     string `json:"value"`
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
			Width    int    `json:"width"`
			CodexHue string `json:"codexHue"`
			Accent   string `json:"accent"`
			Pages    []page `json:"pages"`
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
				if !l.Inside {
					t.Errorf("%s, %s: the window %q stands out of the limits", at, p.Name, l.Name)
				}
				// A window reads its caption, share and reset on a line each; the
				// line of no week has no share.
				for i, n := range l.Lines {
					if n > 1 {
						t.Errorf("%s, %s: a line of the window %q breaks in %d (%v)", at, p.Name, l.Name, n, l.Lines)
						break
					}
					if n == 0 && !(i == 1 && l.Value == "") {
						t.Errorf("%s, %s: a line of the window %q is missing (%v)", at, p.Name, l.Name, l.Lines)
						break
					}
				}
			}
		}
		// oneRow says whether the windows of a page fill one row from left to
		// right, as tall and as wide as one another.
		oneRow := func(p page) bool {
			first, last := p.Plates[0], p.Plates[len(p.Plates)-1]
			if first.X > 1 || last.X+last.Width < p.Width-1 {
				return false
			}
			for i, l := range p.Plates[1:] {
				if l.Y != first.Y || l.Height != first.Height || l.X <= p.Plates[i].X ||
					l.Width < first.Width-2 || l.Width > first.Width+2 {
					return false
				}
			}
			return true
		}
		rowOf := func(p page) string {
			var out []string
			for _, l := range p.Plates {
				out = append(out, fmt.Sprintf("%q at %d,%d %dx%d", l.Name, l.X, l.Y, l.Width, l.Height))
			}
			return fmt.Sprintf("%s in %dpx", strings.Join(out, ", "), p.Width)
		}

		acme := got.Pages[0]
		if acme.Name != "Acme" || len(acme.Plates) != 3 {
			t.Fatalf("%s: Acme reads %+v, expected claude's two windows and codex's week", at, acme)
		}
		if !oneRow(acme) {
			t.Errorf("%s: Acme's windows stand %s, expected three equal windows in one row", at, rowOf(acme))
		}
		five, week, cx := acme.Plates[0], acme.Plates[1], acme.Plates[2]
		if five.Name != "5 hours" || five.Value != "7%" || five.Sub != "in 3 h" || five.Title != "5 hours: 7%, resets in 3 h" ||
			week.Name != "7 days" || week.Value != "41%" || week.Sub != "in 98 h" || week.Title != "7 days: 41%, resets in 98 h" ||
			five.Agent != "" || week.Agent != "" {
			t.Errorf("%s: claude's windows of Acme read %+v and %+v, expected them short with the whole in the title", at, five, week)
		}
		if cx.Agent != "codex" || cx.Word != "Codex" || cx.WordAgent != "codex" || cx.Name != "Codex" ||
			cx.Value != "38%" || cx.FillShare != 38 || cx.Sub != "in 52 h" || cx.Title != "Codex · 7 days: 38%, resets in 52 h" {
			t.Errorf("%s: codex's week of Acme reads %+v, expected it signed Codex with 38%% and its reset in 52 h, "+
				"its week and reset whole in the title", at, cx)
		}
		if cx.WordHue != got.CodexHue || cx.FillHue != got.CodexHue || cx.FillHue == five.FillHue {
			t.Errorf("%s: the word Codex is %s and its bar %s, expected codex's hue %s apart from claude's bar %s",
				at, cx.WordHue, cx.FillHue, got.CodexHue, five.FillHue)
		}
		if acme.Dim != "1" || len(acme.Notes) != 0 {
			t.Errorf("%s: fresh numbers of Acme are dimmed (%s) or noted %v", at, acme.Dim, acme.Notes)
		}

		personal := got.Pages[1]
		if personal.Name != "personal" || len(personal.Plates) != 2 || len(personal.Notes) != 0 {
			t.Fatalf("%s: a contour without codex reads %+v, expected claude's two windows as they were", at, personal)
		}
		if l, r := personal.Plates[0], personal.Plates[1]; l.Name != "5 hours" || l.Sub != "resets in 2 h" || l.Title != "" ||
			r.Name != "7 days" || r.Sub != "resets in 100 h" || r.Title != "" || !oneRow(personal) {
			t.Errorf("%s: a contour without codex reads %s, %+v and %+v, expected claude's two windows as they were, "+
				"each half the row with its reset in full", at, rowOf(personal), l, r)
		}

		lab := got.Pages[2]
		if lab.Name != "lab" || lab.None != "" || len(lab.Plates) != 1 {
			t.Fatalf("%s: a contour codex alone spent in reads %+v, expected codex's window alone", at, lab)
		}
		if l := lab.Plates[0]; l.Agent != "codex" || l.Word != "Codex" || l.Sub != "codex has told no weekly window yet" ||
			l.Value != "" || l.FillShare != -1 || l.Title != "" {
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
		if old.Value != "92%" || old.Sub != "in 102 h" || old.Dim == "1" || globex.Plates[0].Dim != "1" || globex.Dim != "1" {
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
			t.Errorf("%s: initech's windows stand %s, expected three equal windows in one row", at, rowOf(initech))
		}
		spent, soon, none := initech.Plates[0], initech.Plates[1], initech.Plates[2]
		if spent.Value != "99.5%" || spent.Sub != "any moment" || spent.Title != "5 hours: 99.5%, resets any moment" ||
			soon.Sub != "in 30 min" || soon.Title != "7 days: 64%, resets in 30 min" {
			t.Errorf("%s: claude's windows of initech read %+v and %+v", at, spent, soon)
		}
		if none.Agent != "codex" || none.Word != "Codex" || none.Value != "" || none.FillShare != -1 ||
			none.Sub != "no week yet" || none.Title != "codex has told no weekly window yet" {
			t.Errorf("%s: the week codex has not told beside claude's reads %+v, expected it said short under the word "+
				"Codex and whole in the title", at, none)
		}
		if initech.Dim != "1" || len(initech.Notes) != 0 {
			t.Errorf("%s: fresh numbers of initech are dimmed (%s) or noted %v", at, initech.Dim, initech.Notes)
		}
	}
}
