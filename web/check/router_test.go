package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouterPicksNearestAnsweringPanel(t *testing.T) {
	const (
		mapAll  = `{"panel":"p1","here":"public","endpoints":[{"kind":"local","url":"http://127.0.0.1:8777"},{"kind":"lan","url":"https://lan.example:8443"},{"kind":"public","url":"https://panel.example"},{"kind":"ts","url":"https://host.tailnet.ts.net"}]}`
		mapLan  = `{"panel":"p1","here":"lan","endpoints":[{"kind":"local","url":"http://127.0.0.1:8777"},{"kind":"lan","url":"https://lan.example:8443"},{"kind":"public","url":"https://panel.example"}]}`
		mapHTTP = `{"panel":"p1","here":"public","endpoints":[{"kind":"lan","url":"http://192.0.2.10:8776"},{"kind":"public","url":"https://panel.example"}]}`
		alive   = `"https://panel.example/probe":{"panel":"p1","kind":"public"}`
	)
	cases := []struct {
		name  string
		world pickWorld
		want  pickOutcome
	}{
		{"at the machine: the loopback answers — the base is the loopback", pickWorld{Map: mapAll, Probes: `{"http://127.0.0.1:8777/probe":{"panel":"p1","kind":"local"},` + alive + `}`, Token: "t1"},
			pickOutcome{Here: "public", Via: "local", Base: "http://127.0.0.1:8777", Token: "t1", Why: "local"}},
		{"at home: the loopback is silent, the LAN answers — the base is the LAN with a bearer", pickWorld{Map: mapAll, Probes: `{"https://lan.example:8443/probe":{"panel":"p1","kind":"lan"},` + alive + `}`, Token: "t1"},
			pickOutcome{Here: "public", Via: "lan", Base: "https://lan.example:8443", Token: "t1", Why: "lan"}},
		{"both answer — take the nearer one, not the faster one", pickWorld{Map: mapAll, Probes: `{"https://lan.example:8443/probe":{"panel":"p1","kind":"lan"},"http://127.0.0.1:8777/probe":{"panel":"p1","kind":"local"},` + alive + `}`, Token: "t1"},
			pickOutcome{Here: "public", Via: "local", Base: "http://127.0.0.1:8777", Token: "t1", Why: "local"}},
		{"from outside: nobody nearer answers — there is no base", pickWorld{Map: mapAll, Probes: `{` + alive + `,"https://host.tailnet.ts.net/probe":{"panel":"p1","kind":"ts"}}`, Token: "t1"},
			pickOutcome{Here: "public", Why: "here"}},
		{"another panel on the loopback — wrong one, there is no base", pickWorld{Map: mapAll, Probes: `{"http://127.0.0.1:8777/probe":{"panel":"other","kind":"local"},` + alive + `}`, Token: "t1"},
			pickOutcome{Here: "public", Why: "here"}},
		{"the address answered with the wrong listener kind — do not trust it", pickWorld{Map: mapAll, Probes: `{"http://127.0.0.1:8777/probe":{"panel":"p1","kind":"public"},` + alive + `}`, Token: "t1"},
			pickOutcome{Here: "public", Why: "here"}},
		{"no bearer issued — the LAN is not taken", pickWorld{Map: mapAll, Probes: `{"https://lan.example:8443/probe":{"panel":"p1","kind":"lan"},` + alive + `}`, Token: ""},
			pickOutcome{Here: "public", Why: "no-token"}},
		{"no bearer issued — the loopback is taken, there is no sign-in there", pickWorld{Map: mapAll, Probes: `{"http://127.0.0.1:8777/probe":{"panel":"p1","kind":"local"},` + alive + `}`, Token: ""},
			pickOutcome{Here: "public", Via: "local", Base: "http://127.0.0.1:8777", Why: "local"}},
		{"an http LAN address is not probed at all from an https page", pickWorld{Map: mapHTTP, Probes: `{"http://192.0.2.10:8776/probe":{"panel":"p1","kind":"lan"},` + alive + `}`, Token: "t1"},
			pickOutcome{Here: "public", Why: "alone", Probed: []string{}}},
		{"from an http page an http LAN address will do", pickWorld{Map: mapHTTP, Probes: `{"http://192.0.2.10:8776/probe":{"panel":"p1","kind":"lan"},` + alive + `}`, Token: "t1", Protocol: "http:"},
			pickOutcome{Here: "public", Via: "lan", Base: "http://192.0.2.10:8776", Token: "t1", Why: "lan"}},
		{"fallback: a dead base is skipped, the next one is taken", pickWorld{Map: mapAll, Probes: `{"http://127.0.0.1:8777/probe":{"panel":"p1","kind":"local"},"https://lan.example:8443/probe":{"panel":"p1","kind":"lan"},` + alive + `}`, Token: "t1", Avoid: "http://127.0.0.1:8777"},
			pickOutcome{Here: "public", Via: "lan", Base: "https://lan.example:8443", Token: "t1", Why: "lan"}},
		{"our own address died, the map comes from storage, tailscale answers — it becomes the base", pickWorld{Stored: mapAll, Probes: `{"https://host.tailnet.ts.net/probe":{"panel":"p1","kind":"ts"}}`, Token: "t1"},
			pickOutcome{Here: "public", Via: "ts", Base: "https://host.tailnet.ts.net", Token: "t1", Why: "ts"}},
		{"our own address died, nobody answers — there is no base", pickWorld{Stored: mapAll, Probes: `{}`, Token: "t1"},
			pickOutcome{Here: "public", Why: "silent"}},
		{"already on the LAN, the loopback answers — nearer still", pickWorld{Map: mapLan, Probes: `{"http://127.0.0.1:8777/probe":{"panel":"p1","kind":"local"},"https://lan.example:8443/probe":{"panel":"p1","kind":"lan"}}`, Token: "t1"},
			pickOutcome{Here: "lan", Via: "local", Base: "http://127.0.0.1:8777", Token: "t1", Why: "local"}},
		{"there is no map anywhere — do nothing", pickWorld{Probes: `{}`, Token: "t1"},
			pickOutcome{Why: "no-map", Probed: []string{}}},
	}

	worlds := make([]pickWorld, 0, len(cases))
	for _, c := range cases {
		worlds = append(worlds, c.world)
	}
	got := runRouterPick(t, worlds)
	for i, c := range cases {
		r := got[i]
		if r.Here != c.want.Here || r.Via != c.want.Via || r.Base != c.want.Base || r.Token != c.want.Token || r.Why != c.want.Why {
			t.Errorf("%s: got here=%q via=%q base=%q token=%q why=%q, expected here=%q via=%q base=%q token=%q why=%q",
				c.name, r.Here, r.Via, r.Base, r.Token, r.Why, c.want.Here, c.want.Via, c.want.Base, c.want.Token, c.want.Why)
		}
		if c.want.Probed != nil && len(r.Probed) != len(c.want.Probed) {
			t.Errorf("%s: probes went to %v, expected %v", c.name, r.Probed, c.want.Probed)
		}
		if r.Stored != r.Base {
			t.Errorf("%s: the storage holds base %q while %q was chosen", c.name, r.Stored, r.Base)
		}
	}
}

