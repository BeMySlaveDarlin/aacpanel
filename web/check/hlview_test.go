package check

import "testing"

// Code is coloured only when it comes near the screen: a block far below the
// window stays plain until the feed is scrolled to it. What was coloured is
// kept — the same code twice is coloured once, and a block drawn again takes
// its colours from what was kept on its first frame.
func TestCodeIsColouredNearTheScreenAndOnce(t *testing.T) {
	var got struct {
		First struct {
			Lit  []bool
			Runs int
			Hits int
			Text string
		}
		Scrolled struct {
			Lit  []bool
			Runs int
		}
		Again struct {
			Lit  []bool
			Runs int
			Sync bool
		}
		Text string
	}
	runFixture(t, "hlview.html", &got)
	f := got.First
	if len(f.Lit) != 3 || !f.Lit[0] || !f.Lit[1] {
		t.Fatalf("the blocks on the screen are coloured %v", f.Lit)
	}
	if f.Lit[2] {
		t.Error("a block far below the window is coloured before the feed comes to it")
	}
	if f.Runs != 1 || f.Hits < 1 {
		t.Errorf("the same code on the screen twice was coloured %d times with %d taken from what was kept", f.Runs, f.Hits)
	}
	if f.Text != got.Text {
		t.Errorf("colouring changed the text of the code: %q → %q", f.Text, got.Text)
	}
	if s := got.Scrolled; len(s.Lit) != 3 || !s.Lit[2] || s.Runs != 2 {
		t.Errorf("scrolled to the far block: coloured %v after %d runs", s.Lit, s.Runs)
	}
	if a := got.Again; !a.Sync || a.Runs != 2 {
		t.Errorf("the block drawn again is coloured on its first frame %v, after %d runs — what was kept went unused", a.Sync, a.Runs)
	}
}
