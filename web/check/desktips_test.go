package check

import (
	"regexp"
	"strings"
	"testing"
)

// TestActionButtonsExplainOnlyWhyTheyAreOff holds the tooltips of the desktop
// to the one thing an icon cannot say for itself.
//
// A tip that repeats the icon costs more than it gives: it darkens the row
// under the pointer at the moment the pointer is aiming at a button beside a
// destructive one, and it is read once and never again. What the icon cannot
// say is why it does nothing — that the host is old, or that the session is
// already closing — and that is the only text left on a button.
func TestActionButtonsExplainOnlyWhyTheyAreOff(t *testing.T) {
	// Marks, chips and readings are not buttons: they stand for a state, and
	// the tip is the whole of what they say.
	buttons := regexp.MustCompile(`dkact|dkib[^s]|dkzoombtn|viewbtn|feedjump`)
	at := regexp.MustCompile(`data-tip=`)
	// A text standing where the working button's tip goes — the branch taken
	// when the host does know the action, with the refusal in the other one.
	working := regexp.MustCompile(`\?\s*"[^"]*"\s*:\s*whyNot\(`)

	seen := 0
	for _, path := range sortedKeys(srcFiles(t)) {
		if !strings.HasPrefix(path, "src/desktop/") && !strings.HasPrefix(path, "src/screens/chat/") {
			continue
		}
		body := withoutComments(srcFiles(t)[path])
		for _, m := range at.FindAllStringIndex(body, -1) {
			// The element is written class first, so its classes stand within
			// a few lines above the tip.
			head := body[max(0, m[0]-320):m[0]]
			if !buttons.MatchString(head) {
				continue
			}
			seen++
			value := body[m[1]:min(len(body), m[1]+320)]
			if cut := strings.Index(value, "onClick"); cut > 0 {
				value = value[:cut]
			}
			if !working.MatchString(value) &&
				(strings.Contains(value, "whyNot(") || strings.Contains(value, "undefined")) {
				continue
			}
			t.Errorf("%s:%d: a button carries a tip that shows even when it works: %s — "+
				"the icon says as much, and the tip covers the row the pointer is aiming at",
				path, lineAt(body, m[0]), strings.TrimSpace(firstLine(value)))
		}
	}
	if seen == 0 {
		t.Fatal("no tips on buttons found at all — the test is useless, check the pattern")
	}
}
