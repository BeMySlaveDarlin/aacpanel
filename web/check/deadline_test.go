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
			world:   deadlineWorld{Name: "the server never answers", Event: "pushsubscriptionchange"},
			aborted: true,
			why:     "the event must end on its own, and the request it was waiting for must be dropped",
		},
		{
			world:   deadlineWorld{Name: "the request does not hear the abort", Event: "pushsubscriptionchange", Deaf: true},
			aborted: false,
			why:     "a request that ignores the abort, or a subscribe that hangs, must not hold the event either",
		},
	}

	worlds := make([]deadlineWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runDeadline(t, worlds)
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

// The button that quiets a push is pressed in the notification shade, and the
// worker asks the panel inside the event of the press. A panel that is
// restarting — a release is exactly when it does — would keep the event, and
// the old worker with it, waiting: the update tapped next waits the grace out
// and comes back. The press gives up at the deadline and says it was not
// taken; a panel that answers is still heard.
func TestTheQuietButtonLetsTheWorkerGoAtItsDeadline(t *testing.T) {
	cases := []struct {
		world   deadlineWorld
		aborted bool
		shown   string
		why     string
	}{
		{
			world:   deadlineWorld{Name: "the server never answers", Event: "notificationclick"},
			aborted: true,
			shown:   "Not quieted",
			why:     "the event must end on its own, drop the request and say the panel did not take it",
		},
		{
			world:   deadlineWorld{Name: "the request does not hear the abort", Event: "notificationclick", Deaf: true},
			aborted: false,
			shown:   "Not quieted",
			why:     "a request that ignores the abort must not hold the event either",
		},
		{
			world: deadlineWorld{Name: "the server takes it", Event: "notificationclick", Answers: true},
			shown: "Quieted: tests",
			why:   "the deadline must not cost a press the panel answered",
		},
	}

	worlds := make([]deadlineWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runDeadline(t, worlds)
	for i, c := range cases {
		r := got[i]
		if r.Outcome != "settled" {
			t.Errorf("%s: the event was still waiting when the test gave up — %s", c.world.Name, c.why)
			continue
		}
		if r.Deadline < 10000 || r.Deadline > 15000 {
			t.Errorf("%s: the press gave itself %d ms, expected between 10000 and 15000 — %s",
				c.world.Name, r.Deadline, c.why)
		}
		if r.Aborted != c.aborted {
			t.Errorf("%s: the request was aborted %v, expected %v — %s",
				c.world.Name, r.Aborted, c.aborted, c.why)
		}
		if len(r.Shown) != 1 || r.Shown[0] != c.shown {
			t.Errorf("%s: the worker showed %q, expected one push %q — %s", c.world.Name, r.Shown, c.shown, c.why)
		}
	}
}

// deadlineWorld is one event the worker gets and the server behind it: Event
// is the event dispatched, Deaf a request that does not hear the abort, and
// Answers a server that takes the request at once.
type deadlineWorld struct {
	Name    string `json:"name"`
	Event   string `json:"event"`
	Deaf    bool   `json:"deaf"`
	Answers bool   `json:"answers"`
}

type deadlineRun struct {
	Outcome  string   `json:"outcome"`
	Deadline int      `json:"deadline"`
	Aborted  bool     `json:"aborted"`
	Shown    []string `json:"shown"`
}

func runDeadline(t *testing.T, worlds []deadlineWorld) []deadlineRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the deadline of the worker's own requests is held by the engine, not by reading the source")
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

// What each event is dispatched with: the press of the quiet button carries
// the source the push named.
const shapes = {
    pushsubscriptionchange: {},
    notificationclick: {
        action: "quiet",
        notification: { close() {}, data: { quiet: { what: "kind", key: "push.test", label: "Quiet: tests" } } },
    },
};

const worlds = JSON.parse(readFileSync(0, "utf8"));
const out = [];
for (const w of worlds) {
    let deadline = 0;
    let aborted = false;
    const shown = [];
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
        clients: { claim: async () => {}, matchAll: async () => [], openWindow: async () => null },
        skipWaiting: () => {},
        registration: {
            pushManager: { subscribe: async () => ({
                endpoint: ENDPOINT,
                toJSON() { return { endpoint: ENDPOINT, keys: { p256dh: "p", auth: "a" } }; },
            }) },
            showNotification: async (title) => { shown.push(title); },
        },
    };
    const caches = { open: async () => ({ match: async () => undefined, put: async () => {}, addAll: async () => {} }), keys: async () => [], delete: async () => true };
    // The server is unreachable: the request never answers. It gives up when it
    // is aborted, unless this world is deaf to that. A server that answers
    // takes the request at once.
    const fetch = (input, init) => new Promise((done, fail) => {
        if (w.answers) {
            done(new Response("{}", { status: 200 }));
            return;
        }
        const signal = init && init.signal;
        if (!signal || w.deaf) return;
        signal.addEventListener("abort", () => {
            aborted = true;
            fail(new DOMException("the request was aborted", "AbortError"));
        });
    });

    boot(self, caches, fetch);
    for (const fn of handlers[w.event] || []) {
        fn({ ...shapes[w.event], waitUntil: (p) => waits.push(Promise.resolve(p).catch(() => {})) });
    }
    let guard;
    const held = new Promise((done) => { guard = tick(() => done("held"), GUARD_MS); });
    const outcome = await Promise.race([Promise.all(waits).then(() => waits.length ? "settled" : "no event"), held]);
    clearTimeout(guard);
    globalThis.setTimeout = tick;
    out.push({ outcome, deadline, aborted, shown });
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
