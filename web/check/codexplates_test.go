package check

import (
	"strings"
	"testing"
)

// On the phone a contour page opens with its limits: claude's five hours and
// seven days, and beside them codex's week where codex has spent in the
// contour. Every window is signed by the word of its agent, Claude or Codex,
// with the share over the bar, the window and the time to its reset under it;
// the window says it all in its title. A contour without codex has claude's
// two; a contour codex alone spent in has codex's alone rather than the word
// that nobody worked there; an account that told no week, ran into its limit
// or left its numbers old says so, and only the numbers left old are marked
// old.
func TestThePhoneContourPageShowsCodexsWeekBesideClaudesLimits(t *testing.T) {
	type plate struct {
		Value     string `json:"value"`
		Span      string `json:"span"`
		Sub       string `json:"sub"`
		Title     string `json:"title"`
		Agent     string `json:"agent"`
		Word      string `json:"word"`
		WordAgent string `json:"wordAgent"`
		Share     string `json:"share"`
		Old       bool   `json:"old"`
	}
	type page struct {
		Name   string   `json:"name"`
		Stale  bool     `json:"stale"`
		Plates []plate  `json:"plates"`
		Notes  []string `json:"notes"`
		None   string   `json:"none"`
	}
	var got struct {
		Pages []page `json:"pages"`
	}
	runFixture(t, "codexplates.html", &got)
	if len(got.Pages) != 6 {
		t.Fatalf("the fixture read %d contour pages: %+v", len(got.Pages), got.Pages)
	}
	for _, p := range got.Pages {
		for _, l := range p.Plates {
			if (l.Word != "Claude" && l.Word != "Codex") || l.WordAgent != strings.ToLower(l.Word) {
				t.Errorf("%s: the window %q is signed %q by %q, expected Claude or Codex by the mark of its agent",
					p.Name, l.Word+" "+l.Span, l.Word, l.WordAgent)
			}
		}
	}

	acme := got.Pages[0]
	if acme.Name != "Acme" || len(acme.Plates) != 3 {
		t.Fatalf("Acme reads %+v, expected claude's two windows and codex's week", acme)
	}
	five, week, cx := acme.Plates[0], acme.Plates[1], acme.Plates[2]
	if five.Word != "Claude" || five.Value != "7%" || five.Span != "5h" || five.Sub != "3 h" ||
		five.Title != "Claude · 5 hours: 7%, resets in 3 h" ||
		week.Word != "Claude" || week.Value != "41%" || week.Span != "7d" || week.Sub != "98 h" ||
		week.Title != "Claude · 7 days: 41%, resets in 98 h" || five.Agent != "" || week.Agent != "" {
		t.Errorf("claude's windows of Acme read %+v and %+v, expected them signed Claude, short, "+
			"with the whole in the title", five, week)
	}
	if cx.Agent != "codex" || cx.Word != "Codex" || cx.Value != "38%" || cx.Share != "38%" || cx.Span != "7d" ||
		cx.Sub != "52 h" || cx.Title != "Codex · 7 days: 38%, resets in 52 h" {
		t.Errorf("codex's week of Acme reads %+v, expected it signed Codex with 38%% and its reset in 52 h, "+
			"its week and reset whole in the title", cx)
	}
	if acme.Stale || five.Old || cx.Old || len(acme.Notes) != 0 {
		t.Errorf("fresh numbers of Acme are marked old (the block %v, claude's %v, codex's %v) or noted %v",
			acme.Stale, five.Old, cx.Old, acme.Notes)
	}

	personal := got.Pages[1]
	if personal.Name != "personal" || len(personal.Plates) != 2 || len(personal.Notes) != 0 {
		t.Fatalf("a contour without codex reads %+v, expected claude's two windows", personal)
	}
	if l, r := personal.Plates[0], personal.Plates[1]; l.Word != "Claude" || l.Span != "5h" || l.Sub != "2 h" ||
		l.Title != "Claude · 5 hours: 12%, resets in 2 h" || r.Word != "Claude" || r.Span != "7d" || r.Sub != "100 h" ||
		r.Title != "Claude · 7 days: 24%, resets in 100 h" {
		t.Errorf("a contour without codex reads %+v and %+v, expected claude's two windows", l, r)
	}

	lab := got.Pages[2]
	if lab.Name != "lab" || lab.None != "" || len(lab.Plates) != 1 {
		t.Fatalf("a contour codex alone spent in reads %+v, expected codex's window alone", lab)
	}
	if l := lab.Plates[0]; l.Agent != "codex" || l.Word != "Codex" || l.Sub != "codex has told no weekly window yet" ||
		l.Value != "" || l.Span != "" || l.Share != "" || l.Title != "" {
		t.Errorf("the week codex has not told reads %+v, expected the line that says so under the word Codex", l)
	}
	if lab.Stale || lab.Plates[0].Old || strings.Join(lab.Notes, " | ") != "codex says the limit is reached" {
		t.Errorf("a codex account at its limit reads marked old (the block %v, codex's %v) with %v",
			lab.Stale, lab.Plates[0].Old, lab.Notes)
	}

	globex := got.Pages[3]
	if globex.Name != "globex" || len(globex.Plates) != 3 {
		t.Fatalf("globex reads %+v, expected claude's two windows and codex's week", globex)
	}
	if old := globex.Plates[2]; old.Value != "92%" || old.Sub != "102 h" || !old.Old || globex.Plates[0].Old || globex.Stale {
		t.Errorf("codex's old week beside claude's fresh windows reads %+v, the block marked old %v and claude's %v; "+
			"expected codex's window alone marked old", old, globex.Stale, globex.Plates[0].Old)
	}
	if len(globex.Notes) != 2 || globex.Notes[0] != "codex says the limit is reached" ||
		!strings.HasPrefix(globex.Notes[1], "codex's numbers are from 1 h ago") {
		t.Errorf("globex notes %v, expected the limit reached and how old codex's numbers are", globex.Notes)
	}

	initech := got.Pages[4]
	if initech.Name != "initech" || len(initech.Plates) != 3 {
		t.Fatalf("initech reads %+v, expected claude's two windows and the week codex has not told", initech)
	}
	spent, soon, none := initech.Plates[0], initech.Plates[1], initech.Plates[2]
	if spent.Value != "99.5%" || spent.Span != "5h" || spent.Sub != "any moment" ||
		spent.Title != "Claude · 5 hours: 99.5%, resets any moment" ||
		soon.Span != "7d" || soon.Sub != "30 min" || soon.Title != "Claude · 7 days: 64%, resets in 30 min" {
		t.Errorf("claude's windows of initech read %+v and %+v", spent, soon)
	}
	if none.Agent != "codex" || none.Word != "Codex" || none.Value != "" || none.Span != "" || none.Share != "" ||
		none.Sub != "no week yet" || none.Title != "codex has told no weekly window yet" {
		t.Errorf("the week codex has not told beside claude's reads %+v, expected it said short under the word "+
			"Codex and whole in the title", none)
	}
	if initech.Stale || spent.Old || none.Old || len(initech.Notes) != 0 {
		t.Errorf("fresh numbers of initech are marked old (the block %v, claude's %v, codex's %v) or noted %v",
			initech.Stale, spent.Old, none.Old, initech.Notes)
	}

	hooli := got.Pages[5]
	if hooli.Name != "hooli" || len(hooli.Plates) != 2 {
		t.Fatalf("an account with no five-hour window reads %+v, expected claude's week and codex's", hooli)
	}
	if l, r := hooli.Plates[0], hooli.Plates[1]; l.Word != "Claude" || l.Span != "7d" || l.Value != "10%" ||
		r.Word != "Codex" || r.Span != "7d" || r.Value != "72%" {
		t.Errorf("hooli's windows read %+v and %+v, expected claude's week, then codex's", l, r)
	}
}
