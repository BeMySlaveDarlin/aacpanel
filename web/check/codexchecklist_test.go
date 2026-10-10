package check

import (
	"strings"
	"testing"
)

// A thread of codex that keeps a checklist through the panel's tool shows it
// in its conversation where a claude session shows its own: on a phone the
// line over the composer with the step it stands on, at a desk the block that
// opens the timeline with every step.
func TestACodexThreadShowsItsChecklistInItsConversation(t *testing.T) {
	type seen struct {
		Wide       bool     `json:"wide"`
		Drawn      bool     `json:"drawn"`
		Line       string   `json:"line"`
		LineNum    string   `json:"lineNum"`
		BlockFirst bool     `json:"blockFirst"`
		BlockSteps []string `json:"blockSteps"`
		Overflow   int      `json:"overflow"`
		Error      string   `json:"error"`
	}
	t.Run("phone", func(t *testing.T) {
		var got seen
		runFixture(t, "codexchecklist.html", &got)
		if got.Error != "" || got.Wide {
			t.Fatalf("the fixture broke: %s (wide %v)", got.Error, got.Wide)
		}
		if !got.Drawn || got.Line != "pin the clock of the cart test" || got.LineNum != "2/3" || len(got.BlockSteps) != 0 {
			t.Errorf("over the composer the thread says %q at %q (drawn %v), and a block of %v", got.Line, got.LineNum,
				got.Drawn, got.BlockSteps)
		}
		if got.Overflow > 0 {
			t.Errorf("the conversation pushes the phone %dpx sideways", got.Overflow)
		}
	})
	t.Run("desk", func(t *testing.T) {
		var got seen
		runWideFixture(t, "codexchecklist.html", &got)
		if got.Error != "" || !got.Wide {
			t.Fatalf("the fixture broke: %s (wide %v)", got.Error, got.Wide)
		}
		want := []string{"read the cart test", "pin the clock of the cart test", "run the suite"}
		if !got.Drawn || !got.BlockFirst || len(got.BlockSteps) != len(want) || got.Line != "" {
			t.Fatalf("the timeline opens with %v (first %v, drawn %v), and over the composer %q", got.BlockSteps,
				got.BlockFirst, got.Drawn, got.Line)
		}
		for i, step := range got.BlockSteps {
			if !strings.Contains(step, want[i]) {
				t.Errorf("step %d of the block reads %q, expected %q", i+1, step, want[i])
			}
		}
	})
}
