package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAnsweredMarkAgainstSnapshot(t *testing.T) {
	const (
		asked     = `answered({ status: "waiting", waitingFor: "input needed" }, "toolu_1", 1000)`
		permit    = `answered({ status: "waiting", waitingFor: "dialog open" }, "", 1000)`
		same      = `{ status: "waiting", waitingFor: "input needed" }`
		stamped   = `answered({ status: "waiting", waitingFor: "input needed", statusUpdatedAt: 700 }, "toolu_1", 1000)`
		sameStamp = `{ status: "waiting", waitingFor: "input needed", statusUpdatedAt: 700 }`
		newStamp  = `{ status: "waiting", waitingFor: "input needed", statusUpdatedAt: 3000 }`
	)
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"an old waiting after the answer lags behind", `lagging(` + asked + `, ` + same + `, 5000)`, true},
		{"another reason for waiting does not lag", `lagging(` + asked + `, { status: "waiting", waitingFor: "dialog open" }, 5000)`, false},
		{"a busy session does not lag", `lagging(` + asked + `, { status: "busy" }, 5000)`, false},
		{"no snapshot, no lag", `lagging(` + asked + `, null, 5000)`, false},
		{"the ceiling has expired — trust the snapshot", `lagging(` + asked + `, ` + same + `, 1000 + ANSWER_LAG_MS)`, false},
		{"a moment before the ceiling it still lags", `lagging(` + asked + `, ` + same + `, 999 + ANSWER_LAG_MS)`, true},
		{"no mark, no lag", `lagging(null, ` + same + `, 5000)`, false},
		{"permission: an old waiting lags", `lagging(` + permit + `, { status: "waiting", waitingFor: "dialog open" }, 5000)`, true},
		{"no reason on either side — lags", `lagging(answered({ status: "waiting" }, "", 1000), { status: "waiting" }, 5000)`, true},
		{"the reason has appeared — no lag", `lagging(answered({ status: "waiting" }, "", 1000), { status: "waiting", waitingFor: "input needed" }, 5000)`, false},

		{"the same waiting after busy is a new dialog", `lagging(settle(` + asked + `, { status: "busy" }), ` + same + `, 5000)`, false},
		{"settle with no news keeps the same mark", `(() => { const m = ` + asked + `; return settle(m, ` + same + `) === m; })()`, true},
		{"settle without a snapshot is news", `settle(` + asked + `, null).settled`, true},
		{"settle of no mark is no mark", `settle(null, ` + same + `) === null`, true},

		{"the same question is hidden", `hidesAsk(` + asked + `, "toolu_1", 5000)`, true},
		{"the next question is shown", `hidesAsk(` + asked + `, "toolu_2", 5000)`, false},
		{"a question without an id is shown", `hidesAsk(` + permit + `, "", 5000)`, false},
		{"the same question after the ceiling is shown", `hidesAsk(` + asked + `, "toolu_1", 1000 + ANSWER_LAG_MS)`, false},
		{"the same question on a busy session is hidden", `hidesAsk(settle(` + asked + `, { status: "busy" }), "toolu_1", 5000)`, true},

		{"the same mark lags", `lagging(` + stamped + `, ` + sameStamp + `, 5000)`, true},
		{"a new mark with the same reason is a new dialog", `lagging(` + stamped + `, ` + newStamp + `, 5000)`, false},
		{"a new mark makes settle report news", `settle(` + stamped + `, ` + newStamp + `).settled`, true},
		{"the same mark makes settle report no news", `(() => { const m = ` + stamped + `; return settle(m, ` + sameStamp + `) === m; })()`, true},
		{"the snapshot has no mark — compare by reason", `lagging(` + stamped + `, ` + same + `, 5000)`, true},
		{"there was no mark at answer time — compare by reason", `lagging(` + asked + `, ` + newStamp + `, 5000)`, true},
		{"the mark is stored as a number", `answered({ status: "waiting", statusUpdatedAt: 700 }, "", 1000).statusAt === 700`, true},
		{"a mark as a string is no mark", `answered({ status: "waiting", statusUpdatedAt: "700" }, "", 1000).statusAt === 0`, true},
		{"a string mark in the snapshot — compare by reason", `lagging(` + stamped + `, { status: "waiting", waitingFor: "input needed", statusUpdatedAt: "700" }, 5000)`, true},
	}

	exprs := make([]string, 0, len(cases))
	for _, c := range cases {
		exprs = append(exprs, c.expr)
	}
	got := runAnsweredJS(t, exprs)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: %s = %v, expected %v", c.name, c.expr, got[i], c.want)
		}
	}
}