type pickWorld struct {
	Map      string `json:"map"`
	Stored   string `json:"stored"`
	Probes   string `json:"probes"`
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
	Avoid    string `json:"avoid"`
}

type pickOutcome struct {
	Here   string   `json:"here"`
	Via    string   `json:"via"`
	Base   string   `json:"base"`
	Token  string   `json:"token"`
	Why    string   `json:"why"`
	Probed []string `json:"probed"`
	Stored string   `json:"stored"`
}

func runRouterPick(t *testing.T, worlds []pickWorld) []pickOutcome {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the router is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "router.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import { pick, ENDPOINTS_KEY, BASE_KEY } from ` + jsString("file://"+path) + `;
const worlds = JSON.parse(readFileSync(0, "utf8"));
const reply = (body) => ({ ok: true, json: async () => body });
const out = [];
for (const w of worlds) {
    const probes = JSON.parse(w.probes || "{}");
    const probed = [];
    const f = async (url, init) => {
        if (url === "/api/endpoints") return w.map ? reply(JSON.parse(w.map)) : { ok: false, json: async () => null };
        if (url.endsWith("/probe")) probed.push(url.replace(/\/probe$/, ""));
        if (url in probes) return reply(probes[url]);
        throw new TypeError("Failed to fetch");
    };
    const store = new Map();
    if (w.stored) store.set(ENDPOINTS_KEY, w.stored);
    const storage = { getItem: (k) => store.get(k) || null, setItem: (k, v) => store.set(k, v), removeItem: (k) => store.delete(k) };
    const r = await pick({ fetch: f, storage, permissions: null, token: async () => w.token, protocol: w.protocol || "https:", avoid: w.avoid || "" });
    out.push({ here: r.here, via: r.via, base: r.base, token: r.token, why: r.why, probed, stored: store.get(BASE_KEY) || "" });
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
	var got []pickOutcome
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got) != len(worlds) {
		t.Fatalf("%d answers to %d worlds", len(got), len(worlds))
	}
	return got
}

func TestRouterMeasureMarksWhoAnswers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "router.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { measure, usable } from ` + jsString("file://"+path) + `;
const map = { panel: "p1", here: "public", endpoints: [
    { kind: "local", url: "http://127.0.0.1:8777" }, { kind: "lan", url: "https://lan.example:8443" }, { kind: "public", url: "https://panel.example" }] };
