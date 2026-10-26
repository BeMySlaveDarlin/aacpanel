package check

import (
	"os"
	"testing"
)

// The marks of a row never take its name: in a narrow column, under the
// session that opened it, a session with an alarm, the automatic restart and
// work waiting for the person still shows its name whole, the marks going to
// the next line rather than over it.
func TestTheMarksOfARowLeaveItsName(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var rows []struct {
		Name         string
		Shown, Needs float64
		Marks        int
	}
	runWideFixture(t, "sessionnamefit.html", &rows)
	if len(rows) != 2 {
		t.Fatalf("the column shows %d sessions, expected the parent and its child: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Marks < 3 {
			t.Errorf("%s carries %d marks, expected the alarm, the restart and the waiting work", r.Name, r.Marks)
		}
		if r.Needs == 0 || r.Shown < r.Needs {
			t.Errorf("%s shows %.0f of the %.0f px its name needs", r.Name, r.Shown, r.Needs)
		}
	}
}
