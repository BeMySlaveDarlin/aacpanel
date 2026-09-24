package check

import "testing"

// The chips of a run of calls are one family: a published page is one more
// kind among them, not the card of a page. Its class once matched the card's,
// and the chip took the card's padding and stood a head taller than its row.
func TestEveryChipOfARunIsOneHeight(t *testing.T) {
	var got struct {
		Kinds   []string `json:"kinds"`
		Heights []int    `json:"heights"`
		Widths  []int    `json:"widths"`
	}
	runFixture(t, "toolchips.html", &got)
	if len(got.Heights) != 2 {
		t.Fatalf("chips drawn: %v", got.Kinds)
	}
	if got.Heights[0] != got.Heights[1] {
		t.Errorf("the chip of a published page is %dpx tall against %dpx: %v", got.Heights[1], got.Heights[0], got.Kinds)
	}
	if got.Widths[1] > got.Widths[0]+8 {
		t.Errorf("the chip of a published page is %dpx wide against %dpx", got.Widths[1], got.Widths[0])
	}
}
