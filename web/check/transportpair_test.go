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

// On the wide screen, where a project lives on the stream, a session on the
// stream has the feed alone: the terminal is tmux, reached by the move, and
// its tab opens the move with its question — it never moves the session by
// itself. A session in tmux moves to the stream from the session panel. The
// window on the host comes with a move to tmux as a choice in it; while a
// window is open nothing moves and the views only pick what to watch the
// session in tmux with. A turn in progress holds the move, and so does a message the session
// has not taken yet, and the panel says so in words.
func TestTheViewsReachTheOtherSideOnlyThroughTheMove(t *testing.T) {
	var got struct {
		StreamOn                                 string     `json:"streamOn"`
		TermMarked                               bool       `json:"termMarked"`
		WindowButtons                            int        `json:"windowButtons"`
		TermSentNothing                          bool       `json:"termSentNothing"`
		ToConsole                                pairSheet  `json:"toConsole"`
		WindowOff                                bool       `json:"windowOff"`
		WithWindow                               *pairSheet `json:"withWindow"`
		ConsoleOn                                string     `json:"consoleOn"`
		ToFeed                                   pairSheet  `json:"toFeed"`
		HeldTip                                  string     `json:"heldTip"`
		HeldTerm                                 string     `json:"heldTerm"`
		HeldOn                                   string     `json:"heldOn"`
		HeldSent                                 int        `json:"heldSent"`
		HeldFeed                                 bool       `json:"heldFeed"`
		HeldMarked                               bool       `json:"heldMarked"`
		BusyOff                                  bool       `json:"busyOff"`
		BusyWhy                                  string     `json:"busyWhy"`
		QueuedOff                                bool       `json:"queuedOff"`
		QueuedWhy                                string     `json:"queuedWhy"`
		QueuedWinOff                             bool       `json:"queuedWindowOff"`
		Radial, EscapeFocus, Rename, MountedOpen bool
		Place, Arrow, Tab, ShiftTab, RemotePlate string
		Opened                                   [][]string
		RemoteOff, RemoteOn                      *pairSent
		CloseWindow, OpenWindow, WithWork        pairSheet
		Stops                                    string
	}
	runWideFixture(t, "transportpair.html", &got)

	if got.StreamOn != "feed" || !got.TermMarked || got.WindowButtons != 0 {
		t.Errorf("a session on the stream shows %q, the terminal marked as a move %v, %d window buttons in the header",
			got.StreamOn, got.TermMarked, got.WindowButtons)
	}
	if !got.TermSentNothing {
		t.Error("the terminal tab of a session on the stream moved it by itself — it is meant to open the move")
	}
	if s := got.ToConsole.Sent; s == nil || s.Kind != "session.switch" || s.Target != "acme" ||
		s.Params["to"] != "console" || s.Params["window"] != nil {
		t.Errorf("the move of a session on the stream sent %+v (sheet %q)", s, got.ToConsole.Title)
	}
	if got.ToConsole.Title != "Move acme to tmux?" {
		t.Errorf("the move asks %q", got.ToConsole.Title)
	}
	if got.WindowOff || got.WithWindow == nil {
		t.Fatalf("the window of a move to tmux is off (%v) or missing", got.WindowOff)
	}
	if s := got.WithWindow.Sent; s == nil || s.Kind != "session.switch" || s.Params["to"] != "console" || s.Params["window"] != true {
		t.Errorf("the move with a window sent %+v — the window comes with the move", s)
	}
	if !strings.HasPrefix(got.WithWindow.Title, "Open a window to acme on ") ||
		!strings.Contains(got.WithWindow.Effect, "stays in tmux") {
		t.Errorf("the move with a window asks %q: %q", got.WithWindow.Title, got.WithWindow.Effect)
	}
	if got.ConsoleOn != "term" {
		t.Errorf("a session in tmux of a project on the stream shows %q", got.ConsoleOn)
	}
	if s := got.ToFeed.Sent; s == nil || s.Params["to"] != "stream" || got.ToFeed.Title != "Move acme-c to the stream?" {
		t.Errorf("the move of a session in tmux sent %+v (sheet %q)", s, got.ToFeed.Title)
	}
	if got.HeldTerm != "term" || got.HeldOn != "feed" || !got.HeldFeed || got.HeldSent != 0 || got.HeldMarked {
		t.Errorf("under an open window the views show %q then %q (feed drawn: %v, a tab marked as a move: %v) and sent %d requests",
			got.HeldTerm, got.HeldOn, got.HeldFeed, got.HeldMarked, got.HeldSent)
	}
	if !strings.Contains(got.HeldTip, "window") {
		t.Errorf("the feed under an open window does not say why nothing moves: %q", got.HeldTip)
	}
	if !got.BusyOff || !strings.Contains(got.BusyWhy, "answering") {
		t.Errorf("during a turn the move is off %v, saying %q", got.BusyOff, got.BusyWhy)
	}
	if !got.QueuedOff || !strings.Contains(got.QueuedWhy, "has not taken 1 message yet") {
		t.Errorf("with a message not taken yet the move is off %v, saying %q", got.QueuedOff, got.QueuedWhy)
	}
	if !got.QueuedWinOff {
		t.Error("with a message not taken yet the window of the move can still be picked")
	}
	if !got.Radial || !strings.Contains(got.Place, "On the stream") {
		t.Errorf("the session button did not open its radial menu at its current place: radial=%v, place=%q", got.Radial, got.Place)
	}
	if !got.EscapeFocus || got.Arrow != "role" || got.Tab != "intent" || got.ShiftTab != "role" {
		t.Errorf("the menu lost its keyboard navigation or opener focus: escape=%v, arrow=%q, Tab=%q, Shift+Tab=%q",
			got.EscapeFocus, got.Arrow, got.Tab, got.ShiftTab)
	}
	if len(got.Opened) != 1 || strings.Join(got.Opened[0], ",") != "https://claude.ai/code/session_123,_blank,noopener" ||
		!strings.Contains(got.RemotePlate, "https://claude.ai/code/session_123") {
		t.Errorf("Remote Control lost its link: opened %v, plate %q", got.Opened, got.RemotePlate)
	}
	for _, check := range []struct {
		sent   *pairSent
		target string
		on     bool
	}{
		{got.RemoteOff, "acme", false}, {got.RemoteOn, "acme-on", true},
	} {
		if check.sent == nil || check.sent.Kind != "session.remote" || check.sent.Target != check.target || check.sent.Params["on"] != check.on {
			t.Errorf("Remote Control of %s sent %+v, want on=%v", check.target, check.sent, check.on)
		}
	}
	if s := got.CloseWindow.Sent; s == nil || s.Kind != "window.close" || s.Target != "acme-w" {
		t.Errorf("closing the window sent %+v", s)
	}
	if s := got.OpenWindow.Sent; s == nil || s.Kind != "window.open" || s.Target != "acme-c" {
		t.Errorf("opening the window sent %+v", s)
	}
	if s := got.WithWork.Sent; s == nil || s.Kind != "session.switch" || s.Params["force"] != true || s.Params["stops"] != "1 background task" ||
		!strings.Contains(got.Stops, "Stops 1 background task") {
		t.Errorf("the move lost what it stops: sent %+v, plate %q", s, got.Stops)
	}

	if !got.MountedOpen {
		t.Error("session tools could not mount already open")
	}
	if !got.Rename {
		t.Error("Rename did not replace the pie with its sheet")
	}
}
