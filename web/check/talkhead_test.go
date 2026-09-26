package check

import (
	"strings"
	"testing"
)

type talkBox struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
}

// talkOff is how the header reads a session with the link gone.
type talkOff struct {
	Word    string `json:"word"`
	Dot     string `json:"dot"`
	Pct     string `json:"pct"`
	Desk    string `json:"desk"`
	DeskDot string `json:"deskDot"`
}

// The header of a conversation on a phone is one line: the way back, the name
// with how the session stands under it, and one button the tools of the
// session live behind. A name too long for the line keeps its tail and loses
// its middle rather than running; the tools open in a sheet a line each, with
// words, and a pick of what to watch puts the sheet down.
func TestThePhoneHeaderIsOneLineWithTheToolsBehindIt(t *testing.T) {
	var got struct {
		Head         *talkBox `json:"head"`
		Back         *talkBox `json:"back"`
		More         *talkBox `json:"more"`
		Buttons      int      `json:"buttons"`
		OldTools     int      `json:"oldTools"`
		Running      int      `json:"running"`
		Dot          string   `json:"dot"`
		Overlay      bool     `json:"overlay"`
		BusyWord     string   `json:"busyWord"`
		Offline      talkOff  `json:"offline"`
		Word         string   `json:"word"`
		Label        string   `json:"label"`
		HeadCut      bool     `json:"headCut"`
		Tail         string   `json:"tail"`
		TailWhole    bool     `json:"tailWhole"`
		Lines        []string `json:"lines"`
		Title        string   `json:"title"`
		Where        string   `json:"where"`
		Segs         []string `json:"segs"`
		Small        []string `json:"small"`
		ClosedOnPick bool     `json:"closedOnPick"`
		FeedGone     bool     `json:"feedGone"`
		MoreStays    bool     `json:"moreStays"`
	}
	runFixture(t, "talkhead.html", &got)

	if got.Head == nil || got.More == nil || got.Back == nil {
		t.Fatalf("the header has no way back or no button for the tools: %+v", got)
	}
	if got.Head.Height > 60 {
		t.Errorf("the header is %v tall — one line of who and how is meant to leave the run the room", got.Head.Height)
	}
	if got.OldTools != 0 || got.Buttons != 2 {
		t.Errorf("the header still carries %d tools of its own among %d buttons — they live behind the one button",
			got.OldTools, got.Buttons)
	}
	for _, b := range []struct {
		name string
		it   *talkBox
	}{{"the way back", got.Back}, {"the tools", got.More}} {
		if b.it.Width < 44 || b.it.Height < 44 {
			t.Errorf("%s is %vx%v — narrower than a finger", b.name, b.it.Width, b.it.Height)
		}
	}
	if got.Running != 0 {
		t.Error("the name runs as a marquee again: a line in motion at the top of the screen pulls the eye all night")
	}
	if got.Overlay {
		t.Error("a piece of the header spreads over the screen: a class of the header took the rules of " +
			"another screen's class of the same name")
	}
	if got.BusyWord != "answering" {
		t.Errorf("a session at work says %q", got.BusyWord)
	}
	// Without a link the state is a snapshot's: dated, not said as now. The
	// chip of the app bar already says the link is gone; the line does not
	// repeat it, it says what the state is true as of.
	if off := got.Offline; off.Word != "as of 22:41" || off.Dot != "off" || !strings.HasPrefix(off.Pct, "12.4") {
		t.Errorf("a session seen without a link reads %q with its dot %q and context %q — meant 12.4%% · as of 22:41, hollow",
			off.Word, off.Dot, off.Pct)
	}
	if off := got.Offline; off.Desk != "as of 22:41" || !strings.Contains(off.DeskDot, "dkoff") {
		t.Errorf("the wide header without a link reads %q with its dot %q", off.Desk, off.DeskDot)
	}
	if got.Dot != "waiting" || got.Word != "waiting for you" {
		t.Errorf("a session waiting on a question stands as %q, saying %q", got.Dot, got.Word)
	}
	if got.Label != "evirma-fingerprint-rotation-review-2" {
		t.Errorf("the name reads %q to a screen reader — it is meant whole", got.Label)
	}
	if !got.HeadCut || got.Tail != "-review-2" || !got.TailWhole {
		t.Errorf("a long name is not cut in the middle: head cut %v, tail %q shown whole %v",
			got.HeadCut, got.Tail, got.TailWhole)
	}

	want := []string{"Watch with", "Window on", "Remote Control"}
	joined := strings.Join(got.Lines, " | ")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("the tools sheet has no line %q: %s", w, joined)
		}
	}
	if !strings.Contains(joined, "Files of the project") {
		t.Errorf("the files of the project are not among the tools: %s", joined)
	}
	if got.Title != "evirma-fingerprint-rotation-review-2" || got.Where != "/srv/proj/panel" {
		t.Errorf("the sheet opens on %q at %q — the name whole and where the session works", got.Title, got.Where)
	}
	if strings.Join(got.Segs, " ") != "Feed:true Terminal:false" {
		t.Errorf("what to watch with reads %v — worded, the feed picked on a phone", got.Segs)
	}
	if len(got.Small) > 0 {
		t.Errorf("buttons of the sheet shorter than a finger: %v", got.Small)
	}
	if !got.ClosedOnPick || !got.FeedGone || !got.MoreStays {
		t.Errorf("picking the terminal: sheet put down %v, the feed gave way %v, the tools still at hand %v",
			got.ClosedOnPick, got.FeedGone, got.MoreStays)
	}
}
