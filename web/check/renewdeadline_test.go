package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

// The renewal of a push subscription runs inside waitUntil, and the browser
// keeps the old worker alive for as long as that event waits. A renewal that
// hangs on a server which is down or restarting therefore holds the release
// back: the panel offers a new version, the worker behind it never leaves, and
// the offer comes back.
func TestTheRenewalLetsTheWorkerGoAtItsDeadline(t *testing.T) {
	cases := []struct {
		world   deadlineWorld
		aborted bool
		why     string
	}{
		{
			world:   deadlineWorld{Name: "the server never answers"},
			aborted: true,
			why:     "the event must end on its own, and the request it was waiting for must be dropped",
		},
		{
			world:   deadlineWorld{Name: "the request does not hear the abort", Deaf: true},
			aborted: false,
			why:     "a request that ignores the abort, or a subscribe that hangs, must not hold the event either",
		},
	}

	worlds := make([]deadlineWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runRenewDeadline(t, worlds)
	for i, c := range cases {
		r := got[i]
		if r.Outcome != "settled" {
			t.Errorf("%s: the event was still waiting when the test gave up — %s", c.world.Name, c.why)
		}
		if r.Deadline < 10000 || r.Deadline > 15000 {
			t.Errorf("%s: the renewal gave itself %d ms, expected between 10000 and 15000 — %s",
				c.world.Name, r.Deadline, c.why)
		}
		if r.Aborted != c.aborted {
			t.Errorf("%s: the request was aborted %v, expected %v — %s",
				c.world.Name, r.Aborted, c.aborted, c.why)
		}
	}
}

type deadlineWorld struct {
	Name string `json:"name"`
	Deaf bool   `json:"deaf"`
}

type deadlineRun struct {
	Outcome  string `json:"outcome"`
	Deadline int    `json:"deadline"`
	Aborted  bool   `json:"aborted"`
}

func runRenewDeadline(t *testing.T, worlds []deadlineWorld) []deadlineRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the deadline of the renewal is held by the engine, not by reading the source")
	}
	script := `
import { readFileSync } from "node:fs";

const ORIGIN = "https://panel.example";
const ENDPOINT = "https://push.example.net/new";
const boot = new Function("self", "caches", "fetch", ` + jsString(builtWorker(t)) + `);

// The deadline is counted in seconds, and the test cannot wait them out: the
// wait is shrunk here, and the length the worker asked for is remembered.
const tick = globalThis.setTimeout;
const GUARD_MS = 2000;

const worlds = JSON.parse(readFileSync(0, "utf8"));
const out = [];
for (const w of worlds) {
    let deadline = 0;
    let aborted = false;
    globalThis.setTimeout = (fn, ms, ...rest) => {
        if (ms >= 1000) {
            deadline = Math.max(deadline, ms);
            ms = 30;
        }
        return tick(fn, ms, ...rest);
    };

    const handlers = {};
    const waits = [];
    const self = {
        location: { origin: ORIGIN },
        addEventListener: (type, fn) => { (handlers[type] ||= []).push(fn); },
        clients: { claim: async () => {}, matchAll: async () => [] },
        skipWaiting: () => {},
        registration: { pushManager: { subscribe: async () => ({
            endpoint: ENDPOINT,
            toJSON() { return { endpoint: ENDPOINT, keys: { p256dh: "p", auth: "a" } }; },
        }) } },
    };
    const caches = { open: async () => ({ match: async () => undefined, put: async () => {}, addAll: async () => {} }), keys: async () => [], delete: async () => true };
    // The server is unreachable: the request never answers. It gives up when it
    // is aborted, unless this world is deaf to that.
    const fetch = (input, init) => new Promise((_, fail) => {
        const signal = init && init.signal;
        if (!signal || w.deaf) return;
        signal.addEventListener("abort", () => {
            aborted = true;
            fail(new DOMException("the request was aborted", "AbortError"));
        });
    });

    boot(self, caches, fetch);
    for (const fn of handlers.pushsubscriptionchange || []) {
        fn({ waitUntil: (p) => waits.push(Promise.resolve(p).catch(() => {})) });
    }
    let guard;
    const held = new Promise((done) => { guard = tick(() => done("held"), GUARD_MS); });
    const outcome = await Promise.race([Promise.all(waits).then(() => "settled"), held]);
    clearTimeout(guard);
    globalThis.setTimeout = tick;
    out.push({ outcome, deadline, aborted });
}
process.stdout.write(JSON.stringify(out));
`
	raw, err := json.Marshal(worlds)
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
	var got []deadlineRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(worlds) {
		t.Fatalf("%d answers to %d worlds", len(got), len(worlds))
	}
	return got
}
