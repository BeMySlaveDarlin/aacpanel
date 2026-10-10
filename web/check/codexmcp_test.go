package check

import (
	"reflect"
	"strings"
	"testing"
)

// Every state codex gives an MCP server of a thread reads in a word and a dot
// of its own, in the tones claude's servers have: codex's starting, not
// started and cancelled are none of claude's, and a server codex gives no
// state — its configuration changed after the thread started it — says so,
// and what a new thread does about it. No server of codex reads "unknown".
func TestTheServersOfACodexThreadReadInEveryStateCodexGives(t *testing.T) {
	var got struct {
		Error   string   `json:"error"`
		Rows    []string `json:"rows"`
		Servers []struct {
			Name   string   `json:"name"`
			Status string   `json:"status"`
			Tone   string   `json:"tone"`
			Issue  string   `json:"issue"`
			Notes  []string `json:"notes"`
		} `json:"servers"`
		Unknown []string `json:"unknown"`
	}
	runFixture(t, "codexmcp.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}

	// A plugin's server is listed first, those of config.toml after it.
	wantRows := []string{
		"context7|warn|config changed",
		"codex_apps|ok|3 tools",
		"slow|off|starting",
		"later|off|not started",
		"locked|warn|needs authentication",
		"broken|crit|",
		"halted|off|cancelled",
		"off|off|disabled",
		"docker|warn|config changed",
	}
	if !reflect.DeepEqual(got.Rows, wantRows) {
		t.Errorf("the list reads\n%s\nexpected\n%s", strings.Join(got.Rows, "\n"), strings.Join(wantRows, "\n"))
	}

	const changed = "config changed since the thread started|warn"
	wantStatus := map[string]string{
		"codex_apps": "connected|ok", "slow": "starting|off", "later": "not started|off",
		"locked": "needs authentication|warn", "broken": "failed|crit", "halted": "cancelled|off",
		"off": "disabled|off", "context7": changed, "docker": changed,
	}
	if len(got.Servers) != len(wantStatus) {
		t.Fatalf("opened %d servers, expected %d", len(got.Servers), len(wantStatus))
	}
	for _, s := range got.Servers {
		if want := wantStatus[s.Name]; s.Status+"|"+s.Tone != want {
			t.Errorf("%s opens as %q with a dot %q, expected %s", s.Name, s.Status, s.Tone, want)
		}
		told := strings.Contains(strings.Join(s.Notes, " "), "A new thread starts it as configured now")
		if told != (wantStatus[s.Name] == changed) {
			t.Errorf("%s says %v", s.Name, s.Notes)
		}
		if s.Name == "broken" && s.Issue != "MCP startup failed: No such file or directory (os error 2)" {
			t.Errorf("a failed server tells the issue %q", s.Issue)
		}
	}
	if len(got.Unknown) > 0 {
		t.Errorf("%v read unknown", got.Unknown)
	}
}
