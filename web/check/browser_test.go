package check

import (
	"slices"
	"testing"
)

func TestBrowserViewerOnPhoneAndDesktop(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var got struct {
			Hidden, Hover, Ratio, Paste, Released bool
			Text, Keys, Touch                     []string
			Frames                                int
			Width, Height                         float64
			Overflow                              bool
		}
		if wide {
			runWideFixture(t, "browser.html", &got)
		} else {
			runFixture(t, "browser.html", &got)
		}
		if !got.Hidden ||
			!got.Hover ||
			!got.Ratio ||
			!got.Paste ||
			!got.Released ||
			got.Frames != 2 ||
			!slices.Equal(got.Touch, []string{"start", "move", "end"}) ||
			!slices.Equal(got.Text, []string{"Hello 🦊", "Desktop paste", "Phone paste"}) ||
			!slices.Equal(got.Keys, []string{"Backspace"}) ||
			got.Width < 300 ||
			got.Height < 500 ||
			got.Overflow {
			t.Errorf("browser viewer wide=%v: %+v", wide, got)
		}
	}
}
