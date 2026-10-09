package check

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// browser is one headless Chrome the fixtures of a run share. Starting Chrome
// costs more than a fixture does, and a fixture needs no process of its own:
// a browser context is as clean as a new profile — its own storage, its own
// cache — and a tab in it is a page of its own. The devtools protocol goes
// over the debugging pipe: no port, nothing to install.
type browser struct {
	cmd     *exec.Cmd
	toMu    sync.Mutex
	to      io.WriteCloser
	profile string

	mu      sync.Mutex
	seq     int
	waiting map[int]chan cdpMessage
	pages   map[string]*page
	dead    error
	stderr  *tailBuffer
}

// page is what the browser tells of one tab besides the answers to its
// commands: what its page threw, and what took the page away from under a
// fixture — the renderer crashing, the tab closing, the main frame going to
// another document.
type page struct {
	thrown  []string
	crashed bool
	closed  bool
	// urls are the documents the main frame committed, in order.
	urls []string
	// news is closed, and replaced, whenever the page crashes, closes or
	// navigates: everyone waiting on the page hears it.
	news chan struct{}
}

// errRendererCrashed is a run cut short by the renderer under the page: the
// machine, not the page, and the fixture is worth running again.
var errRendererCrashed = errors.New("the renderer crashed")

// end says what took the page away from url, or nil while it stands.
func (p *page) end(url string) error {
	if p.crashed {
		return errRendererCrashed
	}
	if p.closed {
		return errors.New("the tab was closed")
	}
	// The first document committed at url is the page itself; whatever the
	// main frame commits after it is the page gone somewhere else.
	for i, at := range p.urls {
		if at == url && i+1 < len(p.urls) {
			return fmt.Errorf("the page navigated to %s", p.urls[i+1])
		}
	}
	return nil
}

func (p *page) tell() {
	close(p.news)
	p.news = make(chan struct{})
}

