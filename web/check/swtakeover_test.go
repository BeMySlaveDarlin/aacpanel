package check

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"aacpanel/internal/webbuild"
)

// heldWorker stands for a worker a phone may still run: it streams every
// answer it takes and knows nothing of letting go. With knows it says what
// it has out when asked — a worker held by something it cannot cut.
const heldWorker = `
const KNOWS = %t;
const live = new Map();
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));
self.addEventListener("message", (event) => {
    const type = event.data && event.data.type;
    if (type === "SKIP_WAITING") self.skipWaiting();
    if (type === "INFLIGHT" && KNOWS && event.ports[0]) {
        const now = Date.now();
        event.ports[0].postMessage({ live: [...live].map(([path, since]) => ({ path, secs: Math.round((now - since) / 1000) })) });
    }
});
self.addEventListener("fetch", (event) => {
    const url = new URL(event.request.url);
    if (!url.pathname.startsWith("/api/") && !url.pathname.startsWith("/dist/")) return;
    live.set(url.pathname, Date.now());
    event.respondWith(fetch(event.request));
});
`

var (
	takeoverOnce   sync.Once
	takeoverPage   string
	takeoverFailed string
)

// takeoverScript bundles the fixture with the panel's own modules, the way
// the release bundles the panel: the note under test is drawn by main.js,
// which imports styles a browser cannot take as a module.
func takeoverScript(t *testing.T) string {
	t.Helper()
	takeoverOnce.Do(func() {
		root, err := webbuild.FindRoot(".")
		if err != nil {
			takeoverFailed = err.Error()
			return
		}
		alias, err := webbuild.Aliases(root)
		if err != nil {
			takeoverFailed = err.Error()
			return
		}
		entry, err := filepath.Abs(filepath.Join("fixtures", "swtakeover.js"))
		if err != nil {
			takeoverFailed = err.Error()
			return
		}
		built := esbuild.Build(esbuild.BuildOptions{
			EntryPoints: []string{entry},
			Bundle:      true,
			Format:      esbuild.FormatESModule,
			Platform:    esbuild.PlatformBrowser,
			Target:      esbuild.ES2022,
			Alias:       alias,
			Loader:      map[string]esbuild.Loader{".css": esbuild.LoaderEmpty, ".svg": esbuild.LoaderDataURL},
			External:    []string{"/static/*", "/dist/*"},
			Write:       false,
		})
		if len(built.Errors) > 0 {
			takeoverFailed = built.Errors[0].Text
			return
		}
		takeoverPage = string(built.OutputFiles[0].Contents)
	})
	if takeoverFailed != "" {
		t.Fatalf("the fixture did not build: %s", takeoverFailed)
	}
	return takeoverPage
}

// takeoverRun is what the fixture page saw: on the load after a reload the
// version it came up under and why the page reloaded, on a page that stayed
// the note it showed.
type takeoverRun struct {
	Error    string `json:"error"`
	Reloaded bool   `json:"reloaded"`
	Reloads  int    `json:"reloads"`
	AfterTap int    `json:"afterTap"`
	Version  string `json:"version"`
	Why      string `json:"why"`
	Stuck    bool   `json:"stuck"`
	Asks     bool   `json:"asks"`
	Toast    string `json:"toast"`
	Sub      string `json:"sub"`
}

