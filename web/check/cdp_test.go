package check

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

	mu      sync.Mutex
	seq     int
	waiting map[int]chan cdpMessage
	thrown  map[string][]string
	dead    error
	stderr  *tailBuffer
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
		waiting: map[int]chan cdpMessage{}, thrown: map[string][]string{},
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
		if msg.ID != 0 {
			if ch := b.waiting[msg.ID]; ch != nil {
				delete(b.waiting, msg.ID)
				ch <- msg
			}
		} else if msg.Method == "Runtime.exceptionThrown" && msg.SessionID != "" {
			var ev struct {
				ExceptionDetails struct {
					Text      string `json:"text"`
					Exception struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			_ = json.Unmarshal(msg.Params, &ev)
			b.thrown[msg.SessionID] = append(b.thrown[msg.SessionID],
				strings.TrimSpace(ev.ExceptionDetails.Text+" "+ev.ExceptionDetails.Exception.Description))
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

// thrownIn returns what the page of a session threw so far.
func (b *browser) thrownIn(session string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.thrown[session]...)
}

func (b *browser) forget(session string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.thrown, session)
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

// run opens a page in a context of its own, waits for the promise the page
// leaves in window.done and returns what it resolved to.
func (b *browser) run(ctx context.Context, url, screen string) (json.RawMessage, error) {
	var created struct {
		BrowserContextID string `json:"browserContextId"`
	}
	raw, err := b.call(ctx, "", "Target.createBrowserContext", map[string]any{})
	if err != nil || json.Unmarshal(raw, &created) != nil {
		return nil, fmt.Errorf("no browser context: %v", err)
	}
	defer b.call(context.Background(), "", "Target.disposeBrowserContext", map[string]any{"browserContextId": created.BrowserContextID})

	var target struct {
		TargetID string `json:"targetId"`
	}
	raw, err = b.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": created.BrowserContextID})
	if err != nil || json.Unmarshal(raw, &target) != nil {
		return nil, fmt.Errorf("no tab: %v", err)
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	raw, err = b.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true})
	if err != nil || json.Unmarshal(raw, &attached) != nil {
		return nil, fmt.Errorf("the tab did not attach: %v", err)
	}
	session := attached.SessionID
	defer b.forget(session)

	for _, step := range []struct {
		method string
		params any
	}{
		{"Runtime.enable", map[string]any{}},
		// Every tab of the browser is in front: a page in the background has
		// its timers slowed and its animation frames stopped.
		{"Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}},
		{"Emulation.setDeviceMetricsOverride", json.RawMessage(screen)},
		{"Page.navigate", map[string]any{"url": url}},
	} {
		if _, err := b.call(ctx, session, step.method, step.params); err != nil {
			return nil, err
		}
	}
	for {
		raw, err := b.call(ctx, session, "Runtime.evaluate", map[string]any{
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