// note keeps what an event of the page's own session says.
func (p *page) note(msg cdpMessage) {
	switch msg.Method {
	case "Runtime.exceptionThrown":
		var ev struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		_ = json.Unmarshal(msg.Params, &ev)
		p.thrown = append(p.thrown, strings.TrimSpace(ev.ExceptionDetails.Text+" "+ev.ExceptionDetails.Exception.Description))
	case "Inspector.targetCrashed":
		p.crashed = true
		p.tell()
	case "Page.frameNavigated":
		var ev struct {
			Frame struct {
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		_ = json.Unmarshal(msg.Params, &ev)
		if ev.Frame.ParentID == "" {
			p.urls = append(p.urls, ev.Frame.URL)
			p.tell()
		}
	}
}

type cdpMessage struct {
	ID        int             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// browsers are started on first use, a few for each kind of pointer: what the
// page is told about its pointer is a flag of the process, not of a tab, and
// one process serving every tab of a parallel run is where the tabs queue.
const browsersPerPointer = 4

var (
	browsersMu sync.Mutex
	browsers   = map[string][]*browser{}
	turns      = map[string]int{}
)

func sharedBrowser(chrome, pointer string) (*browser, error) {
	browsersMu.Lock()
	defer browsersMu.Unlock()
	pool := browsers[pointer]
	if len(pool) < browsersPerPointer {
		pool = append(pool, nil)
	}
	turn := turns[pointer] % len(pool)
	turns[pointer]++
	if b := pool[turn]; b != nil && b.alive() == nil {
		browsers[pointer] = pool
		return b, nil
	}
	b, err := startBrowser(chrome, pointer)
	if err != nil {
		return nil, err
	}
	pool[turn] = b
	browsers[pointer] = pool
	return b, nil
}

// closeBrowsers ends every Chrome the run started, with its profile.
func closeBrowsers() {
	browsersMu.Lock()
	defer browsersMu.Unlock()
	for key, pool := range browsers {
		for _, b := range pool {
			if b != nil {
				b.close()
			}
		}
		delete(browsers, key)
	}
}

func startBrowser(chrome, pointer string) (*browser, error) {
	profile, err := os.MkdirTemp("", "aacpanel-fixture-chrome-")
	if err != nil {
		return nil, err
	}
	// Chrome reads the protocol from its descriptor 3 and writes to 4.
	chromeIn, to, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	from, chromeOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	b := &browser{
		to: to, profile: profile, stderr: &tailBuffer{limit: 8 << 10},
		waiting: map[int]chan cdpMessage{}, pages: map[string]*page{},
	}
	b.cmd = exec.Command(chrome, "--headless=new", "--remote-debugging-pipe", "--user-data-dir="+profile,
		"--password-store=basic", "--no-first-run", "--no-default-browser-check",
		"--use-angle=vulkan", "--enable-features=Vulkan", "--disable-background-networking",
		"--disable-background-timer-throttling", "--disable-renderer-backgrounding",
		"--disable-backgrounding-occluded-windows", "--hide-scrollbars",
		"--blink-settings="+pointer, "about:blank")
	b.cmd.ExtraFiles = []*os.File{chromeIn, chromeOut}
	b.cmd.Stderr = b.stderr
	if err := b.cmd.Start(); err != nil {
		_ = os.RemoveAll(profile)
		return nil, err
	}
	_ = chromeIn.Close()
	_ = chromeOut.Close()
	go b.read(from)
	return b, nil
}

func (b *browser) read(from io.ReadCloser) {
	defer from.Close()
	in := bufio.NewReader(from)
	for {
		raw, err := in.ReadBytes(0)
		if err != nil {
			b.fail(fmt.Errorf("Chrome closed the pipe: %v\n%s", err, b.stderr))
			return
		}
		var msg cdpMessage
		if json.Unmarshal(raw[:len(raw)-1], &msg) != nil {
			continue
		}
		b.mu.Lock()
		switch {
		case msg.ID != 0:
			if ch := b.waiting[msg.ID]; ch != nil {
				delete(b.waiting, msg.ID)
				ch <- msg
			}
		case msg.SessionID != "":
			if p := b.pages[msg.SessionID]; p != nil {
				p.note(msg)
			}
		case msg.Method == "Target.detachedFromTarget":
			var ev struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(msg.Params, &ev)
			if p := b.pages[ev.SessionID]; p != nil {
				p.closed = true
				p.tell()
			}
		}
		b.mu.Unlock()
	}
}

func (b *browser) fail(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dead == nil {
		b.dead = err
	}
	for id, ch := range b.waiting {
		close(ch)
		delete(b.waiting, id)
	}
}

func (b *browser) alive() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dead
}

// call sends one command and waits for its answer.
func (b *browser) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	ch := make(chan cdpMessage, 1)
	b.mu.Lock()
	if b.dead != nil {
		b.mu.Unlock()
		return nil, b.dead
	}
	b.seq++
	id := b.seq
	b.waiting[id] = ch
	b.mu.Unlock()

	line, _ := json.Marshal(cdpMessage{ID: id, Method: method, Params: body, SessionID: session})
	b.toMu.Lock()
	_, err = b.to.Write(append(line, 0))
	b.toMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, b.alive()
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// track starts keeping what the browser tells of the tab of a session.
func (b *browser) track(session string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pages[session] = &page{news: make(chan struct{})}
}

// thrownIn returns what the page of a session threw so far.
func (b *browser) thrownIn(session string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.pages[session]; p != nil {
		return append([]string(nil), p.thrown...)
	}
	return nil
}

// ended waits until the browser tells what took the page of a session away
// from url and returns it, or nil once done is closed first.
func (b *browser) ended(done <-chan struct{}, session, url string) error {
	for {
		b.mu.Lock()
		p := b.pages[session]
		var err error
		var news chan struct{}
		if p != nil {
			err, news = p.end(url), p.news
		}
		b.mu.Unlock()
		if p == nil || err != nil {
			return err
		}
		select {
		case <-news:
		case <-done:
			return nil
		}
	}
}

// pageCall is call on the page of a session that ends with the page. A
// renderer that crashed leaves the command unanswered, and a navigation
// answers it with a bare "navigated or closed": either way the error says what
// took the page away from url.
func (b *browser) pageCall(ctx context.Context, session, url, method string, params any) (json.RawMessage, error) {
	callCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		if end := b.ended(callCtx.Done(), session, url); end != nil {
			cancel(end)
		}
	}()
	raw, err := b.call(callCtx, session, method, params)
	cancel(nil)
	<-watched
	if err == nil || ctx.Err() != nil {
		return raw, err
	}
	if end := context.Cause(callCtx); end != context.Canceled {
		return nil, end
	}
	// The answer cut short can come before the event that says why.
	if b.alive() == nil {
		within, stop := context.WithTimeout(ctx, endWait)
		defer stop()
		if end := b.ended(within.Done(), session, url); end != nil {
			return nil, fmt.Errorf("%w (%v)", end, err)
		}
	}
	return nil, err
}

func (b *browser) forget(session string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pages, session)
}

