package check

import (
	"strings"
	"testing"
)

// TestAlertsBadgeOpensTheAlertsScreen makes sure the header bell, and the count
// on it, lead to the one screen where alerts are actually drawn, fed from the
// same state the count itself came from — a second fetch there would drift
// from the number on the bell the moment an alert closes between the two.
func TestAlertsBadgeOpensTheAlertsScreen(t *testing.T) {
	files := srcFiles(t)
	shell := files["src/desktop/shell.js"]
	if shell == "" {
		t.Fatal("src/desktop/shell.js not found")
	}
	clean := stripComments(shell)

	if !strings.Contains(clean, `import { Alerts } from "../screens/alerts.js";`) {
		t.Fatal("src/desktop/shell.js does not import the alerts screen — the test guards the wrong place")
	}

	at := strings.Index(clean, "<${AlertsButton}")
	if at < 0 {
		t.Fatal("there is no AlertsButton in the header — the bell is gone")
	}
	head := clean[at:min(len(clean), at+200)]
	if !strings.Contains(head, `onClick=${() => goSection("alerts")}`) {
		t.Error(`the alerts bell does not call goSection("alerts") — a press lands somewhere other than the alerts screen`)
	}

	center := clean[strings.Index(clean, "const center = "):]
	branch := strings.Index(center, `section === "alerts"`)
	if branch < 0 {
		t.Fatal(`the center has no branch for section === "alerts" — goSection("alerts") opens nothing`)
	}
	body := center[branch:min(len(center), branch+300)]
	if !strings.Contains(body, "<${Alerts}") {
		t.Error(`section "alerts" does not render the Alerts screen — the bell opens an empty page`)
	}
	if !strings.Contains(body, "alerts=${alerts}") {
		t.Error("the alerts screen is not given the same alerts state the bell counted — the two can end up showing different numbers")
	}

	if strings.Contains(clean, "useAlerts(") {
		t.Error("src/desktop/shell.js fetches alerts on its own — a second source drifts from the count already computed once, in app.js, for the bell")
	}
}

// TestAlertsButtonNeedsNoOpenAlertToBePressed makes sure the bell itself is
// always in the header: the badge on it is only how it says something is
// waiting, and hiding the whole button along with the badge would leave no way
// to the screen once the last open alert closes.
func TestAlertsButtonNeedsNoOpenAlertToBePressed(t *testing.T) {
	shell := stripComments(srcFiles(t)["src/desktop/shell.js"])
	if shell == "" {
		t.Fatal("src/desktop/shell.js not found")
	}

	at := strings.Index(shell, "<${AlertsButton}")
	if at < 0 {
		t.Fatal("there is no AlertsButton in the header — the bell is gone")
	}
	before := shell[max(0, at-120):at]
	if strings.Contains(before, "openAlerts > 0 &&") || strings.Contains(before, "openAlerts &&") {
		t.Error("the alerts button is still wrapped in a check for an open alert — at zero it disappears " +
			"along with the only door to the screen")
	}
}
