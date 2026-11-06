package check

import "testing"

// A session of a narrow column with every mark at once, under the session
// that opened it: the restart stands in the place of its cap, and the work
// waiting for the person is the state's line.
func TestARowWithEveryMarkSaysItsRestartAndItsWaitingWork(t *testing.T) {
	var rows []struct {
		Name    string
		Restart bool
		State   string
	}
	runWideFixture(t, "sessionnamefit.html", &rows)
	if len(rows) != 2 {
		t.Fatalf("the column shows %d sessions, expected the parent and its child: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if !r.Restart || r.State != "request Rotate the token" {
			t.Errorf("%s says %q with the restart marked %v, expected the waiting request and the restart", r.Name, r.State, r.Restart)
		}
	}
}
