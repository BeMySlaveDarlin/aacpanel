package check

import (
	"strings"
	"testing"
)

type pairSent struct {
	Kind   string         `json:"kind"`
	Target string         `json:"target"`
	Params map[string]any `json:"params"`
}

type pairSheet struct {
	Title  string    `json:"title"`
	Effect string    `json:"effect"`
	Sent   *pairSent `json:"sent"`
}

// Where a project lives in the feed, the pair of views is the pair of sides:
// the feed is the session on the stream, the terminal is the console, and the
// other one moves the session there. The window on the host takes a session
// in the feed to the console and holds it: while it is open the pair only
// picks what to watch the console with. A turn in progress holds the move, and
// so does a message the session has not taken yet.
func TestThePairOfViewsMovesTheSessionBetweenSides(t *testing.T) {
	var got struct {
		StreamOn      string     `json:"streamOn"`
		WindowButtons int        `json:"windowButtons"`
		ToConsole     pairSheet  `json:"toConsole"`
		WindowOff     bool       `json:"windowOff"`
		WithWindow    *pairSheet `json:"withWindow"`
		ConsoleOn     string     `json:"consoleOn"`
		ToFeed        pairSheet  `json:"toFeed"`
		HeldTip       string     `json:"heldTip"`
		HeldTerm      string     `json:"heldTerm"`
		HeldOn        string     `json:"heldOn"`
		HeldSent      int        `json:"heldSent"`
		HeldFeed      bool       `json:"heldFeed"`
		BusyOff       bool       `json:"busyOff"`
		BusyWhy       string     `json:"busyWhy"`
		QueuedOff     bool       `json:"queuedOff"`
		QueuedWhy     string     `json:"queuedWhy"`
		QueuedWinOff  bool       `json:"queuedWindowOff"`
		QueuedWinWhy  string     `json:"queuedWindowWhy"`
	}
	runFixture(t, "transportpair.html", &got)

	if got.StreamOn != "feed" || got.WindowButtons != 1 {
		t.Errorf("a session on the stream shows %q, with %d buttons beside the pair — the pair is the switch now",
			got.StreamOn, got.WindowButtons)
	}
	if s := got.ToConsole.Sent; s == nil || s.Kind != "session.switch" || s.Target != "evirma" ||
		s.Params["to"] != "console" || s.Params["window"] != nil {
		t.Errorf("the terminal of a session on the stream sent %+v (sheet %q)", s, got.ToConsole.Title)
	}
	if got.ToConsole.Title != "Move evirma to the console?" {
		t.Errorf("the terminal asks %q", got.ToConsole.Title)
	}
	if got.WindowOff || got.WithWindow == nil {
		t.Fatalf("the window of a session on the stream is off (%v) or missing", got.WindowOff)
	}
	if s := got.WithWindow.Sent; s == nil || s.Kind != "session.switch" || s.Params["to"] != "console" || s.Params["window"] != true {
		t.Errorf("the window of a session on the stream sent %+v — it takes the session to the console with it", s)
	}
	if !strings.HasPrefix(got.WithWindow.Title, "Open a window to evirma on ") ||
		!strings.Contains(got.WithWindow.Effect, "console") {
		t.Errorf("the window asks %q: %q", got.WithWindow.Title, got.WithWindow.Effect)
	}
	if got.ConsoleOn != "term" {
		t.Errorf("a console of a project in the feed shows %q", got.ConsoleOn)
	}
	if s := got.ToFeed.Sent; s == nil || s.Params["to"] != "stream" || got.ToFeed.Title != "Move evirma-c to the feed?" {
		t.Errorf("the feed of a console sent %+v (sheet %q)", s, got.ToFeed.Title)
	}
	if got.HeldTerm != "term" || got.HeldOn != "feed" || !got.HeldFeed || got.HeldSent != 0 {
		t.Errorf("under an open window the pair shows %q then %q (feed drawn: %v) and sent %d requests",
			got.HeldTerm, got.HeldOn, got.HeldFeed, got.HeldSent)
	}
	if !strings.Contains(got.HeldTip, "window") {
		t.Errorf("the feed under an open window does not say why nothing moves: %q", got.HeldTip)
	}
	if !got.BusyOff || !strings.Contains(got.BusyWhy, "answering") {
		t.Errorf("during a turn the feed is off %v, saying %q", got.BusyOff, got.BusyWhy)
	}
	if !got.QueuedOff || !strings.Contains(got.QueuedWhy, "has not taken 1 message yet") {
		t.Errorf("with a message not taken yet the terminal is off %v, saying %q", got.QueuedOff, got.QueuedWhy)
	}
	if !got.QueuedWinOff || !strings.Contains(got.QueuedWinWhy, "has not taken 1 message yet") {
		t.Errorf("with a message not taken yet the window is off %v, saying %q", got.QueuedWinOff, got.QueuedWinWhy)
	}
}
