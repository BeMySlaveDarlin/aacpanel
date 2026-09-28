package check

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// phone390 is the screen the owner reads the panel on: a phone 390 points
// across and 844 high.
const phone390 = `{"width":390,"height":844,"deviceScaleFactor":3,"mobile":true}`

// dockFrame is the container of a dock: its material, its frame, the corners
// over it (Radius) apart from the corners under it (Under), and where it
// stands across the screen.
type dockFrame struct {
	Background, Border, Radius, Under, Padding, Shadow string

	Left, Right, Width int
}

type dockAnswer struct {
	Row struct {
		Background, Border, Radius string
		Height                     int
	}
	Button struct{ Padding, MinHeight, Gap string }
	Words  struct{ Size, Weight, LineHeight, Color string }
	Mark   struct{ Width, Height, Border, Size string }
	List   struct{ Gap string }

	WordsTop    int
	WordsBottom int
}

type dockType struct{ Size, Weight, LineHeight, Spacing, Color string }

// permitShown is what permitphone.html measures: the dock of a question of two
// short answers, then the dock of a permission in the same place.
type permitShown struct {
	Ask struct {
		Frame   dockFrame
		Answer  dockAnswer
		Eyebrow dockType
		Title   dockType
	}
	Permit struct {
		Viewport   int
		Width      int
		Dock       struct{ Top, Bottom, Height int }
		HeadTop    int
		DeckTop    *int
		FeedBottom int
		LastOption struct{ Top, Bottom int }
		Frame      dockFrame
		Answer     dockAnswer
		Eyebrow    dockType
		Title      dockType
		TitleOver  bool
		Request    struct {
			Size       string
			LineHeight float64
			Padding    float64
			Height     int
			Whole      int
			Text       bool
		}
		Note struct {
			LineHeight float64
			Padding    float64
			Height     int
			Whole      int
			Text       bool
		}
		Code struct{ Shell, Call string }
		Fold bool
		Open *struct {
			NoteWhole    bool
			RequestWhole bool
			LastBottom   int
		}
	}
}

// A permission asked on a phone by a tool whose input is a long text: a
// comment for the tracker, in Russian, and five lines of why the console asks.
// The dock stands where the composer stood and has one job — the answers — so
// they stay on the screen with the head and the start of the request, and the
// rest of the request and the note is one press away rather than a wall of
// code the answers sit under. And the dock is the sheet a question opens in,
// the other thing that takes that place: edge to edge, rounded over and
// square under, the eyebrow and the title of a question, its answer rows.
func TestAPermissionOnAPhoneIsAQuestionsDockWithItsAnswersInSight(t *testing.T) {
	var got permitShown
	runFixtureOn(t, "permitphone.html", phone390, phonePointer, &got)
	p := got.Permit

	t.Run("the answers stay in sight", func(t *testing.T) {
		if p.Viewport != 844 {
			t.Fatalf("the screen is %d high, not the phone the case is about", p.Viewport)
		}
		if limit := int(0.55 * float64(p.Viewport)); p.Dock.Height > limit {
			t.Errorf("the dock is %d px of a %d px screen — more than the %d px the sheet of a question "+
				"takes; the conversation above it is gone", p.Dock.Height, p.Viewport, limit)
		}
		if p.HeadTop < 0 || p.LastOption.Bottom > p.Viewport {
			t.Errorf("the head stands at %d and the last answer ends at %d on a screen %d high: "+
				"the answers are not on the screen together with what they answer",
				p.HeadTop, p.LastOption.Bottom, p.Viewport)
		}

		// The fixture has to carry a request and a note longer than the fold,
		// or the caps below pass on a case that never had the fault.
		if lines := (float64(p.Request.Whole) - p.Request.Padding) / p.Request.LineHeight; lines < 12 {
			t.Fatalf("the whole request is %.1f lines: the fixture no longer carries a long one", lines)
		}
		if lines := (float64(p.Note.Whole) - p.Note.Padding) / p.Note.LineHeight; lines < 3 {
			t.Fatalf("the whole note is %.1f lines: the fixture no longer carries a long one", lines)
		}
		if lines := (float64(p.Request.Height) - p.Request.Padding) / p.Request.LineHeight; lines > 8.05 {
			t.Errorf("the request shows %.1f lines before the answers: a whole edit is a wall of code "+
				"the answers stand under", lines)
		}
		if lines := (float64(p.Note.Height) - p.Note.Padding) / p.Note.LineHeight; lines > 2.05 {
			t.Errorf("the note shows %.1f lines folded", lines)
		}

		// A tool's input is code, and code has a size in this panel.
		size := cssPx(t, p.Request.Size)
		if limit := math.Min(cssPx(t, p.Code.Shell), cssPx(t, p.Code.Call)); size > limit {
			t.Errorf("the request is drawn at %s — larger than the %s the conversation draws a tool's "+
				"input and output at", p.Request.Size, p.Code.Call)
		}

		// What is folded is not lost.
		if !p.Request.Text || !p.Note.Text {
			t.Error("the end of the request or of the note is not in the dock at all")
		}
		if !p.Fold || p.Open == nil {
			t.Fatal("nothing unfolds the request and the note: the rest of them cannot be read")
		}
		if !p.Open.NoteWhole || !p.Open.RequestWhole {
			t.Errorf("unfolded, the note is whole: %v, the request is whole or scrolls: %v",
				p.Open.NoteWhole, p.Open.RequestWhole)
		}
		if p.Open.LastBottom > p.Viewport {
			t.Errorf("unfolded, the last answer ends at %d on a screen %d high", p.Open.LastBottom, p.Viewport)
		}
	})

	t.Run("it is the sheet of a question", func(t *testing.T) {
		a := got.Ask
		pf, af := p.Frame, a.Frame
		// The sheet stands on the bottom of the screen and the dock on the row
		// under it, so the two differ in the corners under them and nowhere else.
		if pf.Background != af.Background || pf.Border != af.Border || pf.Radius != af.Radius ||
			pf.Padding != af.Padding || pf.Shadow != af.Shadow {
			t.Errorf("the dock of a permission is framed\n  %+v\nand the sheet of a question\n  %+v", pf, af)
		}
		if pf.Under != "0px 0px" {
			t.Errorf("the corners under the dock are %s: a sheet stands on what is under it", pf.Under)
		}
		if apart(pf.Left, 0) || apart(pf.Right, p.Width) {
			t.Errorf("the dock runs from %d to %d on a screen %d across: it is a card set into the page, "+
				"not the sheet a question opens in, which runs edge to edge", pf.Left, pf.Right, p.Width)
		}
		if p.DeckTop == nil {
			t.Error("the row under the composer is not on the screen: the fixture no longer draws a phone")
		} else if apart(p.Dock.Bottom, *p.DeckTop) {
			t.Errorf("the dock ends at %d and the row under it starts at %d: a sheet stands on the row, "+
				"with no strip of the page between", p.Dock.Bottom, *p.DeckTop)
		}

		if p.Eyebrow != a.Eyebrow {
			t.Errorf("the eyebrow of the dock is %+v, of a question %+v", p.Eyebrow, a.Eyebrow)
		}
		if p.Title != a.Title {
			t.Errorf("the title of the dock is %+v, of a question %+v", p.Title, a.Title)
		}
		if p.TitleOver {
			t.Error("the name of the tool runs past the edge of its title")
		}
		sameAnswers(t, p.Answer, a.Answer)
	})
}

