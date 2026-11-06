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
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

	mu       sync.Mutex
	seq      int
	received int
	waiting  map[int]chan cdpMessage
	pages    map[string]*page
	dead     error
	stderr   *tailBuffer
	// disposeWait bounds the dispose of a run's browser context; zero is
	// disposeBound.
	disposeWait time.Duration
	// started is closed once the Chrome has served its first page, or failed
	// to within startBound.
	started chan struct{}
}

// page keeps a tab's failures and last observed state, and what took its
// document away from the fixture: closing the tab or navigating elsewhere.
// The state is what the browser told as it happened, not what the page
// answers when asked: a page starved of its main thread answers nothing
// before the deadline, and the timeout would have nothing to say about it.
type page struct {
	thrown                    []string
	closed                    bool
	readyState, doneType, url string
	diagnostics               tailBuffer
	requests                  map[string]string
	requestIDs                []string
	// frame is the main frame: a move of its history is a move of the page.
	frame string
	// urls are the documents the main frame committed, in order.
	urls []string
	// news is closed, and replaced, whenever the page closes or navigates:
	// everyone waiting on the page hears it.
	news chan struct{}
}

const (
	pageDiagnosticLimit = 8 << 10
	pageRequestLimit    = 1024
)

// doneWatch runs in the main frame of every document before the page's own
// scripts and tells the harness, through the binding doneBinding, "undefined"
// at once and then the type of every value the page puts into window.done.
const (
	doneBinding = "aacpFixtureDone"
	doneWatch   = `(() => {
    if (window !== window.top) return;
    const tell = window.` + doneBinding + `;
    let done;
    Object.defineProperty(window, "done", {
        configurable: true, enumerable: true,
        get: () => done,
        set: (value) => { done = value; tell(typeof value); },
    });
    tell("undefined");
})();`
)

// end says what took the page away from url, or nil while it stands.
func (p *page) end(url string) error {
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
	case "Page.frameNavigated":
		var ev struct {
			Frame struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
			} `json:"frame"`
		}
		_ = json.Unmarshal(msg.Params, &ev)
		if ev.Frame.ParentID == "" {
			p.frame = ev.Frame.ID
			p.urls = append(p.urls, ev.Frame.URL)
			p.readyState, p.doneType = "loading", "unknown"
			p.url = ev.Frame.URL
			p.tell()
		}
	case "Page.navigatedWithinDocument":
		var ev struct{ FrameID, URL string }
		_ = json.Unmarshal(msg.Params, &ev)
		if ev.FrameID == p.frame {
			p.url = ev.URL
		}
	case "Runtime.bindingCalled":
		var ev struct{ Name, Payload string }
		_ = json.Unmarshal(msg.Params, &ev)
		if ev.Name == doneBinding {
			p.doneType = ev.Payload
		}
	case "Page.domContentEventFired":
		p.readyState = "interactive"
	case "Page.loadEventFired":
		p.readyState = "complete"
	case "Log.entryAdded":
		var ev struct {
			Entry struct{ Level, Text, URL string } `json:"entry"`
		}
		_ = json.Unmarshal(msg.Params, &ev)
		if ev.Entry.Level == "error" || ev.Entry.Level == "warning" {
			fmt.Fprintf(&p.diagnostics, "[Log %s] %s: %s\n", ev.Entry.Level, ev.Entry.URL, ev.Entry.Text)
		}
	case "Runtime.consoleAPICalled":
		var ev struct {
			Type string `json:"type"`
			Args []struct {
				Value                                  json.RawMessage `json:"value"`
				Description, UnserializableValue, Type string
			} `json:"args"`
		}
		_ = json.Unmarshal(msg.Params, &ev)
		if ev.Type != "error" && ev.Type != "warning" {
			return
		}
		parts := make([]string, 0, len(ev.Args))
		for _, arg := range ev.Args {
			value := arg.Description
			if len(arg.Value) > 0 {
				if json.Unmarshal(arg.Value, &value) != nil {
					value = string(arg.Value)
				}
			} else if value == "" {
				value = arg.UnserializableValue
				if value == "" {
					value = arg.Type
				}
			}
			parts = append(parts, value)
		}
		fmt.Fprintf(&p.diagnostics, "[console %s] %s\n", ev.Type, strings.Join(parts, " "))
	case "Network.requestWillBeSent":
		var ev struct {
			RequestID string               `json:"requestId"`
			Request   struct{ URL string } `json:"request"`
		}
		_ = json.Unmarshal(msg.Params, &ev)
		if p.requests == nil {
			p.requests = map[string]string{}
		}
		if _, seen := p.requests[ev.RequestID]; !seen {
			p.requestIDs = append(p.requestIDs, ev.RequestID)
			// A page can leave requests pending forever; their URL lookup is
			// bounded along with the diagnostics it serves.
			if len(p.requestIDs) > pageRequestLimit {
				delete(p.requests, p.requestIDs[0])
				p.requestIDs = p.requestIDs[1:]
			}
		}
		p.requests[ev.RequestID] = ev.Request.URL
	case "Network.loadingFailed", "Network.loadingFinished":
		var ev struct{ RequestID, ErrorText, BlockedReason string }
		_ = json.Unmarshal(msg.Params, &ev)
		if msg.Method == "Network.loadingFailed" {
			url := p.requests[ev.RequestID]
			if url == "" {
				url = "request " + ev.RequestID
			}
			fmt.Fprintf(&p.diagnostics, "[Network] %s: %s %s\n", url, ev.ErrorText, ev.BlockedReason)
		}
		delete(p.requests, ev.RequestID)
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

