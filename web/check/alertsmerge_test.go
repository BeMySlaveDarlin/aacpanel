package check

import (
	"strings"
	"testing"
)

// The first page of alerts is reloaded while the older pages stay. An alert
// loaded below the cursor moves back to the top once it gets a new event, so
// the two lists overlap, and the screen must draw each alert once.
func TestAlertsScreenDrawsEachAlertOnce(t *testing.T) {
	src := withoutComments(srcFiles(t)["src/screens/alerts.js"])
	if !strings.Contains(src, "merged(state.alerts, state.older).map(") {
		t.Fatal("src/screens/alerts.js: the first page and the older ones are drawn without a merge — an alert that came back to the top is drawn twice")
	}
	start := strings.Index(src, "function merged(")
	if start < 0 {
		t.Fatal("src/screens/alerts.js: there is no merged() to check")
	}
	body := src[start:]
	if end := strings.Index(body, "\n}"); end >= 0 {
		body = body[:end]
	}
	if !strings.Contains(body, ".filter(") || !strings.Contains(body, ".has(alert.id)") {
		t.Error("src/screens/alerts.js: merged() does not drop from the older pages the ids the first page already shows")
	}
}
