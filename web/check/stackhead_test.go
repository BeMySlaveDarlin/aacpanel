package check

import (
	"os"
	"testing"
)

// The facts in the head of a stack are read one by one: a value and its
// caption stand apart, and so do the facts. Left without their rules they
// run together into one word, "0%cpu192 MBmemory21 MBdisk".
func TestStackHeadFactsStandApart(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: the gaps are the stylesheet's business — run make front first")
	}
	var got struct {
		Facts   []string `json:"facts"`
		Inner   []int    `json:"inner"`
		Between []int    `json:"between"`
		Row     string   `json:"row"`
	}
	runWideFixture(t, "stackhead.html", &got)
	if len(got.Facts) != 3 {
		t.Fatalf("the head of the stack shows %d facts with a value (%v), expected cpu, memory and disk", len(got.Facts), got.Facts)
	}
	if got.Row != "flex" {
		t.Errorf("the line of facts is laid out as %q: without its rule the facts fall into one run of text", got.Row)
	}
	for i, gap := range got.Inner {
		if gap < 3 {
			t.Errorf("%q: the value and its caption stand %d px apart — they read as one word", got.Facts[i], gap)
		}
	}
	for i, gap := range got.Between {
		if gap < 10 {
			t.Errorf("%q and %q stand %d px apart — the facts run together", got.Facts[i], got.Facts[i+1], gap)
		}
	}
}
