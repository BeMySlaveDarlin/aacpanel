package check

import (
	"reflect"
	"strings"
	"testing"
)

// /mcp in a session on the stream opens the panel's own screen instead of
// going to the session: the servers by where each comes from, a dot of how
// each stands, and one server with its facts and the changes a phone can make.
// In a console the command is a screen driven by keys, and nothing is sent.
func TestMcpOpensTheServersOfTheSession(t *testing.T) {
	var got struct {
		Hints         []string `json:"hints"`
		SendReady     bool     `json:"sendReady"`
		Title         string   `json:"title"`
		Count         string   `json:"count"`
		Groups        []string `json:"groups"`
		Docker        string   `json:"docker"`
		Dots          []string `json:"dots"`
		ComposerAfter string   `json:"composerAfter"`
		SentBefore    int      `json:"sentBefore"`
		Server        string   `json:"server"`
		Facts         []string `json:"facts"`
		Buttons       []string `json:"buttons"`
		Sent          []string `json:"sent"`
		AskedAgain    bool     `json:"askedAgain"`
		ConsoleReady  bool     `json:"consoleReady"`
		ConsoleHint   string   `json:"consoleHint"`
	}
	runFixture(t, "mcpsheet.html", &got)

	if !contains("MCP servers", got.Hints) {
		t.Errorf("typing /mc does not offer /mcp: %v", got.Hints)
	}
	if !got.SendReady || got.Title != "MCP servers" || got.ComposerAfter != "" || got.SentBefore != 0 {
		t.Fatalf("/mcp: ready %v, opened %q, left %q in the composer, sent %d", got.SendReady, got.Title,
			got.ComposerAfter, got.SentBefore)
	}
	if got.Count != "20 servers" || !reflect.DeepEqual(got.Groups, []string{"User", "claude.ai", "Plugins and built-in"}) {
		t.Errorf("the list reads %q grouped %v", got.Count, got.Groups)
	}
	if !strings.HasPrefix(got.Docker, "docker") || !strings.Contains(got.Docker, "56 tools") {
		t.Errorf("the docker row reads %q", got.Docker)
	}
	for _, want := range []string{"docker:ok", "tg-shadow:crit", "plugin:data:hex:warn", "plugin:data:snowflake:off"} {
		if !contains(want, got.Dots) {
			t.Errorf("no row %s among %v", want, got.Dots)
		}
	}
	if got.Server != "tg-shadow" || !contains("IssueECONNREFUSED: Unable to connect. Is the computer able to access the url?", got.Facts) ||
		!contains("URLhttp://127.0.0.1:8901/mcp", got.Facts) {
		t.Errorf("the server %q shows %v", got.Server, got.Facts)
	}
	if !reflect.DeepEqual(got.Buttons, []string{"Reconnect", "Disable"}) {
		t.Errorf("the server offers %v", got.Buttons)
	}
	if !reflect.DeepEqual(got.Sent, []string{`session.mcp:{"server":"tg-shadow","do":"disable"}`}) || !got.AskedAgain {
		t.Errorf("the host got %v, and the list was asked again: %v", got.Sent, got.AskedAgain)
	}
	if got.ConsoleReady || !strings.Contains(got.ConsoleHint, "keys") {
		t.Errorf("in a console /mcp is ready %v, the hint %q", got.ConsoleReady, got.ConsoleHint)
	}
}
