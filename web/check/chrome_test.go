package check

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// chromeBinary finds a Chrome to run a fixture in, or "" when the machine has none.
func chromeBinary() string {
	if path := os.Getenv("AACP_CHROME"); path != "" {
		return path
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	for _, path := range []string{"/opt/google/chrome/chrome", "/usr/lib/chromium/chromium"} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// The driver speaks the devtools protocol to a headless Chrome over its
// debugging pipe — no sockets, nothing to install — opens the fixture at the
// address it is given, waits for the promise the page leaves in window.done
// and prints what it resolved to.
const chromeDriver = `
import { spawn } from "node:child_process";
const chrome = process.env.AACP_FIXTURE_CHROME, url = process.env.AACP_FIXTURE_URL, profile = process.env.AACP_FIXTURE_PROFILE;
const screen = JSON.parse(process.env.AACP_FIXTURE_SCREEN);
const proc = spawn(chrome, ["--headless=new", "--remote-debugging-pipe", "--user-data-dir=" + profile, "--password-store=basic",
    "--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-background-networking", "--hide-scrollbars",
    "--blink-settings=" + process.env.AACP_FIXTURE_POINTER, "about:blank"],
    { stdio: ["ignore", "ignore", "pipe", "pipe", "pipe"] });
let stderr = ""; proc.stderr.on("data", (d) => { stderr += d; });
const out = proc.stdio[3], inp = proc.stdio[4];
let seq = 0; const pending = new Map(); let buf = ""; const errors = [];
inp.on("data", (d) => {
    buf += d;
    let i;
    while ((i = buf.indexOf("\0")) >= 0) {
        const msg = JSON.parse(buf.slice(0, i)); buf = buf.slice(i + 1);
        if (msg.method === "Runtime.exceptionThrown") errors.push(msg.params.exceptionDetails.text + " " + (msg.params.exceptionDetails.exception?.description || ""));
        const p = pending.get(msg.id); if (!p) continue;
        pending.delete(msg.id);
        if (msg.error) p.rej(new Error(p.method + ": " + msg.error.message)); else p.res(msg.result);
    }
});
const send = (method, params = {}, sessionId) => new Promise((res, rej) => {
    const id = ++seq; pending.set(id, { res, rej, method });
    out.write(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }) + "\0");
});
const deadline = setTimeout(() => { console.error("the fixture did not finish in time\n" + stderr); proc.kill("SIGKILL"); process.exit(2); }, 60000);
try {
    const { targetId } = await send("Target.createTarget", { url: "about:blank" });
    const { sessionId } = await send("Target.attachToTarget", { targetId, flatten: true });
    await send("Runtime.enable", {}, sessionId);
    await send("Emulation.setDeviceMetricsOverride", screen, sessionId);
    await send("Page.navigate", { url }, sessionId);
    let result;
    for (;;) {
        const r = await send("Runtime.evaluate", { expression: "window.done", awaitPromise: true, returnByValue: true }, sessionId);
        if (r.exceptionDetails) throw new Error("the fixture failed: " + r.exceptionDetails.text + " " + (r.exceptionDetails.exception?.description || ""));
        if (r.result.value !== undefined) { result = r.result.value; break; }
        if (errors.length) throw new Error("the page threw: " + errors.join("; "));
        await new Promise((res) => setTimeout(res, 100));
    }
    if (errors.length) throw new Error("the page threw: " + errors.join("; "));
    process.stdout.write(JSON.stringify(result));
} finally {
    clearTimeout(deadline);
    proc.kill("SIGKILL");
}
`

// phoneScreen and deskScreen are the two screens a fixture is run on. The
// width decides which half of the styles applies: the desktop rules live
// behind a media query, and a desktop panel measured on a phone screen is
// measured without a single rule that shapes it.
var (
	phoneScreen = `{"width":393,"height":852,"deviceScaleFactor":2,"mobile":true}`
	deskScreen  = `{"width":1440,"height":900,"deviceScaleFactor":1,"mobile":false}`

	// What kind of pointer the page is told it has. Headless has none of its
	// own, and Emulation.setEmulatedMedia does not answer for hover or pointer:
	// a rule behind (hover: hover) is then switched off, and a fixture that
	// measures one of them measures nothing while reporting a pass. Blink is
	// told at startup instead — 1 is none, 2 is coarse or hover, 4 is fine.
	phonePointer = "primaryHoverType=1,availableHoverTypes=1,primaryPointerType=2,availablePointerTypes=2"
	deskPointer  = "primaryHoverType=2,availableHoverTypes=2,primaryPointerType=4,availablePointerTypes=4"
)

// runFixture serves the frontend tree with the fixture page on top of it,
// opens the page in a headless Chrome and returns what its window.done
// resolved to. Without node or Chrome the test is skipped: the fixture runs
// the real components in a real engine, and there is no reading them out of
// the source instead.
func runFixture(t *testing.T, fixture string, into any) {
	t.Helper()
	runFixtureOn(t, fixture, phoneScreen, phonePointer, into)
}

// runWideFixture is runFixture on a screen wide enough for the desktop shell.
func runWideFixture(t *testing.T, fixture string, into any) {
	t.Helper()
	runFixtureOn(t, fixture, deskScreen, deskPointer, into)
}

func runFixtureOn(t *testing.T, fixture, screen, pointer string, into any) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the fixture is driven through node")
	}
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine: the fixture runs the components in a real engine")
	}
	page, err := os.ReadFile(filepath.Join("fixtures", fixture))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(webDir)))
	mux.HandleFunc("/fixture.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cmd := exec.Command(node, "--input-type=module", "-e", chromeDriver)
	cmd.Env = append(os.Environ(),
		"AACP_FIXTURE_CHROME="+chrome,
		"AACP_FIXTURE_URL="+server.URL+"/fixture.html",
		"AACP_FIXTURE_PROFILE="+t.TempDir(),
		"AACP_FIXTURE_SCREEN="+screen,
		"AACP_FIXTURE_POINTER="+pointer,
	)
	started := time.Now()
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("%s under Chrome (%s): %v\n%s", fixture, time.Since(started).Round(time.Millisecond), err, stderr)
	}
	if err := json.Unmarshal(out, into); err != nil {
		t.Fatalf("the fixture's answer did not parse: %v: %s", err, out)
	}
}
