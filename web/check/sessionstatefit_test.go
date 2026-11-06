package check

import "testing"

// On a phone the state of a session stands on its own line, and the quiet
// words of the session — where it lives, its cap and automatic restart,
// Remote Control, a worktree — stand on a line of their own beside nothing of
// the state. A long state is said in full.
func TestTheQuietWordsOfAStateStandOnALineOfTheirOwn(t *testing.T) {
	var got struct {
		Text     string
		Marks    int
		LongText string
	}
	runFixture(t, "sessionstatefit.html", &got)
	if got.Text != "working" || got.Marks < 4 {
		t.Fatalf("the card says %q with %d quiet words, expected working with where it lives, its restart and the rest", got.Text, got.Marks)
	}
	want := "asks Which of the three migration plans goes first, and who reviews the rollback script"
	if got.LongText != want {
		t.Errorf("the long state reads %q, expected it in full", got.LongText)
	}
}
