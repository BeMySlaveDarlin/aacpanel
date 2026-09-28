package check

import (
	"math"
	"testing"
)

type badgeShot struct {
	Badges []struct {
		Kind string  `json:"kind"`
		W    float64 `json:"w"`
		H    float64 `json:"h"`
		IW   float64 `json:"iw"`
		IH   float64 `json:"ih"`
		Top  float64 `json:"top"`
	} `json:"badges"`
	Rows     []float64 `json:"rows"`
	InColumn int       `json:"inColumn"`
}

// Every badge on the timeline is drawn the same size with the same icon,
// whatever it counts — a kind of call, thinking, the end of a turn — and
// stands at the height of the row its work led to. The kind of a published
// page is one more kind among them, whatever a card of a page looks like.
func TestEveryBadgeOfTheTimelineIsTheSameSize(t *testing.T) {
	var got badgeShot
	runFixture(t, "toolbadges.html", &got)
	if len(got.Badges) < 12 {
		t.Fatalf("marks drawn: %+v", got.Badges)
	}
	if got.InColumn != 0 {
		t.Errorf("%d badges stand in the column of prose: the work is on the timeline", got.InColumn)
	}
	first := got.Badges[0]
	kinds := map[string]bool{}
	for i, b := range got.Badges {
		kinds[b.Kind] = true
		if b.W != first.W || b.H != first.H || b.IW != first.IW || b.IH != first.IH {
			t.Errorf("the %s mark is %.1fx%.1f with a %.1fx%.1f icon; the %s one is %.1fx%.1f with %.1fx%.1f",
				b.Kind, b.W, b.H, b.IW, b.IH, first.Kind, first.W, first.H, first.IW, first.IH)
		}
		// Mark i stands beside row 2i+1... in the column: the rows are the replies,
		// one after every mark, and the person's message after the turn.
		if i < len(got.Rows) && math.Abs(b.Top-got.Rows[i]) > 1 {
			t.Errorf("the %s mark stands at %.1f, the row its work led to at %.1f", b.Kind, b.Top, got.Rows[i])
		}
	}
	for _, want := range []string{"artifact", "hook", "think", "turn"} {
		if !kinds[want] {
			t.Errorf("no %s mark among %v", want, kinds)
		}
	}
}
