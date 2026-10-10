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
