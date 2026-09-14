package check

import (
	"strings"
	"testing"
)

// The alerts badge shows how many alerts stand open and unseen. The screen
// holds one page of alerts, the newest by their latest event, so an open alert
// with a page of fresher events above it is not on it. The server counts them
// all and the badge takes its number from there.
func TestAlertBadgeTakesTheCountFromTheServer(t *testing.T) {
	files := srcFiles(t)

	app := withoutComments(files["src/app.js"])
	if strings.Contains(app, "unread(alerts.alerts)") {
		t.Error("src/app.js: the badge counts the page of alerts on the screen; the open ones below it are lost")
	}
	if !strings.Contains(app, "openCount(alerts)") {
		t.Fatal("src/app.js: the badge does not ask openCount() for its number")
	}

	src := withoutComments(files["src/alerts.js"])
	if !strings.Contains(src, "unread: count(body.unread)") {
		t.Error("src/alerts.js: the answer of /api/alerts is read without the counters it carries")
	}

	body := funcBody(t, src, "export function openCount(")
	if !strings.Contains(body, "state.unread") {
		t.Error("src/alerts.js: openCount() does not take the count the server sent")
	}
	if !strings.Contains(body, "unread(state.alerts)") {
		t.Error("src/alerts.js: openCount() has no fallback — against a server without counters the badge would go blank")
	}
}
