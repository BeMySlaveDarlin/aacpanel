package check

import (
	"strings"
	"testing"
)

type consoleViewSeen struct {
	Wide              bool     `json:"wide"`
	First             string   `json:"first"`
	Watch             []string `json:"watch"`
	Move              string   `json:"move"`
	Picked            string   `json:"picked"`
	PickSent          []string `json:"pickSent"`
	PickAsked         string   `json:"pickAsked"`
	PickPanel         bool     `json:"pickPanel"`
	Neighbour         string   `json:"neighbour"`
	Back              string   `json:"back"`
	Reloaded          string   `json:"reloaded"`
	ReloadedNeighbour string   `json:"reloadedNeighbour"`
	Stream            string   `json:"stream"`
	StreamWatch       []string `json:"streamWatch"`
	StreamTip         string   `json:"streamTip"`
	StreamMove        string   `json:"streamMove"`
	Sent              []string `json:"sent"`
}

// A session in tmux whose project lives on the stream is watched either way
// and stays in tmux, where whoever started it may be reading its pane: the
// pair of views only picks what to watch it with, and the move to the stream
// is a line in the tools of the session. What a session is watched with is its
// own choice on the device — another session in tmux opens by the width of the
// screen, and a reload brings each back the way it was left. A session on the
// stream keeps the feed alone, with the terminal behind the move to tmux.
func TestAConsoleIsWatchedAsAFeedWithoutMoving(t *testing.T) {
	for _, c := range []struct {
		screen      string
		run         func(*testing.T, string, any)
		first       string
		other       string
		watch       string
		streamWatch string
		streamTip   string
	}{
		{"on the wide screen", runWideFixture, "term", "feed",
			"Feed:false Terminal:true Files:false",
			"Feed:true Terminal:false:moves Files:false",
			"The session is on the stream: move it to tmux first"},
		{"on a phone", runFixture, "feed", "term",
			"Feed:true Terminal:false",
			"",
			""},
	} {
		t.Run(c.screen, func(t *testing.T) {
			var got consoleViewSeen
			c.run(t, "consoleview.html", &got)

			if got.First != c.first {
				t.Errorf("a session in tmux opens as %q, not by the width of the screen (%q)", got.First, c.first)
			}
			if w := strings.Join(got.Watch, " "); w != c.watch {
				t.Errorf("the pair of views of a session in tmux reads %q, expected %q — both views are a choice, neither a move", w, c.watch)
			}
			if got.Move != "Move to the stream" {
				t.Errorf("the tools of the session offer %q, not the move to the stream", got.Move)
			}
			if got.Picked != c.other {
				t.Errorf("picking %q shows %q", c.other, got.Picked)
			}
			if len(got.PickSent) > 0 || got.PickAsked != "" || got.PickPanel {
				t.Errorf("picking a view sent %v, asked %q and left the tools open %v — the session is meant to stay in tmux",
					got.PickSent, got.PickAsked, got.PickPanel)
			}
			if got.Neighbour != c.first || got.ReloadedNeighbour != c.first {
				t.Errorf("another session in tmux opens as %q, after a reload as %q — the pick of one session is not the other's (%q)",
					got.Neighbour, got.ReloadedNeighbour, c.first)
			}
			if got.Back != c.other {
				t.Errorf("back on the session the pick was made in, it shows %q, not %q", got.Back, c.other)
			}
			if got.Reloaded != c.other {
				t.Errorf("after a reload the session in tmux opens as %q, not as it was left (%q)", got.Reloaded, c.other)
			}

			if got.Stream != "feed" {
				t.Errorf("a session on the stream opens as %q", got.Stream)
			}
			if w := strings.Join(got.StreamWatch, " "); w != c.streamWatch {
				t.Errorf("the pair of views of a session on the stream reads %q, expected %q", w, c.streamWatch)
			}
			if got.StreamTip != c.streamTip {
				t.Errorf("the terminal of a session on the stream says %q, expected %q", got.StreamTip, c.streamTip)
			}
			if got.StreamMove != "Move to tmux" {
				t.Errorf("the tools of a session on the stream offer %q, not the move to tmux", got.StreamMove)
			}
			if len(got.Sent) > 0 {
				t.Errorf("reading the views and the tools sent %v to the panel", got.Sent)
			}
		})
	}
}
