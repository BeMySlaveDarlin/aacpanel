package check

import "testing"

type badgeShot struct {
	Badges []struct {
		Kind string  `json:"kind"`
		W    float64 `json:"w"`
		H    float64 `json:"h"`
		IW   float64 `json:"iw"`
		IH   float64 `json:"ih"`
	} `json:"badges"`
}

// Every badge of a run is drawn the same height with the same icon, whatever
// kind of call it counts: a class of a card elsewhere in the panel once made
// one of them larger than the rest.
func TestEveryBadgeOfARunIsTheSameSize(t *testing.T) {
	var got badgeShot
	runFixture(t, "toolbadges.html", &got)
	if len(got.Badges) < 10 {
		t.Fatalf("badges drawn: %+v", got.Badges)
	}
	first := got.Badges[0]
	for _, b := range got.Badges {
		if b.H != first.H || b.IW != first.IW || b.IH != first.IH {
			t.Errorf("the %s badge is %.1fx%.1f with a %.1fx%.1f icon; the %s one is %.1fx%.1f with %.1fx%.1f",
				b.Kind, b.W, b.H, b.IW, b.IH, first.Kind, first.W, first.H, first.IW, first.IH)
		}
	}
}
