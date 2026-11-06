package check

import (
	"strings"
	"testing"
)

type streamCardBell struct {
	Title string `json:"title"`
	Icon  bool   `json:"icon"`
}

type streamCardRun struct {
	Cards []struct {
		Name        string `json:"name"`
		Mark        string `json:"mark"`
		State       string `json:"state"`
		Since       string `json:"since"`
		Agent       string `json:"agent"`
		AgentKey    string `json:"agentKey"`
		Work        string `json:"work"`
		TapPct      string `json:"tapPct"`
		TapTag      string `json:"tapTag"`
		TapRC       string `json:"tapRC"`
		RC          string `json:"rc"`
		RCElsewhere bool   `json:"rcElsewhere"`
	} `json:"cards"`
	Called struct {
		Before  map[string]streamCardBell `json:"before"`
		Visited map[string]streamCardBell `json:"visited"`
		Again   map[string]streamCardBell `json:"again"`
	} `json:"called"`
}

// The phone's card says the name, place, state, model and background work:
// where it lives and Remote Control in their places on the line of its
// settings, what runs counted after the state. Tapping the place, percentage
// or Remote Control opens the conversation just as tapping the words does.
func TestAStreamSessionIsMarkedOnItsCard(t *testing.T) {
	var run streamCardRun
	runFixture(t, "streamcard.html", &run)
	got := run.Cards
	if len(got) != 3 {
		t.Fatalf("%d cards, wanted 3: %+v", len(got), got)
	}
	for _, c := range got {
		session := c.Name
		if !strings.HasPrefix(session, "acme") && session != "aacpanel" && session != "person" {
			t.Fatalf("an unknown block %q", session)
		}
		stream := session != "aacpanel"
		remote := strings.HasPrefix(session, "acme")
		want := map[bool]string{true: "stream", false: "tmux"}[stream]
		if c.Mark != want {
			t.Errorf("%s: the row does not say %q: %+v", session, want, c)
		}
		if remote && c.RC != "RC" {
			t.Errorf("%s: Remote Control is up and the state's line does not say so: %+v", session, c)
		}
		if !remote && c.RC != "" {
			t.Errorf("%s: the row says Remote Control is up, and it is not", session)
		}
		if c.RCElsewhere {
			t.Errorf("%s: Remote Control is said somewhere else on the card than its place among the settings", session)
		}
		if c.TapPct != "open:"+session || c.TapTag != "open:"+session || (remote && c.TapRC != "open:"+session) {
			t.Errorf("%s: a tap on the percentage opens %q, on where it lives %q, on Remote Control %q — the whole row opens the conversation",
				session, c.TapPct, c.TapTag, c.TapRC)
		}
		// The session that called waits for the person until its feed has
		// been on screen; what runs is counted after the state, a command
		// and an observer, and an alarm runs nothing.
		wantState, wantSince, wantWork := "idle", "Claude Opus 5.5 Extra", ""
		switch {
		case session == "aacpanel":
			wantState, wantWork = "called you", "1 background commands running, 1 observers watching"
		case session == "person":
			wantState = "no replies yet"
		}
		if c.State != wantState {
			t.Errorf("%s: the state reads %q, expected %q", session, c.State, wantState)
		}
		if c.Since != wantSince {
			t.Errorf("%s: under the state stands %q, expected %q", session, c.Since, wantSince)
		}
		if c.Work != wantWork {
			t.Errorf("%s: the line of what runs in the background reads %q, expected %q", session, c.Work, wantWork)
		}
		if c.Agent != "Claude" || c.AgentKey != "claude" {
			t.Errorf("%s: the line under the state opens with %q painted as %q's, expected the word of its agent, Claude", session, c.Agent, c.AgentKey)
		}
	}
}

// A session that called the person waits on its card until the person opens
// it, its glyph a bell: the card of a session that never called says nothing
// of it, the device that opened the feed takes the call off without waiting
// for the host to say so, and a call made after the visit brings it back.
func TestACallMarksTheCardUntilTheSessionIsOpened(t *testing.T) {
	var run streamCardRun
	runFixture(t, "streamcard.html", &run)
	got := run.Called
	bell, ok := got.Before["aacpanel"]
	if len(got.Before) != 1 || !ok {
		t.Fatalf("the bells before the visit stand on %v, wanted the one session that called", got.Before)
	}
	if !bell.Icon || bell.Title != "called you · 2m" {
		t.Errorf("the call of the session is %+v: a glyph on its card, and the corner says when it called", bell)
	}
	if len(got.Visited) != 0 {
		t.Errorf("the feed of the session was opened and closed, and the list still rings: %v", got.Visited)
	}
	if _, ok := got.Again["aacpanel"]; !ok || len(got.Again) != 1 {
		t.Errorf("a call after the visit marks %v, wanted the session that called again", got.Again)
	}
}