// browsers are started on first use, one for each kind of pointer: what the
// page is told about its pointer is a flag of the process, not of a tab. Two
// kinds of pointer make two Chromes a run: each Chrome spreads its renderers
// over cores of its own, and a shared machine gives a run of tests no more
// than that beside the tests themselves; the tabs of a parallel run queue in
// them instead.
const browsersPerPointer = 1

var (
	browsersMu sync.Mutex
	browsers   = map[string][]*browser{}
	turns      = map[string]int{}
	// over is set once the run has closed its Chromes: a page still in the
	// line of a run that stopped early starts none of its own, which would
	// outlive the run with its profile.
	over bool
)

// sharedBrowser gives a Chrome that serves pages: one still starting is waited
// for, so a test's own bound, set after it has the browser, counts its own
// work and none of the start.
func sharedBrowser(chrome, pointer string) (*browser, error) {
	b, err := pooledBrowser(chrome, pointer)
	if err != nil {
		return nil, err
	}
	<-b.started
	if err := b.alive(); err != nil {
		return nil, err
	}
	return b, nil
}

func pooledBrowser(chrome, pointer string) (*browser, error) {
	browsersMu.Lock()
	defer browsersMu.Unlock()
	if over {
		return nil, errors.New("the run is over")
	}
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
	// A Chrome that died is ended with its profile before another takes its
	// place: the profile is a few hundred megabytes nobody would remove.
	if old := pool[turn]; old != nil {
		old.close()
	}
	b, err := startBrowser(chrome, pointer)
	if err != nil {
		return nil, err
	}
	pool[turn] = b
	browsers[pointer] = pool
	return b, nil
}

const profilePrefix = "chrome-"

// profileRoot is where the Chromes of the fixtures keep their profiles: a
// directory of their own inside the temporary one, shared by every program on
// the machine, so that what a run sweeps is the fixtures' alone.
func profileRoot() string {
	return filepath.Join(os.TempDir(), "aacpanel-fixtures")
}

// sweepProfiles removes the profiles Chromes of runs before this one left in
// root: a run killed on its timeout or by a panic never reaches
// closeBrowsers, and each profile holds a few hundred megabytes. A profile a
// live process names — a run at work beside this one — stays.
func sweepProfiles(root string) {
	dirs, err := filepath.Glob(filepath.Join(root, profilePrefix+"*"))
	if err != nil {
		return
	}
	for _, dir := range dirs {
		if !profileInUse(dir) {
			_ = os.RemoveAll(dir)
		}
	}
}

// profileInUse reports whether a process of the machine runs with dir as its
// profile.
func profileInUse(dir string) bool {
	procs, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return true
	}
	needle := "--user-data-dir=" + dir + "\x00"
	for _, proc := range procs {
		raw, err := os.ReadFile(proc)
		if err == nil && strings.Contains(string(raw)+"\x00", needle) {
			return true
		}
	}
	return false
}

// closeBrowsers ends every Chrome the run started, with its profile.
func closeBrowsers() {
	browsersMu.Lock()
	defer browsersMu.Unlock()
	over = true
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
	if err := os.MkdirAll(profileRoot(), 0o700); err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp(profileRoot(), profilePrefix)
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
		waiting: map[int]chan cdpMessage{}, pages: map[string]*page{}, started: make(chan struct{}),
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
	go b.warm()
	return b, nil
}

// startBound bounds the start of a Chrome, from its process to the first page
// it has drawn. On a cold machine that takes seconds — the binary read from
// disk, the GPU process failing over to software, the first renderers — and
// none of it is the time of the fixture that happens to ask first: counted in
// a test's bound of a few seconds, it leaves the test nothing of its own.
const startBound = 60 * time.Second

