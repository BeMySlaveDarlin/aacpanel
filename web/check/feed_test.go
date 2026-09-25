package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
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

// A line that arrives inside a run of calls — a background task done, a
// warning of claude — hangs under the run's badges and does not split it; the
// same line between two messages is a row of its own. A finished task still
// marks its call done.
func TestALineInsideARunHangsUnderIt(t *testing.T) {
	bash := func(pos int, run int, use string) map[string]any {
		return map[string]any{"role": "tools", "kind": "bash", "run": run, "pos": pos,
			"calls": []map[string]any{{"name": "Bash", "pos": pos, "index": 0, "use": use}}}
	}
	notice := map[string]any{"role": "notice", "text": "Unknown command: /storage", "level": "warn", "pos": 2}
	done := map[string]any{"role": "taskdone", "use": "u1", "status": "completed", "summary": "Background command \"make\" completed", "pos": 4}
	ai := func(pos int) map[string]any { return map[string]any{"role": "ai", "text": "ok", "pos": pos} }

	cases := []struct {
		name     string
		items    []map[string]any
		incoming []map[string]any
		want     []string
	}{
		{"a line inside a run hangs under it, and the run stays one row",
			[]map[string]any{bash(1, 1, "u1"), notice, bash(3, 1, "u3"), done}, nil,
			[]string{"toolrow[bash:2 done:1]{notice,taskdone}"}},
		{"a line between two messages is a row of its own",
			[]map[string]any{ai(1), notice, ai(3)}, nil,
			[]string{"ai", "notice", "ai"}},
		{"a call arriving live after a line joins the run the line hangs under",
			[]map[string]any{bash(1, 1, "u1"), notice}, []map[string]any{bash(3, 3, "u3")},
			[]string{"toolrow[bash:2 done:0]{notice}"}},
	}
	for _, c := range cases {
		got := runRowsJS(t, c.items, c.incoming)
		if strings.Join(got, " | ") != strings.Join(c.want, " | ") {
			t.Errorf("%s: %v, expected %v", c.name, got, c.want)
		}
	}
}

func runRowsJS(t *testing.T, items, incoming []map[string]any) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the feed is folded by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "feed.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import { merge, rows, weld } from ` + jsString("file://"+path) + `;
const [items, incoming] = JSON.parse(readFileSync(0, "utf8"));
const shape = (row) => {
    if (row.role !== "toolrow") return row.role;
    const groups = row.groups.map((g) => g.kind + ":" + g.calls.length
        + " done:" + g.calls.filter((c) => c.done).length).join("+");
    return "toolrow[" + groups + "]{" + (row.lines || []).map((l) => l.role).join(",") + "}";
};
process.stdout.write(JSON.stringify(rows(weld(merge(items, incoming || []))).map(shape)));
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
