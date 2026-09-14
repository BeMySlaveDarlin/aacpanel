package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

func TestUpdateApplyWaitsForTheTakeover(t *testing.T) {
	cases := []struct {
		world       pwaWorld
		told        []string
		reloadAt    int
		reloadBy    string
		reloadsBy5s int
		why         string
	}{
		{
			world:       pwaWorld{Name: "the ordinary path: the worker waits and takes over", Waiting: "new", Controller: true, OnSkip: "activate"},
			told:        []string{"new"},
			reloadAt:    0,
			reloadBy:    "controllerchange",
			reloadsBy5s: 1,
			why:         "the update is applied and the page reloads on controllerchange",
		},
		{
			world:       pwaWorld{Name: "another version arrived before the tap", Waiting: "newer", Announced: "stale", Controller: true, OnSkip: "activate"},
			told:        []string{"newer"},
			reloadAt:    0,
			reloadBy:    "controllerchange",
			reloadsBy5s: 1,
			why: "the message has to go to the current worker from the registration: the one saved by " +
				"then is redundant, and the panel is rolled out several times an evening",
		},
		{
			world:       pwaWorld{Name: "controllerchange never arrived", Waiting: "new", Controller: true, OnSkip: "silent"},
			told:        []string{"new"},
			reloadAt:    1000,
			reloadBy:    "page",
			reloadsBy5s: 1,
			why:         "the worker took over without a controllerchange — the page reloads itself a second later",
		},
		{
			world:       pwaWorld{Name: "the old worker holds the new one back", Waiting: "new", Controller: true, OnSkip: "hold"},
			told:        []string{"new"},
			reloadAt:    15000,
			reloadBy:    "page",
			reloadsBy5s: 0,
			why: "the browser keeps the new worker waiting while the old one has an event in flight; " +
				"a reload under the old worker only brings the banner back, so the page waits out the grace first",
		},
		{
			world:       pwaWorld{Name: "the tap came while the worker was still installing", Installing: "next", Controller: true, OnSkip: "activate"},
			told:        []string{"next"},
			reloadAt:    500,
			reloadBy:    "controllerchange",
			reloadsBy5s: 1,
			why: "a worker still installing cannot be told to take over: the message waits for installed, " +
				"and a blind reload before that comes up under the old worker",
		},
		{
			world:       pwaWorld{Name: "the announced worker went redundant, a newer one is installing", Announced: "stale", Installing: "next", Controller: true, OnSkip: "activate"},
			told:        []string{"next"},
			reloadAt:    500,
			reloadBy:    "controllerchange",
			reloadsBy5s: 1,
			why:         "a redundant worker is dropped: the message goes to the one that replaced it, once it has installed",
		},
		{
			world:       pwaWorld{Name: "the worker is gone entirely", Controller: true},
			told:        []string{},
			reloadAt:    0,
			reloadBy:    "page",
			reloadsBy5s: 1,
			why:         "there is nobody to wait for: a reload removes the banner and brings up what is already installed",
		},
		{
			world:       pwaWorld{Name: "the page was loaded before any worker controlled it", Waiting: "new", OnSkip: "activate"},
			told:        []string{"new"},
			reloadAt:    0,
			reloadBy:    "controllerchange",
			reloadsBy5s: 1,
			why: "the first worker takes the page over without a reload, but a takeover the page asked for " +
				"is reloaded on controllerchange however the page was loaded",
		},
	}

	worlds := make([]pwaWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runPWA(t, worlds)
	for i, c := range cases {
		if strings.Join(got[i].Told, " ") != strings.Join(c.told, " ") {
			t.Errorf("%s: SKIP_WAITING went to %v, expected %v — %s", c.world.Name, got[i].Told, c.told, c.why)
		}
		if got[i].ReloadAt != c.reloadAt || got[i].ReloadBy != c.reloadBy {
			t.Errorf("%s: reload at %dms by %q, expected %dms by %q — %s",
				c.world.Name, got[i].ReloadAt, got[i].ReloadBy, c.reloadAt, c.reloadBy, c.why)
		}
		if got[i].ReloadsBy5s != c.reloadsBy5s {
			t.Errorf("%s: %d reloads within five seconds, expected %d — %s", c.world.Name, got[i].ReloadsBy5s, c.reloadsBy5s, c.why)
		}
	}
}

// pwaWorld is the state of the registration when the banner is tapped.
// Waiting and Installing name workers at the registration; Announced names a
// worker the banner was shown for that a newer one has since replaced. OnSkip
// says what a worker does when told to take over: "activate" (controllerchange
// follows), "silent" (it activates, no controllerchange) or "hold" (it stays
// installed, as it does while the old worker has an event in flight). Without
// Controller the page was loaded before any worker controlled it.
type pwaWorld struct {
	Name       string `json:"name"`
	Waiting    string `json:"waiting"`
	Installing string `json:"installing"`
	Announced  string `json:"announced"`
	Controller bool   `json:"controller"`
	OnSkip     string `json:"onSkip"`
}

// pwaRun is what the page did on the harness clock: who got SKIP_WAITING, when
// the first reload came (-1: never) and what caused it — "controllerchange" or
// "page", the page's own decision — and how many reloads came within five seconds.
type pwaRun struct {
	Told        []string `json:"told"`
	ReloadAt    int      `json:"reloadAt"`
	ReloadBy    string   `json:"reloadBy"`
	ReloadsBy5s int      `json:"reloadsBy5s"`
}

func runPWA(t *testing.T, worlds []pwaWorld) []pwaRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: applying the update is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "pwa.js"))
	if err != nil {
		t.Fatal(err)
	}
	built := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Format:      esbuild.FormatESModule,
		Platform:    esbuild.PlatformNeutral,
		Alias:       alias,
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("pwa.js did not build: %v", built.Errors[0].Text)
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "pwa.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
const worlds = JSON.parse(readFileSync(0, "utf8"));
const out = [];
const flush = async () => { for (let i = 0; i < 20; i++) await new Promise((r) => setImmediate(r)); };
for (const world of worlds) {
    // The clock: timers fire when the harness advances it, in the order they are due.
    let now = 0;
    let seq = 0;
    const timers = new Map();
    globalThis.setTimeout = (fn, ms) => { const id = ++seq; timers.set(id, { due: now + (ms || 0), fn }); return id; };
    globalThis.clearTimeout = (id) => { timers.delete(id); };
    Date.now = () => now;
    const advance = async (to) => {
        for (;;) {
            let next = null;
            for (const [id, timer] of timers) if (timer.due <= to && (!next || timer.due < next.timer.due)) next = { id, timer };
            if (!next) break;
            timers.delete(next.id);
            now = next.timer.due;
            next.timer.fn();
            await flush();
        }
        now = to;
    };

    const told = [];
    let reloads = 0;
    let reloadAt = -1;
    let reloadBy = "";
    let changed = false;
    const listeners = {};
    const fire = (type) => { for (const fn of listeners[type] || []) fn(); };
    const registration = { waiting: null, installing: null, active: {}, addEventListener() {}, update: () => Promise.resolve() };
    const worker = (name, state) => {
        const w = {
            name, state, handlers: [],
            addEventListener: (type, fn) => { if (type === "statechange") w.handlers.push(fn); },
            removeEventListener: (type, fn) => { w.handlers = w.handlers.filter((h) => h !== fn); },
            set: (s) => { w.state = s; for (const fn of [...w.handlers]) fn(); },
            postMessage: (msg) => {
                if (!msg || msg.type !== "SKIP_WAITING") return;
                told.push(name);
                if (world.onSkip === "hold") return;
                registration.waiting = null;
                registration.active = w;
                w.set("activating");
                if (world.onSkip === "activate") { changed = true; fire("controllerchange"); }
                w.set("activated");
            },
        };
        return w;
    };
    // Defined rather than assigned: a newer node already owns some of these names
    // as getters, and a plain assignment to one of those throws.
    const define = (name, value) =>
        Object.defineProperty(globalThis, name, { value, configurable: true, writable: true });
    const sw = {
        controller: world.controller ? {} : null,
        register: () => Promise.resolve(registration),
        addEventListener: (type, fn) => { (listeners[type] = listeners[type] || []).push(fn); },
    };
    define("navigator", { serviceWorker: sw });
    define("location", { reload: () => {
        reloads++;
        if (reloadAt < 0) { reloadAt = now; reloadBy = changed ? "controllerchange" : "page"; }
    } });
    define("document", { addEventListener() {} });
    define("window", { addEventListener() {} });

    const pwa = await import(` + "`" + `${` + jsString("file://"+bundle) + `}?w=${encodeURIComponent(world.name)}` + "`" + `);
    if (world.announced) {
        // The banner was shown for this worker; a newer one has replaced it since.
        const stale = worker(world.announced, "installed");
        registration.waiting = stale;
        await pwa.register(() => {});
        registration.waiting = null;
        stale.state = "redundant";
    }
    if (world.waiting) registration.waiting = worker(world.waiting, "installed");
    if (world.installing) {
        const next = worker(world.installing, "installing");
        registration.installing = next;
        setTimeout(() => { registration.installing = null; registration.waiting = next; next.set("installed"); }, 500);
    }
    await pwa.register(() => {});
    // The first worker has taken the page over by the time the banner is tapped.
    if (!world.controller) sw.controller = {};

    pwa.apply();
    await flush();
    await advance(5000);
    const reloadsBy5s = reloads;
    await advance(20000);
    out.push({ told, reloadAt, reloadBy, reloadsBy5s });
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
	var got []pwaRun
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(worlds) {
		t.Fatalf("%d answers to %d worlds", len(got), len(worlds))
	}
	return got
}
