package check

import (
	"regexp"
	"strings"
	"testing"
)

// The home session of the host is the one the panel itself lives next to: the
// panel does not close it, just as it does not stop its own container. The phone
// card leaves the session without a close button at all; the desktop row keeps
// the same rule — not a disabled button with a hint, no button.
func TestDesktopHidesCloseForTheHomeSession(t *testing.T) {
	files := srcFiles(t)

	const phone = "src/screens/sessions/card.js"
	card := stripComments(files[phone])
	if card == "" {
		t.Fatalf("%s not found — the phone rule the desktop mirrors is left unchecked", phone)
	}
	action := jsUntil(t, phone, card, "const action =", `"session.close"`)
	if !regexp.MustCompile(`!session\.home\s*&&`).MatchString(action) {
		t.Errorf("%s: the phone card offers to close the home session — the panel does not close the main session of the host, just as it does not stop its own container", phone)
	}

	const desk = "src/desktop/sessions.js"
	src := stripComments(files[desk])
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", desk)
	}
	row := funcBody(t, src, "function SessionLine(")
	acts := jsUntil(t, desk, row, `<span class="dkrowacts"`, "</span>")
	if !strings.Contains(acts, `"session.close"`) {
		t.Fatalf("%s: the row actions slot holds no close action — the test guards the wrong place", desk)
	}
	gate := regexp.MustCompile(`!s\.home\s*&&`).FindStringIndex(acts)
	if gate == nil {
		t.Fatalf("%s: the desktop row offers to close the home session — the panel does not close the main session of the host, just as it does not stop its own container", desk)
	}
	if icon := strings.Index(acts, "<i"); icon < 0 || icon < gate[0] {
		t.Errorf("%s: the home check comes after the close icon — the button is still drawn, only its handler is gated", desk)
	}
}
