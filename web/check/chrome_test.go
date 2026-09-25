package check

import (
	"context"
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
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine: the fixture runs the components in a real engine")
	}
	page, err := os.ReadFile(filepath.Join("fixtures", fixture))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	parallel(t)
	// A server of its own is an origin of its own: nothing a fixture keeps in
	// its storage is there for the next one.
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(webDir)))
	mux.HandleFunc("/fixture.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	started := time.Now()
	b, err := sharedBrowser(chrome, pointer)
	if err != nil {
		t.Fatalf("Chrome did not start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := b.run(ctx, server.URL+"/fixture.html", screen)
	if err != nil {
		t.Fatalf("%s under Chrome (%s): %v", fixture, time.Since(started).Round(time.Millisecond), err)
	}
	if err := json.Unmarshal(out, into); err != nil {
		t.Fatalf("the fixture's answer did not parse: %v: %s", err, out)
	}
}