func (b *browser) close() {
	_ = b.to.Close()
	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_ = b.cmd.Wait()
	}
	_ = os.RemoveAll(b.profile)
}

// tailBuffer keeps the last bytes Chrome wrote to its stderr, for a failure
// to say what Chrome said.
type tailBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// parallel lets a test that runs a fixture go alongside the others: a fixture
// waits on Chrome, not on the CPU. A test that runs two fixtures asks once.
var parallelAsked sync.Map

func parallel(t *testing.T) {
	if _, again := parallelAsked.LoadOrStore(t, true); !again {
		t.Parallel()
	}
}

// disposeWait bounds the dispose of a run's browser context.
var disposeWait = 10 * time.Second

// endWait bounds how long a command cut short waits for the browser to say
// what took the page away.
const endWait = 2 * time.Second

// run opens a page in a context of its own, waits for the promise the page
// leaves in window.done and returns what it resolved to. A page taken away
// before it answers says how: the renderer crashed (errRendererCrashed), the
// tab closed, or the page navigated, and to where.
func (b *browser) run(ctx context.Context, url, screen string) (json.RawMessage, error) {
	session, close, err := b.open(ctx, url, screen)
	if err != nil {
		return nil, err
	}
	defer close()
	for {
		raw, err := b.pageCall(ctx, session, url, "Runtime.evaluate", map[string]any{
			"expression": "window.done", "awaitPromise": true, "returnByValue": true})
		if err != nil {
			return nil, err
		}
		var r struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
			ExceptionDetails *struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		if r.ExceptionDetails != nil {
			return nil, fmt.Errorf("the fixture failed: %s %s", r.ExceptionDetails.Text, r.ExceptionDetails.Exception.Description)
		}
		thrown := b.thrownIn(session)
		if len(r.Result.Value) > 0 {
			if len(thrown) > 0 {
				return nil, fmt.Errorf("the page threw: %s", strings.Join(thrown, "; "))
			}
			return r.Result.Value, nil
		}
		if len(thrown) > 0 {
			return nil, fmt.Errorf("the page threw: %s", strings.Join(thrown, "; "))
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("the fixture did not finish in time: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// open makes a tab in a browser context of its own and sends it to url.
// close takes the context away, whatever became of the page.
func (b *browser) open(ctx context.Context, url, screen string) (session string, close func(), err error) {
	var created struct {
		BrowserContextID string `json:"browserContextId"`
	}
	raw, err := b.call(ctx, "", "Target.createBrowserContext", map[string]any{})
	if err != nil || json.Unmarshal(raw, &created) != nil {
		return "", nil, fmt.Errorf("no browser context: %v", err)
	}
	// The context goes on a bound of its own: a Chrome that stopped answering
	// would hold the dispose, and the whole package with it, long after the
	// run itself gave up.
	dispose := func() {
		done, cancel := context.WithTimeout(context.Background(), disposeWait)
		defer cancel()
		_, _ = b.call(done, "", "Target.disposeBrowserContext", map[string]any{"browserContextId": created.BrowserContextID})
	}

	var target struct {
		TargetID string `json:"targetId"`
	}
	raw, err = b.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": created.BrowserContextID})
	if err != nil || json.Unmarshal(raw, &target) != nil {
		dispose()
		return "", nil, fmt.Errorf("no tab: %v", err)
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	raw, err = b.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true})
	if err != nil || json.Unmarshal(raw, &attached) != nil {
		dispose()
		return "", nil, fmt.Errorf("the tab did not attach: %v", err)
	}
	session = attached.SessionID
	b.track(session)
	close = func() {
		b.forget(session)
		dispose()
	}

	for _, step := range []struct {
		method string
		params any
	}{
		{"Runtime.enable", map[string]any{}},
		// What the main frame commits and a crash of the renderer: a page that
		// goes before it answers says how it went.
		{"Page.enable", map[string]any{}},
		{"Inspector.enable", map[string]any{}},
		// Every tab of the browser is in front: a page in the background has
		// its timers slowed and its animation frames stopped.
		{"Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}},
		{"Emulation.setDeviceMetricsOverride", json.RawMessage(screen)},
		{"Page.navigate", map[string]any{"url": url}},
	} {
		if _, err := b.call(ctx, session, step.method, step.params); err != nil {
			close()
			return "", nil, err
		}
	}
	return session, close, nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	closeBrowsers()
	os.Exit(code)
}

// A page that throws is a failed fixture even when it goes on to leave its
// answer: the error is what a person would have met on the screen. One that
// throws and never answers fails at once, not a minute later.
func TestAPageThatThrowsIsAFailure(t *testing.T) {
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine")
	}
	pages := map[string]string{
		"and never answers": `setTimeout(() => { throw new Error("the card lost its rows"); }, 0);`,
		"while waited for": `window.done = new Promise((r) => setTimeout(() => r({ ok: true }), 400));
setTimeout(() => { throw new Error("the card lost its rows"); }, 150);`,
	}
	for name, script := range pages {
		t.Run(name, func(t *testing.T) {
			parallel(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprintf(w, "<script>%s</script>", script)
			}))
			defer server.Close()
			b, err := sharedBrowser(chrome, phonePointer)
			if err != nil {
				t.Fatalf("Chrome did not start: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err = b.run(ctx, server.URL, phoneScreen)
			if err == nil || !strings.Contains(err.Error(), "the card lost its rows") {
				t.Fatalf("a page that threw passed as a fixture: %v", err)
			}
		})
	}
}

// A Chrome that stops answering in the middle of a run holds nothing past the
// run's own bound: the fake answers the browser context and then keeps quiet,
// and the run comes back with an error instead of waiting on the dispose.
func TestARunOnAChromeThatStoppedAnsweringComesBack(t *testing.T) {
	was := disposeWait
	disposeWait = 100 * time.Millisecond
	t.Cleanup(func() { disposeWait = was })

	toChrome, to, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	from, fromChrome, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = to.Close(); _ = fromChrome.Close(); _ = toChrome.Close() })
	b := &browser{to: to, stderr: &tailBuffer{limit: 1 << 10},
		waiting: map[int]chan cdpMessage{}, pages: map[string]*page{}}
	go b.read(from)
	go func() {
		in := bufio.NewReader(toChrome)
		line, err := in.ReadBytes(0)
		if err != nil {
			return
		}
		var msg cdpMessage
		_ = json.Unmarshal(line[:len(line)-1], &msg)
		reply, _ := json.Marshal(cdpMessage{ID: msg.ID, Result: json.RawMessage(`{"browserContextId":"c1"}`)})
		_, _ = fromChrome.Write(append(reply, 0))
		_, _ = io.Copy(io.Discard, in)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := b.run(ctx, "about:blank", "{}")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a Chrome that answered nothing but the context gave a result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run on a Chrome that stopped answering never came back: the dispose waits on it unbounded")
	}
}

// pageAt finds the tab of the shared browsers whose main frame stands at url.
func pageAt(url string) (*browser, string) {
	browsersMu.Lock()
	defer browsersMu.Unlock()
	for _, pool := range browsers {
		for _, b := range pool {
			if b == nil {
				continue
			}
			b.mu.Lock()
			for session, p := range b.pages {
				if n := len(p.urls); n > 0 && p.urls[n-1] == url {
					b.mu.Unlock()
					return b, session
				}
			}
			b.mu.Unlock()
		}
	}
	return nil, ""
}

// A page taken away before it answers says how it went. A renderer crashed
// under it is the machine, not the page: the fixture runs once more and
// passes, and the log says why. A page that navigates is the page's own doing:
// it fails at once, with where it went, and is not run again.
func TestAPageTakenAwaySaysHow(t *testing.T) {
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine")
	}
	serve := func(t *testing.T, pages map[string]func(w http.ResponseWriter, r *http.Request)) string {
		mux := http.NewServeMux()
		for path, h := range pages {
			mux.HandleFunc(path, h)
		}
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)
		return server.URL
	}
	htmlPage := func(script string) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<script>%s</script>", script)
		}
	}

	t.Run("the renderer crashed", func(t *testing.T) {
		parallel(t)
		var visits atomic.Int32
		var url string
		url = serve(t, map[string]func(w http.ResponseWriter, r *http.Request){
			"/page": htmlPage(`window.done = fetch("/visit").then((r) => (r.ok ? r.json() : new Promise(() => {})));`),
			"/visit": func(w http.ResponseWriter, r *http.Request) {
				if visits.Add(1) > 1 {
					fmt.Fprint(w, `{"ok":true}`)
					return
				}
				// The first visit has its renderer crashed under it.
				for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
					if b, session := pageAt(url + "/page"); b != nil {
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							_, _ = b.call(ctx, session, "Page.crash", map[string]any{})
						}()
						break
					}
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			},
		})
		var logMu sync.Mutex
		var logged []string
		logf := func(format string, args ...any) {
			logMu.Lock()
			defer logMu.Unlock()
			logged = append(logged, fmt.Sprintf(format, args...))
		}
		out, err := runInChrome(chrome, phonePointer, url+"/page", phoneScreen, logf)
		if err != nil {
			t.Fatalf("a page whose renderer crashed once did not pass on the run that followed: %v", err)
		}
		if string(out) != `{"ok":true}` {
			t.Errorf("the run after the crash answered %s", out)
		}
		if visits.Load() < 2 {
			t.Errorf("the page was visited %d times — the crash was never met, the test proves nothing", visits.Load())
		}
		logMu.Lock()
		defer logMu.Unlock()
		if len(logged) != 1 || !strings.Contains(logged[0], "renderer crashed") {
			t.Errorf("the run once more is logged as %q, not as the renderer crashing", logged)
		}
	})

	t.Run("the page navigated", func(t *testing.T) {
		parallel(t)
		var loads atomic.Int32
		url := serve(t, map[string]func(w http.ResponseWriter, r *http.Request){
			"/page": func(w http.ResponseWriter, r *http.Request) {
				loads.Add(1)
				htmlPage(`window.done = new Promise(() => {});
setTimeout(() => { location.href = "/elsewhere"; }, 100);`)(w, r)
			},
			"/elsewhere": htmlPage(""),
		})
		_, err := runInChrome(chrome, phonePointer, url+"/page", phoneScreen, t.Logf)
		if err == nil || !strings.Contains(err.Error(), "navigated to "+url+"/elsewhere") {
			t.Fatalf("a page that navigated away failed as %v, not with where it went", err)
		}
		if loads.Load() != 1 {
			t.Errorf("a page that navigated away was loaded %d times — a navigation is the page's doing, not to be run again", loads.Load())
		}
	})
}
