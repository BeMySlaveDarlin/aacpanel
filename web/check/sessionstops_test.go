package check

import (
	"strings"
	"testing"
)

// Live cards offer their actions directly, while the gate owns every
// confirmation and request. The exclusions protect sessions the panel must
// never close or cannot restart; a press on the card still opens its chat.
func TestThePhoneCardsOfferRestartAndCloseThroughTheGate(t *testing.T) {
	var got struct {
		MobileActions map[string][]struct {
			Text     string  `json:"text"`
			Label    string  `json:"label"`
			Top      bool    `json:"top"`
			Width    float64 `json:"width"`
			Disabled bool    `json:"disabled"`
			Icon     bool    `json:"icon"`
			Height   float64 `json:"height"`
			Clear    bool    `json:"clear"`
			Inside   bool    `json:"inside"`
			Hit      bool    `json:"hit"`
		} `json:"mobileActions"`
		Gates []struct {
			Name, Kind, Title, Effect, OK string
			Harsher                       []string
			Before, Opened                int
			Sent                          []struct{ Kind, Target string }
		} `json:"gates"`
		Opened      []struct{ Name, ID string } `json:"opened"`
		More        int                         `json:"more"`
		Overflow    int                         `json:"overflow"`
		Notes       map[string]string           `json:"notes"`
		Tips        map[string]*string          `json:"tips"`
		Unavailable []struct {
			Off bool
			Why string
		} `json:"unavailable"`
		Busy bool `json:"busy"`
	}
	runFixture(t, "sessionstops.html", &got)

	want := map[string]string{
		"helios": "restart", "lab": "restart,close", "quiet": "restart,close",
		"codex-5afc361b": "close", "outside": "",
	}
	if len(got.MobileActions) != len(want) {
		t.Fatalf("the list has %d cards, expected %d", len(got.MobileActions), len(want))
	}
	for name, labels := range want {
		var words []string
		for _, b := range got.MobileActions[name] {
			words = append(words, strings.TrimSuffix(b.Label, " session "+name))
			if b.Text != "" || !b.Top || b.Disabled || !b.Icon || b.Height < 36 || b.Width < 36 || !b.Clear || !b.Inside || !b.Hit {
				t.Errorf("%s: its action does not stand readable and reachable on its own: %+v", name, b)
			}
		}
		if strings.Join(words, ",") != labels {
			t.Errorf("%s offers %v, expected %q", name, words, labels)
		}
	}
	if got.More != 0 || got.Overflow > 0 {
		t.Errorf("the list has %d menus and overflows the phone by %dpx", got.More, got.Overflow)
	}
	if len(got.Opened) != len(want) {
		t.Errorf("the cards opened %+v", got.Opened)
	}
	for _, opened := range got.Opened {
		if _, ok := want[opened.Name]; !ok || opened.ID == "" {
			t.Errorf("a card opened %+v instead of its named conversation", opened)
		}
	}
	if len(got.Gates) != 4 {
		t.Fatalf("the presses reached %d confirmations, expected four", len(got.Gates))
	}
	for _, g := range got.Gates {
		if g.Before != 0 || g.Opened != 0 || len(g.Sent) != 1 ||
			g.Sent[0].Kind != "session."+g.Kind || g.Sent[0].Target != g.Name {
			t.Errorf("the confirmation did not own the action: %+v", g)
		}
		if !strings.Contains(g.Title, g.Name) {
			t.Errorf("the confirmation does not name %s: %q", g.Name, g.Title)
		}
		switch {
		case g.Kind == "restart":
			if g.OK != "Restart" || !strings.Contains(g.Effect, "empty context") || len(g.Harsher) != 0 {
				t.Errorf("restart reached the wrong confirmation: %+v", g)
			}
		case g.Name == "codex-5afc361b":
			if g.OK != "Close" || !strings.Contains(g.Effect, "daemon unloads it") || len(g.Harsher) != 0 {
				t.Errorf("codex close reached the wrong confirmation: %+v", g)
			}
		default:
			if g.OK != "Close gently" || !strings.Contains(g.Effect, "transcript") ||
				len(g.Harsher) != 1 || !strings.Contains(g.Harsher[0], "kill -9") {
				t.Errorf("claude close lost its gentle close or escalation: %+v", g)
			}
		}
	}
	if len(got.Unavailable) != 2 || !got.Busy {
		t.Errorf("unavailable actions %+v, busy disabled %v", got.Unavailable, got.Busy)
	}
	for _, b := range got.Unavailable {
		if !b.Off || !strings.Contains(b.Why, "the host does not know") {
			t.Errorf("an action unknown to the host remains available or unexplained: %+v", b)
		}
	}
	for name, want := range map[string]string{
		"helios": "stops 1 background task, 2 agents · a new session",
		"lab":    "stops 3 background tasks, 1 workflow · the conversation stays",
	} {
		if !strings.Contains(got.Notes[name], want) {
			t.Errorf("the end of %s reads %q, expected it to name %q", name, got.Notes[name], want)
		}
	}
	if strings.Contains(got.Notes["quiet"], "stops") || !strings.Contains(got.Notes["quiet"], "the conversation stays") {
		t.Errorf("the end of a session with nothing at work reads %q", got.Notes["quiet"])
	}
	saysCodexClose(t, "the phone card", got.Notes["codex-5afc361b"])
	for name, want := range map[string]string{"helios": "Stops 1 background task, 2 agents", "lab": "Stops 3 background tasks, 1 workflow", "quiet": ""} {
		if got.Tips[name] == nil || *got.Tips[name] != want {
			t.Errorf("the desktop button of %s tips %v, expected %q", name, got.Tips[name], want)
		}
	}
}