// runTakeover serves the page, two panels and two versions of the worker, and
// follows the page across its reloads. old is the first version: "takeover"
// and "asking" for the panel's own worker, "silent" and "answers" for
// heldWorker.
func runTakeover(t *testing.T, old string) takeoverRun {
	t.Helper()
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine: a worker steps aside in a real engine, not in its source")
	}
	parallel(t)
	script := takeoverScript(t)
	workers := map[int64]string{2: builtWorkerAs(t, "v2")}
	switch old {
	case "takeover", "asking":
		workers[1] = builtWorkerAs(t, "v1")
	default:
		workers[1] = fmt.Sprintf(heldWorker, old == "answers")
	}
	var version, held atomic.Int64
	version.Store(1)

	// An answer that begins and never ends — until the request goes away.
	quit := make(chan struct{})
	stall := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, "// the rest never comes\n")
		w.(http.Flusher).Flush()
		held.Add(1)
		defer held.Add(-1)
		select {
		case <-r.Context().Done():
		case <-quit:
		}
	}
	// The panel of the local network the way a phone asks it: across
	// origins, with a bearer, so every request but a stream asks first.
	var streams, slow, polls atomic.Int64
	cors := func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization")
		return r.Method == http.MethodOptions
	}
	stream := func(w http.ResponseWriter, r *http.Request) {
		cors(w, r)
		w.Header().Set("Content-Type", "text/event-stream")
		streams.Add(1)
		for {
			fmt.Fprint(w, "data: {}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-quit:
				return
			case <-time.After(time.Second):
			}
		}
	}
	nearMux := http.NewServeMux()
	nearMux.HandleFunc("/api/stall", stall)
	nearMux.HandleFunc("/dist/stall.js", stall)
	nearMux.HandleFunc("/api/stream", stream)
	nearMux.HandleFunc("/api/chat/stream", stream)
	// An answer that takes seconds, asked again as soon as it comes: one is
	// always out.
	nearMux.HandleFunc("/api/slow", func(w http.ResponseWriter, r *http.Request) {
		if cors(w, r) {
			return
		}
		slow.Add(1)
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
			return
		case <-quit:
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	nearMux.HandleFunc("/api/poll", func(w http.ResponseWriter, r *http.Request) {
		if cors(w, r) {
			return
		}
		polls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	near := httptest.NewServer(nearMux)

	homeMux := http.NewServeMux()
	homeMux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><div id="root"></div><script type="module" src="/fixture.js"></script>`)
	})
	homeMux.HandleFunc("/fixture.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, script)
	})
	homeMux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, workers[version.Load()])
	})
	homeMux.HandleFunc("/bump", func(w http.ResponseWriter, r *http.Request) {
		version.Store(2)
		fmt.Fprint(w, "ok")
	})
	homeMux.HandleFunc("/held", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"count":%d}`, held.Load())
	})
	homeMux.HandleFunc("/seen", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"streams":%d,"slow":%d,"polls":%d}`, streams.Load(), slow.Load(), polls.Load())
	})
	homeMux.HandleFunc("/dist/stall.js", stall)
	home := httptest.NewServer(homeMux)
	defer func() {
		close(quit)
		near.CloseClientConnections()
		home.CloseClientConnections()
		near.Close()
		home.Close()
	}()

	b, err := sharedBrowser(chrome, phonePointer)
	if err != nil {
		t.Fatalf("Chrome did not start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, closeTab, err := b.open(ctx, home.URL+"/app?old="+old+"&near="+near.URL, phoneScreen)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTab()

	// The page is asked over and over, and a question that lands while it
	// reloads goes unanswered: the answer is whatever the last load leaves.
	for {
		raw, err := b.call(ctx, session, "Runtime.evaluate", map[string]any{"expression": "window.result", "returnByValue": true})
		if err == nil {
			var r struct {
				Result struct {
					Value json.RawMessage `json:"value"`
				} `json:"result"`
			}
			if json.Unmarshal(raw, &r) == nil && len(r.Result.Value) > 0 {
				if thrown := b.thrownIn(session); len(thrown) > 0 {
					t.Fatalf("the page threw: %v", thrown)
				}
				var got takeoverRun
				if err := json.Unmarshal(r.Result.Value, &got); err != nil {
					t.Fatalf("the fixture's answer did not parse: %v: %s", err, r.Result.Value)
				}
				if got.Error != "" {
					t.Fatalf("the fixture could not set the scene: %s", got.Error)
				}
				return got
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the page left no answer in time: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// The browser keeps the new worker waiting for as long as the old one streams
// an answer to a page, and an answer that stalls never ends — the update the
// banner offered would wait minutes for a connection nobody closes. One tap
// brings the new version up: no reload under the old worker, no grace waited out.
func TestAnUpdateTakesOverPastAnAnswerThatNeverEnds(t *testing.T) {
	got := runTakeover(t, "takeover")
	if !got.Reloaded {
		t.Fatal("the page did not reload after the tap")
	}
	if got.Version != "v2" || got.Why != "takeover" {
		t.Errorf("the page came back under %q, reloaded for %q; expected v2 after a takeover — the old "+
			"worker kept streaming, and the page reloaded under it once the grace ran out", got.Version, got.Why)
	}
	if got.Reloads != 1 {
		t.Errorf("%d reloads after one tap, expected one", got.Reloads)
	}
	if got.AfterTap > 10000 {
		t.Errorf("the new version came up %d ms after the tap: the takeover waited on the old worker", got.AfterTap)
	}
}

// A phone keeps asking: its api on the panel of the local network, the
// snapshot and the feed as streams, a screen that asks again as soon as an
// answer comes. The browser hands the page to the new worker only at a moment
// the old one has nothing out, and a page that keeps one request out at any
// time never gives it one: the update waits, the page reloads under the old
// worker and offers it again. One tap brings the new version up all the same.
func TestAnUpdateTakesOverAPageThatKeepsAsking(t *testing.T) {
	got := runTakeover(t, "asking")
	if !got.Reloaded {
		t.Fatal("the page did not reload after the tap")
	}
	if got.Version != "v2" || got.Why != "takeover" {
		t.Errorf("the page came back under %q, reloaded for %q; expected v2 after a takeover — the page went "+
			"on asking through the old worker, and the new one waited the grace out", got.Version, got.Why)
	}
	if got.Reloads != 1 {
		t.Errorf("%d reloads after one tap, expected one", got.Reloads)
	}
	if got.AfterTap > 10000 {
		t.Errorf("the new version came up %d ms after the tap: the takeover waited on the old worker", got.AfterTap)
	}
}

// A phone may still run a worker that knows nothing of letting go. The page
// asks it anyway, comes to no harm, and goes the way it went before: it waits
// the grace out, and on a page that has been round once already it says the
// update is not installing — without inventing a reason the worker never gave.
func TestAnUpdateHeldByAnOldWorkerSaysSoWithoutGuessing(t *testing.T) {
	got := runTakeover(t, "silent")
	if got.Reloaded {
		t.Fatalf("the page reloaded (%q) where it should have stopped and said the update is not installing", got.Why)
	}
	if !got.Stuck || got.Toast != "The update is not installing" {
		t.Fatalf("the page showed %q (stuck: %v), expected the note that the update is not installing", got.Toast, got.Stuck)
	}
	if got.AfterTap < 15000 {
		t.Errorf("the note came %d ms after the tap: the page did not wait the grace out", got.AfterTap)
	}
	if !got.Asks {
		t.Error("after the note the page's requests of the api stay held: the update did not install, and " +
			"the page no longer asks anything")
	}
	if got.Sub != "a reload brought the page back to the same version" {
		t.Errorf("the note says %q: a worker that does not answer has named nothing, and the note "+
			"must not say what holds the update", got.Sub)
	}
}

// A worker that cannot let go may still say what it has out, and that is the
// one clue a phone has to what holds an update: the note names it.
func TestAnUpdateHeldByAnOldWorkerNamesWhatHoldsIt(t *testing.T) {
	got := runTakeover(t, "answers")
	if got.Reloaded {
		t.Fatalf("the page reloaded (%q) where it should have stopped and said the update is not installing", got.Why)
	}
	if !got.Stuck || got.Toast != "The update is not installing" {
		t.Fatalf("the page showed %q (stuck: %v), expected the note that the update is not installing", got.Toast, got.Stuck)
	}
	m := regexp.MustCompile(`^held by (/\S+) (\d+)s, (/\S+) (\d+)s$`).FindStringSubmatch(got.Sub)
	if m == nil {
		t.Fatalf("the note says %q, expected the two requests the old worker has out, with their seconds", got.Sub)
	}
	named := map[string]bool{m[1]: true, m[3]: true}
	if !named["/api/stall"] || !named["/dist/stall.js"] {
		t.Errorf("the note names %s and %s, expected /api/stall and /dist/stall.js", m[1], m[3])
	}
	for _, secs := range []string{m[2], m[4]} {
		if n, _ := strconv.Atoi(secs); n < 15 {
			t.Errorf("the note says a request hangs %ss: it has been out since before the tap and the grace after it", secs)
		}
	}
}