// warmPage is the first page a Chrome serves: a script that runs and a frame
// that is drawn, which is all a fixture needs of it to begin.
const warmPage = `data:text/html,<p>ready</p><script>window.done = new Promise((r) => requestAnimationFrame(() => r(true)));</script>`

// warm serves the first page of the Chrome and lets the fixtures waiting for
// it go. A Chrome that serves none within startBound is dead to them, and the
// next fixture to ask starts another.
func (b *browser) warm() {
	defer close(b.started)
	ctx, cancel := context.WithTimeout(context.Background(), startBound)
	defer cancel()
	if _, err := b.run(ctx, warmPage, phoneScreen); err != nil {
		b.fail(fmt.Errorf("Chrome served no page after its start: %w", err))
	}
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
		b.received++
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
		requests, received := b.seq, b.received
		b.mu.Unlock()
		// A timeout can leave the pipe open: keep Chrome's startup errors and
		// its identity so failures on a shared browser can be traced together.
		pid := 0
		if b.cmd != nil && b.cmd.Process != nil {
			pid = b.cmd.Process.Pid
		}
		return nil, fmt.Errorf("%s: %w (Chrome pid=%d, request=%d, requests=%d, received=%d)\n%s",
			method, ctx.Err(), pid, id, requests, received, b.stderr)
	}
}

// track starts keeping what the browser tells of the tab of a session.
func (b *browser) track(session string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pages[session] = &page{news: make(chan struct{}),
		readyState: "unknown", doneType: "unknown", url: "unknown", diagnostics: tailBuffer{limit: pageDiagnosticLimit}}
}

// A timed-out context cannot ask the renderer for more evidence without
// extending the fixture's deadline, so the report reads only saved state.
func (b *browser) pageTimeout(session string, err error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.pages[session]
	if p == nil {
		return err
	}
	return fmt.Errorf("%w\nlast page state: readyState=%s, window.done=%s, url=%s\npage diagnostics (tail):\n%s",
		err, p.readyState, p.doneType, p.url, p.diagnostics.String())
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

// pageCall is call on the page of a session that ends with the page: a
// command waiting on a page that is gone comes back as soon as the browser
// says what took it away from url, and the bare "navigated or closed" the
// browser answers with is told apart into a navigation, with where to, and a
// closed tab.
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
// waits on Chrome, not on the CPU. A test that runs two fixtures asks once. A
// test whose cases run fixtures asks for itself as well: one that does not
// holds every test after it until its cases are done.
var parallelAsked sync.Map

func parallel(t *testing.T) {
	if _, again := parallelAsked.LoadOrStore(t, true); !again {
		t.Parallel()
	}
}

// disposeBound bounds the dispose of a run's browser context.
const disposeBound = 10 * time.Second

// endWait bounds how long a command cut short waits for the browser to say
// what took the page away.
const endWait = 2 * time.Second

// run opens a page in a context of its own, waits for the promise the page
// leaves in window.done and returns what it resolved to. A page taken away
// before it answers says how: the tab closed, or the page navigated, and to
// where.
func (b *browser) run(ctx context.Context, url, screen string) (json.RawMessage, error) {
	session, close, err := b.open(ctx, url, screen)
	if err != nil {
		return nil, err
	}
	defer close()
	return b.wait(ctx, session, url)
}

func (b *browser) wait(ctx context.Context, session, url string) (out json.RawMessage, err error) {
	defer func() {
		if errors.Is(err, context.DeadlineExceeded) {
			err = b.pageTimeout(session, err)
		}
	}()
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
		bound := b.disposeWait
		if bound == 0 {
			bound = disposeBound
		}
		done, cancel := context.WithTimeout(context.Background(), bound)
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
		{"Log.enable", map[string]any{}},
		{"Network.enable", map[string]any{}},
		// What the main frame commits: a page that navigates before it answers
		// says where it went.
		{"Page.enable", map[string]any{}},
		// What the page puts into window.done, told as it happens.
		{"Runtime.addBinding", map[string]any{"name": doneBinding}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": doneWatch}},
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
	if code, reran := runInNetworkNamespace(); reran {
		os.Exit(code)
	}
	sweepProfiles(profileRoot())
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

// A failed module never assigns the fixture's answer and need not throw a
// JavaScript exception. Its timeout keeps the browser's explanation, as does
// a timeout already waiting on a promise that never settles.
func TestATimedOutPageKeepsItsDiagnostics(t *testing.T) {
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine")
	}
	for _, c := range []struct{ name, answer, doneType string }{
		{"missing answer", "", "undefined"},
		{"pending answer", "window.done = new Promise(() => {});", "object"},
	} {
		t.Run(c.name, func(t *testing.T) {
			parallel(t)
			ready := make(chan struct{})
			mux := http.NewServeMux()
			mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprintf(w, `<script>
%s
console.error("discarded page error");
console.warn("x".repeat(%d));
console.error("fixture console error", 17, new Error("fixture console detail"));
console.warn("fixture console warning");
console.info("discarded console noise");
const failed = fetch("/failed-load").catch(() => {});
addEventListener("load", async () => {
    await failed;
    history.replaceState(null, "", "/waiting");
    fetch("/ready");
}, { once: true });
</script><script type="module">import "/missing-module.js"; window.done = { ok: true };</script>`, c.answer, pageDiagnosticLimit)
			})
			mux.HandleFunc("/missing-module.js", http.NotFound)
			mux.HandleFunc("/failed-load", func(w http.ResponseWriter, r *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack the failing load: %v", err)
					return
				}
				_ = conn.Close()
			})
			mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
				close(ready)
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			b, err := sharedBrowser(chrome, phonePointer)
			if err != nil {
				t.Fatalf("Chrome did not start: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			url := server.URL + "/page"
			session, close, err := b.open(ctx, url, phoneScreen)
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			select {
			case <-ready:
			case <-ctx.Done():
				t.Fatal("the page did not finish its loads")
			}
			details := []string{
				"readyState=complete", "window.done=" + c.doneType, "url=" + server.URL + "/waiting",
				"[Log error] " + server.URL + "/missing-module.js", "404",
				"[Network] " + server.URL + "/failed-load: net::ERR_EMPTY_RESPONSE",
				"[console error] fixture console error 17 Error: fixture console detail",
				"[console warning] fixture console warning",
			}
			// The events of the page come to the harness on a way of their
			// own, and /ready at the server says nothing of when: the window
			// of the timeout opens once the harness holds all of them, so what
			// is checked is that the timeout keeps what the browser told.
			for told := ""; ; time.Sleep(10 * time.Millisecond) {
				told = b.pageTimeout(session, errors.New("")).Error()
				missing := slices.IndexFunc(details, func(d string) bool { return !strings.Contains(told, d) })
				if missing < 0 {
					break
				}
				if ctx.Err() != nil {
					t.Fatalf("the page never told %q: %s", details[missing], told)
				}
			}
			within, stop := context.WithTimeout(ctx, 250*time.Millisecond)
			defer stop()
			_, err = b.wait(within, session, url)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("a page without an answer returned %v", err)
			}
			said := err.Error()
			for _, detail := range details {
				if !strings.Contains(said, detail) {
					t.Errorf("the timeout lost %q: %s", detail, said)
				}
			}
			for _, discarded := range []string{"discarded page error", "discarded console noise"} {
				if strings.Contains(said, discarded) {
					t.Errorf("the timeout kept %q", discarded)
				}
			}
			tail := strings.SplitN(said, "page diagnostics (tail):\n", 2)
			if len(tail) != 2 || len(tail[1]) > pageDiagnosticLimit {
				t.Errorf("the page diagnostics have no bounded tail: %s", said)
			}
		})
	}
}

