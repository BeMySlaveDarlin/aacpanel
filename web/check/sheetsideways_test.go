package check

import "testing"

// A sheet can be dragged, so it takes the gesture away from the browser — and
// a box inside it loses its own sideways scroll along with it. The sheet
// carries that scroll by hand instead: without it a wide table in a sheet has
// a scrollbar and no way to reach the columns under it.
func TestASidewaysSwipeScrollsTheBoxAndNotTheSheet(t *testing.T) {
	var got struct {
		Scrollable int `json:"scrollable"`
		Sideways   struct {
			ScrollLeft     float64 `json:"scrollLeft"`
			SheetTransform string  `json:"sheetTransform"`
		} `json:"sideways"`
		AfterUp struct {
			ScrollLeft     float64 `json:"scrollLeft"`
			SheetTransform string  `json:"sheetTransform"`
		} `json:"afterUp"`
		DownOnTable struct {
			Moved          float64 `json:"moved"`
			SheetTransform string  `json:"sheetTransform"`
		} `json:"downOnTable"`
		DownOnText struct {
			SheetTransform string `json:"sheetTransform"`
		} `json:"downOnText"`
	}
	runFixture(t, "sheetsideways.html", &got)

	if got.Scrollable <= 0 {
		t.Fatalf("the table in the fixture is not wider than its box (%d): there is no gesture to test", got.Scrollable)
	}
	if got.Sideways.ScrollLeft < 150 {
		t.Errorf("a swipe of 180 px across the table moved it by %v: the columns under the finger stay out of reach",
			got.Sideways.ScrollLeft)
	}
	if got.Sideways.SheetTransform != "" {
		t.Errorf("the sheet moved while the table was being read: transform %q", got.Sideways.SheetTransform)
	}
	if got.AfterUp.ScrollLeft != got.Sideways.ScrollLeft {
		t.Errorf("the table sprang back when the finger was lifted: %v against %v",
			got.AfterUp.ScrollLeft, got.Sideways.ScrollLeft)
	}
	if got.DownOnTable.Moved != 0 {
		t.Errorf("a downward gesture on the table scrolled it sideways by %v", got.DownOnTable.Moved)
	}
	if got.DownOnTable.SheetTransform != "" {
		t.Errorf("a downward gesture that began on a table drags the sheet: transform %q — "+
			"the list under the finger is what should move", got.DownOnTable.SheetTransform)
	}
	if got.DownOnText.SheetTransform == "" {
		t.Error("a pull down on the text of the sheet no longer moves the sheet at all — " +
			"dragging is how a sheet is closed on a phone")
	}
}
