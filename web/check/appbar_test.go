package check

import (
	"strings"
	"testing"
)

type barBox struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}

type barState struct {
	Top       *barBox `json:"top"`
	Row       *barBox `json:"row"`
	Chip      *barBox `json:"chip"`
	Cbox      *barBox `json:"cbox"`
	Text      string  `json:"text"`
	Cut       bool    `json:"cut"`
	Wrapped   bool    `json:"wrapped"`
	Tone      string  `json:"tone"`
	Dot       string  `json:"dot"`
	DotFill   string  `json:"dotFill"`
	Label     string  `json:"label"`
	SignIn    string  `json:"signIn"`
	Still     *string `json:"still"`
	SignInBox *barBox `json:"signInBox"`
	Host      *barBox `json:"host"`
	Caret     bool    `json:"caretShown"`
	Flag      bool    `json:"flag"`
	CaretTone string  `json:"caret"`
	Buttons   []struct {
		Label string `json:"label"`
		barBox
	} `json:"buttons"`
	Overflow bool `json:"overflow"`
}

type appBar struct {
	Width  float64              `json:"width"`
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
		Retry      *barBox `json:"retry"`
		Retried    int     `json:"retried"`
		Rechecked  int     `json:"rechecked"`
		Remeasured int     `json:"remeasured"`
	} `json:"sheet"`
	Menu struct {
		Items           []string  `json:"items"`
		Theme           []string  `json:"theme"`
		ThemeBox        []*barBox `json:"themeBox"`
		DarkOnDark      int       `json:"darkOnDark"`
		LightOnDark     int       `json:"lightOnDark"`
		Installs        int       `json:"installs"`
		ClosedOnInstall int       `json:"closedOnInstall"`
		ItemsInstalled  []string  `json:"itemsInstalled"`
		ThemeLight      []string  `json:"themeLight"`
	} `json:"menu"`
}

const narrowPhone = `{"width":360,"height":780,"deviceScaleFactor":3,"mobile":true}`

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

// Words that cannot fit are cut inside their box, and only there.
var barCut = map[string]bool{"longleg": true}

