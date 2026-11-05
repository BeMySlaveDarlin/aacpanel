package check

import (
	"os"
	"testing"
)

// On a phone the state of a session stands whole on its own line, and the
// quiet words of the session — where it lives, its cap and automatic restart,
// Remote Control, a worktree — stand on a line of their own, so they never
// break the state in the middle of a word. A state longer than the line
// wraps, whole, inside the card.
func TestTheQuietWordsOfAStateLeaveItsWordsWhole(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		Text       string
		Lines      int
		Marks      int
		LongText   string
		LongLines  int
		LongCut    bool
		LongInside bool
	}
	runFixture(t, "sessionstatefit.html", &got)
	if got.Text != "working" || got.Marks < 4 {
		t.Fatalf("the card says %q with %d quiet words, expected working with where it lives, its restart and the rest", got.Text, got.Marks)
	}
	if got.Lines != 1 {
		t.Errorf("%q is laid out on %d lines — the words beside it break it mid-word", got.Text, got.Lines)
	}
	want := "asks Which of the three migration plans goes first, and who reviews the rollback script"
	if got.LongText != want || got.LongLines < 2 || got.LongCut || !got.LongInside {
		t.Errorf("the long state reads %q on %d lines (cut %v, inside %v), expected it whole, wrapped inside the card",
			got.LongText, got.LongLines, got.LongCut, got.LongInside)
	}
}
