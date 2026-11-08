package check

import (
	"regexp"
	"strings"
	"testing"
)

// The home session of the host is the one the panel itself lives next to: the
// panel does not close it, just as it does not stop its own container. At a
// desk a live card carries no controls; its menu is where the close lives,
// and the menu leaves the item out entirely for the home session, rather than
// dimming it.
const (
	cardFile     = "src/screens/sessions/sessioncard.js"
	controlsFile = "src/screens/sessions/controls.js"
	menuFile     = "src/desktop/sessionmenu.js"
)

func TestDesktopHidesCloseForTheHomeSession(t *testing.T) {
	files := srcFiles(t)
	src := stripComments(files[menuFile])
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", menuFile)
	}
	menu := funcBody(t, src, "function ClaudeMenu(")
	closeItem := strings.Index(menu, `key="close"`)
	if closeItem < 0 || !strings.Contains(menu, `run("session.close"`) {
		t.Fatalf("%s: the menu of a session holds no close — the test guards the wrong place", menuFile)
	}
	gate := regexp.MustCompile(`!s\.home\s*&&`).FindStringIndex(menu)
	if gate == nil {
		t.Fatalf("%s: the menu offers to close the home session — the panel does not close the main session of the host, just as it does not stop its own container", menuFile)
	}
	if gate[0] > closeItem {
		t.Errorf("%s: the home check comes after the close item — the item is still drawn, only its handler is gated", menuFile)
	}
}

// The home session gets the one action the panel does have for it: a restart
// from scratch. The menu of every claude session offers it through the host
// check; the home one offers no close.
func TestHomeSessionRestartsFromScratchOnTheDesktop(t *testing.T) {
	files := srcFiles(t)
	src := stripComments(files[menuFile])
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", menuFile)
	}
	menu := funcBody(t, src, "function ClaudeMenu(")
	restart := strings.Index(menu, `key="restart"`)
	if restart < 0 {
		t.Fatalf("%s: the menu of a session has no restart", menuFile)
	}
	item := menu[restart:]
	if end := strings.Index(item, "/>"); end >= 0 {
		item = item[:end]
	}
	if !strings.Contains(item, `run("session.restart"`) {
		t.Errorf("%s: the restart of a session does not restart", menuFile)
	}
	if strings.Contains(item, "!s.home") || strings.Contains(item, `"session.close"`) {
		t.Errorf("%s: the restart of a session is gated by the home check or closes it", menuFile)
	}
	if !strings.Contains(menu, "endsOf(") ||
		!strings.Contains(funcBody(t, src, "function endsOf("), `knows(exec, "session.restart")`) {
		t.Errorf("%s: the restart item does not ask whether the host can do it", menuFile)
	}

	block := after(files[registryFile], `"session.restart":`)
	if block == "" {
		t.Fatalf("%s: the registry has no session.restart — the menu would press an unknown action", registryFile)
	}
	for _, want := range []string{"danger: true", `ok: "Restart"`, "choice: restartChoice"} {
		if !strings.Contains(block, want) {
			t.Errorf("%s: session.restart does not say %q — the sheet has to name what the person loses and what stays", registryFile, want)
		}
	}
}
