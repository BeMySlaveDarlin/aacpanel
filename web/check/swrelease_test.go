package check

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// releaseRun is what the page saw of the worker in releaseWorld: the requests
// the worker said it had out before RELEASE, the ones RELEASE said it cut and
// the ones still out after it, and how each answer the page was reading ended
// — "read" to the end of its body, "torn" when its body failed, "failed" when
// the answer itself did, "open" when it was still waiting at the end. Leftover
// is what the worker still counts once every later answer has ended.
type releaseRun struct {
	Before   []liveRequest     `json:"before"`
	Released []liveRequest     `json:"released"`
	After    []liveRequest     `json:"after"`
	Ended    map[string]string `json:"ended"`
	Leftover []liveRequest     `json:"leftover"`
}

type liveRequest struct {
	Path string  `json:"path"`
	Secs float64 `json:"secs"`
}

// releaseWorld runs the bundled worker in node. The network hands every
// stalled answer its first bytes and nothing more, and it does not hear the
// abort once the answer has begun: the page's body ends only if the worker
// itself cuts it.
const releaseWorld = `
const ORIGIN = "https://panel.example";
const NEAR = "http://127.0.0.1:8777";
const boot = new Function("self", "caches", "fetch", WORKER);
const handlers = {};
const self = {
    location: { origin: ORIGIN },
    addEventListener: (type, fn) => { (handlers[type] ||= []).push(fn); },
    clients: { claim: async () => {}, matchAll: async () => [] },
    skipWaiting: () => {},
};
const store = new Map();
const caches = {
    open: async (name) => {
        if (!store.has(name)) store.set(name, new Map());
        const box = store.get(name);
        return {
            match: async (req) => box.get(typeof req === "string" ? req : req.url),
            put: async (req, res) => { box.set(typeof req === "string" ? req : req.url, res); },
            addAll: async () => {},
        };
    },
    keys: async () => [...store.keys()],
    delete: async (name) => store.delete(name),
};
const enc = (s) => new TextEncoder().encode(s);
const fetch = (input, init) => {
    const url = new URL(typeof input === "string" ? input : input.url);
    const signal = init && init.signal;
    if (url.pathname === "/api/pending") {
        return new Promise((_, reject) => signal.addEventListener("abort", () => reject(signal.reason)));
    }
    if (url.pathname === "/dist/broken.js" && url.origin === NEAR) {
        return Promise.resolve(new Response("gone", { status: 503 }));
    }
    if (url.pathname === "/api/empty") return Promise.resolve(new Response(null, { status: 204 }));
    if (url.pathname.includes("stall") || url.pathname === "/app" || url.pathname === "/static/icon.svg") {
        return Promise.resolve(new Response(new ReadableStream({ start(ctl) { ctl.enqueue(enc("[1,")); } }), { status: 200 }));
    }
    return Promise.resolve(new Response("done", { status: 200 }));
};
boot(self, caches, fetch);

const tick = () => new Promise((r) => setImmediate(r));
const settle = async () => { for (let i = 0; i < 30; i++) await tick(); };
const tell = (data) => {
    let answer = null;
    const port = { postMessage: (d) => { answer = d; } };
    for (const fn of handlers.message || []) fn({ data, ports: [port], waitUntil: () => {} });
    return answer;
};
const ask = (path, mode = "cors") => {
    let answer = null;
    const url = new URL(path, ORIGIN).href;
    const request = mode === "navigate"
        ? { url, method: "GET", mode, headers: new Headers() }
        : new Request(url);
    for (const fn of handlers.fetch || []) fn({ request, respondWith: (p) => { answer = p; }, waitUntil: () => {} });
    return answer;
};

const ended = {};
// read takes the answer the way a page does, to the end of its body.
const read = (name, answer) => {
    ended[name] = "open";
    answer.then(
        async (response) => {
            const reader = response.body.getReader();
            try {
                while (!(await reader.read()).done);
                ended[name] = "read";
            } catch {
                ended[name] = "torn";
            }
        },
        () => { ended[name] = "failed"; },
    );
};

tell({ type: "ENDPOINTS", origins: [NEAR] });
tell({ type: "BASE", base: NEAR });
await settle();

// What a page leaves hanging on the worker: an answer of the api that stalls,
// code from the nearest panel that stalls, an answer that never begins, a
// file of the shell and the shell itself, each stalling halfway.
read("data", ask("/api/stall?session=a"));
read("code", ask("/dist/stall.js?v=2"));
read("pending", ask("/api/pending?x=1"));
read("asset", ask("/static/icon.svg"));
read("navigation", ask("/app", "navigate"));
await settle();
const before = tell({ type: "INFLIGHT" }).live;
const released = tell({ type: "RELEASE" }).released;
await settle();
const after = tell({ type: "INFLIGHT" }).live;

// What must leave nothing behind: an answer read to the end, one without a
// body, code the nearest panel refused, and an answer the page walked away from.
const full = ask("/api/host?x=1");
await (await full).text();
await ask("/api/empty");
const broken = ask("/dist/broken.js");
await (await broken).text();
const dropped = await ask("/api/stall-dropped");
const reader = dropped.body.getReader();
await reader.read();
await reader.cancel();
await settle();
const leftover = tell({ type: "INFLIGHT" }).live;

process.stdout.write(JSON.stringify({ before, released, after, ended, leftover }));
`

