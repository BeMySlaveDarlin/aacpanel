package check

import (
	"strings"
	"testing"
)

// A device that sat down on an older build has no way out of its own: a phone
// has no developer tools to unregister a worker with, and closing the
// application does not always end it. This is that way out — and it has to
// take the panel's own caches and nothing else.
func TestReinstallTakesTheWorkerAndThePanelCachesOffTheDevice(t *testing.T) {
	var got struct {
		Registered           int      `json:"registered"`
		RegisterError        string   `json:"registerError"`
		Before               []string `json:"before"`
		After                []string `json:"after"`
		Left                 int      `json:"left"`
		RunningWithoutWorker string   `json:"runningWithoutWorker"`
		Served               string   `json:"served"`
		AskedWithCache       string   `json:"askedWithCache"`
		Gone                 struct {
			Workers int      `json:"workers"`
			Caches  []string `json:"caches"`
		} `json:"gone"`
	}
	runFixture(t, "pwareinstall.html", &got)

	if got.RegisterError != "" {
		t.Fatalf("the fixture could not register a worker to take off: %s", got.RegisterError)
	}
	if got.Registered == 0 {
		t.Fatal("no worker was registered, so nothing proves one is taken off")
	}
	if got.Left != 0 {
		t.Errorf("%d registration(s) survived the scrub: the stuck worker is still in charge of the device", got.Left)
	}
	if got.Gone.Workers != got.Registered {
		t.Errorf("scrub reports %d worker(s) taken off against %d registered", got.Gone.Workers, got.Registered)
	}

	after := strings.Join(got.After, " ")
	if !strings.Contains(after, "someone-elses-cache") {
		t.Errorf("the caches left are %v: the panel swept a cache that is not its own — a browser "+
			"keeps every site's caches in one place", got.After)
	}
	for _, name := range []string{"aacpanel-shell-e23511dea1f0", "aacpanel-data", "aacpanel-route"} {
		if strings.Contains(after, name) {
			t.Errorf("%s survived the scrub: the device comes back on the same build it was stuck on", name)
		}
	}

	if got.RunningWithoutWorker != "" {
		t.Errorf("a page no worker controls claims to be running version %q", got.RunningWithoutWorker)
	}
	if got.Served != "abc123def456" {
		t.Errorf("the version the server hands out was read as %q, expected abc123def456 — "+
			"without it the screen cannot say whether the device is behind", got.Served)
	}
	if got.AskedWithCache != "no-store" {
		t.Errorf("the served version was asked for with cache %q: a cached answer is the running "+
			"version told twice, which is exactly the question being asked", got.AskedWithCache)
	}
}

// The scrub alone leaves the page on the code it already loaded. What makes it
// a reinstall is coming back from the server afterwards.
func TestReinstallEndsInAReload(t *testing.T) {
	src := stripComments(srcFiles(t)["src/pwa.js"])
	body := funcBody(t, src, "export async function reinstall(")
	for _, want := range []struct{ code, why string }{
		{"await scrub()", "the worker and the caches stay where they are, and the button does nothing but reload"},
		{"forget()", "the note about the last self-reload survives a reinstall, and the guard against " +
			"going in circles refuses the next honest update"},
		{"location.reload()", "the page stays on the code it already has: the worker is gone and nothing went to fetch a new one"},
	} {
		if !strings.Contains(body, want.code) {
			t.Errorf("reinstall has no %s: %s", want.code, want.why)
		}
	}
}
