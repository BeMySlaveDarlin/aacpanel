package check

import (
	"strings"
	"testing"
)

type pageShot struct {
	Whole  bool    `json:"whole"`
	Size   int     `json:"size"`
	Asked  []int   `json:"asked"`
	More   int     `json:"more"`
	Doc    bool    `json:"doc"`
	Back   bool    `json:"back"`
	ViewW  float64 `json:"viewW"`
	ViewH  float64 `json:"viewH"`
	SheetW float64 `json:"sheetW"`
	SheetH float64 `json:"sheetH"`
	FrameH float64 `json:"frameH"`
	Over   struct {
		Size  int    `json:"size"`
		Said  string `json:"said"`
		Drawn bool   `json:"drawn"`
		Asked []int  `json:"asked"`
	} `json:"over"`
	Other struct {
		More  int   `json:"more"`
		Asked []int `json:"asked"`
	} `json:"other"`
}

// pageWindow is the window a page is read by: the largest the collector gives
// when asked. Every other file is read by its default window, 64 KB.
const pageWindow = 1 << 20

// pageWhole is the largest page the panel draws.
const pageWhole = 8 << 20

// A page is read whole before it is drawn, by the window of a page and with no
// press for the rest: half a document is a broken page, and a page of a few
// megabytes read by the default window is dozens of requests in a row.
func checkPageIsWhole(t *testing.T, got pageShot) {
	t.Helper()
	if got.Size <= 2<<20 || got.Size > pageWhole {
		t.Fatalf("the fixture page is %d bytes: it has to be a few megabytes and under the cap", got.Size)
	}
	few := (got.Size + pageWindow - 1) / pageWindow
	if !got.Whole || len(got.Asked) == 0 || len(got.Asked) > few {
		t.Errorf("a %d byte page was drawn from %d reads, whole: %v — it is read to its end by %d reads at most",
			got.Size, len(got.Asked), got.Whole, few)
	}
	for n, asked := range got.Asked {
		if asked != pageWindow {
			t.Errorf("read %d of the page asked for a window of %d bytes, not the window of a page, %d", n, asked, pageWindow)
		}
	}
	if got.More != 0 {
		t.Errorf("a page asks for a press to read the rest of it")
	}
	if !got.Doc || !got.Back {
		t.Errorf("a page opens as a document with its own way back: doc %v, back %v", got.Doc, got.Back)
	}
}

// A page past the cap is not drawn and not read on: it says to save it, after
// the one read that told its size.
func checkPageOverTheCap(t *testing.T, got pageShot) {
	t.Helper()
	over := got.Over
	if over.Size <= pageWhole {
		t.Fatalf("the fixture page over the cap is %d bytes, under the cap", over.Size)
	}
	if over.Drawn || !strings.Contains(over.Said, "Too large to draw here") || !strings.Contains(over.Said, "Save it") {
		t.Errorf("a %d byte page drawn: %v, saying %q — past the cap it says to save it", over.Size, over.Drawn, over.Said)
	}
	if len(over.Asked) != 1 {
		t.Errorf("a page past the cap was read %d times — the first read tells its size, and there is nothing to read on for", len(over.Asked))
	}
}

// Any other file is read by the default window, and the rest of it by a press.
func checkOtherFileByDefault(t *testing.T, got pageShot) {
	t.Helper()
	other := got.Other
	if len(other.Asked) != 1 || other.Asked[0] != 0 {
		t.Errorf("a log was read with the windows %v — it is read once, by the default window", other.Asked)
	}
	if other.More != 1 {
		t.Errorf("a log longer than a window has %d presses for the rest of it, not one", other.More)
	}
}

// On a phone a page takes the screen above the navigation, as a brief does,
// and its frame the room under the head.
func TestAPageOpensOnThePhoneAsTheWholeScreen(t *testing.T) {
	var got pageShot
	runFixture(t, "pagefile.html", &got)
	checkPageIsWhole(t, got)
	checkPageOverTheCap(t, got)
	checkOtherFileByDefault(t, got)
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
	checkPageOverTheCap(t, got)
	checkOtherFileByDefault(t, got)
	if got.SheetW < got.ViewW*0.75 || got.FrameH < got.ViewH*0.7 {
		t.Errorf("on a %.0fx%.0f screen the page stands in a %.0fx%.0f window with a %.0f tall frame — "+
			"a page needs the width and the height of the screen", got.ViewW, got.ViewH, got.SheetW, got.SheetH, got.FrameH)
	}
}