// TestAnsweredMarkOutlivesTheScreen checks the module store: the chat screen unmounts on
// navigation, and a mark kept only in its state dies with it — the composer locks again
// and the answered card comes back until the snapshot catches up.
func TestAnsweredMarkOutlivesTheScreen(t *testing.T) {
	const (
		asked  = `answered({ status: "waiting", waitingFor: "input needed" }, "toolu_1", 1000)`
		permit = `answered({ status: "waiting", waitingFor: "dialog open" }, "", 1000)`
		same   = `{ status: "waiting", waitingFor: "input needed" }`
	)
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"the mark is read back after the screen is gone", `(() => { const m = remember("s1", ` + asked + `); return recall("s1", 5000) === m; })()`, true},
		{"a second read still sees it", `(() => { remember("s2", ` + asked + `); recall("s2", 5000); return recall("s2", 5000) !== null; })()`, true},
		{"the answered question stays hidden on return", `(() => { remember("s3", ` + asked + `); return hidesAsk(recall("s3", 5000), "toolu_1", 5000); })()`, true},
		{"an old waiting still lags on return", `(() => { remember("s4", ` + asked + `); return lagging(recall("s4", 5000), ` + same + `, 5000); })()`, true},
		{"a mark settled before leaving stays settled", `(() => { remember("s5", ` + asked + `); remember("s5", settle(recall("s5", 5000), { status: "busy" })); return lagging(recall("s5", 5000), ` + same + `, 5000); })()`, false},

		{"the ceiling has expired — the mark is gone", `(() => { remember("s6", ` + asked + `); return recall("s6", 1000 + ANSWER_LAG_MS) === null; })()`, true},
		{"a moment before the ceiling it is still there", `(() => { remember("s7", ` + asked + `); return recall("s7", 999 + ANSWER_LAG_MS) !== null; })()`, true},
		{"forgetting clears the mark", `(() => { remember("s8", ` + asked + `); remember("s8", null); return recall("s8", 5000) === null; })()`, true},
		{"a session never answered has no mark", `recall("never", 5000) === null`, true},

		{"another session does not see the mark", `(() => { remember("s9", ` + asked + `); return recall("s10", 5000) === null; })()`, true},
		{"the mark of one session hides nothing in another", `(() => { remember("s11", ` + asked + `); return hidesAsk(recall("s12", 5000), "toolu_1", 5000); })()`, false},
		{"the mark hides only its own question", `(() => { remember("s13", ` + asked + `); return hidesAsk(recall("s13", 5000), "toolu_2", 5000); })()`, false},
		{"a new answer replaces the old one", `(() => { remember("s14", ` + asked + `); const m = remember("s14", ` + permit + `); return recall("s14", 5000) === m; })()`, true},
		{"after a new answer the old question is shown", `(() => { remember("s15", ` + asked + `); remember("s15", ` + permit + `); return hidesAsk(recall("s15", 5000), "toolu_1", 5000); })()`, false},
	}

	exprs := make([]string, 0, len(cases))
	for _, c := range cases {
		exprs = append(exprs, c.expr)
	}
	got := runAnsweredJS(t, exprs)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: %s = %v, expected %v", c.name, c.expr, got[i], c.want)
		}
	}
}

func runAnsweredJS(t *testing.T, exprs []string) []bool {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the answer mark is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "answered.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import * as m from ` + jsString("file://"+path) + `;
const exprs = JSON.parse(readFileSync(0, "utf8"));
const names = Object.keys(m);
const out = exprs.map((src) => Boolean(new Function(...names, "return (" + src + ");")(...names.map((n) => m[n]))));
process.stdout.write(JSON.stringify(out));
`
	raw, err := json.Marshal(exprs)
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
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(exprs) {
		t.Fatalf("%d answers to %d expressions", len(got), len(exprs))
	}
	return got
}
