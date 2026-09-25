package check

import (
	"strings"
	"testing"
)

func TestThoughtWithTextIsReadAsTextNotAsACounter(t *testing.T) {
	src := srcFiles(t)["src/screens/chat/rows.js"]
	if src == "" {
		t.Fatal("src/screens/chat/rows.js not found")
	}

	at := strings.Index(src, `item.role === "mind"`)
	if at < 0 {
		t.Fatal("there is no branch for a thought with text at all — the thought falls into " +
			"the common reply rendering and stands there as an answer bubble")
	}
	row := src[at:]
	if end := strings.Index(row, "\n    if (item.role"); end > 0 {
		row = row[:end]
	}

	if !strings.Contains(row, "render(item.text)") {
		t.Error("the thought text is not rendered through markdown — yet it is the same prose " +
			"as the model's answer")
	}
	if !strings.Contains(row, "msg mmind") {
		t.Error("the thought row has lost its class: it has neither the muting nor the origin " +
			"mark, and is indistinguishable from the model's answer")
	}
	if !strings.Contains(row, "item.cut") {
		t.Error("a truncated thought says nothing about the truncation — the shortened text " +
			"lies about what the model was thinking")
	}

	mark := cssBlock(t, cssSrc(t), ".msg.mmind")
	if !strings.Contains(mark, "color:") {
		t.Error("the thought in the feed has lost its tone — next to the model's answer the " +
			"two can no longer be told apart")
	}
}

// A thought reads two steps of the scale under the answer beside it, and its
// mark stands on its first line.
func TestAThoughtReadsTwoStepsSmallerThanTheAnswer(t *testing.T) {
	var got struct {
		Mind    float64 `json:"mind"`
		Answer  float64 `json:"answer"`
		Sm      float64 `json:"sm"`
		Base    float64 `json:"base"`
		IconOff float64 `json:"iconOff"`
	}
	runFixture(t, "thought.html", &got)
	if got.Answer != got.Base || got.Mind != got.Sm || got.Mind >= got.Answer {
		t.Errorf("the thought reads at %.2fpx beside an answer at %.2fpx: two steps under it, %.2fpx",
			got.Mind, got.Answer, got.Sm)
	}
	if got.IconOff > 2 {
		t.Errorf("the mark of the thought stands %.1fpx off the middle of its first line", got.IconOff)
	}
}
