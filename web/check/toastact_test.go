package check

import "testing"

// A toast may carry one way back from what it tells of: a button a finger can
// press, taking touches though the toast lets them through, doing its deed
// once and taking the note down. A toast without one has no button at all.
func TestAToastOffersOneWayBack(t *testing.T) {
	var got struct {
		PlainButtons int `json:"plainButtons"`
		Button       *struct {
			Width   float64 `json:"width"`
			Height  float64 `json:"height"`
			Label   string  `json:"label"`
			Pointer string  `json:"pointer"`
		} `json:"button"`
		HitsButton bool `json:"hitsButton"`
		Ran        int  `json:"ran"`
		OnAfter    bool `json:"onAfter"`
	}
	runFixture(t, "toastact.html", &got)
	if got.PlainButtons != 0 {
		t.Errorf("a plain note grew %d buttons", got.PlainButtons)
	}
	if got.Button == nil || got.Button.Label != "Undo" {
		t.Fatalf("the note offers no way back: %+v", got.Button)
	}
	if got.Button.Height < 44 || got.Button.Width < 44 {
		t.Errorf("Undo is %vx%v — narrower than a finger", got.Button.Width, got.Button.Height)
	}
	if got.Button.Pointer == "none" || !got.HitsButton {
		t.Error("a touch on Undo goes through the toast to the screen under it")
	}
	if got.Ran != 1 || got.OnAfter {
		t.Errorf("pressing Undo ran it %d times and left the note on: %v", got.Ran, got.OnAfter)
	}
}