// At a desk a question opens in a window the width of a question, and the
// permission, which stays under the conversation it belongs to, takes that
// window's frame and width rather than stretching across the column.
func TestAPermissionAtADeskTakesTheWindowOfAQuestion(t *testing.T) {
	var got permitShown
	runWideFixture(t, "permitphone.html", &got)
	p, a := got.Permit, got.Ask

	if p.Dock.Top < p.FeedBottom-1 {
		t.Errorf("the dock starts at %d over a conversation that ends at %d: it is not under it",
			p.Dock.Top, p.FeedBottom)
	}
	if p.LastOption.Bottom > p.Viewport {
		t.Errorf("the last answer ends at %d on a screen %d high", p.LastOption.Bottom, p.Viewport)
	}
	pf, af := p.Frame, a.Frame
	pf.Left, pf.Right, af.Left, af.Right = 0, 0, 0, 0
	if pf != af {
		t.Errorf("the dock of a permission is\n  %+v\nand the window of a question\n  %+v", pf, af)
	}
	if p.Eyebrow != a.Eyebrow || p.Title != a.Title {
		t.Errorf("the head of the dock is %+v %+v, of a question %+v %+v", p.Eyebrow, p.Title, a.Eyebrow, a.Title)
	}
	sameAnswers(t, p.Answer, a.Answer)
}

// sameAnswers holds the answer rows of a permission to the option rows of a
// question: one kind of row, pressed the same way.
func sameAnswers(t *testing.T, pa, aa dockAnswer) {
	t.Helper()
	if pa.Row != aa.Row {
		t.Errorf("an answer row is\n  %+v\nand a question's option\n  %+v", pa.Row, aa.Row)
	}
	if pa.Button != aa.Button || pa.List != aa.List {
		t.Errorf("an answer is laid out %+v %+v, a question's option %+v %+v",
			pa.Button, pa.List, aa.Button, aa.List)
	}
	if pa.Words != aa.Words {
		t.Errorf("the words of an answer are %+v, of a question's option %+v", pa.Words, aa.Words)
	}
	if pa.Mark != aa.Mark {
		t.Errorf("the mark in front of an answer is %+v, of a question's option %+v", pa.Mark, aa.Mark)
	}
	for _, one := range []struct {
		who string
		a   dockAnswer
	}{{"an answer of a permission", pa}, {"an option of a question", aa}} {
		if d := one.a.WordsBottom - one.a.WordsTop; d > 1 || d < -1 {
			t.Errorf("%s of one line has %d px over its words and %d px under them: the words sit "+
				"at the top of a finger-high button", one.who, one.a.WordsTop, one.a.WordsBottom)
		}
	}
}

// apart reports two positions more than a pixel apart — the rounding of a
// fractional layout, and no more.
func apart(got, want int) bool {
	return got-want > 1 || want-got > 1
}

// cssPx reads a computed length in pixels.
func cssPx(t *testing.T, value string) float64 {
	t.Helper()
	n, err := strconv.ParseFloat(strings.TrimSuffix(value, "px"), 64)
	if err != nil {
		t.Fatalf("%q is not a length in pixels", value)
	}
	return n
}
