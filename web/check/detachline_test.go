package check

import (
	"slices"
	"strings"
	"testing"
)

// The tools of a session on a phone take it from under the session it stands
// under, a claude session and a thread of codex alike, and only a session
// standing under another has the line; the press names the parent on the
// sheet before it goes to the host.
func TestTheToolsOfASessionTakeItFromUnderItsParent(t *testing.T) {
	var got struct {
		Child  []string `json:"child"`
		Alone  []string `json:"alone"`
		Thread []string `json:"thread"`
		Note   string   `json:"note"`
		Titles []string `json:"titles"`
		Sent   []string `json:"sent"`
	}
	runFixture(t, "detachline.html", &got)

	const item = "Detach from parent"
	if !slices.Contains(got.Child, item) || !slices.Contains(got.Thread, item) {
		t.Errorf("the tools of a session and of a thread aacpanel opened read %v and %v — without %q",
			got.Child, got.Thread, item)
	}
	if slices.Contains(got.Alone, item) {
		t.Errorf("the tools of a session standing on its own offer %q: %v", item, got.Alone)
	}
	if !strings.HasPrefix(got.Note, "under aacpanel") || !strings.Contains(got.Note, "stands on its own in the list") {
		t.Errorf("the line says %q — it should name the parent and say the session stands on its own in the list",
			got.Note)
	}
	if want := []string{"Detach review from aacpanel?", "Detach codex-2 from aacpanel?"}; !slices.Equal(got.Titles, want) {
		t.Errorf("the sheets before the detach ask %q, meant %q", got.Titles, want)
	}
	if want := []string{`session.detach:review:{}`, `session.detach:codex-2:{}`}; !slices.Equal(got.Sent, want) {
		t.Errorf("the presses sent %v, meant %v", got.Sent, want)
	}
}

func TestTheToolsChooseAParentWithinTheProject(t *testing.T) {
	var got struct {
		Child, Alone, Thread, Outside []string
		Parents                       []string
		ParentSent                    []ctxSent
		Blocked                       map[string]struct {
			Off    bool
			Reason string
		}
		Refused struct {
			Message string
			Open    bool
		}
	}
	runFixture(t, "detachline.html", &got)
	const label = "Stand under…"
	for name, labels := range map[string][]string{"claude": got.Child, "codex": got.Thread} {
		at := slices.Index(labels, "Detach from parent")
		if at < 0 || at+1 >= len(labels) || labels[at+1] != label {
			t.Errorf("%s tools do not put %q next to Detach: %v", name, label, labels)
		}
	}
	if !slices.Contains(got.Alone, label) || slices.Contains(got.Outside, label) {
		t.Errorf("parent choice on a standalone session: %v; outside the panel: %v", got.Alone, got.Outside)
	}
	if want := []string{"solo", "Codex parent"}; !slices.Equal(got.Parents, want) {
		t.Errorf("parent choices are %v, want %v: exclude self, current parent, descendants and other or unknown projects", got.Parents, want)
	}
	for id, why := range map[string]string{
		"empty":    "no other live session in this project",
		"off-map":  "belongs to no project of the map",
		"old-host": "does not know the action “session.parent”",
	} {
		row := got.Blocked[id]
		if !row.Off || !strings.Contains(row.Reason, why) {
			t.Errorf("%s parent line: %+v, want disabled with %q", id, row, why)
		}
	}
	if !got.Refused.Open || !strings.Contains(got.Refused.Message, "Not done") ||
		!strings.Contains(got.Refused.Message, "the chosen parent has ended") {
		t.Errorf("a refused parent choice must show the gate's error and allow retry: %+v", got.Refused)
	}
	want := [][2]string{{"review", "codex-2"}, {"review", "codex-2"}, {"codex-2", "solo"}, {"solo", "review"}}
	if len(got.ParentSent) != len(want) {
		t.Fatalf("parent choices sent %+v, want %d requests", got.ParentSent, len(want))
	}
	for i, pair := range want {
		a := got.ParentSent[i]
		if a.Kind != "session.parent" || a.Target != pair[0] || len(a.Params) != 1 || a.Params["parent"] != pair[1] {
			t.Errorf("parent choice %d sent %+v, want session.parent of %s with parent %s", i, a, pair[0], pair[1])
		}
	}
}
