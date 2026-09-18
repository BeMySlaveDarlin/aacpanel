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
		if got[i].Stuck {
			t.Errorf("%s: the page said the update is not installing on a first round — %s", c.world.Name, c.why)
		}
	}
}

// TestUpdateStopsAfterAFruitlessReload runs the page twice: it reloads itself,
// and comes back to the same worker in the same tab. The second round is where
// a page that reloads over and over would start another one.
func TestUpdateStopsAfterAFruitlessReload(t *testing.T) {
	cases := []struct {
		world        pwaWorld
		reloads      int
		againTold    []string
		againReloads int
		againStuck   bool
		why          string
	}{
		{
			world: pwaWorld{
				Name: "the worker is held back, and held still after the reload", Waiting: "new", Controller: true, OnSkip: "hold",
				Again: &pwaWorld{Name: "held, second round", Waiting: "new", Controller: true, OnSkip: "hold"},
			},
			reloads:      1,
			againTold:    []string{"new"},
			againReloads: 0,
			againStuck:   true,
			why: "the reload brought the page back to the same banner over the same worker: another round " +
				"ends where this one did, so the page stays and the person is told",
		},
		{
			world: pwaWorld{
				Name: "the controller changed and the banner came back with the page", Waiting: "new", Controller: true, OnSkip: "activate",
				Again: &pwaWorld{Name: "the banner came back, second round", Waiting: "new", Controller: true, OnSkip: "activate"},
			},
			reloads:      1,
			againTold:    []string{"new"},
			againReloads: 0,
			againStuck:   true,
			why: "a takeover followed by the same banner is the shape of the loop: the page reloads on it once " +
				"and refuses the second time, whatever leaves the update pending",
		},
		{
			world: pwaWorld{
				Name: "the reload freed the worker, a newer one arrives after it", Waiting: "new", Controller: true, OnSkip: "hold",
				Again: &pwaWorld{Name: "a newer update after a clean load", Installing: "newer", Controller: true, OnSkip: "hold"},
			},
			reloads:      1,
			againTold:    []string{"newer"},
			againReloads: 1,
			againStuck:   false,
			why: "the page came up with nothing waiting — the reload did its job — and the update after it " +
				"is not held against the one before",
		},
		{
			world: pwaWorld{
				Name: "the worker was held, and takes over on the second try", Waiting: "new", Controller: true, OnSkip: "hold",
				Again: &pwaWorld{Name: "the worker is free, second round", Waiting: "new", Controller: true, OnSkip: "activate"},
			},
			reloads:      1,
			againTold:    []string{"new"},
			againReloads: 1,
			againStuck:   false,
			why: "the round that was waited out and the takeover that followed are not the same round: " +
				"an update that installs takes the page with it, banner or no banner",
		},
		{
			world: pwaWorld{
				Name: "site data is switched off", Waiting: "new", Controller: true, OnSkip: "hold", NoStorage: true,
				Again: &pwaWorld{Name: "site data is off, second round", Waiting: "new", Controller: true, OnSkip: "hold", NoStorage: true},
			},
			reloads:      1,
			againTold:    []string{"new"},
			againReloads: 1,
			againStuck:   false,
			why: "there is nowhere to leave the note, so the guard is not armed: the page reloads as it always " +
				"did rather than failing over storage it cannot reach",
		},
	}

	worlds := make([]pwaWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runPWA(t, worlds)
	for i, c := range cases {
		if got[i].Reloads != c.reloads {
			t.Errorf("%s: %d reloads in the first round, expected %d — %s", c.world.Name, got[i].Reloads, c.reloads, c.why)
		}
		if got[i].Stuck {
			t.Errorf("%s: the page gave up on the first round — %s", c.world.Name, c.why)
		}
		again := got[i].Again
		if again == nil {
			t.Fatalf("%s: the page never came back for a second round", c.world.Name)
		}
		if strings.Join(again.Told, " ") != strings.Join(c.againTold, " ") {
			t.Errorf("%s: SKIP_WAITING went to %v on the second round, expected %v — %s",
				c.world.Name, again.Told, c.againTold, c.why)
		}
		if again.Reloads != c.againReloads {
			t.Errorf("%s: %d reloads on the second round, expected %d — %s",
				c.world.Name, again.Reloads, c.againReloads, c.why)
		}
		if again.Stuck != c.againStuck {
			t.Errorf("%s: the page telling the person the update is not installing: %v, expected %v — %s",
				c.world.Name, again.Stuck, c.againStuck, c.why)
		}
	}
}

// pwaWorld is the state of the registration when the banner is tapped.
// Waiting and Installing name workers at the registration; Announced names a
// worker the banner was shown for that a newer one has since replaced. OnSkip
// says what a worker does when told to take over: "activate" (controllerchange
// follows), "silent" (it activates, no controllerchange) or "hold" (it stays
// installed, as it does while the old worker has an event in flight). Without
// Controller the page was loaded before any worker controlled it. NoStorage is
// a window with site data switched off, where the storage throws instead of
// answering. Again is the page after its own reload: the same tab, the same
// storage, the clock running on.
type pwaWorld struct {
	Name       string    `json:"name"`
	Waiting    string    `json:"waiting"`
	Installing string    `json:"installing"`
	Announced  string    `json:"announced"`
	Controller bool      `json:"controller"`
	OnSkip     string    `json:"onSkip"`
	NoStorage  bool      `json:"noStorage"`
	Again      *pwaWorld `json:"again,omitempty"`
}

// pwaRun is what the page did on the harness clock: who got SKIP_WAITING, when
// the first reload came (-1: never, counted from the start of the round) and
// what caused it — "controllerchange" or "page", the page's own decision — how
// many reloads came within five seconds and in the round altogether, and
// whether the page told the person the update is not installing. Again is the
// round the reload led to, when the world asked for one.
type pwaRun struct {
	Told        []string `json:"told"`
	ReloadAt    int      `json:"reloadAt"`
	ReloadBy    string   `json:"reloadBy"`
	ReloadsBy5s int      `json:"reloadsBy5s"`
	Reloads     int      `json:"reloads"`
	Stuck       bool     `json:"stuck"`
	Again       *pwaRun  `json:"again"`
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
let loaded = 0;
for (const top of worlds) {
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

    // The storage of the tab outlives a reload, and so does the note the page
    // leaves itself; the clock runs on. A second round is the page coming back
    // from the reload the first one asked for.
    const store = new Map();
    const storage = {
        getItem: (key) => (store.has(key) ? store.get(key) : null),
        setItem: (key, value) => { store.set(key, String(value)); },
        removeItem: (key) => { store.delete(key); },
    };

    const round = async (world) => {
        const base = now;
        const told = [];
        let reloads = 0;
        let reloadAt = -1;
        let reloadBy = "";
        let stuck = false;
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
            if (reloadAt < 0) { reloadAt = now - base; reloadBy = changed ? "controllerchange" : "page"; }
        } });
        define("document", { addEventListener() {} });
        define("window", { addEventListener() {} });
        if (world.noStorage) {
            // Site data switched off: the storage throws on the way in, not on the call.
            Object.defineProperty(globalThis, "sessionStorage", { configurable: true, get() { throw new Error("site data is off"); } });
        } else {
            define("sessionStorage", storage);
        }

        const pwa = await import(` + "`" + `${` + jsString("file://"+bundle) + `}?w=${encodeURIComponent(world.name)}&n=${++loaded}` + "`" + `);
        pwa.watchStuck(() => { stuck = true; });
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
        await advance(base + 5000);
        const reloadsBy5s = reloads;
        await advance(base + 20000);
        return { told, reloadAt, reloadBy, reloadsBy5s, reloads, stuck, again: null };
    };

    const run = await round(top);
    if (top.again) run.again = await round(top.again);
    out.push(run);
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
