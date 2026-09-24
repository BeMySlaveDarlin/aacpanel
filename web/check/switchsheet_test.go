package check

import (
	"strings"
	"testing"
)

// A session on the stream offers to move to the console. What runs inside its
// process does not survive the move, so the sheet names it before the press,
// the press is marked as the one that costs something, and what goes to the
// panel says where to go and that the person agreed.
func TestTheSwitchSheetNamesWhatStops(t *testing.T) {
	var got struct {
		Button   bool   `json:"button"`
		Disabled bool   `json:"disabled"`
		Title    string `json:"title"`
		Effect   string `json:"effect"`
		Danger   bool   `json:"danger"`
		Sent     []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
	}
	runFixture(t, "switchsheet.html", &got)

	if !got.Button {
		t.Fatal("the header of a stream session offers no way to the console")
	}
	if got.Disabled {
		t.Fatal("the switch is off for an idle session")
	}
	if got.Title != "Move evirma to the console?" {
		t.Errorf("the sheet asks %q", got.Title)
	}
	for _, say := range []string{"1 background task", "does not bring them back", "same conversation"} {
		if !strings.Contains(got.Effect, say) {
			t.Errorf("the sheet does not say %q: %q", say, got.Effect)
		}
	}
	if !got.Danger {
		t.Error("a press that stops running work is drawn as a harmless one")
	}
	if len(got.Sent) != 1 {
		t.Fatalf("%d requests went to the panel, expected one", len(got.Sent))
	}
	s := got.Sent[0]
	if s.Kind != "session.switch" || s.Target != "evirma" || s.Params["to"] != "console" || s.Params["force"] != true {
		t.Errorf("the panel was asked %+v — where to go and the agreement must go together", s)
	}
}
