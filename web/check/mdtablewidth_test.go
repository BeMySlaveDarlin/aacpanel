package check

import "testing"

// A table is the one block of an answer that can be wider than the screen it
// is read on. It scrolls inside its own box; what it must never do is take the
// feed with it, because then the whole conversation slides sideways under the
// finger and the composer goes off the edge with it.
func TestAFeedTableStaysInsideTheScreen(t *testing.T) {
	var got struct {
		Screen     float64 `json:"screen"`
		Feed       float64 `json:"feed"`
		FeedScroll float64 `json:"feedScroll"`
		FeedClient float64 `json:"feedClient"`
		PageScroll float64 `json:"pageScroll"`
		Narrow     table   `json:"narrow"`
		Longcell   table   `json:"longcell"`
		Wide       table   `json:"wide"`
		Mine       table   `json:"mine"`
		Sheet      *struct {
			Body       float64 `json:"body"`
			BodyScroll int     `json:"bodyScroll"`
			BodyClient int     `json:"bodyClient"`
			Bubble     float64 `json:"bubble"`
			Box        float64 `json:"box"`
			BoxScroll  int     `json:"boxScroll"`
			BoxClient  int     `json:"boxClient"`
		} `json:"sheet"`
	}
	runFixture(t, "mdtable.html", &got)

	if got.FeedScroll > got.FeedClient {
		t.Errorf("the feed scrolls sideways: %v wide against %v of screen — a table pushed the conversation out of the window",
			got.FeedScroll, got.FeedClient)
	}
	if got.PageScroll > got.Screen {
		t.Errorf("the page itself scrolls sideways: %v against a screen of %v", got.PageScroll, got.Screen)
	}
	t.Logf("screen %v feed %v (scroll %v) | narrow %+v | wide %+v | mine %+v",
		got.Screen, got.Feed, got.FeedScroll, got.Narrow, got.Wide, got.Mine)
	for _, c := range []struct {
		name string
		rows int
		it   table
	}{{"a table a phone fits", 2, got.Narrow}, {"a table with a path in one cell", 2, got.Longcell},
		{"a table no phone fits", 3, got.Wide}, {"the same table in my own bubble", 3, got.Mine}} {
		if c.it.Bubble > got.Feed {
			t.Errorf("%s: the bubble is %v wide inside a feed of %v — the table stretched the message",
				c.name, c.it.Bubble, got.Feed)
		}
		if c.it.Box > c.it.Bubble {
			t.Errorf("%s: the scrolling box is %v wide inside a bubble of %v", c.name, c.it.Box, c.it.Bubble)
		}
		if c.it.Rows != c.rows {
			t.Errorf("%s: %d rows reached the screen, expected %d", c.name, c.it.Rows, c.rows)
		}
	}
	// The same table in a sheet. A sheet is the other place a table is read in
	// — an answer of a subagent, a reading, a brief — and it is narrower than
	// the feed, so a table that fits one can still push the other open.
	if got.Sheet == nil {
		t.Fatal("the fixture found no sheet: the second place a table is read in went unchecked")
	}
	if got.Sheet.BodyScroll > got.Sheet.BodyClient {
		t.Errorf("the sheet scrolls sideways: %d against %d — the table opened it wider than the screen",
			got.Sheet.BodyScroll, got.Sheet.BodyClient)
	}
	if got.Sheet.Box > got.Sheet.Bubble+1 {
		t.Errorf("in the sheet the scrolling box is %v wide inside a bubble of %v", got.Sheet.Box, got.Sheet.Bubble)
	}

	if got.Narrow.BoxScroll > got.Narrow.BoxClient {
		t.Errorf("a table of two short columns is being scrolled sideways (%d against %d): that is a table the phone fits",
			got.Narrow.BoxScroll, got.Narrow.BoxClient)
	}

	// The wrapping itself. A cell of prose is given a ceiling and wraps at it;
	// without the ceiling one sentence sets the width of the table, and the
	// reader scrolls a screen and a half to the right to read a word.
	for _, c := range []struct {
		name string
		it   table
	}{{"in an answer", got.Wide}, {"in my own bubble", got.Mine}, {"beside a path", got.Longcell}} {
		if c.it.Sentence == nil {
			t.Fatalf("%s: the fixture found no cell with a sentence in it", c.name)
		}
		if c.it.Sentence.Lines < 2 {
			t.Errorf("%s: a sentence of eleven words stands on %d line(s) %v wide — the cell is not wrapping",
				c.name, c.it.Sentence.Lines, c.it.Sentence.Width)
		}
		if c.it.Sentence.Width > 400 {
			t.Errorf("%s: a cell is %v wide — past the ceiling a column of prose is held to", c.name, c.it.Sentence.Width)
		}
	}
}

type table struct {
	Bubble       float64 `json:"bubble"`
	BubbleScroll int     `json:"bubbleScroll"`
	Box          float64 `json:"box"`
	BoxScroll    int     `json:"boxScroll"`
	BoxClient    int     `json:"boxClient"`
	Table        float64 `json:"table"`
	Rows         int     `json:"rows"`
	FirstCell    string  `json:"firstCell"`
	Sentence     *cell   `json:"sentence"`
}

type cell struct {
	Width float64 `json:"width"`
	Lines int     `json:"lines"`
}
