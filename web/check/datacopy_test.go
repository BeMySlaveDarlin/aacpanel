package check

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// The worker answers a data request as soon as the answer arrives and stores
// its copy on the side: a fetch event that waited for the copy would stay in
// flight for as long as the body streams, and the browser holds a new worker
// back — the one the update banner asked for — while the old one has an event
// in flight.
func TestDataAnswerDoesNotWaitForItsCopy(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the worker is run by the engine, not read out of the source")
	}
	script := `
const ORIGIN = "https://panel.example";
const boot = new Function("self", "caches", "fetch", ` + jsString(builtWorker(t)) + `);
const handlers = {};
const store = new Map();
const at = (req) => (typeof req === "string" ? new URL(req, ORIGIN).href : req.url);
const self = {
    location: { origin: ORIGIN },
    addEventListener: (type, fn) => { (handlers[type] ||= []).push(fn); },
    clients: { claim: async () => {}, matchAll: async () => [] },
    skipWaiting: () => {},
};
const caches = {
    open: async (name) => {
        if (!store.has(name)) store.set(name, new Map());
        const box = store.get(name);
        return {
            match: async (req) => box.get(at(req)),
            put: async (req, res) => { box.set(at(req), res); },
            addAll: async () => {},
        };
    },
    keys: async () => [...store.keys()],
    delete: async (name) => store.delete(name),
};
// The answer's body stays open until the harness releases it.
let release = null;
const fetch = async () => new Response(new ReadableStream({
    start(ctl) {
        ctl.enqueue(new TextEncoder().encode('{"at":1}'));
        release = () => ctl.close();
    },
}), { status: 200, headers: { "Content-Type": "application/json" } });
boot(self, caches, fetch);

let answer = null;
const event = {
    request: new Request(ORIGIN + "/api/host"),
    respondWith: (p) => { answer = p; },
    waitUntil: () => {},
};
for (const fn of handlers.fetch || []) fn(event);
const tick = () => new Promise((r) => setImmediate(r));
let settled = false;
answer.then(() => { settled = true; }, () => { settled = true; });
for (let i = 0; i < 20; i++) await tick();
const before = settled;
release();
const body = await (await answer).text();
for (let i = 0; i < 20; i++) await tick();
const copy = (store.get("aacpanel-data") || new Map()).get(ORIGIN + "/api/host");
process.stdout.write(JSON.stringify({
    before,
    body,
    copied: copy ? await copy.text() : "",
    stamped: Boolean(copy && copy.headers.get("X-Cached-At")),
}));
`
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	raw, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got struct {
		Before  bool   `json:"before"`
		Body    string `json:"body"`
		Copied  string `json:"copied"`
		Stamped bool   `json:"stamped"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, raw)
	}
	if !got.Before {
		t.Error("the answer reached the page only after its body had ended — the fetch event stays in flight " +
			"for as long as the body streams, and the browser holds the next worker back for that long")
	}
	if got.Body != `{"at":1}` {
		t.Errorf("the page got %q, expected the answer as it came", got.Body)
	}
	if got.Copied != `{"at":1}` || !got.Stamped {
		t.Errorf("the copy in the data cache is %q (stamped: %v): the answer is still kept for the time without a connection", got.Copied, got.Stamped)
	}
}
