package check

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

func TestWorkerRenewsARotatedSubscription(t *testing.T) {
	const origin = "https://panel.example"
	keyBytes := []byte{4, 1, 2, 3}
	key := base64.RawURLEncoding.EncodeToString(keyBytes)

	cases := []struct {
		world  renewWorld
		asked  []string
		keyed  []byte
		posted string
		failed bool
		why    string
	}{
		{
			world:  renewWorld{Name: "the browser rotated the subscription", Key: key, Endpoint: "https://push.example.net/new"},
			asked:  []string{"GET " + origin + "/api/push/key", "POST " + origin + "/api/push/subscription"},
			keyed:  keyBytes,
			posted: "https://push.example.net/new",
			why: "the server keeps sending to the old endpoint otherwise, the push service answers that it is gone, " +
				"and the device is removed while the screen still says \"on\"",
		},
		{
			world:  renewWorld{Name: "the server has no key yet", Key: key, KeyStatus: 503, Endpoint: "https://push.example.net/new"},
			asked:  []string{"GET " + origin + "/api/push/key"},
			failed: true,
			why:    "with no key there is nothing to subscribe with, and the failure is not swallowed",
		},
	}

	worlds := make([]renewWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runRenew(t, worlds)
	for i, c := range cases {
		r := got[i]
		if strings.Join(r.Asked, " ") != strings.Join(c.asked, " ") {
			t.Errorf("%s: requests went to %v, expected %v — %s", c.world.Name, r.Asked, c.asked, c.why)
		}
		if !bytes.Equal(r.Keyed, c.keyed) {
			t.Errorf("%s: subscribed with the key %v, expected %v — %s", c.world.Name, r.Keyed, c.keyed, c.why)
		}
		if r.Posted != c.posted {
			t.Errorf("%s: the server was told %q, expected %q — %s", c.world.Name, r.Posted, c.posted, c.why)
		}
		if r.Failed != c.failed {
			t.Errorf("%s: the renewal failed %v, expected %v — %s", c.world.Name, r.Failed, c.failed, c.why)
		}
	}
}

type renewWorld struct {
	Name      string `json:"name"`
	Key       string `json:"key"`
	KeyStatus int    `json:"keyStatus"`
	Endpoint  string `json:"endpoint"`
}

type renewRun struct {
	Asked  []string `json:"asked"`
	Keyed  []byte   `json:"keyed"`
	Posted string   `json:"posted"`
	Failed bool     `json:"failed"`
}

func runRenew(t *testing.T, worlds []renewWorld) []renewRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the renewal of the subscription is run by the engine, not by reading the source")
	}
	script := `
import { readFileSync } from "node:fs";

const ORIGIN = "https://panel.example";
const boot = new Function("self", "caches", "fetch", ` + jsString(builtWorker(t)) + `);

const worlds = JSON.parse(readFileSync(0, "utf8"));
const out = [];
for (const w of worlds) {
    const asked = [];
    const waits = [];
    const handlers = {};
    let keyed = null;
    let posted = "";
    let failed = false;
    const self = {
        location: { origin: ORIGIN },
        addEventListener: (type, fn) => { (handlers[type] ||= []).push(fn); },
        clients: { claim: async () => {}, matchAll: async () => [] },
        skipWaiting: () => {},
        registration: { pushManager: { subscribe: async (options) => {
            keyed = Array.from(options.applicationServerKey);
            return {
                endpoint: w.endpoint,
                toJSON() { return { endpoint: w.endpoint, keys: { p256dh: "p", auth: "a" } }; },
            };
        } } },
    };
    const caches = { open: async () => ({ match: async () => undefined, put: async () => {}, addAll: async () => {} }), keys: async () => [], delete: async () => true };
    const fetch = async (input, init) => {
        const url = new URL(typeof input === "string" ? input : input.url, ORIGIN).href;
        const method = (init && init.method) || "GET";
        asked.push(method + " " + url);
        if (url.endsWith("/api/push/key")) {
            const status = w.keyStatus || 200;
            return new Response(status === 200 ? JSON.stringify({ key: w.key }) : "not yet", { status });
        }
        if (method === "POST" && url.endsWith("/api/push/subscription")) {
            posted = JSON.parse(init.body).endpoint;
            return new Response(null, { status: 204 });
        }
        return new Response("", { status: 404 });
    };

    boot(self, caches, fetch);
    for (const fn of handlers.pushsubscriptionchange || []) {
        fn({ waitUntil: (p) => waits.push(Promise.resolve(p).catch(() => { failed = true; })) });
    }
    await Promise.all(waits);
    out.push({ asked, keyed, posted, failed });
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
	var got []renewRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(worlds) {
		t.Fatalf("%d answers to %d worlds", len(got), len(worlds))
	}
	return got
}

func TestPageTellsTheServerAboutARotatedSubscription(t *testing.T) {
	const (
		origin = "https://panel.example"
		ask    = "GET " + origin + "/api/push/subscription"
		tell   = "POST " + origin + "/api/push/subscription"
	)
	cases := []struct {
		world  pageWorld
		asked  []string
		posted string
		state  string
		why    string
	}{
		{
			world: pageWorld{Name: "the server knows the same endpoint", Browser: "https://push.example.net/a", Status: 200, Server: "https://push.example.net/a"},
			asked: []string{ask}, state: "on",
			why: "nothing has changed, and a post on every start would reset the delivery marks for nothing",
		},
		{
			world:  pageWorld{Name: "the server knows the endpoint from before the rotation", Browser: "https://push.example.net/b", Status: 200, Server: "https://push.example.net/a"},
			asked:  []string{ask, tell},
			posted: "https://push.example.net/b", state: "on",
			why: "the worker's own handler was missed, and the server sends to an endpoint the push service no longer has",
		},
		{
			world:  pageWorld{Name: "the server lost the subscription", Browser: "https://push.example.net/b", Status: 404},
			asked:  []string{ask, tell},
			posted: "https://push.example.net/b", state: "on",
			why: "the push service answered that the old endpoint is gone and the row was removed; the browser side is alive",
		},
		{
			world: pageWorld{Name: "the server cannot answer", Browser: "https://push.example.net/b", Status: 503},
			asked: []string{ask}, state: "on",
			why: "a post into a database that is down would fail too, and the state of the screen is about the browser",
		},
		{
			world: pageWorld{Name: "the session cannot carry a subscription", Browser: "https://push.example.net/b", Status: 403},
			asked: []string{ask}, state: "on",
			why: "a token session hangs on no device, the post would be refused the same way",
		},
		{
			world: pageWorld{Name: "the browser has no subscription", Status: 404},
			asked: []string{}, state: "off",
			why: "there is nothing to compare and nothing to post",
		},
	}

	worlds := make([]pageWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runPage(t, worlds)
	for i, c := range cases {
		r := got[i]
		if strings.Join(r.Asked, " ") != strings.Join(c.asked, " ") {
			t.Errorf("%s: requests went to %v, expected %v — %s", c.world.Name, r.Asked, c.asked, c.why)
		}
		if r.Posted != c.posted {
			t.Errorf("%s: the server was told %q, expected %q — %s", c.world.Name, r.Posted, c.posted, c.why)
		}
		if r.State != c.state {
			t.Errorf("%s: the screen says %q, expected %q — %s", c.world.Name, r.State, c.state, c.why)
		}
	}
}

type pageWorld struct {
	Name    string `json:"name"`
	Browser string `json:"browser"`
	Status  int    `json:"status"`
	Server  string `json:"server"`
}

type pageRun struct {
	Asked  []string `json:"asked"`
	Posted string   `json:"posted"`
	State  string   `json:"state"`
}

func runPage(t *testing.T, worlds []pageWorld) []pageRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the check of the subscription is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	dir := t.TempDir()

	// The hook runs once, on mount: effects fire right after the render, state
	// setters are written down instead of re-rendering.
	const shim = `
