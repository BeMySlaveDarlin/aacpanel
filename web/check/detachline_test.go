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
