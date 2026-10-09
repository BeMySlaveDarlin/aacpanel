package check

import (
	"strings"
	"testing"
)

type secretsShot struct {
	Wide  bool     `json:"wide"`
	Rows  []string `json:"rows"`
	Dir   string   `json:"dir"`
	When  string   `json:"when"`
	Head  []string `json:"head"`
	Asked struct {
		Title     string `json:"title"`
		Sent      int    `json:"sent"`
		Reachable bool   `json:"reachable"`
	} `json:"asked"`
	Cancelled struct {
		Sent int `json:"sent"`
		Rows int `json:"rows"`
	} `json:"cancelled"`
	Dropped struct {
		Sent []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
		Rows []string `json:"rows"`
	} `json:"dropped"`
	Empty struct {
		Rows  int    `json:"rows"`
		Words string `json:"words"`
	} `json:"empty"`
	Error string `json:"error"`
}

// The secrets of the host stand in the settings, on the phone and at a desk:
// the directory, a row per file with its size and time and never its text,
// and a removal that the gate asks about before secret.drop goes out.
func TestSecretsListInTheSettings(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{
		{"phone", runFixture},
		{"desk", runWideFixture},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got secretsShot
			screen.run(t, "secrets.html", &got)
			if got.Error != "" {
				t.Fatalf("the fixture broke: %s", got.Error)
			}
			if got.Wide != (screen.name == "desk") {
				t.Fatalf("the fixture ran the %s screen as wide=%v", screen.name, got.Wide)
			}
			checkSecretsList(t, got)
		})
	}
}

func checkSecretsList(t *testing.T, got secretsShot) {
	want := []string{
		"evirma-db.env | 2.0 KB · " + got.When,
		"github-token | 41 B · " + got.When,
	}
	if strings.Join(got.Rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("the rows read\n%s\nnot\n%s", strings.Join(got.Rows, "\n"), strings.Join(want, "\n"))
	}
	if got.Dir != "/home/u/.local/state/aacpanel/secrets" {
		t.Errorf("the directory reads %q", got.Dir)
	}
	if !contains("Secrets of sessions", got.Head) {
		t.Errorf("the settings have no section of the secrets: %v", got.Head)
	}

	if got.Asked.Title != "Remove the secret github-token?" || got.Asked.Sent != 0 {
		t.Errorf("the first press asked %q and sent %d actions", got.Asked.Title, got.Asked.Sent)
	}
	if !got.Asked.Reachable {
		t.Error("the confirmation stands where a press does not reach it")
	}
	if got.Cancelled.Sent != 0 || got.Cancelled.Rows != 2 {
		t.Errorf("Cancel sent %d actions and left %d rows", got.Cancelled.Sent, got.Cancelled.Rows)
	}

	d := got.Dropped
	if len(d.Sent) != 1 || d.Sent[0].Kind != "secret.drop" || d.Sent[0].Target != "github-token" || len(d.Sent[0].Params) != 0 {
		t.Errorf("the confirmation sent %+v, not secret.drop of github-token", d.Sent)
	}
	if len(d.Rows) != 1 || !strings.HasPrefix(d.Rows[0], "evirma-db.env |") {
		t.Errorf("after the removal the list reads %v", d.Rows)
	}

	if got.Empty.Rows != 0 || !strings.HasPrefix(got.Empty.Words, "No secrets on the host.") {
		t.Errorf("a host with no secrets shows %d rows and says %q", got.Empty.Rows, got.Empty.Words)
	}
}
