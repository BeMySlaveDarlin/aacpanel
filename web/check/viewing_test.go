package check

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

func TestPollCarriesTheOpenSession(t *testing.T) {
	const shim = `
let slots = [];
let cursor = 0;
let queue = [];
let cleanups = [];

export function __render(fn) {
    cursor = 0;
    queue = [];
    const out = fn();
    for (const effect of queue) {
        const off = effect();
        if (typeof off === "function") cleanups.push(off);
    }
    return out;
}

export function __unmount() {
    for (const off of cleanups) off();
    cleanups = [];
}

export function useEffect(fn, deps) {
    const i = cursor++;
    const prev = slots[i];
    const same = prev && prev.deps && deps && deps.length === prev.deps.length
        && deps.every((d, k) => d === prev.deps[k]);
    slots[i] = { deps };
    if (!same) queue.push(fn);
}
`
	const probe = `
import { loadHost, useViewing } from %BUNDLE%;
import { __render, __unmount } from %SHIM%;

const asked = [];
globalThis.fetch = async (url) => {
    asked.push(String(url));
    return new Response(JSON.stringify({ at: 1000, sessions: [] }), {
        status: 200, headers: { "Content-Type": "application/json" },
    });
};

await loadHost();
__render(() => useViewing("aacpanel"));
await loadHost();
__render(() => useViewing("demo.site-2"));
await loadHost();
__render(() => useViewing(""));
await loadHost();
__render(() => useViewing("home"));
__unmount();
await loadHost();

process.stdout.write(JSON.stringify(asked));
`

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the presence mark is run by the engine, not by reading the source")
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
	shimPath := filepath.Join(dir, "hooks.mjs")
	if err := os.WriteFile(shimPath, []byte(shim), 0o600); err != nil {
		t.Fatal(err)
	}
	alias["preact/hooks"] = shimPath

	data, err := filepath.Abs(filepath.Join(webDir, "src", "data.js"))
	if err != nil {
		t.Fatal(err)
	}
	viewing, err := filepath.Abs(filepath.Join(webDir, "src", "viewing.js"))
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "entry.mjs")
	seam := "export { loadHost } from " + jsString(data) + ";\n" +
		"export { useViewing } from " + jsString(viewing) + ";\n"
	if err := os.WriteFile(entry, []byte(seam), 0o600); err != nil {
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
		t.Fatalf("the snapshot poll did not build: %v", built.Errors[0].Text)
	}
	bundle := filepath.Join(dir, "poll.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := strings.NewReplacer(
		"%BUNDLE%", jsString("file://"+bundle),
		"%SHIM%", jsString("file://"+shimPath),
	).Replace(probe)
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var asked []string
	if err := json.Unmarshal(out, &asked); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}

	want := []string{
		"/api/host",
		"/api/host?viewing=aacpanel",
		"/api/host?viewing=demo.site-2",
		"/api/host",
		"/api/host",
	}
	if len(asked) != len(want) {
		t.Fatalf("the poll went out %d times, expected %d: %v", len(asked), len(want), asked)
	}
	why := []string{
		"there is no conversation on the screen — nothing to mark",
		"the conversation is open: without the name the service pushes a question that is already on screen",
		"the session changed under the same screen — the new one has to be marked",
		"an archived conversation shows no question and must not be marked: session names are reused",
		"the screen is gone — the mark has to be dropped by the same poll, not go stale in forty seconds",
	}
	for i, w := range want {
		if asked[i] != w {
			t.Errorf("poll %d went to %q, expected %q — %s", i+1, asked[i], w, why[i])
		}
	}
}

func TestChatScreenNamesOnlyTheLiveSession(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(webDir, "src", "screens", "chat.js"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, `import { useViewing } from "../viewing.js";`) {
		t.Fatal("the conversation screen does not take the presence mark: a push arrives for a question already on screen")
	}
	if !strings.Contains(text, "useViewing(live ? name : \"\")") {
		t.Error("the conversation screen announces itself wrongly: a live session is marked by name, an archived one by nothing")
	}
}

func TestViewingMarkKeepsOneOfflineSnapshot(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the cache key is run by the engine, not by reading the source")
	}
	script := `
const ORIGIN = "https://panel.example";
const boot = new Function("self", "caches", "fetch", ` + jsString(builtWorker(t)) + `);

const store = new Map();
const at = (req) => {
    if (typeof req === "string") return new URL(req, ORIGIN).href;
    return req instanceof URL ? req.href : req.url;
};
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
let online = true;
const fetch = async () => {
    if (!online) throw new TypeError("Failed to fetch");
    return new Response(JSON.stringify({ at: 1000 }), {
        status: 200, headers: { "Content-Type": "application/json" },
    });
};
boot(self, caches, fetch);

const ask = async (path) => {
    let answer = null;
    const event = {
        request: new Request(new URL(path, ORIGIN).href),
        respondWith: (p) => { answer = p; },
        waitUntil: () => {},
    };
    for (const fn of handlers.fetch || []) fn(event);
    if (!answer) return "past the worker";
    try {
        return await (await answer).text();
    } catch (err) {
        return "no snapshot";
    }
};

await ask("/api/host?viewing=aacpanel");
online = false;
const out = {
    other: await ask("/api/host?viewing=home"),
    none: await ask("/api/host"),
    keys: [...(store.get("aacpanel-data") || new Map()).keys()],
};
process.stdout.write(JSON.stringify(out));
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
		Other string   `json:"other"`
		None  string   `json:"none"`
		Keys  []string `json:"keys"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, raw)
	}

	if !strings.Contains(got.Other, `"at":1000`) {
		t.Errorf("offline with a conversation open: %q — the snapshot has to be found, not disappear along with the network", got.Other)
	}
	if !strings.Contains(got.None, `"at":1000`) {
		t.Errorf("offline without a mark: %q — the snapshot is stored under the same key", got.None)
	}
	if len(got.Keys) != 1 || !strings.HasSuffix(got.Keys[0], "/api/host") {
		t.Errorf("the data cache holds %v — the snapshot has to live under one key, without the session name", got.Keys)
	}
}
