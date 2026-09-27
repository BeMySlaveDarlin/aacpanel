package check

import (
	"strings"
	"testing"
)

type blockShot struct {
	Height, PreHeight  float64
	BtnInside          bool
	BtnTop, BtnRight   float64
	TextRight, BtnLeft float64
	FirstLineTop       float64
	BtnBottom          float64
	Position           string
	Opacity            float64
}

type blocksShot struct {
	One, Many blockShot
	OneClass  string
	Table     struct {
		Plate                  string
		HeadWeight, BodyWeight int
		HeadGround, BodyGround string
		Vertical               []string
		RowRule                string
		Align, Fonts           []string
		BtnInside              bool
		Height, TableHeight    float64
	}
	List struct {
		Buttons int
		Wrapped bool
	}
	Bars int
}

// A block of code has no bar over it: the copy button lies over its top right
// corner, half seen through, and the block is as tall as its code. A block of
// one line is a command to run, and the button keeps its own room at the end
// of the line, so the command never runs under it.
func TestACodeBlockCarriesItsButtonOverItsCorner(t *testing.T) {
	var got blocksShot
	runFixture(t, "feedblocks.html", &got)
	if got.Bars != 0 {
		t.Errorf("%d bars and wrappers of a copy button stand in the answer", got.Bars)
	}
	for name, b := range map[string]blockShot{"one line": got.One, "several lines": got.Many} {
		if b.Position != "absolute" || !b.BtnInside {
			t.Errorf("%s: the copy button is %s and inside the block %v — it takes a row of its own", name, b.Position, b.BtnInside)
		}
		if b.Height-b.PreHeight > 0.5 {
			t.Errorf("%s: the block is %.1fpx tall around %.1fpx of code — a bar is back over it", name, b.Height, b.PreHeight)
		}
		if b.BtnTop > 8 || b.BtnRight > 8 {
			t.Errorf("%s: the button stands %.1fpx from the top and %.1fpx from the right — not on the corner", name, b.BtnTop, b.BtnRight)
		}
		if b.Opacity >= 1 {
			t.Errorf("%s: the button is not half seen through", name)
		}
	}
	if !strings.Contains(got.OneClass, "oneline") {
		t.Errorf("the command of one line is not told from a block: %q", got.OneClass)
	}
	if got.One.TextRight > got.One.BtnLeft {
		t.Errorf("the command runs under the button: its text ends at %.1f, the button starts at %.1f", got.One.TextRight, got.One.BtnLeft)
	}
	if got.Many.FirstLineTop >= got.Many.BtnBottom {
		t.Errorf("the button of a block of several lines stands over no line of it: the first line at %.1f, the button ends at %.1f",
			got.Many.FirstLineTop, got.Many.BtnBottom)
	}
}

// A table is a plate of its own: the head set apart by weight and ground,
// rows parted by hairlines and no vertical rules, a column of numbers set
// right in the code face, the copy button over the corner.
func TestATableIsAPlateOfItsOwn(t *testing.T) {
	var got blocksShot
	runFixture(t, "feedblocks.html", &got)
	tb := got.Table
	if tb.Plate == "none" {
		t.Error("the table stands on no plate: it reads as text in a grid of lines")
	}
	if tb.HeadWeight < 600 || tb.HeadWeight <= tb.BodyWeight || tb.HeadGround == tb.BodyGround {
		t.Errorf("the head of the table weighs %d on %s against the rows' %d on %s", tb.HeadWeight, tb.HeadGround, tb.BodyWeight, tb.BodyGround)
	}
	for _, v := range tb.Vertical {
		if v != "0px/0px" {
			t.Errorf("a cell carries vertical rules: %s", v)
			break
		}
	}
	if tb.RowRule == "none" {
		t.Error("the rows of the table are not parted")
	}
	if strings.Join(tb.Align, ",") != "left,right,right" {
		t.Errorf("the columns are aligned %v: words left, numbers right", tb.Align)
	}
	if tb.Fonts[1] == tb.Fonts[0] || tb.Fonts[2] != tb.Fonts[1] {
		t.Errorf("the numbers are set in %v: the columns of numbers in the code face, the words not", tb.Fonts)
	}
	if !tb.BtnInside || tb.Height-tb.TableHeight > 0.5 {
		t.Errorf("the copy button of the table is inside it %v, the plate %.1fpx tall around a table of %.1fpx", tb.BtnInside, tb.Height, tb.TableHeight)
	}
	if got.List.Buttons != 0 || !got.List.Wrapped {
		t.Errorf("a list carries %d copy buttons and stands in a wrapper %v: a list is prose", got.List.Buttons, !got.List.Wrapped)
	}
}