const answers = { "http://127.0.0.1:8777/probe": { panel: "other", kind: "local" }, "https://panel.example/probe": { panel: "p1", kind: "public" } };
const f = async (url) => {
    if (url === "/api/endpoints") return { ok: true, json: async () => map };
    if (url in answers) return { ok: true, json: async () => answers[url] };
    throw new TypeError("no");
};
const storage = { getItem: () => null, setItem: () => {}, removeItem: () => {} };
let t = 0;
const got = await measure({ fetch: f, storage, now: () => (t += 7) });
const out = {
    here: got.here,
    rows: got.rows.map((r) => r.kind + ":" + (r.ok ? "ok" : "bad")).join(" "),
    usable: [
        usable({ url: "http://127.0.0.1:8777" }, "https:"), usable({ url: "http://localhost:8777" }, "https:"),
        usable({ url: "http://192.0.2.10:8776" }, "https:"), usable({ url: "https://lan.example" }, "https:"),
        usable({ url: "http://192.0.2.10:8776" }, "http:"),
    ].map(String).join(" "),
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
	var got struct{ Here, Rows, Usable string }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("node output: %v: %s", err, raw)
	}
	if got.Here != "public" || got.Rows != "local:bad lan:bad public:ok" {
		t.Errorf("measurement: here=%q rows=%q — another panel's fingerprint on the loopback has to read as unreachable", got.Here, got.Rows)
	}
	if got.Usable != "true true false true true" {
		t.Errorf("mixed content: %q — from an https page only the loopback will do over http", got.Usable)
	}
}

func TestRouterKeepsAddressThatStillAnswers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "router.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { failed, ENDPOINTS_KEY } from ` + jsString("file://"+path) + `;
const map = { panel: "p1", here: "public", endpoints: [
    { kind: "lan", url: "https://lan.example:8443" }, { kind: "public", url: "https://panel.example" }] };
const answers = {
    "https://lan.example:8443/probe": { panel: "p1", kind: "lan" },
    "https://other.example/probe": { panel: "another", kind: "lan" },
    "https://liar.example/probe": { panel: "p1", kind: "public" },
};
const f = async (url) => {
    if (url in answers) return { ok: true, json: async () => answers[url] };
    throw new TypeError("Failed to fetch");
};
const stored = new Map([[ENDPOINTS_KEY, JSON.stringify(map)]]);
const storage = { getItem: (k) => stored.get(k) || null, setItem: () => {}, removeItem: () => {} };
const empty = { getItem: () => null, setItem: () => {}, removeItem: () => {} };
const out = {
    answering: await failed("https://lan.example:8443", { fetch: f, storage }),
    silent: await failed("https://dead.example", { fetch: f, storage }),
    stranger: await failed("https://other.example", { fetch: f, storage }),
    wrongKind: await failed("https://liar.example", { fetch: f, storage: { ...storage, getItem: (k) => (k === ENDPOINTS_KEY ? JSON.stringify({ panel: "p1", here: "public", endpoints: [{ kind: "lan", url: "https://liar.example" }] }) : null) } }),
    noMap: await failed("https://lan.example:8443", { fetch: f, storage: empty }),
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
	var got struct{ Answering, Silent, Stranger, WrongKind, NoMap bool }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("node output: %v: %s", err, raw)
	}
	if got.Answering {
		t.Error("the address answered its own probe and was crossed out anyway: one failed request moves traffic off the nearest address for minutes")
	}
	if !got.Silent {
		t.Error("the address is silent and stayed the base: requests keep falling into a dead address until somebody re-measures")
	}
	if !got.Stranger {
		t.Error("another panel sits at the address, and it was left as the base")
	}
	if !got.WrongKind {
		t.Error("the address answered with the wrong listener kind, and it was left as the base")
	}
	if got.NoMap {
		t.Error("there is no map in the storage and the address answered — there is nothing to trust but the probe, and it passed")
	}
}

func TestAppAsksAddressBeforeDroppingIt(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(webDir, "src", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "api.watchFailures(")
	if start < 0 {
		t.Fatal("app.js has no handler for a request failing through the base")
	}
	handler := body[start:]
	if end := strings.Index(handler, "return () => api.watchFailures(null)"); end > 0 {
		handler = handler[:end]
	}
	ask, drop := strings.Index(handler, "failed("), strings.Index(handler, "api.use({})")
	if ask < 0 {
		t.Fatal("a failed request crosses the address out without asking it with a probe")
	}
	if drop >= 0 && drop < ask {
		t.Error("the base is dropped before the address is asked: a live nearby address is removed on a single failure")
	}
}
