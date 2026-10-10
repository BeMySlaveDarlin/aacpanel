package chat

import (
	"encoding/json"
	"testing"
)

// The question a codex thread waits on crosses the service whole — the id of
// each question, whether it takes words, hides them or is required, and who
// asks — and the lists of the state stay lists.
func TestTheQuestionOfCodexCrossesTheServiceWhole(t *testing.T) {
	sent := `{"tasks":[],"agents":[],"ask":{"sessionId":"t","toolUseId":"0","at":"2026-10-10T08:40:00Z",` +
		`"server":"tracker","message":"File the bug","questions":[{"id":"severity","text":"Severity","header":"severity",` +
		`"multi":false,"options":[{"label":"low"}],"other":true,"secret":true,"required":true}]}}`
	var w Work
	if err := json.Unmarshal([]byte(sent), &w); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(w)
	want := `{"tasks":[],"agents":[],"ask":{"sessionId":"t","toolUseId":"0","at":"2026-10-10T08:40:00Z",` +
		`"questions":[{"id":"severity","text":"Severity","header":"severity","multi":false,"options":[{"label":"low"}],` +
		`"other":true,"secret":true,"required":true}],"server":"tracker","message":"File the bug"}}`
	if string(got) != want {
		t.Errorf("the state reaches the screen as\n%s\nmeant\n%s", got, want)
	}
}
