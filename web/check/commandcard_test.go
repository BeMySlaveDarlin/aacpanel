package check

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

type commandCardShot struct {
	Before struct {
		Tag      string   `json:"tag"`
		Title    string   `json:"title"`
		Figure   string   `json:"figure"`
		Height   int      `json:"height"`
		Bar      []string `json:"bar"`
		Messages float64  `json:"messages"`
		Coloured bool     `json:"coloured"`
		FeedText bool     `json:"feedText"`
	} `json:"before"`
	Opened []string `json:"opened"`
	Found  bool     `json:"found"`
	Inside struct {
		Head       string   `json:"head"`
		Sub        string   `json:"sub"`
		Cats       []string `json:"cats"`
		Pcts       []string `json:"pcts"`
		Later      []string `json:"later"`
		Parts      []string `json:"parts"`
		RowsClosed int      `json:"rowsClosed"`
	} `json:"inside"`
	Servers      []string `json:"servers"`
	ServerTokens []string `json:"serverTokens"`
	Skills       []string `json:"skills"`
}

// The answer of /context is a grid of glyphs in a terminal and a table as long
// as the list of skills elsewhere; laid into the feed as text it was a
// paragraph of glyphs. It is one line there now: how full the window is, and
// a bar of what fills it.
func TestTheContextAnswerIsOneCardInTheFeed(t *testing.T) {
	var got commandCardShot
	runFixture(t, "commandcard.html", &got)

	b := got.Before
	if b.Title != "Context window" {
		t.Errorf("the card is titled %q", b.Title)
	}
	if b.Figure != "259k / 1m (26%)" {
		t.Errorf("the card says %q about how full the window is", b.Figure)
	}
	if b.Height > 90 {
		t.Errorf("the card is %dpx tall: it is a line of the feed, not the breakdown", b.Height)
	}
	if b.FeedText {
		t.Errorf("a table of the answer reached the feed as text")
	}
	if b.Tag != "BUTTON" {
		t.Errorf("the card is a %s while it opens the breakdown", b.Tag)
	}
}

// The bar is the window as it fills: what is used from the left, the free
// space as the track, the buffer held for compaction at the far end. What is
// loaded on demand takes no room until it is loaded, and a bar that drew it
// would say the window is fuller than it is.
func TestTheContextBarDrawsOnlyWhatTakesRoom(t *testing.T) {
	var got commandCardShot
	runFixture(t, "commandcard.html", &got)

	want := []string{"t-system", "t-tools", "t-agents", "t-memory", "t-skills", "t-messages", "track", "t-buffer"}
	if !reflect.DeepEqual(got.Before.Bar, want) {
		t.Errorf("the bar is drawn as %v, want %v", got.Before.Bar, want)
	}
	// Messages are 205830 of a million.
	if math.Abs(got.Before.Messages-0.206) > 0.01 {
		t.Errorf("messages take %.3f of the bar, want about 0.206 of it", got.Before.Messages)
	}
	if !got.Before.Coloured {
		t.Errorf("a piece of the bar has no colour of its own")
	}
}

// The card opens the breakdown: every part of the window with its share, what
// waits to be loaded kept apart from it, and the long lists folded to a count
// until asked for.
func TestTheContextCardOpensTheBreakdown(t *testing.T) {
	var got commandCardShot
	runFixture(t, "commandcard.html", &got)

	if !reflect.DeepEqual(got.Opened, []string{"context"}) || !got.Found {
		t.Fatalf("the tap opened %v, sheet found %v", got.Opened, got.Found)
	}
	in := got.Inside
	if in.Head != "Context window" || !strings.Contains(in.Sub, "259k of 1m tokens") ||
		!strings.Contains(in.Sub, "opus-5-5[1m]") {
		t.Errorf("the sheet opens as %q / %q", in.Head, in.Sub)
	}
	cats := []string{"System prompt", "System tools", "Custom agents", "Memory files", "Skills",
		"Messages", "Autocompact buffer", "Free space"}
	if !reflect.DeepEqual(in.Cats, cats) {
		t.Errorf("the window is broken into %v, want %v", in.Cats, cats)
	}
	if len(in.Pcts) != len(cats) || in.Pcts[5] != "21%" || in.Pcts[0] != "0.4%" {
		t.Errorf("the shares read %v", in.Pcts)
	}
	if !reflect.DeepEqual(in.Later, []string{"MCP tools", "System tools"}) {
		t.Errorf("what is loaded on demand reads %v", in.Later)
	}
	if !reflect.DeepEqual(in.Parts, []string{"MCP servers", "Custom agents", "Skills"}) {
		t.Errorf("the lists are %v", in.Parts)
	}
	if in.RowsClosed != 0 {
		t.Errorf("%d rows of the lists are open before anyone asked", in.RowsClosed)
	}
	if len(got.Servers) != 5 || got.Servers[0] != "claude_ai_Gmail" || got.ServerTokens[0] != "23k" {
		t.Errorf("the servers open as %v %v", got.Servers, got.ServerTokens)
	}
	// The largest first: a person opens the list to see what costs the most.
	if len(got.Skills) != 8 || got.Skills[0] != "brief" || got.Skills[7] != "anthropic-skills:xlsx" {
		t.Errorf("the skills open as %v", got.Skills)
	}
}
