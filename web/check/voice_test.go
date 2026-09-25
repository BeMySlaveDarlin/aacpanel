package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskVoiceTellsSilenceFromLongPause(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) string {
		return now.Add(-d).Format(time.RFC3339)
	}
	cases := []struct {
		name string
		task map[string]any
		want string
	}{
		{"a command keeps silent in its own way", map[string]any{"kind": "bash", "event": ago(time.Minute)}, "null"},
		{"a fresh event", map[string]any{"kind": "aacpanel", "event": ago(4 * time.Minute)},
			`{"silent":false,"hush":false}`},
		{"an hour fifty is still ordinary", map[string]any{"kind": "aacpanel", "event": ago(110 * time.Minute)},
			`{"silent":false,"hush":false}`},
		{"over two hours the tone changes", map[string]any{"kind": "aacpanel", "event": ago(3 * time.Hour)},
			`{"silent":false,"hush":true}`},
		{"there were no events at all", map[string]any{"kind": "aacpanel", "at": ago(19 * time.Hour)},
			`{"silent":true,"hush":true}`},
		{"the timestamp does not parse — say nothing", map[string]any{"kind": "aacpanel", "event": "yesterday"}, "null"},
	}
	calls := make([][]any, 0, len(cases))
	for _, c := range cases {
		calls = append(calls, []any{c.task, now.UnixMilli()})
	}
	got := runVoiceJS(t, calls)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: taskVoice = %s, expected %s", c.name, got[i], c.want)
		}
	}
}

func runVoiceJS(t *testing.T, calls [][]any) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the tone decision is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "voice.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import { taskVoice } from ` + jsString("file://"+path) + `;
const calls = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(calls.map(([task, now]) => JSON.stringify(taskVoice(task, now)))));
`
	raw, err := json.Marshal(calls)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(calls) {
		t.Fatalf("%d answers to %d calls", len(got), len(calls))
	}
	return got
}
