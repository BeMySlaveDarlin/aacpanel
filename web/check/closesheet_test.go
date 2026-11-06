package check

import (
	"strings"
	"testing"
)

// The sheet of a close tells what follows by whom it closes. A claude session
// is a process: the host waits for its transcript, and a kill stands under the
// sheet for the one that does not end. A codex thread is the daemon's: its
// turn breaks off and the panel lets it go, codex resume brings it back, and
// there is nothing to kill — the host refuses a kill of codex, so the sheet
// does not offer one. A name the host does not list is closed as a claude:
// the panel knows no better of it.
func TestTheSheetOfACloseSpeaksForWhomItCloses(t *testing.T) {
	type sheet struct {
		gateSheet
		Answered  bool       `json:"answered"`
		Cancelled bool       `json:"cancelled"`
		Then      *gateSheet `json:"then"`
	}
	var got struct {
		Codex  sheet `json:"codex"`
		Claude sheet `json:"claude"`
		Stray  sheet `json:"stray"`
		Sent   int   `json:"sent"`
	}
	runFixture(t, "closesheet.html", &got)

	const name = "codex-5afc361b"
	codex := got.Codex
	if codex.Trouble != "" {
		t.Fatal(codex.Trouble)
	}
	if codex.Title != "Close session "+name+"?" {
		t.Errorf("the sheet of the codex close is titled %q", codex.Title)
	}
	for _, want := range []string{"turn codex is running breaks off", "lets the thread go", "codex resume", "tmux"} {
		if !strings.Contains(codex.Effect, want) {
			t.Errorf("the sheet of the codex close says %q: %q is missing", codex.Effect, want)
		}
	}
	if strings.Contains(codex.Effect, "transcript") {
		t.Errorf("the sheet of the codex close promises to wait for a transcript: %q", codex.Effect)
	}
	if codex.OK != "Close" || len(codex.Harsher) != 0 {
		t.Errorf("the sheet of the codex close presses %q with %v under it, expected Close and no kill", codex.OK, codex.Harsher)
	}
	if !codex.Answered || len(codex.Sent) != 1 || codex.Sent[0].Kind != "session.close" || codex.Sent[0].Target != name {
		t.Errorf("the press of the codex close sent %+v (answered %v), expected session.close of %s",
			codex.Sent, codex.Answered, name)
	}

	for _, c := range []struct {
		who   string
		sheet sheet
	}{{"the claude session", got.Claude}, {"a name the host does not list", got.Stray}} {
		s := c.sheet
		if s.Trouble != "" {
			t.Errorf("%s: %s", c.who, s.Trouble)
			continue
		}
		if !strings.Contains(s.Effect, "15 seconds") || !strings.Contains(s.Effect, "transcript") {
			t.Errorf("the sheet of the close of %s says %q, expected the wait for the transcript", c.who, s.Effect)
		}
		if strings.Contains(s.Effect, "codex") {
			t.Errorf("the sheet of the close of %s speaks of codex: %q", c.who, s.Effect)
		}
		if s.OK != "Close gently" || strings.Join(s.Harsher, ",") != "Kill right now (kill -9)" {
			t.Errorf("the sheet of the close of %s presses %q with %v under it, expected Close gently and the kill",
				c.who, s.OK, s.Harsher)
		}
	}
	claude := got.Claude
	if claude.Then == nil || claude.Then.Title != "Kill shop without warning?" || claude.Then.OK != "Kill" {
		t.Errorf("the kill under the claude close led to %+v, expected the sheet of the kill", claude.Then)
	}
	if !claude.Cancelled || got.Sent != 1 {
		t.Errorf("the claude close was let go (%v), and %d requests went out in all: only the codex close presses",
			claude.Cancelled, got.Sent)
	}
}

// The application hands the registry the live sessions of every answer of
// the host: without them the sheet of a close takes every codex thread for a
// claude, and offers to kill what the host refuses to.
func TestTheApplicationTellsTheCloseWhomItCloses(t *testing.T) {
	type says struct {
		Effect  string `json:"effect"`
		Harsher string `json:"harsher"`
	}
	var got struct {
		Before   says `json:"before"`
		Answered int  `json:"answered"`
		Codex    says `json:"codex"`
		Claude   says `json:"claude"`
	}
	runFixture(t, "appsessions.html", &got)

	if strings.Contains(got.Before.Effect, "codex") || got.Before.Harsher != "session.kill" {
		t.Errorf("before the host answered, the close of the thread reads %+v: the registry knew it already", got.Before)
	}
	if got.Answered == 0 {
		t.Fatal("the application never asked the host")
	}
	if !strings.Contains(got.Codex.Effect, "codex resume") || got.Codex.Harsher != "" {
		t.Errorf("after the host answered, the close of the codex thread reads %+v: the application did not hand "+
			"the registry the live sessions", got.Codex)
	}
	if strings.Contains(got.Claude.Effect, "codex") || got.Claude.Harsher != "session.kill" {
		t.Errorf("the close of the claude session reads %+v", got.Claude)
	}
}

// layersSeen is how a page drew its scrims, sheets and toasts: every one of
// them while all were closed, the sheet that came up and its scrim, the toast
// that followed, and every scrim and sheet once the sheet was shut again.
type layersSeen struct {
	Closed map[string][]string `json:"closed"`
	Open   map[string]string   `json:"open"`
	Toast  string              `json:"toast"`
	Shut   map[string][]string `json:"shut"`
}

// A scrim, a sheet and a toast stay in the page while closed, and a closed one
// is not drawn at all: a transparent layer of frosted glass is still blurred
// again on every frame anything under it moves, a pulse of the work or a chart
// of the home. An open one is drawn, on a phone and at a desk alike.
func TestAClosedLayerIsNotDrawn(t *testing.T) {
	var phone struct {
		Layers layersSeen `json:"layers"`
	}
	runFixture(t, "closesheet.html", &phone)
	desk := ctxMenuPage(t)
	for _, c := range []struct {
		where string
		seen  layersSeen
	}{{"on a phone", phone.Layers}, {"at a desk", desk.Layers}} {
		for _, kind := range []string{"scrim", "sheet", "toast"} {
			drawn := c.seen.Closed[kind]
			if len(drawn) == 0 {
				t.Errorf("%s the page has no %s at all — the case proves nothing", c.where, kind)
			}
			for n, v := range drawn {
				if v != "hidden" {
					t.Errorf("%s %s %d of %d is %q while closed — its glass is blurred again on every frame", c.where, kind, n+1, len(drawn), v)
				}
			}
		}
		if c.seen.Open["sheet"] != "visible" || c.seen.Open["scrim"] != "visible" {
			t.Errorf("%s the open sheet and its scrim are drawn as %+v — a sheet that came up is not seen", c.where, c.seen.Open)
		}
		if c.seen.Toast != "visible" {
			t.Errorf("%s the toast that came up is %q — the note of the action is not seen", c.where, c.seen.Toast)
		}
		for _, kind := range []string{"scrim", "sheet"} {
			for n, v := range c.seen.Shut[kind] {
				if v != "hidden" {
					t.Errorf("%s %s %d is %q once the sheet was shut — it is drawn again after its fade", c.where, kind, n+1, v)
				}
			}
		}
	}
}
