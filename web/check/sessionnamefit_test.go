package check

import (
	"os"
	"testing"
)

// The marks of a card never take its name: in a narrow column, under the
// session that opened it, a session with the automatic restart and work
// waiting for the person still shows its name whole — the restart stands in
// the place of its cap, the waiting work is the state's line.
func TestTheMarksOfARowLeaveItsName(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var rows []struct {
		Name         string
		Shown, Needs float64
		Restart      bool
		State        string
	}
	runWideFixture(t, "sessionnamefit.html", &rows)
	if len(rows) != 2 {
		t.Fatalf("the column shows %d sessions, expected the parent and its child: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if !r.Restart || r.State != "request Rotate the token" {
			t.Errorf("%s says %q with the restart marked %v, expected the waiting request and the restart", r.Name, r.State, r.Restart)
		}
		if r.Needs == 0 || r.Shown < r.Needs {
			t.Errorf("%s shows %.0f of the %.0f px its name needs", r.Name, r.Shown, r.Needs)
		}
	}
}
