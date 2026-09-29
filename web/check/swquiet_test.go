package check

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// A push that names its source carries a button that quiets it; the button
// asks the panel the worker belongs to, with the session of that panel, and
// the phone is told how it went. A tap on the push itself still opens it.
func TestTheButtonOnAPushQuietsItsSource(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the worker is run by the engine, not by reading the source")
	}
	script := `
const ORIGIN = "https://panel.example";
const boot = new Function("self", "caches", "fetch", ` + jsString(builtWorker(t)) + `);

async function run(answer, action) {
    const handlers = {};
    const shown = [];
    const asked = [];
    const waits = [];
    let opened = "";
    const self = {
        location: { origin: ORIGIN },
        addEventListener: (type, fn) => { (handlers[type] ||= []).push(fn); },
        clients: { claim: async () => {}, matchAll: async () => [], openWindow: async (u) => { opened = u; } },
        registration: { showNotification: async (title, opts) => { shown.push({ title, opts }); } },
        skipWaiting: () => {},
    };
    const fetch = async (url, init) => {
        asked.push({ url: String(url), method: init && init.method, body: init && init.body,
                     credentials: init && init.credentials });
        return new Response("{}", { status: answer });
    };
    boot(self, { open: async () => ({}), keys: async () => [], delete: async () => true }, fetch);
    const payload = { title: "Stack down · shop", body: "b", tag: "stack:shop", severity: "critical",
                      quiet: { what: "stacks", key: "shop", label: "Quiet: shop" } };
    for (const fn of handlers.push) fn({ data: { json: () => payload, text: () => "" }, waitUntil: (p) => waits.push(p) });
    await Promise.all(waits);
    const push = shown[0];
    const click = { action, notification: { data: push.opts.data, close() {} }, waitUntil: (p) => waits.push(p) };
    for (const fn of handlers.notificationclick) fn(click);
    await Promise.all(waits);
    return { actions: push.opts.actions, asked, said: shown.slice(1).map((s) => s.title), opened };
}

const out = { ok: await run(200, "quiet"), refused: await run(401, "quiet"), tap: await run(200, "") };
process.stdout.write(JSON.stringify(out));
`
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, raw)
	}
	type asked struct {
		URL, Method, Body, Credentials string
	}
	type run struct {
		Actions []struct{ Action, Title string } `json:"actions"`
		Asked   []asked                          `json:"asked"`
		Said    []string                         `json:"said"`
		Opened  string                           `json:"opened"`
	}
	var got struct{ Ok, Refused, Tap run }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if len(got.Ok.Actions) != 1 || got.Ok.Actions[0].Action != "quiet" || got.Ok.Actions[0].Title != "Quiet: shop" {
		t.Errorf("the push carries %+v, want one button Quiet: shop", got.Ok.Actions)
	}
	if len(got.Ok.Asked) != 1 {
		t.Fatalf("the button asked %+v", got.Ok.Asked)
	}
	a := got.Ok.Asked[0]
	if a.URL != "https://panel.example/api/push/quiet" || a.Method != "POST" || a.Credentials != "same-origin" ||
		!strings.Contains(a.Body, `"what":"stacks"`) || !strings.Contains(a.Body, `"key":"shop"`) {
		t.Errorf("the button asked %+v: its own panel, with the session of that panel, naming the source", a)
	}
	if strings.Join(got.Ok.Said, "|") != "Quieted: shop" || strings.Join(got.Refused.Said, "|") != "Not quieted" {
		t.Errorf("the phone was told %v when it went and %v when the panel refused", got.Ok.Said, got.Refused.Said)
	}
	if len(got.Tap.Asked) != 0 || got.Tap.Opened == "" {
		t.Errorf("a tap on the push asked %+v and opened %q: it opens the panel and quiets nothing", got.Tap.Asked, got.Tap.Opened)
	}
}
