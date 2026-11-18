package executor

import "testing"

// The tail of a step is what its command printed: not the empty rows of the
// screen under it, nor the line tmux keeps about the exit of a finished pane.
func TestAStepCaptureIsWhatTheCommandPrinted(t *testing.T) {
	screen := "chain-ok\n/opt/proj\n\n\n\n\nPane is dead (status 0, Thu Nov 19 00:10:44 2026)\n\n"
	if got := printed(screen, true); got != "chain-ok\n/opt/proj" {
		t.Errorf("a finished step reads %q", got)
	}
	if got := printed("waiting for the key…\n\n\n", false); got != "waiting for the key…" {
		t.Errorf("a running step reads %q", got)
	}
	// While the command runs, a line of its own is never taken for the pane's.
	if got := printed("Pane is dead (status 1, …)\n", false); got != "Pane is dead (status 1, …)" {
		t.Errorf("a live pane lost a printed line: %q", got)
	}
}
