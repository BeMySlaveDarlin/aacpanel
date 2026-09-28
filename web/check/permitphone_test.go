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

type dockFrame struct {
	Background, Border, Radius, Padding, Shadow string
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

type dockHead struct{ Size, Weight, Color string }

// A permission asked on a phone by a tool whose input is a long text: a
// comment for the tracker, in Russian, and five lines of why the console asks.
// The dock stands where the composer stood and has one job — the answers — so
// they stay on the screen with the head and the start of the request, and the
// rest of the request and the note is one press away rather than a wall of
// code the answers sit under. And the dock is the same thing to the eye as the
// dock of a question, the other one that takes that place.
func TestAPermissionOnAPhoneIsAQuestionsDockWithItsAnswersInSight(t *testing.T) {
	var got struct {
		Ask struct {
			Frame  dockFrame
			Answer dockAnswer
			Head   dockHead
		}
		Permit struct {
			Viewport   int
			Dock       struct{ Top, Height int }
			HeadTop    int
			LastOption struct{ Top, Bottom int }
			Frame      dockFrame
			Answer     dockAnswer
			Head       dockHead
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

	t.Run("it is drawn as the dock of a question", func(t *testing.T) {
		a := got.Ask
		if a.Frame != p.Frame {
			t.Errorf("the dock of a permission is framed\n  %+v\nand the dock of a question\n  %+v", p.Frame, a.Frame)
		}
		if a.Head != p.Head {
			t.Errorf("the words of the head are %+v, and of a question's head %+v", p.Head, a.Head)
		}
		pa, aa := p.Answer, a.Answer
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
	})
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
