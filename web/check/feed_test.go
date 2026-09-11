package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMergeReplacesBubbleWithWakeCard(t *testing.T) {
	bubble := map[string]any{"role": "me", "text": "Continue the loop", "pos": 140}
	shots := map[string]any{"role": "shots", "pos": 140}
	card := map[string]any{"role": "wake", "text": "Continue the loop", "pos": 140, "fixes": "me"}
	plain := map[string]any{"role": "wake", "text": "Continue the loop", "pos": 140}

	cases := []struct {
		name  string
		items []map[string]any
		incom []map[string]any
		want  []string
	}{
		{"the card replaces the bubble", []map[string]any{bubble}, []map[string]any{card}, []string{"wake"}},
		{"without the marker both stay", []map[string]any{bubble}, []map[string]any{plain}, []string{"me", "wake"}},
		{"another role at the same position is left alone",
			[]map[string]any{shots, bubble}, []map[string]any{card}, []string{"shots", "wake"}},
		{"replacing does not breed a third row on a repeat",
			[]map[string]any{bubble}, []map[string]any{card, card}, []string{"wake"}},
	}
	for _, c := range cases {
		got := runFeedJS(t, c.items, c.incom)
		if len(got) != len(c.want) {
			t.Errorf("%s: %d rows, expected %d: %v", c.name, len(got), len(c.want), got)
			continue
		}
		for i, want := range c.want {
			if got[i] != want {
				t.Errorf("%s: row %d is %q, expected %q", c.name, i, got[i], want)
			}
		}
	}
}

func runFeedJS(t *testing.T, items, incoming []map[string]any) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the feed merge is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "feed.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import { merge } from ` + jsString("file://"+path) + `;
const [items, incoming] = JSON.parse(readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(merge(items, incoming).map((i) => i.role)));
`
	raw, err := json.Marshal([]any{items, incoming})
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
	return got
}