// A Chrome that stops answering in the middle of a run holds nothing past the
// run's own bound: the fake answers the browser context and then keeps quiet,
// and the run comes back with an error instead of waiting on the dispose.
func TestARunOnAChromeThatStoppedAnsweringComesBack(t *testing.T) {
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
		waiting: map[int]chan cdpMessage{}, pages: map[string]*page{}, disposeWait: 100 * time.Millisecond}
	_, _ = b.stderr.Write([]byte("Chrome startup diagnostic"))
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
			t.Fatal("a Chrome that answered nothing but the context gave a result")
		}
		for _, detail := range []string{"Target.createTarget", "Chrome pid=0, request=2, requests=2, received=1", "Chrome startup diagnostic"} {
			if !strings.Contains(err.Error(), detail) {
				t.Errorf("the stalled command lost %q: %v", detail, err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run on a Chrome that stopped answering never came back: the dispose waits on it unbounded")
	}
}

// A page that navigates before it answers fails with where it went, not with
// the bare "navigated or closed" the browser answers: that names neither the
// page nor what became of it.
func TestAPageThatNavigatesAwaySaysWhere(t *testing.T) {
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine")
	}
	parallel(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<script>window.done = new Promise(() => {});
setTimeout(() => { location.href = "/elsewhere"; }, 100);</script>`)
	})
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	b, err := sharedBrowser(chrome, phonePointer)
	if err != nil {
		t.Fatalf("Chrome did not start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = b.run(ctx, server.URL+"/page", phoneScreen)
	if err == nil || !strings.Contains(err.Error(), "navigated to "+server.URL+"/elsewhere") {
		t.Fatalf("a page that navigated away failed as %v, not with where it went", err)
	}
}
