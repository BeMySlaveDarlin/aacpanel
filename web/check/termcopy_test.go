package check

import (
	"reflect"
	"testing"
)

// What tmux copies in the terminal of the panel — a selection made with the
// mouse, a copy from its own menu — comes to the browser as OSC 52 and is to
// land in the clipboard of the device the person works at, not stay in the
// buffer on the host.
func TestTheTerminalCopiesIntoTheBrowser(t *testing.T) {
	var got struct {
		Tmux  []string `json:"tmux"`
		Toast []struct {
			Title string `json:"title"`
			Sub   string `json:"sub"`
			Bad   bool   `json:"bad"`
		} `json:"toast"`
		Named      []string `json:"named"`
		Query      []string `json:"query"`
		Answered   []string `json:"answered"`
		Broken     []string `json:"broken"`
		Away       []string `json:"away"`
		AwayToasts int      `json:"awayToasts"`
		Session    []string `json:"session"`
	}
	runFixture(t, "termcopy.html", &got)
	if !reflect.DeepEqual(got.Tmux, []string{"héllo ✓\nline two"}) {
		t.Errorf("tmux handed over a copy and the clipboard got %q: the text as it was, in UTF-8", got.Tmux)
	}
	if len(got.Toast) != 1 || got.Toast[0].Title != "Copied" || got.Toast[0].Sub != "héllo ✓" || got.Toast[0].Bad {
		t.Errorf("the copy from tmux says %+v: Copied, and its first line", got.Toast)
	}
	if !reflect.DeepEqual(got.Named, []string{"plain"}) {
		t.Errorf("a copy into the named clipboard, ended by ST, gave %q", got.Named)
	}
	if len(got.Query) != 0 || len(got.Answered) != 0 {
		t.Errorf("a query of the clipboard copied %q and answered %q: the clipboard of the person is not read out", got.Query, got.Answered)
	}
	if len(got.Broken) != 0 {
		t.Errorf("a copy that is not base64 put %q into the clipboard", got.Broken)
	}
	if !reflect.DeepEqual(got.Session, []string{"from the session"}) {
		t.Errorf("the terminal of the panel passed on %q of the copy tmux handed over in its stream", got.Session)
	}
	if len(got.Away) != 0 || got.AwayToasts != 0 {
		t.Errorf("a device out of focus took the copy %q and said %d toasts: tmux hands it to every device on the pane, "+
			"only the one in focus takes it", got.Away, got.AwayToasts)
	}
}
