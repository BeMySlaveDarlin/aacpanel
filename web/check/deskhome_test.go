package check

import (
	"regexp"
	"strings"
	"testing"
)

func TestDesktopStartsOnHome(t *testing.T) {
	files := srcFiles(t)
	shell := stripComments(files["src/desktop/shell.js"])
	if shell == "" {
		t.Fatal("src/desktop/shell.js not found — the test looks in the wrong place")
	}

	start := regexp.MustCompile(`\[\s*section\s*,\s*setSection\s*\]\s*=\s*useState\(([^)]*)\)`).FindStringSubmatch(shell)
	if start == nil {
		t.Fatal("the desktop shell has no section state — the test looks in the wrong place")
	}
	switch got := strings.TrimSpace(start[1]); {
	case got == `"home"`:
	case strings.HasPrefix(got, `"`):
		t.Errorf("on a wide screen the panel opens on section %s instead of home", got)
	default:
		t.Errorf("the starting section is computed (%s) — \"home opens on startup\" stops "+
			"being true from the second visit on", got)
	}

	at := strings.Index(shell, `section === "home"`)
	if at < 0 {
		t.Fatal("the center has no home branch — the starting section opens into emptiness")
	}
	if tail := shell[at:min(len(shell), at+200)]; !strings.Contains(tail, "<${Home}") {
		t.Error("the home branch opens something other than home — on startup a person sees not what makes it the starting one")
	}

	wide := regexp.MustCompile(`const wide = ([^;]*);`).FindStringSubmatch(shell)
	if wide == nil {
		t.Fatal("the desktop shell has no full-width section flag — the test looks in the wrong place")
	}
	if !strings.Contains(wide[1], `section === "home"`) {
		t.Error("home does not take the full width — a column of the neighbouring section stays next to its widgets")
	}

	mobile := stripComments(files["src/mobile/shell.js"])
	if mobile == "" {
		t.Fatal("src/mobile/shell.js not found — the boundary with the phone is left unchecked")
	}
	tab := regexp.MustCompile(`\[\s*tab\s*,\s*setTab\s*\]\s*=\s*useState\(([^)]*)\)`).FindStringSubmatch(mobile)
	if tab == nil {
		t.Fatal("the mobile shell has no tab state — the test looks in the wrong place")
	}
	if strings.Contains(tab[1], `"home"`) {
		t.Error("the phone now starts on the desktop home — the mobile layout has a starting screen of its own")
	}
}
