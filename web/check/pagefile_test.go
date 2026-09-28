package check

import "testing"

type pageShot struct {
	Whole  bool    `json:"whole"`
	Reads  int     `json:"reads"`
	More   int     `json:"more"`
	Doc    bool    `json:"doc"`
	Back   bool    `json:"back"`
	ViewW  float64 `json:"viewW"`
	ViewH  float64 `json:"viewH"`
	SheetW float64 `json:"sheetW"`
	SheetH float64 `json:"sheetH"`
	FrameH float64 `json:"frameH"`
}

// A page larger than the window a file is read by is read whole before it is
// drawn, with no press for the rest: half a document is a broken page.
func checkPageIsWhole(t *testing.T, got pageShot) {
	t.Helper()
	if !got.Whole || got.Reads < 4 {
		t.Errorf("the page was drawn from %d reads, whole: %v — it has to be read to its end first", got.Reads, got.Whole)
	}
	if got.More != 0 {
		t.Errorf("a page asks for a press to read the rest of it")
	}
	if !got.Doc || !got.Back {
		t.Errorf("a page opens as a document with its own way back: doc %v, back %v", got.Doc, got.Back)
	}
}

// On a phone a page takes the screen above the navigation, as a brief does,
// and its frame the room under the head.
func TestAPageOpensOnThePhoneAsTheWholeScreen(t *testing.T) {
	var got pageShot
	runFixture(t, "pagefile.html", &got)
	checkPageIsWhole(t, got)
	if got.SheetH < got.ViewH*0.85 || got.FrameH < got.SheetH*0.75 {
		t.Errorf("on a %.0fx%.0f screen the page is a %.0f tall sheet with a %.0f tall frame — "+
			"it is read, not glanced into, and takes the screen", got.ViewW, got.ViewH, got.SheetH, got.FrameH)
	}
}

// At a desk a page opens in a wide window, its frame the height of the window.
func TestAPageOpensAtADeskInAWideWindow(t *testing.T) {
	var got pageShot
	runWideFixture(t, "pagefile.html", &got)
	checkPageIsWhole(t, got)
	if got.SheetW < got.ViewW*0.75 || got.FrameH < got.ViewH*0.7 {
		t.Errorf("on a %.0fx%.0f screen the page stands in a %.0fx%.0f window with a %.0f tall frame — "+
			"a page needs the width and the height of the screen", got.ViewW, got.ViewH, got.SheetW, got.SheetH, got.FrameH)
	}
}
