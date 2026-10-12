package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/browserlink"
	"aacpanel/internal/cdp"
)

type shot struct {
	data       []byte
	generation uint64
	size       browserlink.Size
}

type tabSession struct {
	id    string
	ready bool
}

type chrome struct {
	pipe       *cdp.Pipe
	cmd        *exec.Cmd
	ended      chan error
	cancel     context.CancelFunc
	mu         sync.Mutex
	castMu     sync.Mutex
	targets    map[string]string
	tabs       map[string]tabSession
	version    string
	language   string
	current    string
	size       browserlink.Size
	casting    bool
	generation uint64
	latest     *shot
	changed    chan struct{}
	events     chan cdp.Message
	failure    chan error
	refused    chan string
	proxy      *Egress
	server     *http.Server
}

// Each process gets an owned home and cache as well as its profile. The host
// desktop environment is not inherited, so Chrome cannot find its keyring or
// desktop profile when the configured program is a wrapper.
func startChrome(ctx context.Context, c Config, id string) (*chrome, error) {
	dir, err := DataDir(c, id)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	b := &chrome{
		targets:  make(map[string]string),
		tabs:     make(map[string]tabSession),
		language: hostLanguage(),
		changed:  make(chan struct{}, 1),
		events:   make(chan cdp.Message, 128),
		failure:  make(chan error, 1),
		refused:  make(chan string, 4),
		ended:    make(chan error, 1),
		cancel:   cancel,
		proxy:    NewEgress(),
	}
	b.server = &http.Server{
		Handler:           b.proxy,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		if err := b.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			b.fail(err)
		}
	}()
	args := []string{
		"--headless=new",
		"--remote-debugging-pipe",
		// Headless enables webdriver even without --enable-automation. This
		// browser is driven by the person viewing it, including during sign-in.
		"--disable-blink-features=AutomationControlled",
		"--user-data-dir=" + filepath.Join(dir, "data"),
		"--password-store=basic",
		"--no-first-run",
		"--no-default-browser-check",
		"--use-angle=vulkan",
		"--enable-features=Vulkan",
		"--force-device-scale-factor=" + strconv.Itoa(browserlink.RenderDPR),
		"--proxy-server=http://" + ln.Addr().String(),
		"--proxy-bypass-list=<-loopback>",
		"--disable-quic",
		"--webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--disable-extensions",
		"--disable-background-networking",
		"about:blank",
	}
	b.cmd = exec.Command(c.Program, args...)
	b.cmd.Dir = dir
	b.cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(dir, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(dir, "home"),
		"XDG_CACHE_HOME=" + filepath.Join(dir, "cache"),
		"TMPDIR=tmp",
		"CHROME_PASSWORD_STORE=basic",
		"LANG=C.UTF-8",
	}
	// Browser diagnostics can include page addresses. They must not enter the
	// host action journal alongside passwords and signed URLs.
	b.pipe, err = cdp.Start(b.cmd, func(m cdp.Message) {
		// Frames bypass the event queue: only the most recent JPEG is kept.
		if m.Method == "Page.screencastFrame" {
			b.frame(m)
			return
		}
		select {
		case b.events <- m:
		default:
			b.fail(errors.New("the browser event reader fell behind"))
		}
	})
	if err != nil {
		b.server.Close()
		b.proxy.Close()
		cancel()
		return nil, err
	}
	go func() { b.ended <- b.cmd.Wait(); close(b.ended) }()
	var version struct{ Product string }
	if err = b.call(ctx, "", "Browser.getVersion", struct{}{}, &version); err == nil {
		_, b.version, _ = strings.Cut(version.Product, "/")
		if b.version == "" {
			err = errors.New("the browser did not report its version")
		}
	}
	if err != nil {
		b.close()
		return nil, err
	}
	go b.readEvents(ctx)
	if err = b.call(
		ctx,
		"",
		"Browser.setDownloadBehavior",
		map[string]interface{}{"behavior": "deny", "eventsEnabled": true},
		nil,
	); err == nil {
		err = b.call(
			ctx,
			"",
			"Target.setAutoAttach",
			map[string]interface{}{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true},
			nil,
		)
	}
	if err != nil {
		b.close()
		return nil, err
	}
	return b, nil
}

