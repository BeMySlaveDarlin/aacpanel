package chat

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// A run of codex exec a claude session started stands among its agents with
// the word that it is codex's and the id of its thread, which its feed opens
// by. The state crosses the service through Work, and a key Work has no field
// for is dropped without a word: the run is made here by the collector's own
// functions, and every key it carries has to come out the other side.
func TestACodexRunCrossesTheServiceWhole(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 was not found: the run is made by the collector itself")
	}
	cmd := exec.Command(python, filepath.Join("testdata", "codexrun.py"), filepath.Join("..", "..", "agent"))
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	raw, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("the collector did not make the run: %v\n%s", err, ee.Stderr)
		}
		t.Fatal(err)
	}
	var sent, got struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("the state did not parse: %v\n%s", err, raw)
	}
	var w Work
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(w)
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(sent.Agents) != 1 || len(got.Agents) != 1 {
		t.Fatalf("the state sent %s and reached the screen as %s", raw, out)
	}
	for key, value := range sent.Agents[0] {
		if !reflect.DeepEqual(got.Agents[0][key], value) {
			t.Errorf("%q of the run reached the screen as %v, the collector sent %v", key, got.Agents[0][key], value)
		}
	}
	if got.Agents[0]["agent"] != "codex" || got.Agents[0]["id"] != "01a12600-0000-7000-8000-0000000000e1" {
		t.Errorf("the run reached the screen as %v: not codex's, or not by its thread", got.Agents[0])
	}
}

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
