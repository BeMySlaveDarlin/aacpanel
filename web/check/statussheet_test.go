package check

import (
	"reflect"
	"testing"
)

// /status opens the panel's card of the session in both kinds of session: on
// the stream with the account claude named at the handshake and its MCP
// servers in a line, in a console with what the panel knows without asking.
func TestStatusOpensTheCardOfTheSession(t *testing.T) {
	var got struct {
		Title           string   `json:"title"`
		Sections        []string `json:"sections"`
		Rows            []string `json:"rows"`
		ConsoleSections []string `json:"consoleSections"`
		ConsoleRows     []string `json:"consoleRows"`
	}
	runFixture(t, "statussheet.html", &got)

	if got.Title != "Session info" || !reflect.DeepEqual(got.Sections, []string{"Session", "Environment", "Account"}) {
		t.Fatalf("the card is %q with %v", got.Title, got.Sections)
	}
	for _, want := range []string{
		"Claude Code2.1.282", "Runs inthe feed", "ModelOpus 5.5 · Extra", "PermissionsAuto", "Started3 h ago",
		"Session ID5f94ee29-aead-423a-a0c8-35c6244e253d",
		"MCP servers2 connected · 1 needs authentication · 1 failed",
		"Signed in asowner@example.com · Max", "Organizationowner@example.com's Organization",
	} {
		if !contains(want, got.Rows) {
			t.Errorf("the card has no row %q: %v", want, got.Rows)
		}
	}
	if !reflect.DeepEqual(got.ConsoleSections, []string{"Session", "Environment"}) ||
		!contains("Claude Code2.1.280", got.ConsoleRows) || !contains("Runs ina console", got.ConsoleRows) ||
		!contains("MCP serverson its own screen, /mcp with keys", got.ConsoleRows) {
		t.Errorf("in a console the card is %v: %v", got.ConsoleSections, got.ConsoleRows)
	}
}
