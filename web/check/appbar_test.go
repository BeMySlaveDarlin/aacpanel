package check

import (
	"strings"
	"testing"
)

type barState struct {
	Text          string  `json:"text"`
	Tone          string  `json:"tone"`
	Dot           string  `json:"dot"`
	Label         string  `json:"label"`
	SignIn        string  `json:"signIn"`
	Still         *string `json:"still"`
	Caret         bool    `json:"caret"`
	HostButton    bool    `json:"hostButton"`
	HostFocusable bool    `json:"hostFocusable"`
	Search        bool    `json:"search"`
	Flag          bool    `json:"flag"`
	Buttons       []struct {
		Label string `json:"label"`
	} `json:"buttons"`
}

type appBar struct {
	Bars   map[string]*barState `json:"bars"`
	Opened int                  `json:"opened"`
	Sheet  struct {
		Say      string `json:"say"`
		Tone     string `json:"tone"`
		Insecure string `json:"insecure"`
		Rows     []struct {
			Kind  string `json:"kind"`
			Tag   string `json:"tag"`
			Where string `json:"where"`
			Mark  string `json:"mark"`
		} `json:"rows"`
		Retry      bool `json:"retry"`
		Retried    int  `json:"retried"`
		Rechecked  int  `json:"rechecked"`
		Remeasured int  `json:"remeasured"`
	} `json:"sheet"`
	Menu struct {
		Items           []string `json:"items"`
		Theme           []string `json:"theme"`
		DarkOnDark      int      `json:"darkOnDark"`
		LightOnDark     int      `json:"lightOnDark"`
		Installs        int      `json:"installs"`
		ClosedOnInstall int      `json:"closedOnInstall"`
		ItemsInstalled  []string `json:"itemsInstalled"`
		ThemeLight      []string `json:"themeLight"`
	} `json:"menu"`
}

// The chip says one thing at a time: a healthy link by its leg, with the age of
// the snapshot once it is worth a word, and a broken one by what broke.
var barWords = map[string]struct{ text, tone, dot string }{
	"fresh":        {"localhost", "", "on"},
	"aging":        {"localhost · 25 s", "", "on"},
	"warn":         {"localhost · 3 min", "warn", "on"},
	"crit":         {"localhost · 12 min", "crit", "on"},
	"hours":        {"localhost · 2 h", "crit", "on"},
	"noagent":      {"No agent snapshot", "crit", "on"},
	"stale":        {"Snapshot · 22:41", "warn", "off"},
	"offline":      {"No connection · 22:41", "warn", "off"},
	"offlineEmpty": {"No connection", "warn", "off"},
	"signedout":    {"Signed out", "crit", ""},
	"loading":      {"Loading…", "", "off"},
	"install":      {"localhost", "", "on"},
	"nomap":        {"live", "", "on"},
	"via":          {"LAN", "", "on"},
	"longleg":      {"wireguard-backup-leg · 12 min", "crit", "on"},
	"longhost":     {"No connection · 22:41", "warn", "off"},
}

// A line that shows a state of its own dates it from the moment the chip stops
// calling the link live, with the time the snapshot was taken — not the time
// the link last answered, which a live link with an old snapshot keeps at now.
// The states missing here are live; an empty time is a snapshot with none.
var barStill = map[string]string{
	"warn": "22:38", "crit": "22:29", "hours": "20:39", "noagent": "22:41", "stale": "22:40",
	"offline": "22:41", "offlineEmpty": "", "signedout": "", "longleg": "22:29", "longhost": "22:41",
}

