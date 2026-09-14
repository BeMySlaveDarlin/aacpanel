package check

import (
	"strings"
	"testing"
)

// A session that runs out on an open screen sends the visitor to the login with
// the address to come back to. The query is part of that address: a push about
// a question opens a named session, and after the login it must open the same
// one, not the panel at large.
func TestTheWayBackFromTheLoginKeepsTheQuery(t *testing.T) {
	src := withoutComments(srcFiles(t)["src/app.js"])
	body := funcBody(t, src, "function toLogin(")
	if !strings.Contains(body, "location.pathname + location.search") {
		t.Error("src/app.js: the way back to the screen is remembered without its query — the login lands on the panel instead of the session that was open")
	}
}
