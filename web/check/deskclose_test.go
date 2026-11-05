package check

import (
	"regexp"
	"strings"
	"testing"
)

// The home session of the host is the one the panel itself lives next to: the
// panel does not close it, just as it does not stop its own container. The
// card — the one card of the phone and the desk — leaves the action out
// entirely, rather than disabling it.
const cardFile = "src/screens/sessions/sessioncard.js"

func TestDesktopHidesCloseForTheHomeSession(t *testing.T) {
	files := srcFiles(t)
	src := stripComments(files[cardFile])
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", cardFile)
	}
	acts := funcBody(t, src, "function Controls(")
	if !strings.Contains(acts, `"session.close"`) {
		t.Fatalf("%s: the controls of a card hold no close action — the test guards the wrong place", cardFile)
	}
	gate := regexp.MustCompile(`!s\.home\s*&&`).FindStringIndex(acts)
	if gate == nil {
		t.Fatalf("%s: the card offers to close the home session — the panel does not close the main session of the host, just as it does not stop its own container", cardFile)
	}
	if icon := strings.Index(acts, `label=${`+"`"+`close session`); icon < 0 || icon < gate[0] {
		t.Errorf("%s: the home check comes after the close control — the button is still drawn, only its handler is gated", cardFile)
	}
}

// The home session gets the one action the panel does have for it: a restart
// from scratch. Every card of claude offers that action through the host
// check; the home card offers no close.
func TestHomeSessionRestartsFromScratchOnTheDesktop(t *testing.T) {
	files := srcFiles(t)
	src := stripComments(files[cardFile])
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", cardFile)
	}
	acts := funcBody(t, src, "function Controls(")
	restart := strings.Index(acts, "restart session")
	if restart < 0 {
		t.Fatalf("%s: the controls of a card have no restart", cardFile)
	}
	start := strings.LastIndex(acts[:restart], "<${Act}")
	end := strings.Index(acts[restart:], "Icon.refresh()")
	if start < 0 || end < 0 {
		t.Fatalf("%s: the restart of a card is not one control with its icon", cardFile)
	}
	home := acts[start : restart+end+len("Icon.refresh()")]
	for _, want := range []string{`"session.restart"`, "Icon.refresh"} {
		if !strings.Contains(home, want) {
			t.Errorf("%s: the restart of a card has no %s", cardFile, want)
		}
	}
	if strings.Contains(home, `"session.close"`) {
		t.Errorf("%s: the restart of a card closes the session inside its own block", cardFile)
	}
	if !strings.Contains(acts, `knows(exec, "session.restart")`) {
		t.Errorf("%s: the restart control does not ask whether the host can do it", cardFile)
	}

	block := after(files[registryFile], `"session.restart":`)
	if block == "" {
		t.Fatalf("%s: the registry has no session.restart — the buttons would press an unknown action", registryFile)
	}
	for _, want := range []string{"danger: true", `ok: "Restart"`, "choice: restartChoice"} {
		if !strings.Contains(block, want) {
			t.Errorf("%s: session.restart does not say %q — the sheet has to name what the person loses and what stays", registryFile, want)
		}
	}
}