func runRelease(t *testing.T) releaseRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the worker lets go in an engine, not in its source")
	}
	script := strings.Replace(releaseWorld, "WORKER", jsString(builtWorker(t)), 1)
	out, err := exec.Command(node, "--input-type=module", "-e", script).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got releaseRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	return got
}

func paths(list []liveRequest) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.Path)
	}
	sort.Strings(out)
	return out
}

// The browser holds a new worker back for as long as the old one streams an
// answer to a page, and an answer that stalls never ends. Asked to let go, the
// worker cuts every request it has out — the bodies already on their way to a
// page with them, since those are what the browser counts — and says which
// they were.
func TestTheWorkerLetsGoOfWhatItHasOut(t *testing.T) {
	got := runRelease(t)
	want := []string{"/api/pending", "/api/stall", "/app", "/dist/stall.js", "/static/icon.svg"}

	if strings.Join(paths(got.Before), " ") != strings.Join(want, " ") {
		t.Errorf("before RELEASE the worker says it has %v out, expected %v — a path with its query, "+
			"or a request it does not count, is a holder the page cannot name", paths(got.Before), want)
	}
	if strings.Join(paths(got.Released), " ") != strings.Join(want, " ") {
		t.Errorf("RELEASE says it cut %v, expected %v", paths(got.Released), want)
	}
	for _, r := range got.Released {
		if r.Secs < 0 {
			t.Errorf("%s: hanging for %v seconds", r.Path, r.Secs)
		}
	}
	for name, how := range map[string]string{
		"data": "torn", "code": "torn", "asset": "torn", "navigation": "torn", "pending": "failed",
	} {
		if got.Ended[name] != how {
			t.Errorf("the %s answer ended %q after RELEASE, expected %q — a body the page is still "+
				"reading keeps the old worker at work, and the update waits behind it", name, got.Ended[name], how)
		}
	}
	if len(got.After) != 0 {
		t.Errorf("after RELEASE the worker still counts %v as out", paths(got.After))
	}
}

// A request leaves the count with its answer: read to the end, without a body,
// refused, or dropped by the page halfway. One that stays is a holder named on
// the phone that holds nothing, and a count that only grows.
func TestTheWorkerForgetsAnAnswerThatEnded(t *testing.T) {
	got := runRelease(t)
	if len(got.Leftover) != 0 {
		t.Errorf("answers that ended are still counted as out: %v", paths(got.Leftover))
	}
}
