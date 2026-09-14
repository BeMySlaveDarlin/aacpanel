package check

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestTapOnAPushOpensItsScreen(t *testing.T) {
	const screen = "/app?session=warden"
	cases := []struct {
		world     tapWorld
		focused   bool
		navigated string
		posted    string
		opened    string
		why       string
	}{
		{
			world:     tapWorld{Name: "a push about a session, the panel is open", URL: screen, Window: true, Navigate: true},
			focused:   true,
			navigated: screen,
			why:       "the open window is brought up and taken to the session — the home screen is not where the question waits",
		},
		{
			world:  tapWorld{Name: "a push about a session, the panel is closed", URL: screen},
			opened: screen,
			why:    "with no window to drive, the panel opens straight on the session",
		},
		{
			world:   tapWorld{Name: "a push with no address, the panel is open", Window: true, Navigate: true},
			focused: true,
			why:     "a push about the machine only brings the panel up, wherever it was",
		},
		{
			world:  tapWorld{Name: "a push with no address, the panel is closed"},
			opened: "/app",
			why:    "the panel opens on its home screen",
		},
		{
			world:   tapWorld{Name: "the browser does not let the worker drive the window", URL: screen, Window: true},
			focused: true,
			posted:  screen,
			why:     "the page is told the address and goes there itself",
		},
		{
			world:   tapWorld{Name: "the window is not controlled by the worker", URL: screen, Window: true, Navigate: true, Refuse: true},
			focused: true,
			posted:  screen,
			why:     "a refused navigation is not the end: the page is told the address",
		},
		{
			world:  tapWorld{Name: "the address is not a screen of the panel", URL: "https://evil.example/app"},
			opened: "/app",
			why:    "only a path of the panel is followed",
		},
	}

	worlds := make([]tapWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runTap(t, worlds)
	for i, c := range cases {
		r := got[i]
		if r.Focused != c.focused {
			t.Errorf("%s: focused %v, expected %v — %s", c.world.Name, r.Focused, c.focused, c.why)
		}
		if r.Navigated != c.navigated {
			t.Errorf("%s: the window was taken to %q, expected %q — %s", c.world.Name, r.Navigated, c.navigated, c.why)
		}
		if r.Posted != c.posted {
			t.Errorf("%s: the page was told %q, expected %q — %s", c.world.Name, r.Posted, c.posted, c.why)
		}
		if r.Opened != c.opened {
			t.Errorf("%s: a window was opened on %q, expected %q — %s", c.world.Name, r.Opened, c.opened, c.why)
		}
	}
}

func TestPageOpensTheSessionTheAddressNames(t *testing.T) {
	files := srcFiles(t)
	app := withoutComments(files["src/app.js"])
	if !strings.Contains(app, `.get("session")`) || !strings.Contains(app, "location.search") {
		t.Error("src/app.js: the session named in the address is never read — a tap on a push about it lands on the home screen")
	}
	if !strings.Contains(app, "watchOpen(") {
		t.Error("src/app.js: the page does not listen to the worker — when the browser refuses the navigation the tap does nothing")
	}
	if pwa := withoutComments(files["src/pwa.js"]); !strings.Contains(pwa, `type === "OPEN"`) {
		t.Error("src/pwa.js: the address sent by the worker is not handed to the page")
	}
	for path, want := range map[string]string{
		"src/mobile/shell.js":  "goHome(jump.name, jump.id)",
		"src/desktop/shell.js": "openChat({ name: jump.name, id: jump.id })",
	} {
		if !strings.Contains(withoutComments(files[path]), want) {
			t.Errorf("%s: the session the address names is not opened — no %s", path, want)
		}
	}
}

type tapWorld struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Window   bool   `json:"window"`
	Navigate bool   `json:"navigate"`
	Refuse   bool   `json:"refuse"`
}

type tapRun struct {
	Focused   bool   `json:"focused"`
	Navigated string `json:"navigated"`
	Posted    string `json:"posted"`
	Opened    string `json:"opened"`
}

func runTap(t *testing.T, worlds []tapWorld) []tapRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the tap on a notification is run by the engine, not by reading the source")
	}
	script := `
import { readFileSync } from "node:fs";

const ORIGIN = "https://panel.example";
const boot = new Function("self", "caches", "fetch", ` + jsString(builtWorker(t)) + `);

const worlds = JSON.parse(readFileSync(0, "utf8"));
const out = [];
for (const w of worlds) {
    const waits = [];
    const handlers = {};
    let shown = null;
    let focused = false;
    let navigated = "";
    let posted = "";
    let opened = "";
    const client = {
        url: ORIGIN + "/app",
        focus: async () => { focused = true; return client; },
        postMessage: (msg) => { if (msg && msg.type === "OPEN") posted = msg.url; },
    };
    if (w.navigate) {
        client.navigate = async (url) => {
            if (w.refuse) throw new TypeError("the client is not controlled by this worker");
            navigated = url;
            return client;
        };
    }
    const self = {
        location: { origin: ORIGIN },
        addEventListener: (type, fn) => { (handlers[type] ||= []).push(fn); },
        clients: {
            claim: async () => {},
            matchAll: async () => (w.window ? [client] : []),
            openWindow: async (url) => { opened = url; return null; },
        },
        registration: { showNotification: async (title, options) => { shown = { title, options }; } },
        skipWaiting: () => {},
    };
    const caches = { open: async () => ({ match: async () => undefined, put: async () => {}, addAll: async () => {} }), keys: async () => [], delete: async () => true };
    const fetch = async () => new Response("", { status: 404 });

    boot(self, caches, fetch);
    const payload = { title: "Question · warden", body: "Which way?", tag: "ask:u-1:1", severity: "critical" };
    if (w.url) payload.url = w.url;
    for (const fn of handlers.push || []) {
        fn({ data: { json: () => payload, text: () => JSON.stringify(payload) }, waitUntil: (p) => waits.push(p) });
    }
    await Promise.all(waits);
    if (!shown) throw new Error("the push did not show a notification");
    for (const fn of handlers.notificationclick || []) {
        fn({ notification: { close() {}, data: shown.options.data }, waitUntil: (p) => waits.push(p) });
    }
    await Promise.all(waits);
    out.push({ focused, navigated, posted, opened });
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
	var got []tapRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(worlds) {
		t.Fatalf("%d answers to %d worlds", len(got), len(worlds))
	}
	return got
}
