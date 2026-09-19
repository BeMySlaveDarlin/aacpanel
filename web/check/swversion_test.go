package check

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// The worker answers which version it is. It is the only way a phone has of
// telling a worker that stepped aside from one that merely says it did: there
// are no developer tools there to read it off the registration.
func TestTheWorkerSaysWhichVersionItIs(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the worker answers in an engine, not in its source")
	}
	script := `
const boot = new Function("self", "caches", "fetch", ` + jsString(builtWorker(t)) + `);
const handlers = {};
const self = {
    location: { origin: "https://panel.example" },
    addEventListener: (type, fn) => { (handlers[type] ||= []).push(fn); },
    clients: { claim: async () => {}, matchAll: async () => [] },
    skipWaiting: () => {},
};
const caches = { open: async () => ({ match: async () => undefined, put: async () => {}, addAll: async () => {} }),
                 keys: async () => [], delete: async () => true };
boot(self, caches, async () => new Response(""));

const said = [];
const port = { postMessage: (data) => said.push(data) };
for (const fn of handlers.message || []) fn({ data: { type: "VERSION" }, ports: [port], waitUntil: () => {} });
// A message with no port to answer on must not throw: the page can send one.
for (const fn of handlers.message || []) fn({ data: { type: "VERSION" }, waitUntil: () => {} });
process.stdout.write(JSON.stringify(said));
`
	out, err := exec.Command(node, "--input-type=module", "-e", script).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var said []map[string]any
	if err := json.Unmarshal(out, &said); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(said) != 1 {
		t.Fatalf("the worker answered %d times, expected once: %s", len(said), out)
	}
	if got, _ := said[0]["version"].(string); got != "test" {
		t.Errorf("the worker says it is version %q, and it was built as %q", got, "test")
	}
}

// The page asks the worker over a channel of its own and gives up rather than
// waiting for an answer that is not coming.
func TestAskingTheWorkerForItsVersionHasADeadline(t *testing.T) {
	src := stripComments(srcFiles(t)["src/pwa.js"])
	body := funcBody(t, src, "export async function runningVersion(")
	for _, want := range []struct{ code, why string }{
		{"navigator.serviceWorker.controller", "the version is asked of whatever worker is at hand rather than of the one running the page"},
		{"new MessageChannel()", "the answer comes back over the general message channel, where it is mixed with the rest of what the worker says"},
		{"setTimeout", "a worker that never answers leaves the settings screen reading forever"},
	} {
		if !strings.Contains(body, want.code) {
			t.Errorf("runningVersion has no %s: %s", want.code, want.why)
		}
	}
}
