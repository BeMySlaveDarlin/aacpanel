package check

import (
	"strings"
	"testing"
)

type loadShot struct {
	Before struct {
		Names  []string `json:"names"`
		Values []string `json:"values"`
		Tones  []string `json:"tones"`
		Fills  []int    `json:"fills"`
		Bars   int      `json:"bars"`
		Tips   []string `json:"tips"`
	} `json:"before"`
	Opened int `json:"opened"`
	Blank  int `json:"blank"`
}

// The rail keeps the host readings available while the centre changes section.
func TestTheRailCarriesTheLoadOfTheHost(t *testing.T) {
	var got loadShot
	runWideFixture(t, "headload.html", &got)

	want := []string{"cpu", "mem", "disk"}
	if len(got.Before.Names) != 3 {
		t.Fatalf("gauges drawn: %v", got.Before.Names)
	}
	for i, name := range want {
		if got.Before.Names[i] != name {
			t.Errorf("gauge %d is %q, the order is cpu, mem, disk", i+1, got.Before.Names[i])
		}
	}
	if got.Before.Values[0] != "74%" || got.Before.Values[1] != "23%" {
		t.Errorf("the numbers do not match the host: %v", got.Before.Values)
	}
}

// The disk shown is the fullest one. A host runs out of one mount at a time,
// and the root disk sitting at a fifth says nothing about the one at 94%.
func TestTheDiskGaugeShowsTheFullestMount(t *testing.T) {
	var got loadShot
	runWideFixture(t, "headload.html", &got)

	if got.Before.Values[2] != "94%" {
		t.Errorf("the disk gauge reads %q while a mount of the host is at 94%%", got.Before.Values[2])
	}
	if !strings.Contains(got.Before.Tips[2], "/mnt/storage") {
		t.Errorf("nothing says which disk is meant: %q", got.Before.Tips[2])
	}
	if got.Before.Tones[2] != "crit" {
		t.Errorf("a disk at 94%% is drawn as %q", got.Before.Tones[2])
	}
	if got.Before.Tones[0] != "warn" {
		t.Errorf("a processor at 74%% is drawn as %q", got.Before.Tones[0])
	}
	// The chip is the number in another form: a fifth used looks a fifth filled.
	if got.Before.Fills[1] < 18 || got.Before.Fills[1] > 28 {
		t.Errorf("the fill does not follow the numbers: %v", got.Before.Fills)
	}
	if got.Before.Bars != 0 {
		t.Errorf("%d gauges still carry a bar of their own beside the number", got.Before.Bars)
	}
}

// The gauges lead into the machine screen, and a snapshot without a host draws
// nothing at all: three zeros would be a lie about a machine nobody asked.
func TestTheLoadLeadsIntoTheMachineAndVanishesWithoutASnapshot(t *testing.T) {
	var got loadShot
	runWideFixture(t, "headload.html", &got)

	if got.Opened == 0 {
		t.Error("the gauges are not a way into the machine screen")
	}
	if got.Blank != 0 {
		t.Errorf("a snapshot without a host still drew %d gauges", got.Blank)
	}
}

