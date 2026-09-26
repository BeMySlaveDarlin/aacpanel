package check

import (
	"strings"
	"testing"
)

type remoteLook struct {
	Button   bool   `json:"button"`
	Says     string `json:"says"`
	Pressed  string `json:"pressed"`
	Disabled bool   `json:"disabled"`
	Note     string `json:"note"`
	Link     string `json:"link"`
}

// Remote Control is a line among the tools of a session: it says which way it
// stands and where the bridge leads, a press asks the host for the other way,
// and a session whose bridge would open in another account than the Claude
// app's is not offered the switch but told why in words.
func TestRemoteControlIsALineAmongTheTools(t *testing.T) {
	var got struct {
		Before   map[string]remoteLook `json:"before"`
		AfterOff remoteLook            `json:"afterOff"`
		Sent     []string              `json:"sent"`
	}
	runFixture(t, "remoteline.html", &got)

	off, on, work, old := got.Before["off"], got.Before["on"], got.Before["work"], got.Before["old"]
	if !off.Button || off.Says != "Turn on" || off.Pressed != "false" || off.Disabled || off.Note != "off" {
		t.Errorf("a session without the bridge reads %+v", off)
	}
	const url = "https://claude.ai/code/session_015aXCcdHpKReTaPGrYZVxrT"
	if on.Says != "Turn off" || on.Pressed != "true" || !strings.Contains(on.Note, "claude.ai/code/session_015aXCcdHpKReTaPGrYZVxrT") ||
		on.Link != url {
		t.Errorf("a session with the bridge reads %+v: on, naming where it is, with the way there", on)
	}
	if work.Button || !strings.Contains(work.Note, "evirma") {
		t.Errorf("a session of a contour on another account reads %+v: no switch, and saying whose account", work)
	}
	if !old.Disabled {
		t.Errorf("a host whose executor does not know the switch offers it: %+v", old)
	}
	want := []string{`session.remote:atlas:{"on":true}`, `session.remote:aacpanel:{"on":false}`}
	if strings.Join(got.Sent, " | ") != strings.Join(want, " | ") {
		t.Errorf("the presses sent %v, not %v", got.Sent, want)
	}
	if got.AfterOff.Says != "Turn off" || !got.AfterOff.Disabled {
		t.Errorf("a switch that went through does not stand the new way until the snapshot catches up: %+v", got.AfterOff)
	}
}
