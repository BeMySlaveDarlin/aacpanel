package check

import (
	"strings"
	"testing"
)

// The bottom menu is where a screen lives when it is opened often: the
// terminals stand there, and usage, briefs and the journal are under the
// logo — one door each, and what the sheet opens carries its own way back.
func TestTerminalsSitInTheBottomMenuAndUsageUnderTheLogo(t *testing.T) {
	files := srcFiles(t)
	nav := stripComments(files["src/ui/nav.js"])
	if nav == "" {
		t.Fatal("src/ui/nav.js not found — the test looks in the wrong place")
	}
	if !strings.Contains(nav, `id: "terminals"`) {
		t.Error("the terminals are not in the bottom menu")
	}
	for _, gone := range []string{`id: "usage"`, `id: "briefs"`, `id: "journal"`} {
		if strings.Contains(nav, gone) {
			t.Errorf("%s is in the bottom menu, which has five columns and all of them are taken", gone)
		}
	}

	sheet := shellSheet(t, files)
	for _, page := range []string{"usage", "briefs", "journal"} {
		if !strings.Contains(sheet, `onPage("`+page+`")`) {
			t.Errorf("%s has no entry in the sheet under the logo: there is no way into it at all", page)
		}
	}
	if first := strings.Index(sheet, `onPage(`); first < 0 || !strings.HasPrefix(sheet[first:], `onPage("usage")`) {
		t.Error("usage does not lead the sheet under the logo: it came from the bottom menu, and it is looked for first")
	}

	shell := stripComments(files["src/mobile/shell.js"])
	at := strings.Index(shell, "<${Briefs}")
	if at < 0 {
		t.Fatal("the shell has no page branch that draws the briefs")
	}
	if tail := shell[at:min(len(shell), at+200)]; !strings.Contains(tail, "onBack") {
		t.Error("the briefs are opened from the sheet without a way to close them: the navigation bar is not drawn over them")
	}
	if !strings.Contains(stripComments(files["src/screens/briefs.js"]), "BackHead") {
		t.Error("the briefs have no head with a back button for when they are opened out of the bottom menu")
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