export const __sets = [];
let queue = [];
export function __render(fn) {
    queue = [];
    const out = fn();
    for (const effect of queue) effect();
    return out;
}
export function useState(init) { return [init, (v) => __sets.push(v)]; }
export function useCallback(fn) { return fn; }
export function useEffect(fn) { queue.push(fn); }
`
	shimPath := filepath.Join(dir, "hooks.mjs")
	if err := os.WriteFile(shimPath, []byte(shim), 0o600); err != nil {
		t.Fatal(err)
	}
	alias["preact/hooks"] = shimPath

	push, err := filepath.Abs(filepath.Join(webDir, "src", "push.js"))
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "entry.mjs")
	if err := os.WriteFile(entry, []byte("export { usePush } from "+jsString(push)+";\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	built := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Format:      esbuild.FormatESModule,
		Platform:    esbuild.PlatformNeutral,
		Alias:       alias,
		External:    []string{shimPath},
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("push.js did not build: %v", built.Errors[0].Text)
	}
	bundle := filepath.Join(dir, "push.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { usePush } from ` + jsString("file://"+bundle) + `;
import { __render, __sets } from ` + jsString("file://"+shimPath) + `;

const ORIGIN = "https://panel.example";
// Defined rather than assigned: a newer node already owns some of these names
// as getters, and a plain assignment to one of those throws.
const define = (name, value) =>
    Object.defineProperty(globalThis, name, { value, configurable: true, writable: true });

const worlds = JSON.parse(readFileSync(0, "utf8"));
const out = [];
for (const w of worlds) {
    const asked = [];
    let posted = "";
    const subscription = w.browser ? {
        endpoint: w.browser,
        toJSON() { return { endpoint: w.browser, keys: { p256dh: "p", auth: "a" } }; },
    } : null;
    define("navigator", { serviceWorker: { ready: Promise.resolve({
        pushManager: { getSubscription: async () => subscription },
    }) } });
    define("window", { PushManager: {}, Notification: {} });
    define("Notification", { permission: "granted" });
    globalThis.fetch = async (input, init) => {
        const url = new URL(String(input), ORIGIN).href;
        const method = (init && init.method) || "GET";
        asked.push(method + " " + url);
        if (method === "POST") {
            posted = JSON.parse(init.body).endpoint;
            return new Response(null, { status: 204 });
        }
        if (w.status === 200) {
            return new Response(JSON.stringify({ endpoint: w.server }), {
                status: 200, headers: { "Content-Type": "application/json" },
            });
        }
        return new Response(JSON.stringify({ error: "no" }), { status: w.status });
    };

    __sets.length = 0;
    __render(() => usePush());
    // The effect chains promises through the fakes, and reading a Response
    // body takes a turn of the loop: wait for the loop, not for a microtask.
    await new Promise((resolve) => setTimeout(resolve, 50));
    out.push({ asked, posted, state: __sets[0] || "" });
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
	var got []pageRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(worlds) {
		t.Fatalf("%d answers to %d worlds", len(got), len(worlds))
	}
	return got
}
