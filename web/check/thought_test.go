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
	if !strings.Contains(row, "msg ai mmind") {
		t.Error("the thought row has lost its class: it is not set as an answer, or it cannot be " +
			"told from one")
	}
	if !strings.Contains(row, "item.cut") {
		t.Error("a truncated thought says nothing about the truncation — the shortened text " +
			"lies about what the model was thinking")
	}
}

// A thought is set as the answer beside it is — the same face, size and ink,
// no icon and no italics — and is told apart by a hairline on its left and
// the word over the first of a run of thoughts. A run of thoughts has one
// line, unbroken across the gap between them, even with work between them
// on the timeline.
func TestAThoughtReadsAsAnAnswerWithAMarkOfItsOwn(t *testing.T) {
	var got struct {
		Mind, Answer             float64
		MindColour, AnswerColour string
		MindFont, AnswerFont     string
		MindItalic               string
		Icons                    int
		Tags                     []string
		Rule, AnswerRule         string
		Gap                      *float64
	}
	runFixture(t, "thought.html", &got)
	if got.Mind != got.Answer || got.MindColour != got.AnswerColour || got.MindFont != got.AnswerFont {
		t.Errorf("the thought reads %.2fpx %s %s beside an answer at %.2fpx %s %s — set it as the answer",
			got.Mind, got.MindColour, got.MindFont, got.Answer, got.AnswerColour, got.AnswerFont)
	}
	if got.MindItalic != "normal" {
		t.Errorf("the thought is set %s", got.MindItalic)
	}
	if got.Icons != 0 {
		t.Errorf("the thought carries %d icons — a mark on every paragraph unwinds the feed", got.Icons)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "thinking" {
		t.Errorf("the words over the thoughts: %q — one, over the first of the run", got.Tags)
	}
	if got.Rule == "none" || got.Rule == got.AnswerRule {
		t.Errorf("the thought has no hairline of its own: %q against the answer's %q", got.Rule, got.AnswerRule)
	}
	if got.Gap == nil || *got.Gap > 0.5 {
		t.Errorf("the line of a run of thoughts breaks between them: %v", got.Gap)
	}
}
