package check

import (
	"strings"
	"testing"
)

// The bottom menu is where a screen lives when it is opened often. Briefs are
// read and answered every day; the journal is opened when something went wrong,
// which is rare, so it moves under the logo and briefs take its column.
func TestBriefsSitInTheBottomMenuAndTheJournalUnderTheLogo(t *testing.T) {
	files := srcFiles(t)
	nav := stripComments(files["src/ui/nav.js"])
	if nav == "" {
		t.Fatal("src/ui/nav.js not found — the test looks in the wrong place")
	}
	if !strings.Contains(nav, `id: "briefs"`) {
		t.Error("briefs are not in the bottom menu: they are reached only through the sheet under the logo")
	}
	if strings.Contains(nav, `id: "journal"`) {
		t.Error("the journal is still in the bottom menu, which has five columns and all of them are taken")
	}

	sheet := shellSheet(t, files)
	if !strings.Contains(sheet, `onPage("journal")`) {
		t.Error("the journal left the bottom menu and got no entry in the sheet: there is no way into it at all")
	}
	if strings.Contains(sheet, `onPage("briefs")`) {
		t.Error("briefs are in the bottom menu and in the sheet at once: one screen, two doors")
	}
}

// A screen out of the bottom menu carries its own way back: the navigation bar
// that used to hold it is not drawn over it any more.
func TestTheJournalCarriesItsOwnWayBack(t *testing.T) {
	files := srcFiles(t)
	journal := stripComments(files["src/screens/journal.js"])
	if journal == "" {
		t.Fatal("src/screens/journal.js not found — the test looks in the wrong place")
	}
	if !strings.Contains(journal, "BackHead") {
		t.Error("the journal has no head with a back button, and nothing else on the screen leaves it")
	}

	shell := stripComments(files["src/mobile/shell.js"])
	at := strings.Index(shell, `page === "journal"`)
	if at < 0 {
		t.Fatal("the shell has no page branch for the journal")
	}
	if tail := shell[at:min(len(shell), at+200)]; !strings.Contains(tail, "onBack") {
		t.Error("the journal is opened without a way to close it: the branch passes no onBack")
	}
}