// The app bar on a phone is one line of 48 px at 393 and at 360 alike, in
// every state of the link: nothing in it wraps and nothing is cut. The link is
// one chip, and the host name carries the menu with the theme and the install.
func TestTheAppBarIsOneLineInEveryState(t *testing.T) {
	for _, screen := range []struct{ name, spec string }{{"393", phoneScreen}, {"360", narrowPhone}} {
		t.Run(screen.name, func(t *testing.T) {
			var got appBar
			runFixtureOn(t, "appbar.html", screen.spec, phonePointer, &got)
			if len(got.Bars) != len(barWords) {
				t.Fatalf("the fixture measured %d states, the test knows %d: %v", len(got.Bars), len(barWords), got.Bars)
			}
			for name, want := range barWords {
				bar := got.Bars[name]
				if bar == nil || bar.Top == nil || bar.Row == nil || bar.Cbox == nil {
					t.Errorf("%s: the bar has no header, row or chip: %+v", name, bar)
					continue
				}
				if bar.Top.Height > 48 || bar.Row.Height > 48 {
					t.Errorf("%s: the bar is %v tall (row %v) — one line is 48 px, the rest belongs to the screen",
						name, bar.Top.Height, bar.Row.Height)
				}
				if bar.Wrapped {
					t.Errorf("%s: a piece of the bar left the line at %s px", name, screen.name)
				}
				if !bar.Caret {
					t.Errorf("%s: the caret of the host menu is cut off", name)
				}
				if bar.Cut != barCut[name] {
					t.Errorf("%s: the chip cuts its own words %q at %s px — the state is read by its end (cut: %v)", name, bar.Text, screen.name, bar.Cut)
				}
				if bar.Overflow {
					t.Errorf("%s: the page scrolls sideways at %s px", name, screen.name)
				}
				if bar.Text != want.text || bar.Tone != want.tone || bar.Dot != want.dot {
					t.Errorf("%s: the chip reads %q, tone %q, dot %q; want %q, %q, %q",
						name, bar.Text, bar.Tone, bar.Dot, want.text, want.tone, want.dot)
				}
				// The dot is hollow when there is no live link and filled
				// when there is one: the tone says how bad, the fill whether live.
				if hollow := bar.DotFill == "rgba(0, 0, 0, 0)"; bar.Dot != "" && hollow != (want.dot == "off") {
					t.Errorf("%s: the dot is filled with %q — hollow is for no live link, and only for that", name, bar.DotFill)
				}
				if bar.Cbox.Width > 163 {
					t.Errorf("%s: the chip is %v wide — past 163 it pushes the buttons off a 360 screen", name, bar.Cbox.Width)
				}
				if !strings.HasPrefix(bar.Label, "connection: ") || len(bar.Label) < len("connection: ")+len(want.text)/2 {
					t.Errorf("%s: a screen reader hears the chip as %q", name, bar.Label)
				}
				if bar.Chip.Height < 44 || bar.Chip.Width < 44 || bar.Host.Height < 44 {
					t.Errorf("%s: the chip is %vx%v and the host %vx%v — narrower than a finger",
						name, bar.Chip.Width, bar.Chip.Height, bar.Host.Width, bar.Host.Height)
				}
				labels := []string{}
				for _, b := range bar.Buttons {
					labels = append(labels, b.Label)
					if b.Width < 44 || b.Height < 44 {
						t.Errorf("%s: the %s button is %vx%v", name, b.Label, b.Width, b.Height)
					}
					if b.Right > got.Width+0.5 {
						t.Errorf("%s: the %s button ends at %v, past the screen", name, b.Label, b.Right)
					}
				}
				if strings.Join(labels, ",") != "alerts,search" {
					t.Errorf("%s: the bar ends with %v — alerts and search stay, the theme moved to the menu", name, labels)
				}
				if (name == "signedout") != (bar.SignIn == "/login") {
					t.Errorf("%s: the way to sign in is %q", name, bar.SignIn)
				}
				if name == "signedout" && (bar.SignInBox == nil || bar.SignInBox.Height < 44) {
					t.Errorf("signedout: Sign in is %+v — narrower than a finger", bar.SignInBox)
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
					t.Errorf("%s: the host flags the install: %v", name, bar.Flag)
				}
			}
			if got.Bars["install"].CaretTone == got.Bars["fresh"].CaretTone {
				t.Errorf("the caret is %s with the install waiting and without — the menu gives no sign of it", got.Bars["install"].CaretTone)
			}
			if got.Opened != 1 {
				t.Errorf("a tap on the chip opened the connection %d times", got.Opened)
			}
		})
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
		{"LAN", "ms", "atlas.example.net:8443", ""},
		{"tailscale", "no answer", "atlas.tail0000.ts.net", ""},
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
	if s.Retry == nil || s.Retry.Height < 44 {
		t.Errorf("Retry is %+v", s.Retry)
	}
	if s.Retried != 1 || s.Rechecked != 1 || s.Remeasured < 1 {
		t.Errorf("Retry asked for the data %d times, for the leg %d, pinged the legs %d times",
			s.Retried, s.Rechecked, s.Remeasured)
	}
}

// The host menu took the theme and the install from the bar: the install
// while the app is not installed, the theme as two words with the one in use
// pressed, and the pages that have no tab.
func TestTheHostMenuCarriesThemeAndInstall(t *testing.T) {
	var got appBar
	runFixture(t, "appbar.html", &got)
	m := got.Menu
	if strings.Join(m.Items, "|") != "Install the appnot installed yet|Settings|Devices|Journal|Usage|Sign out" {
		t.Errorf("the menu reads %q", m.Items)
	}
	if strings.Join(m.ItemsInstalled, "|") != "Settings|Devices|Journal|Usage|Sign out" {
		t.Errorf("an installed app still offers the install: %q", m.ItemsInstalled)
	}
	if strings.Join(m.Theme, ",") != "Dark:true,Light:false" || strings.Join(m.ThemeLight, ",") != "Dark:false,Light:true" {
		t.Errorf("the theme reads %v on dark and %v on light", m.Theme, m.ThemeLight)
	}
	for _, b := range m.ThemeBox {
		if b == nil || b.Height < 44 {
			t.Errorf("a theme button is %+v — narrower than a finger", b)
		}
	}
	if m.DarkOnDark != 0 || m.LightOnDark != 1 {
		t.Errorf("Dark on the dark theme switched it %d times, Light %d", m.DarkOnDark, m.LightOnDark-m.DarkOnDark)
	}
	if m.Installs != 1 || m.ClosedOnInstall != 1 {
		t.Errorf("Install ran %d times and closed the menu %d times", m.Installs, m.ClosedOnInstall)
	}
}