func (b *chrome) call(ctx context.Context, sid, method string, params interface{}, out interface{}) error {
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return b.pipe.Call(c, sid, method, params, out)
}

func (b *chrome) fail(err error) {
	select {
	case b.failure <- err:
	default:
	}
	b.cancel()
}

func (b *chrome) signal() {
	select {
	case b.changed <- struct{}{}:
	default:
	}
}

func (b *chrome) readEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.pipe.Done():
			b.fail(b.pipe.Err())
			return
		case m := <-b.events:
			switch m.Method {
			case "Target.attachedToTarget":
				var ev struct {
					SessionID  string `json:"sessionId"`
					TargetInfo struct {
						TargetID string `json:"targetId"`
						Type     string `json:"type"`
					} `json:"targetInfo"`
				}
				if json.Unmarshal(m.Params, &ev) != nil {
					b.fail(errors.New("invalid browser attachment"))
					return
				}
				go b.guardTarget(ctx, ev.SessionID, ev.TargetInfo.TargetID, ev.TargetInfo.Type)
			case "Target.detachedFromTarget":
				var ev struct {
					SessionID string `json:"sessionId"`
				}
				json.Unmarshal(m.Params, &ev)
				b.detached(ctx, ev.SessionID)
			case "Fetch.requestPaused":
				var ev struct {
					RequestID string `json:"requestId"`
					Request   struct {
						URL string `json:"url"`
					} `json:"request"`
				}
				if json.Unmarshal(m.Params, &ev) != nil {
					b.fail(errors.New("invalid browser request"))
					return
				}
				method := "Fetch.continueRequest"
				params := map[string]interface{}{"requestId": ev.RequestID}
				if strings.HasPrefix(strings.ToLower(ev.Request.URL), "file:") {
					method = "Fetch.failRequest"
					params["errorReason"] = "BlockedByClient"
				}
				if err := b.pipe.Send(m.SessionID, method, params); err != nil {
					b.fail(err)
				}
			case "Page.frameNavigated", "Page.navigatedWithinDocument", "Target.targetInfoChanged":
				b.signal()
			case "Page.fileChooserOpened":
				select {
				case b.refused <- "File uploads are unavailable":
				default:
				}
			case "Browser.downloadProgress":
				var ev struct {
					State string `json:"state"`
				}
				if json.Unmarshal(m.Params, &ev) == nil && ev.State == "canceled" {
					select {
					case b.refused <- "Downloads are unavailable":
					default:
					}
				}
			case "Page.javascriptDialogOpening":
				b.pipe.Send(m.SessionID, "Page.handleJavaScriptDialog", map[string]bool{"accept": false})
			}
		}
	}
}

// A page can close itself without an input request. Select a surviving tab
// before publishing its state, otherwise every following input names a dead
// CDP session. A hidden viewer must not restart the screencast by closing a tab.
func (b *chrome) detached(ctx context.Context, sid string) {
	b.castMu.Lock()
	defer b.castMu.Unlock()
	delete(b.targets, sid)
	b.mu.Lock()
	selected := false
	for id, tab := range b.tabs {
		if tab.id == sid {
			selected = id == b.current
			delete(b.tabs, id)
			break
		}
	}
	if selected {
		b.current = ""
		b.latest = nil
		b.generation++
		for id, tab := range b.tabs {
			if tab.ready {
				b.current = id
				break
			}
		}
	}
	resume := selected && b.current != "" && b.casting
	size := b.size
	b.mu.Unlock()
	if resume {
		if err := b.castLocked(ctx, size); err != nil {
			b.fail(err)
		}
	}
	b.signal()
}

