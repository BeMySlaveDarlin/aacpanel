package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"aacpanel/internal/auth"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/termlink"
)

func TestTerminalRoutesFollowTheSwitch(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/term"},
		{http.MethodGet, "/api/term/stream"},
		{http.MethodPost, "/api/term/input"},
		{http.MethodPost, "/api/term/size"},
	}

	off := (&Server{}).routes((&Server{}).publicGate())
	for _, route := range routes {
		if _, pattern := off.Handler(httptest.NewRequest(route.method, route.path, nil)); pattern != "" {
			t.Errorf("%s %s is registered on the main listener without the switch (%s)", route.method, route.path, pattern)
		}
	}

	on := &Server{termPublic: true, auth: &auth.Service{}}
	mux := on.routes(on.publicGate())
	for _, route := range routes {
		_, pattern := mux.Handler(httptest.NewRequest(route.method, route.path, nil))
		if pattern == "" {
			t.Errorf("%s %s is not registered with AACP_TERM_PUBLIC=1", route.method, route.path)
			continue
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session answered %d, expected 401 — the handler stands past the gate",
				route.method, route.path, w.Code)
		}
	}

	local := (&Server{}).routes((&Server{}).localGate())
	for _, route := range routes {
		if _, pattern := local.Handler(httptest.NewRequest(route.method, route.path, nil)); pattern == "" {
			t.Errorf("%s %s is not registered on the local listener", route.method, route.path)
		}
	}

	for _, path := range []string{"/app", "/api/host", "/api/chat", "/api/sessions/archive"} {
		if _, pattern := local.Handler(httptest.NewRequest(http.MethodGet, path, nil)); pattern == "" {
			t.Errorf("%s is missing on the local panel — the screens drifted apart", path)
		}
	}
}

type fakeScreen struct {
	out io.ReadCloser

	mu    sync.Mutex
	in    []byte
	sizes [][2]uint16
}

func (f *fakeScreen) Read(p []byte) (int, error) { return f.out.Read(p) }

func (f *fakeScreen) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, p...)
	return len(p), nil
}

func (f *fakeScreen) Resize(cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, [2]uint16{cols, rows})
	return nil
}

func (f *fakeScreen) Close() error   { return f.out.Close() }
func (f *fakeScreen) Kind() string   { return "tmux" }
func (f *fakeScreen) Detail() string { return "aacpanel (pane aacpanel:0.0)" }

func (f *fakeScreen) typed() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.in)
}

func (f *fakeScreen) resized() [][2]uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]uint16(nil), f.sizes...)
}

type screenOpener struct {
	screen *fakeScreen
	err    error

	mu     sync.Mutex
	target string
	cols   uint16
	rows   uint16
}

func (o *screenOpener) Open(_ context.Context, target string, cols, rows uint16) (termlink.Terminal, error) {
	o.mu.Lock()
	o.target, o.cols, o.rows = target, cols, rows
	o.mu.Unlock()
	if o.err != nil {
		return nil, o.err
	}
	return o.screen, nil
}

func (o *screenOpener) asked() (string, uint16, uint16) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.target, o.cols, o.rows
}