func TestDesktopRailsCarryConnectionLoadAndScale(t *testing.T) {
	type railState struct {
		Names, Values, Panels               []string
		Readable, Labels, SameButtons, Zoom bool
	}
	var got struct {
		Rail struct {
			InRail, HasStatus, Host bool
			HostName, Route, Tip    string
		}
		Expanded, Compact, Full railState
		Opened                  int
		PanelOpened             []bool
		Routes                  []struct {
			Text string
			Fits bool
		}
		RouteOpened, Near, ZoomLarger, ZoomSmaller    bool
		Failed, Stale, Cookie, Recovered, FooterClean bool
		Error                                         string
	}
	runWideFixture(t, "headload.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	if !got.Rail.InRail || !got.Rail.HasStatus || !got.Rail.Host || got.Rail.HostName != "helios▾" || got.Rail.Route != "LAN" || got.Rail.Tip == "" {
		t.Errorf("the rail lost the route, host or readings, or the status bar lost the contour without numbers: %+v", got.Rail)
	}
	for name, state := range map[string]railState{"expanded": got.Expanded, "compact": got.Compact, "full": got.Full} {
		wantValues := []string{"74%", "23%", "94%"}
		if name == "full" {
			wantValues = []string{"100%", "100%", "100%"}
		}
		if !equalStrings(state.Names, []string{"cpu", "mem", "disk"}) || !equalStrings(state.Values, wantValues) ||
			!equalStrings(state.Panels, []string{"Session archive", "Projects", "Action journal"}) || !state.Readable || !state.Labels || !state.SameButtons || !state.Zoom {
			t.Errorf("%s rail lost a reading, a panel label or its shared controls: %+v", name, state)
		}
	}
	if got.Opened != 3 {
		t.Errorf("%d readings opened Machine, expected all three", got.Opened)
	}
	if len(got.PanelOpened) != 4 {
		t.Fatalf("panel states: %v", got.PanelOpened)
	}
	for _, open := range got.PanelOpened {
		if !open {
			t.Error("the rail failed to mark an opened panel")
		}
	}
	if len(got.Routes) != 4 {
		t.Fatalf("route readings: %+v", got.Routes)
	}
	for i, want := range []string{"LAN", "domain", "tailscale", "localhost"} {
		if got.Routes[i].Text != want || !got.Routes[i].Fits {
			t.Errorf("route %d: %+v, expected %q without clipping", i, got.Routes[i], want)
		}
	}
	for name, ok := range map[string]bool{"route sheet": got.RouteOpened, "near route": got.Near,
		"larger scale": got.ZoomLarger, "smaller scale": got.ZoomSmaller, "host failure": got.Failed,
		"stale snapshot": got.Stale, "cookie alert": got.Cookie, "recovered": got.Recovered, "clean footer": got.FooterClean} {
		if !ok {
			t.Errorf("%s: the desktop did not show the expected state", name)
		}
	}
}

func TestStatusBarShowsTheLimitsOfTheContourInTheColumn(t *testing.T) {
	var got struct {
		Limits struct {
			Name                   string
			Groups, Alerts         []string
			Readings               []struct{ Value, Tone string }
			HeadingReadings, Notes int
			SilentIcon, StaleTip   bool
			Details                string
			Changed                struct {
				Name   string
				Values []string
				Closed bool
			}
			Empty struct {
				Name, Note string
				Groups     int
			}
		}
		Error string
	}
	runWideFixture(t, "headload.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	l := got.Limits
	if l.Name != "personal" || strings.Join(l.Groups, ",") != "codex,gemini,claude" || l.HeadingReadings != 0 {
		t.Errorf("the footer lost the selected contour or forecast order, or the heading still has readings: %+v", l)
	}
	want := []struct{ Value, Tone string }{{"12%", "crit"}, {"15%", "ok"}, {"42%", "warn"}, {"88%", "ok"}, {"90%", "ok"}}
	if len(l.Readings) != len(want) {
		t.Fatalf("the footer has readings %+v, expected %+v", l.Readings, want)
	}
	for i, reading := range l.Readings {
		if reading != want[i] {
			t.Errorf("reading %d: %+v, expected %+v", i, reading, want[i])
		}
	}
	if strings.Join(l.Alerts, ",") != "cookie without Secure,silent 1" || !l.SilentIcon || !l.StaleTip || l.Notes != 0 {
		t.Errorf("the footer lost alerts or the stale tooltip, or shows notes beside the chips: %+v", l)
	}
	if !strings.HasPrefix(l.Details, "Codex limits5h12% used") {
		t.Errorf("the footer chip opened details %q", l.Details)
	}
	if l.Changed.Name != "Evirma" || strings.Join(l.Changed.Values, ",") != "5%,99%" || !l.Changed.Closed {
		t.Errorf("the footer did not follow the column or kept the previous bubble open: %+v", l.Changed)
	}
	if l.Empty.Name != "Empty" || l.Empty.Note != "no numbers yet" || l.Empty.Groups != 0 {
		t.Errorf("the contour without limits has %+v, expected its name and no numbers yet without frames", l.Empty)
	}
}
