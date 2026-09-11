package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerTakesCodeFromNearestPanel(t *testing.T) {
	const (
		origin = "https://panel.example"
		near   = "http://127.0.0.1:8777"
	)
	cases := []struct {
		world  swWorld
		asked  []string
		bodies []string
		cached []string
	}{
		{
			world:  swWorld{Name: "no base — the code comes from our own origin", Requests: []string{"/dist/bundle.js"}},
			asked:  []string{origin + "/dist/bundle.js"},
			bodies: []string{"code from " + origin},
			cached: []string{origin + "/dist/bundle.js"},
		},
		{
			world: swWorld{
				Name: "the base is named — the whole of /dist/ comes from it", Tell: true, Base: near,
				Requests: []string{"/dist/bundle.js", "/dist/bundle.css", "/dist/term.js"},
			},
			asked:  []string{near + "/dist/bundle.js", near + "/dist/bundle.css", near + "/dist/term.js"},
			bodies: []string{"code from " + near, "code from " + near, "code from " + near},
			cached: []string{},
		},
		{
			world:  swWorld{Name: "the worker restarted — the base lives on disk", Keep: true, Requests: []string{"/dist/bundle.js"}},
			asked:  []string{near + "/dist/bundle.js"},
			bodies: []string{"code from " + near},
			cached: []string{},
		},
		{
			world: swWorld{
				Name: "the near address is silent — fall back to the origin, and the second file no longer waits for it",
				Keep: true, Dead: []string{near}, Requests: []string{"/dist/bundle.js", "/dist/bundle.css"},
			},
			asked:  []string{near + "/dist/bundle.js", origin + "/dist/bundle.js", origin + "/dist/bundle.css"},
			bodies: []string{"code from " + origin, "code from " + origin},
			cached: []string{origin + "/dist/bundle.js", origin + "/dist/bundle.css"},
		},
		{
			world:  swWorld{Name: "a dead base is removed from disk too", Keep: true, Requests: []string{"/dist/term.js"}},
			asked:  []string{origin + "/dist/term.js"},
			bodies: []string{"code from " + origin},
			cached: []string{origin + "/dist/bundle.js", origin + "/dist/bundle.css", origin + "/dist/term.js"},
		},
		{
			world:  swWorld{Name: "the worker does not rewrite /api/", Tell: true, Base: near, Requests: []string{"/api/host"}},
			asked:  []string{origin + "/api/host"},
			bodies: []string{"code from " + origin},
			cached: []string{},
		},
		{
			world:  swWorld{Name: "the shell statics are not rewritten", Keep: true, Tell: true, Base: near, Requests: []string{"/static/icons/icon.svg"}},
			asked:  []string{origin + "/static/icons/icon.svg"},
			bodies: []string{"code from " + origin},
			cached: []string{origin + "/static/icons/icon.svg"},
		},
		{
			world:  swWorld{Name: "the router said there is nobody nearer — back to our own origin", Keep: true, Tell: true, Base: "", Requests: []string{"/dist/bundle.js"}},
			asked:  []string{origin + "/dist/bundle.js"},
			bodies: []string{"code from " + origin},
			cached: []string{origin + "/static/icons/icon.svg", origin + "/dist/bundle.js"},
		},
		{
			world:  swWorld{Name: "an empty base lives on disk as well", Keep: true, Requests: []string{"/dist/bundle.js"}},
			asked:  []string{origin + "/dist/bundle.js"},
			bodies: []string{"code from " + origin},
			cached: []string{origin + "/static/icons/icon.svg", origin + "/dist/bundle.js"},
		},
	}

	worlds := make([]swWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runWorker(t, worlds)
	for i, c := range cases {
		r := got[i]
		if strings.Join(r.Asked, " ") != strings.Join(c.asked, " ") {
			t.Errorf("%s: requests went to %v, expected %v", c.world.Name, r.Asked, c.asked)
		}
		if strings.Join(r.Bodies, " ") != strings.Join(c.bodies, " ") {
			t.Errorf("%s: the page got %v, expected %v", c.world.Name, r.Bodies, c.bodies)
		}
		if strings.Join(r.Cached, " ") != strings.Join(c.cached, " ") {
			t.Errorf("%s: the shell cache holds %v, expected %v", c.world.Name, r.Cached, c.cached)
		}
	}
}

func TestChatImagesTravelAsBytes(t *testing.T) {
	src := screenSrc(t, "src/screens/chat.js")
	made := strings.Count(src, "createObjectURL")
	freed := strings.Count(src, "revokeObjectURL")
	if made == 0 {
		t.Error("the chat screen has no blob addresses: attachments are asked for by the browser through `img src` again, that is from the domain and past the nearest address")
	}
	if made != freed {
		t.Errorf("createObjectURL %d times against revokeObjectURL %d: an unreleased address holds the image bytes until the page reloads", made, freed)
	}
}

type swWorld struct {
	Name     string   `json:"name"`
	Keep     bool     `json:"keep"`
	Tell     bool     `json:"tell"`
	Base     string   `json:"base"`
	Dead     []string `json:"dead"`
	Requests []string `json:"requests"`
}

type swRun struct {
	Asked  []string `json:"asked"`
	Bodies []string `json:"bodies"`
	Cached []string `json:"cached"`
}

func runWorker(t *testing.T, worlds []swWorld) []swRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the route of the statics is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "sw.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";

const ORIGIN = "https://panel.example";
const src = readFileSync(` + jsString(path) + `, "utf8")
    .replaceAll("__VERSION__", JSON.stringify("test"))
    .replaceAll("__ASSETS__", "[]");
const boot = new Function("self", "caches", "fetch", src);

const worlds = JSON.parse(readFileSync(0, "utf8"));
const at = (req) => {
    if (typeof req === "string") return new URL(req, ORIGIN).href;
    return req instanceof URL ? req.href : req.url;
};
const out = [];
let store = null;

for (const w of worlds) {
    if (!w.keep || !store) store = new Map();
    const asked = [];
    const waits = [];
    const handlers = {};
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
                match: async (req) => { const hit = box.get(at(req)); return hit ? hit.clone() : undefined; },
                put: async (req, res) => { box.set(at(req), res); },
                addAll: async (list) => { for (const u of list) box.set(at(u), new Response("shell")); },
            };
        },
        keys: async () => [...store.keys()],
        delete: async (name) => store.delete(name),
    };
    const dead = new Set(w.dead || []);
    const fetch = (input, init) => {
        const url = at(input);
        asked.push(url);
        const from = new URL(url).origin;
        if (dead.has(from)) {
            return new Promise((_, reject) => {
                const signal = init && init.signal;
                if (signal) signal.addEventListener("abort", () => reject(new Error("aborted on deadline")));
            });
        }
        return Promise.resolve(new Response("code from " + from, {
            status: 200, headers: { "Content-Type": "text/javascript" },
        }));
    };

    boot(self, caches, fetch);
    if (w.tell) {
        for (const fn of handlers.message || []) {
            fn({ data: { type: "BASE", base: w.base }, waitUntil: (p) => waits.push(p) });
        }
    }
    const bodies = [];
    for (const path of w.requests) {
        let answer = null;
        const event = {
            request: new Request(new URL(path, ORIGIN).href),
            respondWith: (p) => { answer = p; },
            waitUntil: (p) => waits.push(p),
        };
        for (const fn of handlers.fetch || []) fn(event);
        bodies.push(answer ? await (await answer).text() : "past the worker");
    }
    await Promise.all(waits);
    out.push({ asked, bodies, cached: [...(store.get("aacpanel-shell-test") || new Map()).keys()] });
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
	var got []swRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(worlds) {
		t.Fatalf("%d answers to %d worlds", len(got), len(worlds))
	}
	return got
}
