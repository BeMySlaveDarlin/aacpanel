package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A call the feed already shows comes again when the host learns more of it:
// its result arrived, or it failed. The new object takes the place of the one
// shown — wherever the run folded it, and whether the group comes back whole
// or with one call — so the flags reach the badges; no call is drawn twice,
// and a call folded into the group from elsewhere stays in it.
func TestAKnownCallIsReplacedByTheOneThatCameAgain(t *testing.T) {
	call := func(pos int, flags map[string]any) map[string]any {
		c := map[string]any{"name": "Bash", "pos": pos, "index": 0, "use": "u"}
		for k, v := range flags {
			c[k] = v
		}
		return c
	}
	tools := func(pos, run int, kind string, calls ...map[string]any) map[string]any {
		return map[string]any{"role": "tools", "kind": kind, "run": run, "pos": pos, "calls": calls}
	}
	open := map[string]any{"open": true}
	failed := map[string]any{"failed": true}
	ai := map[string]any{"role": "ai", "pos": 20, "text": "ok"}

	cases := []struct {
		name  string
		steps [][]map[string]any
		want  string
	}{
		{"the group comes back whole with its result",
			[][]map[string]any{{tools(10, 10, "bash", call(10, open))}, {tools(10, 10, "bash", call(10, nil))}},
			"tools@10[10-0]"},
		{"a call folded into an earlier group gets its flag there, and the group keeps its own",
			[][]map[string]any{{tools(10, 10, "bash", call(10, open))}, {tools(12, 12, "bash", call(12, open))},
				{tools(12, 12, "bash", call(12, failed))}},
			"tools@10[10-0:open 12-0:failed]"},
		{"a group of the run that came back after a reply does not come again after it",
			[][]map[string]any{{tools(10, 10, "bash", call(10, open)), ai}, {tools(10, 10, "bash", call(10, failed))}},
			"tools@10[10-0:failed] ai@20"},
		{"a group that came back with a call more keeps the known one and gains the new",
			[][]map[string]any{{tools(10, 10, "bash", call(10, open))},
				{tools(10, 10, "bash", call(10, nil), map[string]any{"name": "Bash", "pos": 10, "index": 1, "open": true})}},
			"tools@10[10-0 10-1:open]"},
		{"a group of another kind at the same place is left alone",
			[][]map[string]any{{tools(10, 10, "bash", call(10, open)),
				tools(10, 10, "files", map[string]any{"name": "Read", "pos": 10, "index": 1, "open": true})},
				{tools(10, 10, "files", map[string]any{"name": "Read", "pos": 10, "index": 1})}},
			"tools@10[10-0:open] tools@10[10-1]"},
		{"a call new to a group joins its own kind, not another group at the same place",
			[][]map[string]any{{tools(10, 10, "bash", call(10, open)),
				tools(10, 10, "files", map[string]any{"name": "Read", "pos": 10, "index": 1})},
				{tools(10, 10, "files", map[string]any{"name": "Read", "pos": 10, "index": 1},
					map[string]any{"name": "Read", "pos": 10, "index": 2, "open": true})}},
			"tools@10[10-0:open] tools@10[10-1 10-2:open]"},
	}
	for _, c := range cases {
		if got := runMergeCallsJS(t, c.steps); got != c.want {
			t.Errorf("%s: %s, expected %s", c.name, got, c.want)
		}
	}
}

// runMergeCallsJS merges each step into the feed built so far and returns the
// feed: every item with its calls and the flags they carry.
func runMergeCallsJS(t *testing.T, steps [][]map[string]any) string {
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
const steps = JSON.parse(readFileSync(0, "utf8"));
let feed = [];
for (const step of steps) feed = merge(feed, step);
const flags = (c) => ["open", "failed"].filter((f) => c[f]).join(",");
process.stdout.write(JSON.stringify(feed.map((i) => i.role + "@" + i.pos
    + (i.calls ? "[" + i.calls.map((c) => c.pos + "-" + c.index + (flags(c) ? ":" + flags(c) : "")).join(" ") + "]" : ""))
    .join(" ")));
`
	raw, err := json.Marshal(steps)
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
	var got string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	return strings.TrimSpace(got)
}
