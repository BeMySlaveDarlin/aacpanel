package check

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
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
// resolved to. Without Chrome the test is skipped: the fixture runs the real
// components in a real engine, and there is no reading them out of the
// source instead.
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
	runFixtureServing(t, fixture, screen, pointer, nil, into)
}

// runFixtureServing is runFixtureOn with answers of the service's own next to
// the tree: a fixture that draws from what the service says asks the real
// code for it rather than carrying a copy that would go stale.
//
// A page with nothing of a test's own beside it answers the same to every
// test that reads it, so it runs once a run for each screen it is run on, and
// every test reading a part of its answer gets that answer: the tests of one
// screen are many, and a run of the page is seconds of its own pauses. A page
// served with a test's handlers runs for that test alone — the handlers are
// the test's, and it may read what they were asked.
func runFixtureServing(t *testing.T, fixture, screen, pointer string, serve map[string]http.Handler, into any) {
	t.Helper()
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine: the fixture runs the components in a real engine")
	}
	page, err := os.ReadFile(filepath.Join("fixtures", fixture))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	open := func() (json.RawMessage, error) {
		return servePage(chrome, fixture, page, screen, pointer, serve)
	}
	var run *linedPage
	if serve == nil {
		run = sharedRun(t, fixtureKey{fixture, screen, pointer}, open)
	} else {
		run = lineUp(t, false, open)
	}
	parallel(t)
	out, err := run.wait()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, into); err != nil {
		t.Fatalf("the fixture's answer did not parse: %v: %s", err, out)
	}
}

// fixtureKey is what decides a page's answer: the page and the screen and
// pointer it runs on.
type fixtureKey struct{ fixture, screen, pointer string }

// linedPage is one run of a page in the line, and what it answered once done.
// readers are the tests that took the answer of a shared run: a test asking
// again is the run repeated by -count, and it gets a run of its own, so a
// repeated run still looks for a page that answers differently from one time
// to the next.
type linedPage struct {
	open    func() (json.RawMessage, error)
	done    chan struct{}
	out     json.RawMessage
	err     error
	readers map[string]bool
}

// wait gives the answer of the page once it has run, a failure included.
func (r *linedPage) wait() (json.RawMessage, error) {
	<-r.done
	return r.out, r.err
}

var (
	sharedRunsMu sync.Mutex
	sharedRuns   = map[fixtureKey]*linedPage{}
)

func sharedRun(t *testing.T, key fixtureKey, open func() (json.RawMessage, error)) *linedPage {
	sharedRunsMu.Lock()
	defer sharedRunsMu.Unlock()
	run := sharedRuns[key]
	if run == nil || run.readers[t.Name()] {
		run = lineUp(t, false, open)
		run.readers = map[string]bool{}
		sharedRuns[key] = run
	}
	run.readers[t.Name()] = true
	return run
}

// line is where the pages of a run wait for their turn, as many at a time as
// the run lets tests go alongside each other (-parallel). A page goes into it
// as its test asks for it, before the test pauses to run alongside the
// others: the tests go through their opening part one after another and only
// then run side by side, so the pages of the whole run are lined up while the
// first of them are already open. A test that holds one of the run's places
// and waits on its page is not what limits the pages — the line is — and a
// place held by a test that only waits costs nothing while the line goes on.
//
// The pages of a test that already runs go first: it waits on them now. So do
// the pages a caller knows to be long: started last, a page that sits out a
// wait of seconds is the whole tail of the run.
var line struct {
	sync.Mutex
	first, rest []*linedPage
	working     int
}

func lineUp(t *testing.T, long bool, open func() (json.RawMessage, error)) *linedPage {
	run := &linedPage{open: open, done: make(chan struct{})}
	_, running := parallelAsked.Load(t)
	line.Lock()
	defer line.Unlock()
	if running || long {
		line.first = append(line.first, run)
	} else {
		line.rest = append(line.rest, run)
	}
	if line.working < pagesAtOnce() {
		line.working++
		go workTheLine()
	}
	return run
}

func workTheLine() {
	for {
		line.Lock()
		var run *linedPage
		switch {
		case len(line.first) > 0:
			run, line.first = line.first[0], line.first[1:]
		case len(line.rest) > 0:
			run, line.rest = line.rest[0], line.rest[1:]
		default:
			line.working--
			line.Unlock()
			return
		}
		line.Unlock()
		run.out, run.err = run.open()
		close(run.done)
	}
}

// pagesAtOnce is the -parallel of the run: what a run of the tests is allowed
// to do at once is what its pages do at once.
func pagesAtOnce() int {
	if f := flag.Lookup("test.parallel"); f != nil {
		if n, err := strconv.Atoi(f.Value.String()); err == nil && n > 0 {
			return n
		}
	}
	return runtime.GOMAXPROCS(0)
}

// servePage serves the tree with the page on top and returns what the page
// answered under Chrome.
func servePage(chrome, fixture string, page []byte, screen, pointer string, serve map[string]http.Handler) (json.RawMessage, error) {
	// A server of its own is an origin of its own: nothing a fixture keeps in
	// its storage is there for the next one.
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(webDir)))
	for pattern, h := range serve {
		mux.Handle(pattern, h)
	}
	mux.HandleFunc("/fixture.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	started := time.Now()
	b, err := sharedBrowser(chrome, pointer)
	if err != nil {
		return nil, fmt.Errorf("Chrome did not start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := b.run(ctx, server.URL+"/fixture.html", screen)
	if err != nil {
		return nil, fmt.Errorf("%s under Chrome (%s): %v", fixture, time.Since(started).Round(time.Millisecond), err)
	}
	return out, nil
}
