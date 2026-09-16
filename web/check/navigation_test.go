package check

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEveryMenuSectionOpensSomething(t *testing.T) {
	files := srcFiles(t)
	nav := files["src/ui/nav.js"]
	app := files["src/mobile/shell.js"]
	if nav == "" || app == "" {
		t.Fatal("src/ui/nav.js or src/mobile/shell.js not found")
	}

	ids := func(name string) []string {
		block := regexp.MustCompile(`(?s)export const ` + name + ` = \[(.*?)\];`).FindStringSubmatch(nav)
		if block == nil {
			t.Fatalf("nav.js has no %s list", name)
		}
		found := regexp.MustCompile(`id: "([a-z]+)"`).FindAllStringSubmatch(block[1], -1)
		if len(found) == 0 {
			t.Fatalf("the %s list is empty — a menu without items", name)
		}
		out := make([]string, 0, len(found))
		for _, m := range found {
			out = append(out, m[1])
		}
		return out
	}

	screen := app[strings.Index(app, "function Screen("):]
	for _, id := range ids("TABS") {
		if strings.Contains(screen, `tab === "`+id+`"`) {
			continue
		}
		if strings.Contains(screen, `<${Containers}`) && id == "containers" {
			continue
		}
		t.Errorf("tab %q is in the menu, but Screen does not know it — the button lights up and opens nothing", id)
	}

	for _, id := range ids("PAGES") {
		if !strings.Contains(app, `page === "`+id+`"`) {
			t.Errorf("section %q is in the menu, but app.js has no page === %q branch — the button opens nothing", id, id)
		}
	}
}

func TestEveryDesktopViewOpensSomething(t *testing.T) {
	files := srcFiles(t)
	shell := files["src/desktop/shell.js"]
	if shell == "" {
		t.Fatal("src/desktop/shell.js not found")
	}

	list := func(name, pattern string) []string {
		block := regexp.MustCompile(`(?s)` + name + ` = \[(.*?)\];`).FindStringSubmatch(shell)
		if block == nil {
			t.Fatalf("desktop/shell.js has no %s list", name)
		}
		found := regexp.MustCompile(pattern).FindAllStringSubmatch(block[1], -1)
		if len(found) == 0 {
			t.Fatalf("the %s list is empty", name)
		}
		out := make([]string, 0, len(found))
		for _, m := range found {
			out = append(out, m[1])
		}
		return out
	}

	center := shell[strings.Index(shell, "const center = "):]
	opens := func(id string) bool {
		if strings.Contains(center, `section === "`+id+`"`) {
			return true
		}
		return id == "sessions" && strings.Contains(center, "<${Chat}")
	}

	ids := append(list("export const SECTIONS", `id: "([a-z]+)"`), "home")
	for _, id := range ids {
		if !opens(id) {
			t.Errorf("section %q is in the header, but the center has no branch for it — the button lights up and opens nothing", id)
		}
	}
	if !strings.Contains(shell, "goSection(it.id)") {
		t.Error("the section buttons do not switch the center")
	}

	panels := srcFiles(t)["src/desktop/panels.js"]
	if panels == "" {
		t.Fatal("src/desktop/panels.js not found")
	}
	for _, id := range regexp.MustCompile(`id: "([a-z]+)", label:`).FindAllStringSubmatch(panels, -1) {
		if !strings.Contains(panels, `tab === "`+id[1]+`"`) {
			t.Errorf("pane %q is declared, but there is no branch for it", id[1])
		}
	}
}

func TestEveryBackCrumbHasBackHandler(t *testing.T) {
	files := srcFiles(t)
	app := files["src/mobile/shell.js"]
	if app == "" || !strings.Contains(app, "useBackClose(") {
		t.Fatal("src/mobile/shell.js has no useBackClose — the page-based screens are left without a back gesture")
	}

	for path, body := range files {
		clean := withoutComments(body)
		crumbs := strings.Count(clean, "chev back") + strings.Count(clean, "<${BackHead}")
		if crumbs == 0 {
			continue
		}
		if strings.HasSuffix(path, "ui/back.js") {
			continue
		}
		covered := strings.Count(body, "useBackClose(")
		name := strings.TrimSuffix(filepath.Base(path), ".js")
		if strings.Contains(app, `page === "`+name+`"`) {
			covered++
		}
		// A document opened on a page of its own shelf — a brief on the shelf of
		// briefs — is covered by that page and must not claim the gesture
		// itself. Two claims made in one turn race each other over the history,
		// and the swipe takes off the page instead of the document on it.
		open := "open" + strings.ToUpper(name[:1]) + name[1:]
		if strings.Contains(app, `page === "`+name+`s"`) && strings.Contains(app, open) {
			covered++
		}
		if covered >= crumbs {
			continue
		}
		t.Errorf("%s: %d layers with a back crumb against %d gesture handlers — a swipe on an uncovered "+
			"one closes the whole app", path, crumbs, covered)
	}
}

func TestSheetTapOnControlIsNotAGesture(t *testing.T) {
	const sheetFile = "src/ui/sheet.js"
	body := srcFiles(t)[sheetFile]
	if body == "" {
		t.Fatalf("%s not found — the test guards the wrong place", sheetFile)
	}
	down := jsBlock(t, sheetFile, body, "const onDown =")
	if !strings.Contains(down, "onControl") || !strings.Contains(down, "button, a, input") {
		t.Errorf("%s: the start of the gesture does not remember whether the finger landed on a control — "+
			"by the end of the gesture there is no way to tell, the pointer is captured by the sheet", sheetFile)
	}
	up := jsBlock(t, sheetFile, body, "const onUp =")
	if !strings.Contains(up, "!state.onControl") {
		t.Errorf("%s: a tap on a control switches the sheet height again — "+
			"pressing Back in a question collapses it instead of answering", sheetFile)
	}
}

func TestClosedSheetIsOutOfReach(t *testing.T) {
	closed := cssBlockFile(t, "src/css/sheet.css", ".sheet:not(.on)")
	if !strings.Contains(closed, "pointer-events: none") {
		t.Error("a closed sheet still catches the finger: the shift takes it past the edge, but it keeps " +
			"accepting taps — and there are five of them, so any one that sticks out becomes a trap")
	}
	if strings.Contains(cssSrc(t), ".sheet.on { pointer-events") {
		t.Error("the pointer of an open sheet is set by a separate rule — those two will diverge")
	}
	if !strings.Contains(cssBlockFile(t, "src/css/sheet.css", ".sheet.draggable:not(.on)"), "translateY") {
		t.Error("a closed sheet no longer slides down — it stays visible")
	}
}

func TestMobileThemeButtonCallsTheHandler(t *testing.T) {
	body := withoutComments(srcFiles(t)["src/mobile/shell.js"])
	if strings.Contains(body, "setTheme(") {
		t.Error("src/mobile/shell.js: calls setTheme, which this module does not have — the theme button throws")
	}
	if !strings.Contains(body, "onTheme=${onTheme}") {
		t.Error("src/mobile/shell.js: the header does not get onTheme from the props")
	}
}