// The app bar on a phone says every state of the link in one chip, and the
// host name is plain text and Settings carries the install mark.
func TestTheAppBarSaysEveryStateOfTheLink(t *testing.T) {
	var got appBar
	runFixture(t, "appbar.html", &got)
	if len(got.Bars) != len(barWords) {
		t.Fatalf("the fixture read %d states, the test knows %d: %v", len(got.Bars), len(barWords), got.Bars)
	}
	for name, want := range barWords {
		bar := got.Bars[name]
		if bar == nil {
			t.Errorf("%s: the bar was not read", name)
			continue
		}
		if bar.Caret || bar.HostButton || bar.HostFocusable {
			t.Errorf("%s: the host name is interactive or carries a menu caret: %+v", name, bar)
		}
		if bar.Search {
			t.Errorf("%s: the header still has a search field", name)
		}
		if bar.Text != want.text || bar.Tone != want.tone || bar.Dot != want.dot {
			t.Errorf("%s: the chip reads %q, tone %q, dot %q; want %q, %q, %q",
				name, bar.Text, bar.Tone, bar.Dot, want.text, want.tone, want.dot)
		}
		if !strings.HasPrefix(bar.Label, "connection: ") || len(bar.Label) < len("connection: ")+len(want.text)/2 {
			t.Errorf("%s: a screen reader hears the chip as %q", name, bar.Label)
		}
		labels := []string{}
		for _, b := range bar.Buttons {
			labels = append(labels, b.Label)
		}
		if strings.Join(labels, ",") != "alerts" {
			t.Errorf("%s: the bar ends with %v, expected only alerts", name, labels)
		}
		if (name == "signedout") != (bar.SignIn == "/login") {
			t.Errorf("%s: the way to sign in is %q", name, bar.SignIn)
		}
		still, dated := barStill[name]
		switch {
		case !dated && bar.Still != nil:
			t.Errorf("%s: the chip calls the link live, and a state is dated %q", name, *bar.Still)
		case dated && bar.Still == nil:
			t.Errorf("%s: the link is not live, and a state is still said as now", name)
		case dated && *bar.Still != still:
			t.Errorf("%s: a state is dated %q, not %q — the time of its snapshot", name, *bar.Still, still)
		}
		if bar.Flag != (name == "install") {
			t.Errorf("%s: Settings flags the install: %v", name, bar.Flag)
		}
	}
	if got.Opened != 1 {
		t.Errorf("a tap on the chip opened the connection %d times", got.Opened)
	}
}

// The sheet behind the chip says the state in a whole sentence, lists every
// leg with how it answers, and tries again on Retry: the data, the nearest leg
// and the ping of every leg. The warning about the cookie lives here now.
func TestTheConnectionSheetSaysItAll(t *testing.T) {
	var got appBar
	runFixture(t, "appbar.html", &got)
	s := got.Sheet
	if s.Say != "No connection — data from 22:41" || s.Tone != "warn" {
		t.Errorf("the sheet says %q in tone %q", s.Say, s.Tone)
	}
	if !strings.Contains(s.Insecure, "Cookie without Secure") {
		t.Errorf("the cookie warning is gone from the sheet: %q", s.Insecure)
	}
	want := []struct{ kind, tag, where, mark string }{
		{"localhost", "no answer", "this device · 127.0.0.1:8777", "page"},
		{"LAN", "ms", "helios.example.net:8443", ""},
		{"tailscale", "no answer", "helios.tail0000.ts.net", ""},
	}
	if len(s.Rows) != len(want) {
		t.Fatalf("the sheet lists %d legs: %+v", len(s.Rows), s.Rows)
	}
	for i, w := range want {
		r := s.Rows[i]
		if r.Kind != w.kind || !strings.HasSuffix(r.Tag, w.tag) || r.Where != w.where || r.Mark != w.mark {
			t.Errorf("leg %d reads %+v, want %+v", i, r, w)
		}
	}
	if !s.Retry {
		t.Error("the sheet has no Retry")
	}
	if s.Retried != 1 || s.Rechecked != 1 || s.Remeasured < 1 {
		t.Errorf("Retry asked for the data %d times, for the leg %d, pinged the legs %d times",
			s.Retried, s.Rechecked, s.Remeasured)
	}
}

// The host menu took the theme and the install from the bar: the live tiles
// first, the install while the app is not installed, the doors that do not
// open yet, the pages that have no tab, and the theme as two words with the
// one in use pressed.
func TestTheHostMenuCarriesThemeAndInstall(t *testing.T) {
	var got appBar
	runFixture(t, "appbar.html", &got)
	m := got.Menu
	if strings.Join(m.Items, "|") != "Machine|Usage|Briefs|Install the appnot installed yet|Claude|Codex|Balancer|Journal|Devices|Settings|Profiles|Sign out" {
		t.Errorf("the menu reads %q", m.Items)
	}
	if strings.Join(m.ItemsInstalled, "|") != "Machine|Usage|Briefs|Claude|Codex|Balancer|Journal|Devices|Settings|Profiles|Sign out" {
		t.Errorf("an installed app still offers the install: %q", m.ItemsInstalled)
	}
	if strings.Join(m.Theme, ",") != "Dark:true,Light:false" || strings.Join(m.ThemeLight, ",") != "Dark:false,Light:true" {
		t.Errorf("the theme reads %v on dark and %v on light", m.Theme, m.ThemeLight)
	}
	if m.DarkOnDark != 0 || m.LightOnDark != 1 {
		t.Errorf("Dark on the dark theme switched it %d times, Light %d", m.DarkOnDark, m.LightOnDark-m.DarkOnDark)
	}
	if m.Installs != 1 || m.ClosedOnInstall != 1 {
		t.Errorf("Install ran %d times and closed the menu %d times", m.Installs, m.ClosedOnInstall)
	}
}
