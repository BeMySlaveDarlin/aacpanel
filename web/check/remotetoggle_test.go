package check

import (
	"strings"
	"testing"
)

type remoteLook struct {
	On       bool   `json:"on"`
	Pressed  string `json:"pressed"`
	Disabled bool   `json:"disabled"`
	Label    string `json:"label"`
}

// Remote Control is a switch in the header of a session: it says which way it
// stands, a press asks the host for the other way, and a session whose bridge
// would open in another account than the Claude app's is not offered it.
func TestRemoteControlIsASwitchInTheHeader(t *testing.T) {
	var got struct {
		Before   map[string]remoteLook `json:"before"`
		AfterOff remoteLook            `json:"afterOff"`
		Sent     []string              `json:"sent"`
	}
	runFixture(t, "remotetoggle.html", &got)

	off, on, work, old := got.Before["off"], got.Before["on"], got.Before["work"], got.Before["old"]
	if off.On || off.Pressed != "false" || off.Disabled {
		t.Errorf("a session without the bridge reads %+v", off)
	}
	if !on.On || on.Pressed != "true" || !strings.Contains(on.Label, "claude.ai/code/session_015aXCcdHpKReTaPGrYZVxrT") {
		t.Errorf("a session with the bridge reads %+v: lit, pressed, and naming where it is", on)
	}
	if !work.Disabled || !strings.Contains(work.Label, "evirma") {
		t.Errorf("a session of a contour on another account reads %+v: off, and saying whose account", work)
	}
	if !old.Disabled {
		t.Errorf("a host whose executor does not know the switch offers it: %+v", old)
	}
	want := []string{`session.remote:atlas:{"on":true}`, `session.remote:aacpanel:{"on":false}`}
	if strings.Join(got.Sent, " | ") != strings.Join(want, " | ") {
		t.Errorf("the presses sent %v, not %v", got.Sent, want)
	}
	if !got.AfterOff.On || !got.AfterOff.Disabled {
		t.Errorf("a switch that went through does not stand the new way until the snapshot catches up: %+v", got.AfterOff)
	}
}
