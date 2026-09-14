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

// The home session gets the one action the panel does have for it: a restart
// from scratch. Both shells offer it where the other sessions have their close,
// with the same icon and the same host check, and to the home session only.
func TestHomeSessionRestartsFromScratchOnBothShells(t *testing.T) {
	files := srcFiles(t)
	homeOnly := regexp.MustCompile(`[^!]s(?:ession)?\.home\s*&&`)

	const phone = "src/screens/sessions/card.js"
	card := stripComments(files[phone])
	if card == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", phone)
	}
	restart := jsUntil(t, phone, card, "const restart =", "`;")
	if !homeOnly.MatchString(restart) {
		t.Errorf("%s: the restart button is not gated on the home session — every session would get a restart it cannot have", phone)
	}
	for _, want := range []string{`"session.restart"`, "Icon.refresh"} {
		if !strings.Contains(restart, want) {
			t.Errorf("%s: the home card's restart has no %s", phone, want)
		}
	}
	if strings.Contains(restart, `"session.close"`) {
		t.Errorf("%s: the home card offers to close the home session under the restart button", phone)
	}
	if !strings.Contains(card, `knows(exec, "session.restart")`) {
		t.Errorf("%s: the restart button does not ask whether the host can do it — on a host with an old executor it silently does nothing", phone)
	}

	const desk = "src/desktop/sessions.js"
	src := stripComments(files[desk])
	if src == "" {
		t.Fatalf("%s not found — the test looks in the wrong place", desk)
	}
	row := funcBody(t, src, "function SessionLine(")
	acts := jsUntil(t, desk, row, `<span class="dkrowacts"`, "</span>")
	gate := homeOnly.FindStringIndex(acts)
	if gate == nil {
		t.Fatalf("%s: the row actions slot has no block for the home session — there is nothing to restart it with", desk)
	}
	home := acts[gate[0]:]
	for _, want := range []string{`"session.restart"`, "Icon.refresh"} {
		if !strings.Contains(home, want) {
			t.Errorf("%s: the home row's block has no %s", desk, want)
		}
	}
	if strings.Contains(home, `"session.close"`) {
		t.Errorf("%s: the home row offers to close the home session inside its own block", desk)
	}
	if !strings.Contains(row, `knows(exec, "session.restart")`) {
		t.Errorf("%s: the restart icon does not ask whether the host can do it", desk)
	}

	block := after(files[registryFile], `"session.restart":`)
	if block == "" {
		t.Fatalf("%s: the registry has no session.restart — the buttons would press an unknown action", registryFile)
	}
	for _, want := range []string{"danger: true", `ok: "Restart"`, "from scratch?", "empty context", "transcript"} {
		if !strings.Contains(block, want) {
			t.Errorf("%s: session.restart does not say %q — the sheet has to name what the person loses and what stays", registryFile, want)
		}
	}
}
