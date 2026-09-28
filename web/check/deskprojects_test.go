package check

import (
	"os"
	"strings"
	"testing"
)

// The Projects button of the sessions section at a desk opens the settings of
// the map in three columns straight away — the first contour on its page, no
// list in a panel at the right before them — and they open again where they
// were left. What the list used to hold lives in the columns: the tree finds
// a group or a project by name and opens the project it found, and adds a new
// contour at its foot; the page of a project starts a new session of it from
// its heading, the start closing the settings, and deletes it at its foot,
// leaving the group on its page.
func TestTheProjectsButtonOpensTheColumns(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built: the columns are laid out by the stylesheet — run make front first")
	}
	var got struct {
		Opened           []string        `json:"opened"`
		Panel            bool            `json:"panel"`
		Active           bool            `json:"active"`
		Tree             []string        `json:"tree"`
		Adds             string          `json:"adds"`
		Found            []string        `json:"found"`
		FoundOpens       []string        `json:"foundOpens"`
		None             string          `json:"none"`
		Start            string          `json:"start"`
		StartAsked       bool            `json:"startAsked"`
		StartSent        *deskCardAction `json:"startSent"`
		ClosedAfterStart bool            `json:"closedAfterStart"`
		Reopened         []string        `json:"reopened"`
		DeleteAsked      bool            `json:"deleteAsked"`
		DeleteSent       map[string]any  `json:"deleteSent"`
		AfterDelete      []string        `json:"afterDelete"`
		TreeAfterDelete  []string        `json:"treeAfterDelete"`
		NewContour       string          `json:"newContour"`
		Closed           bool            `json:"closed"`
	}
	runFixtureServing(t, "deskprojects.html", deskScreen, deskPointer, schemaAnswer(map[string]any{}), &got)

	if strings.Join(got.Opened, "|") != "personal|" || got.Panel || !got.Active {
		t.Errorf("the Projects button opened %v, a panel at the right %v, the button lit %v — expected the columns on the first contour and no panel",
			got.Opened, got.Panel, got.Active)
	}
	if want := "personal2|Home1|Pets3|Evirma2|Common1|Harness1|Algorithmics1|LMS1"; strings.Join(got.Tree, "|") != want {
		t.Errorf("the tree reads %v, expected %s", got.Tree, want)
	}
	if got.Adds != "new contour" {
		t.Errorf("the foot of the tree says %q — a new contour is added there", got.Adds)
	}
	if want := "personal2|Pets3|blog"; strings.Join(got.Found, "|") != want {
		t.Errorf("a query for blo leaves the tree as %v, expected %s — the group and the project found in it", got.Found, want)
	}
	if strings.Join(got.FoundOpens, "|") != "Pets|blog" {
		t.Errorf("a project found in the tree opens %v, expected its group and its page", got.FoundOpens)
	}
	if got.None != "nothing matches the query" {
		t.Errorf("a query that finds nothing says %q", got.None)
	}
	if got.Start != "New session" || !got.StartAsked || got.StartSent == nil || got.StartSent.Kind != "session.open" ||
		got.StartSent.Target != "blog" || got.StartSent.Params["project"] != float64(13) {
		t.Errorf("the heading of a project offers %q, asked %v, sent %+v — expected a new session of blog, addressed by the id of the project",
			got.Start, got.StartAsked, got.StartSent)
	}
	if !got.ClosedAfterStart {
		t.Error("a session started from the settings leaves them open over the column it comes up in")
	}
	if strings.Join(got.Reopened, "|") != "Pets|blog" {
		t.Errorf("the settings open again on %v, expected where they were left", got.Reopened)
	}
	if !got.DeleteAsked || got.DeleteSent["url"] != "/api/projects/13" {
		t.Errorf("the deletion of a project asked %v and sent %v", got.DeleteAsked, got.DeleteSent)
	}
	if strings.Join(got.AfterDelete, "|") != "Pets|" || strings.Join(got.TreeAfterDelete, "|") != "Home1|Pets2|Common1|Harness1|LMS1" {
		t.Errorf("after the deletion the columns read %v and the tree %v — the page of the project goes, its group stays with one fewer",
			got.AfterDelete, got.TreeAfterDelete)
	}
	if got.NewContour != "New contour" {
		t.Errorf("the foot of the tree opened %q", got.NewContour)
	}
	if !got.Closed {
		t.Error("the cross does not close the settings")
	}
}