func (b *chrome) frame(m cdp.Message) {
	var f struct {
		Data      string `json:"data"`
		SessionID int    `json:"sessionId"`
	}
	if json.Unmarshal(m.Params, &f) != nil {
		b.fail(errors.New("invalid browser frame"))
		return
	}
	if err := b.pipe.Send(m.SessionID, "Page.screencastFrameAck", map[string]int{"sessionId": f.SessionID}); err != nil {
		b.fail(err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil || len(data) > (browserlink.MaxFrame-4096)*3/4 {
		b.fail(errors.New("the browser image exceeds its limit"))
		return
	}
	b.mu.Lock()
	if t := b.tabs[b.current]; t.id == m.SessionID {
		b.latest = &shot{data: data, generation: b.generation, size: b.size}
	}
	b.mu.Unlock()
}

// New pages and out-of-process frames stay paused until the file guards and
// viewer identity are installed. A popup's first request can choose a mobile
// site or a sign-in flow before the holder sees its navigation.
func (b *chrome) guardTarget(ctx context.Context, sid, id, kind string) {
	if kind != "page" && kind != "iframe" {
		if err := b.call(ctx, sid, "Runtime.runIfWaitingForDebugger", struct{}{}, nil); err != nil {
			b.fail(err)
		}
		return
	}
	for _, step := range []struct {
		method string
		params interface{}
	}{
		{"Page.enable", struct{}{}},
		{
			"Fetch.enable",
			map[string]interface{}{"patterns": []map[string]string{{"urlPattern": "file:*", "requestStage": "Request"}}},
		},
		{"Page.setInterceptFileChooserDialog", map[string]bool{"enabled": true}},
		{
			"Target.setAutoAttach",
			map[string]bool{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true},
		},
	} {
		if err := b.call(ctx, sid, step.method, step.params, nil); err != nil {
			b.fail(fmt.Errorf("browser guard: %w", err))
			return
		}
	}
	// Resizing and attachment share the capture lock so a target cannot miss
	// a viewer change between reading its size and joining the target set.
	b.castMu.Lock()
	defer b.castMu.Unlock()
	b.mu.Lock()
	size := b.size
	b.mu.Unlock()
	if err := b.emulate(ctx, sid, kind, size); err != nil {
		b.fail(err)
		return
	}
	if err := b.call(ctx, sid, "Runtime.runIfWaitingForDebugger", struct{}{}, nil); err != nil {
		b.fail(err)
		return
	}
	b.targets[sid] = kind
	if kind == "page" {
		b.mu.Lock()
		b.tabs[id] = tabSession{id: sid, ready: true}
		if b.current == "" {
			b.current = id
		}
		b.mu.Unlock()
		b.signal()
	}
}

func (b *chrome) session() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tabs[b.current].id
}

func (b *chrome) ready(ctx context.Context, id string) error {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	bound, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		b.mu.Lock()
		ok := b.tabs[id].ready
		if id == "" {
			ok = b.tabs[b.current].ready
		}
		b.mu.Unlock()
		if ok {
			return nil
		}
		select {
		case <-bound.Done():
			return errors.New("the browser tab did not become ready")
		case err := <-b.failure:
			return err
		case <-tick.C:
		}
	}
}

// Resizing invalidates coordinates of the previous image. Desktop capture
// is downsampled to CSS pixels; a phone may request the bounded render scale.
func (b *chrome) cast(ctx context.Context, size browserlink.Size) error {
	// Attachment needs castMu to install emulation before publishing readiness.
	if err := b.ready(ctx, ""); err != nil {
		return err
	}
	b.castMu.Lock()
	defer b.castMu.Unlock()
	return b.castLocked(ctx, size)
}

