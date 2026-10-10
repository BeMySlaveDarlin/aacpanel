package check

import (
	"os"
	"strings"
	"testing"
)

// On a phone as narrow as 360 the band of a codex thread holds the paperclip
// and its three words whole, with the longest of what they say: the model at
// its longest effort beside each mode. The mode says its short word and the
// thread its icon alone, each called in full for a reader of the screen; the
// sheet behind the mode names the modes in full.
func TestTheBandOfACodexThreadFitsANarrowPhone(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type part struct {
		What  string  `json:"what"`
		Left  float64 `json:"left"`
		Right float64 `json:"right"`
		Said  string  `json:"said"`
		Label string  `json:"label"`
		Cut   bool    `json:"cut"`
	}
	type band struct {
		Left  float64 `json:"left"`
		Right float64 `json:"right"`
		Parts []part  `json:"parts"`
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
	for _, screen := range []struct{ name, spec string }{{"360", narrowPhone}, {"390", phone390}} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Width float64         `json:"width"`
				Bands map[string]band `json:"bands"`
				Sheet []string        `json:"sheet"`
			}
			runFixtureOn(t, "codexfit.html", screen.spec, phonePointer, &got)
			if got.Width != map[string]float64{"360": 360, "390": 390}[screen.name] {
				t.Fatalf("the fixture ran %v wide", got.Width)
			}
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
				for i, p := range b.Parts {
					if p.Cut {
						t.Errorf("%s: %s is cut — it says %q in less room than it needs", w.state, p.What, p.Said)
					}
					if p.Left < b.Left-0.5 || p.Right > b.Right+0.5 {
						t.Errorf("%s: %s stands at %.1f–%.1f, past the band at %.1f–%.1f", w.state, p.What, p.Left, p.Right, b.Left, b.Right)
					}
					if i > 0 && p.Left < b.Parts[i-1].Right-0.5 {
						t.Errorf("%s: %s starts at %.1f, over %s ending at %.1f", w.state, p.What, p.Left, b.Parts[i-1].What, b.Parts[i-1].Right)
					}
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
		})
	}
}
