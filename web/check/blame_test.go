package check

import (
	"strings"
	"testing"
)

// Who wrote a file: a run of lines from one commit is named once, at its top,
// and runs take turns in tone. A line opens its commit right under it; a
// commit that names a conversation leads to it, one written by hand says so
// without looking, and a line nobody committed is the working tree's.
func TestALineLeadsToItsCommitAndOnToItsConversation(t *testing.T) {
	var got struct {
		Labels []string `json:"labels"`
		Tones  []bool   `json:"tones"`
		ByHand struct {
			Card  string `json:"card"`
			Under bool   `json:"under"`
		} `json:"byHand"`
		Uncommitted string   `json:"uncommitted"`
		Talk        string   `json:"talk"`
		Asked       []string `json:"asked"`
		Opened      []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Live bool   `json:"live"`
		} `json:"opened"`
		Cards int `json:"cards"`
	}
	runFixture(t, "blame.html", &got)

	if len(got.Labels) != 5 || !strings.HasPrefix(got.Labels[0], "aaaaaaa · ") || got.Labels[1] != "" ||
		!strings.HasPrefix(got.Labels[2], "bbbbbbb · ") || got.Labels[3] != "" || got.Labels[4] != "not committed" {
		t.Errorf("the lines are named %q: once at the top of each run", got.Labels)
	}
	if len(got.Tones) != 5 || got.Tones[0] != got.Tones[1] || got.Tones[1] == got.Tones[2] || got.Tones[3] == got.Tones[4] {
		t.Errorf("the runs take the tones %v: one tone for a run, the next run the other", got.Tones)
	}
	if !strings.Contains(got.ByHand.Card, "Written without a session") || !strings.Contains(got.ByHand.Card, "Person") {
		t.Errorf("a commit written by hand reads %q", got.ByHand.Card)
	}
	if !got.ByHand.Under {
		t.Error("the commit did not open under the line that was tapped")
	}
	if !strings.Contains(got.Uncommitted, "Not committed yet") {
		t.Errorf("a line nobody committed reads %q", got.Uncommitted)
	}
	if len(got.Asked) != 1 || !strings.Contains(got.Asked[0], "/api/repo/commit?"+strings.Repeat("b", 40)) {
		t.Errorf("the panel was asked %v: only the commit that names a conversation is looked up", got.Asked)
	}
	if !strings.Contains(got.Talk, "«aacpanel»") || !strings.Contains(got.Talk, "closed") {
		t.Errorf("the way to the conversation reads %q", got.Talk)
	}
	if len(got.Opened) != 1 || got.Opened[0].ID != "1a5f36fb-1187-4f80-a9bf-50a2c2eec482" || got.Opened[0].Live {
		t.Errorf("the conversation opened is %+v", got.Opened)
	}
	if got.Cards != 1 {
		t.Errorf("%d commits stand open at once: one line, one card", got.Cards)
	}
}
