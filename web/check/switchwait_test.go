package check

import (
	"os"
	"strings"
	"testing"
)

type switchFrame struct {
	Wait        string `json:"wait"`
	Head        string `json:"head"`
	Feed        bool   `json:"feed"`
	Composer    bool   `json:"composer"`
	Term        bool   `json:"term"`
	Detached    bool   `json:"detached"`
	TermStreams int    `json:"termStreams"`
}

type switchWaitSeen struct {
	Before        switchFrame `json:"before"`
	Pressed       switchFrame `json:"pressed"`
	Gone          switchFrame `json:"gone"`
	Back          switchFrame `json:"back"`
	Opening       switchFrame `json:"opening"`
	Ready         switchFrame `json:"ready"`
	ReadyLive     bool        `json:"readyLive"`
	RefusedDuring switchFrame `json:"refusedDuring"`
	Refused       switchFrame `json:"refused"`
}

// A session moving between the feed and the console shows the move from the
// press until the other side is up: not the feed closing under the person,
// not a conversation with no session, not a terminal that says it detached
// before it ever attached.
func TestAMoveBetweenSidesShowsTheMoveUntilTheOtherSideIsUp(t *testing.T) {
	if _, err := os.Stat(webPath("dist/term.js")); err != nil {
		t.Skip("web/dist/term.js is not built: the terminal of the frames is the real one — run make front first")
	}
	var got switchWaitSeen
	runFixture(t, "switchwait.html", &got)

	if !got.Before.Feed || !got.Before.Composer || got.Before.Wait != "" {
		t.Fatalf("before the press the session on the stream does not show its feed: %+v — the check looks in the wrong place", got.Before)
	}
	moving := func(name string, f switchFrame) {
		t.Helper()
		if !strings.Contains(f.Wait, "Moving to the console") {
			t.Errorf("%s: the screen does not say the session is moving to the console (%q)", name, f.Wait)
		}
		if f.Feed || f.Composer || f.Term {
			t.Errorf("%s: a view stands beside the move — feed %v, composer %v, terminal %v", name, f.Feed, f.Composer, f.Term)
		}
	}
	moving("pressed", got.Pressed)
	if got.Gone.Head != "moving" {
		t.Errorf("gone from the snapshot mid-move, the header says %q — the move reads as the end of the session", got.Gone.Head)
	}
	moving("gone from the snapshot", got.Gone)
	moving("back in the console, its views not yet known", got.Back)
	if got.Back.TermStreams != 0 {
		t.Errorf("the terminal was attached %d times before the panel said the console shows it", got.Back.TermStreams)
	}

	if !got.Opening.Term || !strings.Contains(got.Opening.Wait, "Opening the terminal") {
		t.Errorf("the console came up but the terminal is not opening with a spinner: %+v", got.Opening)
	}
	if got.Opening.Detached {
		t.Error("the terminal says it detached before the stream said anything — an error in place of the opening")
	}
	if !got.ReadyLive || got.Ready.Detached {
		t.Errorf("the terminal did not come up on ready: live %v, detached %v", got.ReadyLive, got.Ready.Detached)
	}

	if !strings.Contains(got.RefusedDuring.Wait, "Moving to the console") {
		t.Fatalf("the second press does not show the move (%q): the refusal below proves nothing", got.RefusedDuring.Wait)
	}
	if !got.Refused.Feed || got.Refused.Wait != "" {
		t.Errorf("a refused move leaves the move on the screen: %+v", got.Refused)
	}
}
