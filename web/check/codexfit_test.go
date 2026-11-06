package check

import (
	"strings"
	"testing"
)

// The band under the field of a codex thread holds the paperclip and three
// words: how codex thinks, what it may do, and the thread by its icon alone.
// The sheet behind the mode names the modes in full.
func TestTheBandOfACodexThreadSaysItsModes(t *testing.T) {
	type part struct {
		What  string `json:"what"`
		Said  string `json:"said"`
		Label string `json:"label"`
	}
	type band struct {
		Parts []part `json:"parts"`
	}
	want := []struct {
		state, think, perm, permLabel string
	}{
		{"auto", "gpt-6-astra · Extra", "Auto", "Approve for me"},
		{"readOnly", "gpt-6-astra · Extra", "Read", "Read only"},
		{"ask", "gpt-6-astra · Extra", "Ask", "Ask"},
		{"plan", "Plan · Extra", "Auto", "Approve for me"},
		{"custom", "gpt-6-astra · Extra", "Custom", "Custom"},
		{"unknown", "gpt-6-astra · Extra", "Unknown", "Unknown"},
	}
	var got struct {
		Bands map[string]band `json:"bands"`
		Sheet []string        `json:"sheet"`
	}
	runFixture(t, "codexfit.html", &got)
	for _, w := range want {
		b, ok := got.Bands[w.state]
		if !ok {
			t.Errorf("%s: no band was read", w.state)
			continue
		}
		pieces := make([]string, len(b.Parts))
		for i, p := range b.Parts {
			pieces[i] = p.What
		}
		if strings.Join(pieces, ",") != "clip,think,perm,thread" {
			t.Errorf("%s: the band holds %v, expected the paperclip and the three words", w.state, pieces)
			continue
		}
		think, perm, thread := b.Parts[1], b.Parts[2], b.Parts[3]
		if think.Said != w.think {
			t.Errorf("%s: how codex thinks says %q, expected %q", w.state, think.Said, w.think)
		}
		if perm.Said != w.perm || !strings.Contains(perm.Label, w.permLabel) {
			t.Errorf("%s: what codex may do says %q and is called %q, expected %q called by %q",
				w.state, perm.Said, perm.Label, w.perm, w.permLabel)
		}
		if thread.Said != "" || !strings.Contains(thread.Label, "the thread") {
			t.Errorf("%s: the thread says %q and is called %q, expected its icon alone, called the thread",
				w.state, thread.Said, thread.Label)
		}
	}
	if strings.Join(got.Sheet, "|") != "Read only|Ask|Approve for me" {
		t.Errorf("the sheet of the modes names %v, expected the modes in full", got.Sheet)
	}
}
