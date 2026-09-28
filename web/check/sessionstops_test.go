package check

import (
	"strings"
	"testing"
)

// A close or a restart ends the agents and background tasks of the session
// with it: the phone's sheet and the desktop's button say what before the
// press, and nothing more when nothing runs.
func TestAnEndSaysWhatItStops(t *testing.T) {
	var got struct {
		Sheets map[string]string  `json:"sheets"`
		Tips   map[string]*string `json:"tips"`
	}
	runWideFixture(t, "sessionstops.html", &got)

	for name, want := range map[string]string{
		"atlas": "stops 1 background task, 2 agents · a new session",
		"lab":   "stops 3 background tasks, 1 workflow · the conversation stays",
	} {
		if !strings.Contains(got.Sheets[name], want) {
			t.Errorf("the end of %s reads %q, expected it to name %q", name, got.Sheets[name], want)
		}
	}
	if strings.Contains(got.Sheets["quiet"], "stops") || !strings.Contains(got.Sheets["quiet"], "the conversation stays") {
		t.Errorf("the end of a session with nothing at work reads %q", got.Sheets["quiet"])
	}
	for name, want := range map[string]string{"atlas": "Stops 1 background task, 2 agents", "lab": "Stops 3 background tasks, 1 workflow"} {
		if got.Tips[name] == nil || *got.Tips[name] != want {
			t.Errorf("the desktop button of %s tips %v, expected %q", name, got.Tips[name], want)
		}
	}
	if got.Tips["quiet"] == nil || *got.Tips["quiet"] != "" {
		t.Errorf("the desktop button of a session with nothing at work tips %v", got.Tips["quiet"])
	}
}
