package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

func TestUpdateApplyAlwaysReloads(t *testing.T) {
	cases := []struct {
		world  pwaWorld
		told   string
		reload bool
		why    string
	}{
		{
			world:  pwaWorld{Name: "the ordinary path: the worker waits and takes over", Waiting: "new", Controller: true, Change: true},
			told:   "new",
			reload: true,
			why:    "the update is applied and the page reloads on controllerchange",
		},
		{
			world: pwaWorld{
				Name: "another version arrived before the tap", Waiting: "newer", Announced: "stale",
				Controller: true, Change: true,
			},
			told:   "newer",
			reload: true,
			why: "the message has to go to the current worker from the registration: the one saved by " +
				"then is redundant, and the panel is rolled out several times an evening",
		},
		{
			world:  pwaWorld{Name: "controllerchange never arrived", Waiting: "new", Controller: true},
			told:   "new",
			reload: true,
			why:    "the fallback has to reload on its own — otherwise the banner hangs forever",
		},
		{
			world:  pwaWorld{Name: "the worker is gone entirely", Controller: true},
			told:   "",
			reload: true,
			why:    "there is nobody to message, but a reload removes the banner and brings up what is already installed",
		},
	}

	worlds := make([]pwaWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runPWA(t, worlds)
	for i, c := range cases {
		if got[i].Told != c.told {
			t.Errorf("%s: SKIP_WAITING went to %q, expected %q — %s", c.world.Name, got[i].Told, c.told, c.why)
		}
		if got[i].Reloaded != c.reload {
			t.Errorf("%s: reload %v, expected %v — %s", c.world.Name, got[i].Reloaded, c.reload, c.why)
		}
	}
}

type pwaWorld struct {
	Name       string `json:"name"`
	Waiting    string `json:"waiting"`
	Announced  string `json:"announced"`
	Controller bool   `json:"controller"`
	Change     bool   `json:"change"`
}

type pwaRun struct {
	Told     string `json:"told"`
	Reloaded bool   `json:"reloaded"`
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
for (const world of worlds) {
    let told = "";
    let reloaded = false;
    const listeners = {};
    const worker = (name) => ({ name, postMessage: (msg) => {
        if (msg && msg.type === "SKIP_WAITING") {
            told = name;
            if (world.change) for (const fn of listeners.controllerchange || []) fn();
        }
    } });
    const waiting = world.waiting ? worker(world.waiting) : null;
    const registration = {
        waiting,
        installing: null,
        addEventListener() {},
        update: () => Promise.resolve(),
    };
    globalThis.navigator = { serviceWorker: {
        controller: world.controller ? {} : null,
        register: () => Promise.resolve(registration),
        addEventListener: (type, fn) => { (listeners[type] = listeners[type] || []).push(fn); },
    } };
    globalThis.location = { reload: () => { reloaded = true; } };
    globalThis.document = { addEventListener() {} };
    globalThis.window = { addEventListener() {} };
    const timers = [];
    globalThis.setTimeout = (fn) => { timers.push(fn); return 0; };

    const pwa = await import(` + "`" + `${` + jsString("file://"+bundle) + `}?w=${encodeURIComponent(world.name)}` + "`" + `);
    await pwa.register(() => {});
    if (world.announced) {
        registration.waiting = worker(world.announced);
        await pwa.register(() => {});
        registration.waiting = waiting;
    }
    pwa.apply();
    for (const fn of timers) fn();
    out.push({ told, reloaded });
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