// A popup closing and a viewer selecting or resizing a tab can arrive at
// once. Their stop/select/start sequence is one operation: parallel CDP
// commands otherwise leave the previous tab casting or start one twice.
func (b *chrome) selectTab(ctx context.Context, id string, size browserlink.Size) error {
	b.castMu.Lock()
	defer b.castMu.Unlock()
	b.mu.Lock()
	ready := b.tabs[id].ready
	b.mu.Unlock()
	if !ready {
		return errors.New("the browser tab is gone")
	}
	b.stopCastLocked(ctx)
	b.mu.Lock()
	b.current = id
	b.mu.Unlock()
	return b.castLocked(ctx, size)
}

func (b *chrome) castLocked(ctx context.Context, size browserlink.Size) error {
	if err := size.Check(); err != nil {
		return err
	}
	sid := b.session()
	if sid == "" {
		return errors.New("the browser has no selected tab")
	}
	b.call(ctx, sid, "Page.stopScreencast", struct{}{}, nil)
	b.mu.Lock()
	changed := b.size != size
	b.mu.Unlock()
	if changed {
		for target, kind := range b.targets {
			if err := b.emulate(ctx, target, kind, size); err != nil {
				return err
			}
		}
	}
	b.mu.Lock()
	b.size = size
	b.generation++
	b.latest = nil
	b.mu.Unlock()
	for _, step := range []struct {
		method string
		params interface{}
	}{
		{"Page.bringToFront", struct{}{}},
		{
			"Page.startScreencast",
			map[string]interface{}{
				"format":        "jpeg",
				"quality":       browserlink.JPEGQuality,
				"maxWidth":      int(float64(size.Width) * size.DPR),
				"maxHeight":     int(float64(size.Height) * size.DPR),
				"everyNthFrame": 1,
			},
		},
	} {
		if err := b.call(ctx, sid, step.method, step.params, nil); err != nil {
			return err
		}
	}
	b.mu.Lock()
	b.casting = true
	b.mu.Unlock()
	return nil
}

func (b *chrome) stopCast(ctx context.Context) {
	b.castMu.Lock()
	defer b.castMu.Unlock()
	b.stopCastLocked(ctx)
}

func (b *chrome) stopCastLocked(ctx context.Context) {
	if sid := b.session(); sid != "" {
		b.call(ctx, sid, "Page.stopScreencast", struct{}{}, nil)
	}
	b.mu.Lock()
	b.casting = false
	b.latest = nil
	b.mu.Unlock()
}

func (b *chrome) take() *shot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.latest
	b.latest = nil
	return s
}

func (b *chrome) state(ctx context.Context) (browserlink.State, error) {
	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Title    string `json:"title"`
			URL      string `json:"url"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if err := b.call(ctx, "", "Target.getTargets", struct{}{}, &targets); err != nil {
		return browserlink.State{}, err
	}
	b.mu.Lock()
	st := browserlink.State{Tabs: []browserlink.Tab{}, Current: b.current}
	for _, t := range targets.TargetInfos {
		if t.Type == "page" && b.tabs[t.TargetID].ready {
			st.Tabs = append(st.Tabs, browserlink.Tab{ID: t.TargetID, Title: t.Title, URL: t.URL})
		}
	}
	b.mu.Unlock()
	var history struct {
		CurrentIndex int               `json:"currentIndex"`
		Entries      []json.RawMessage `json:"entries"`
	}
	if b.session() != "" {
		if err := b.call(ctx, b.session(), "Page.getNavigationHistory", struct{}{}, &history); err == nil {
			st.Back = history.CurrentIndex > 0
			st.Forward = history.CurrentIndex+1 < len(history.Entries)
		}
	}
	return st, nil
}

func (b *chrome) close() {
	b.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if b.pipe != nil {
		b.pipe.Call(ctx, "", "Browser.close", struct{}{}, nil)
		b.pipe.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		select {
		case <-b.ended:
		case <-time.After(2 * time.Second):
			b.cmd.Process.Kill()
			<-b.ended
		}
	}
	b.server.Close()
	b.proxy.Close()
}