func termStand(t *testing.T, opener termlink.Opener) *Server {
	t.Helper()
	path := socketPath(t, "t.sock")
	srv := termlink.NewServer(path, opener)
	ln, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Serve(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &Server{terms: newTerminals(termlink.NewClient(path))}
}

func TestTerminalStreamReachesBrowser(t *testing.T) {
	screen, feed := io.Pipe()
	fake := &fakeScreen{out: screen}
	opener := &screenOpener{screen: fake}
	srv := termStand(t, opener)

	http1 := httptest.NewServer(srv.routes(srv.localGate()))
	t.Cleanup(func() {
		http1.CloseClientConnections()
		http1.Close()
	})
	t.Cleanup(func() { feed.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		http1.URL+"/api/term/stream?name=aacpanel&cols=120&rows=40", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("the stream did not open: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("the output is served as %q: a streaming octet-stream does not reach the page", got)
	}
	br := bufio.NewReader(resp.Body)
	ready := readEvent(t, br)
	if ready.event != "ready" {
		t.Fatalf("the first event was %q", ready.event)
	}
	var info struct{ ID, Kind, Detail string }
	if err := json.Unmarshal([]byte(ready.data), &info); err != nil {
		t.Fatalf("the ready event was not parsed: %v (%s)", err, ready.data)
	}
	id := info.ID
	if id == "" {
		t.Fatal("the terminal id did not arrive")
	}
	if info.Kind != "tmux" {
		t.Errorf("the kind of the terminal is %q", info.Kind)
	}
	if !strings.Contains(info.Detail, "aacpanel") {
		t.Errorf("the caption %q does not name the session", info.Detail)
	}

	go feed.Write([]byte("\x1b[32msession ready\x1b[0m"))
	screenEvent := readEvent(t, br)
	raw, err := base64.StdEncoding.DecodeString(screenEvent.data)
	if err != nil {
		t.Fatalf("the screen arrived not as base64: %v", err)
	}
	if !strings.Contains(string(raw), "session ready") {
		t.Errorf("%q arrived from the screen", raw)
	}

	if target, cols, rows := opener.asked(); target != "aacpanel" || cols != 120 || rows != 40 {
		t.Errorf("%q %dx%d went to the executor", target, cols, rows)
	}

	post := func(path string, body io.Reader) int {
		r, err := http.Post(http1.URL+path, "application/octet-stream", body)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer r.Body.Close()
		return r.StatusCode
	}
	if code := post("/api/term/input?id="+id, strings.NewReader("ls\r\x1b")); code != http.StatusNoContent {
		t.Fatalf("the input was not accepted: %d", code)
	}
	waitUntil(t, func() bool { return fake.typed() == "ls\r\x1b" }, "the input did not reach the session")

	if code := post("/api/term/size?id="+id+"&cols=100&rows=30", nil); code != http.StatusNoContent {
		t.Fatalf("the resize was not accepted: %d", code)
	}
	waitUntil(t, func() bool {
		got := fake.resized()
		return len(got) == 1 && got[0] == [2]uint16{100, 30}
	}, "the resize did not arrive")

	cancel()
	resp.Body.Close()
	waitUntil(t, func() bool {
		return post("/api/term/input?id="+id, strings.NewReader("x")) == http.StatusGone
	}, "the bridge stayed open after the tab left")
}

func TestTerminalStreamNamesRefusal(t *testing.T) {
	srv := termStand(t, &screenOpener{err: errTermMissing{}})
	mux := srv.routes(srv.localGate())

	t.Run("an ordinary client", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/term/stream?name=shop&cols=80&rows=24", nil))
		if rec.Code != http.StatusBadGateway {
			t.Errorf("status %d instead of 502", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "no live session") {
			t.Errorf("the reason was lost: %q", rec.Body.String())
		}
	})

	t.Run("EventSource", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/term/stream?name=shop&cols=80&rows=24", nil)
		r.Header.Set("Accept", "text/event-stream")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)

		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: EventSource will not show the body of a non-200 response, and the reason disappears", rec.Code)
		}
		ev := readEvent(t, bufio.NewReader(rec.Body))
		if ev.event != "end" {
			t.Fatalf("event %q arrived instead of the end", ev.event)
		}
		var body struct{ Reason string }
		if err := json.Unmarshal([]byte(ev.data), &body); err != nil {
			t.Fatalf("the end was not parsed: %v (%s)", err, ev.data)
		}
		if !strings.Contains(body.Reason, "no live session") {
			t.Errorf("the reason was lost: %q", body.Reason)
		}
	})
}

type errTermMissing struct{}

func (errTermMissing) Error() string { return "there is no live session shop" }

func TestTerminalStatusAsksExecutor(t *testing.T) {
	srv := termStand(t, &screenOpener{})
	mux := srv.routes(srv.localGate())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/term", nil))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not json: %s", rec.Body.String())
	}
	if out["available"] != true {
		t.Errorf("a live executor is shown as unavailable: %v", out)
	}

	empty := &Server{terms: newTerminals(termlink.NewClient(""))}
	rec = httptest.NewRecorder()
	empty.routes(empty.localGate()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/term", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not json: %s", rec.Body.String())
	}
	if out["available"] != false || out["reason"] == "" {
		t.Errorf("without an executor the answer is %v — the reason is not named", out)
	}
}

func TestTerminalSizeRefusesNonsense(t *testing.T) {
	srv := termStand(t, &screenOpener{})
	mux := srv.routes(srv.localGate())

	for _, q := range []string{"name=a", "name=a&cols=0&rows=24", "name=a&cols=80&rows=0", "name=a&cols=99999&rows=24"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/term/stream?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q was accepted with status %d", q, rec.Code)
		}
	}
}

func waitUntil(t *testing.T, ok func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

type sseEvent struct{ event, data string }

func readEvent(t *testing.T, br *bufio.Reader) sseEvent {
	t.Helper()
	var out sseEvent
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("the stream broke off: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			out.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			out.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if out.data != "" || out.event != "" {
				return out
			}
		}
	}
}
