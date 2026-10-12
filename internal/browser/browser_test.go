package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aacpanel/internal/browserlink"
)

func TestEgressRejectsNonPublicAndHostAddresses(t *testing.T) {
	own := []net.Addr{&net.IPNet{IP: net.ParseIP("8.8.4.4"), Mask: net.CIDRMask(24, 32)}}
	for _, raw := range []string{
		"127.0.0.1",
		"10.2.3.4",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.2.1",
		"100.64.0.1",
		"0.0.0.0",
		"224.0.0.1",
		"255.255.255.255",
		"::",
		"::1",
		"::ffff:127.0.0.1",
		"fc00::1",
		"fe80::1",
		"ff02::1",
		"8.8.4.4",
		"64:ff9b::7f00:1",
	} {
		if publicAddress(netip.MustParseAddr(raw), own) {
			t.Errorf("allowed nonpublic/host address %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(raw), own) {
			t.Errorf("refused public address %s", raw)
		}
	}
}

func TestEgressDialsOnlyTheCheckedResolution(t *testing.T) {
	e := NewEgress()
	defer e.Close()
	e.interfaces = func() ([]net.Addr, error) { return nil, nil }
	calls := 0
	e.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		calls++
		if calls > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	var dialed string
	e.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	c, err := e.connect(context.Background(), "tcp", "example.org:443")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if calls != 1 || dialed != "1.1.1.1:443" {
		t.Fatalf("resolution was not pinned: calls=%d dial=%s", calls, dialed)
	}
	e.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	dialed = ""
	if c, err = e.connect(context.Background(), "tcp", "mixed.example:80"); err == nil {
		c.Close()
		t.Fatal("a mixed public/private DNS answer was allowed")
	}
	if dialed != "" {
		t.Fatal("a refused address was dialed")
	}
}

func TestBrowserOffRefusesProfileChanges(t *testing.T) {
	c := Config{Program: "/bin/true", Root: filepath.Join(t.TempDir(), "profiles")}
	if err := Create(c, "b-0123456789abcdef", "Work"); err == nil {
		t.Fatal("the disabled browser created a profile")
	}
	if _, err := os.Stat(c.Root); !os.IsNotExist(err) {
		t.Fatal("the disabled browser wrote its state directory")
	}
	if err := Delete(c, "b-0123456789abcdef"); err == nil {
		t.Fatal("the disabled browser accepted deletion")
	}
}

func TestProfilePathsDoNotFollowNamesOrLinks(t *testing.T) {
	c := Config{Enabled: true, Program: "/bin/true", Root: filepath.Join(t.TempDir(), "profiles")}
	id := "b-0123456789abcdef"
	if err := Create(c, id, "Work / Personal"); err != nil {
		t.Fatal(err)
	}
	if err := Create(c, "../escape", "Work"); err == nil {
		t.Fatal("traversal was accepted")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(c.Root, id, "data")); err != nil {
		t.Fatal(err)
	}
	if _, err := DataDir(c, id); err == nil {
		t.Fatal("a linked profile data directory was accepted")
	}
	if err := Delete(c, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("deleting the profile followed its link")
	}
}

func testChrome(t *testing.T) (*chrome, context.Context) {
	t.Helper()
	program := os.Getenv("AACP_CHROME")
	if program == "" {
		for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(name); err == nil {
				program = path
				break
			}
		}
	}
	if program == "" {
		t.Skip("Chrome is not installed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Enabled: true, Program: program, Root: root}
	id := "b-0123456789abcdef"
	if err := Create(c, id, "Test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	b, err := startChrome(ctx, c, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.close)
	if err = b.ready(ctx, ""); err != nil {
		t.Fatal(err)
	}
	return b, ctx
}

func TestChromeInputFramesAndExposureGuards(t *testing.T) {
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	b, ctx := testChrome(t)
	sid := b.session()
	call := func(method string, params interface{}, out interface{}) {
		t.Helper()
		if err := b.call(ctx, sid, method, params, out); err != nil {
			t.Fatal(err)
		}
	}
	eval := func(expression string) string {
		t.Helper()
		var r struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		call("Runtime.evaluate", map[string]interface{}{"expression": expression, "returnByValue": true}, &r)
		return r.Result.Value
	}
	// STUN is outside HTTP. The browser must honor the Chrome preference
	// switch even when a page asks for a local UDP server directly.
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	var rtc struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	gather := fmt.Sprintf(`(async () => {
		const pc = new RTCPeerConnection({iceServers:[{urls:"stun:%s"}]});
		pc.createDataChannel("probe");
		const candidates = [];
		let finish;
		const done = new Promise(resolve => { finish = resolve; });
		pc.onicecandidate = e => {
			if (e.candidate) candidates.push(e.candidate.candidate);
			finish();
		};
		await pc.setLocalDescription(await pc.createOffer());
		await done;
		const result = JSON.stringify({state:pc.iceGatheringState,candidates});
		pc.close();
		return result;
	})()`, udp.LocalAddr())
	call("Runtime.evaluate", map[string]interface{}{
		"expression": gather, "awaitPromise": true, "returnByValue": true,
	}, &rtc)
	var gathered struct {
		State      string
		Candidates []string
	}
	if err := json.Unmarshal([]byte(rtc.Result.Value), &gathered); err != nil {
		t.Fatalf("ICE result: %s (%v)", rtc.Result.Value, err)
	}
	udp.SetReadDeadline(time.Now().Add(25 * time.Millisecond))
	if _, _, err := udp.ReadFrom(make([]byte, 2048)); err == nil {
		t.Error("WebRTC sent UDP directly to a host service")
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal(err)
	}
	if gathered.State != "complete" || len(gathered.Candidates) != 0 {
		t.Fatalf("WebRTC exposed unproxied candidates: %+v", gathered)
	}
	call("Page.setDocumentContent", map[string]string{"frameId": func() string {
		var tree struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		call("Page.getFrameTree", struct{}{}, &tree)
		return tree.FrameTree.Frame.ID
	}(), "html": `
<meta name="viewport" content="width=device-width"><button style="height:60px;width:150px" onclick="this.textContent='Tapped'">Tap</button>
<input style="height:50px" id="entry">
<input id="file" type="file">
<input id="slider" type="range" value="0" style="position:absolute;left:10px;top:160px;width:300px;height:40px">
<div style="height:20000px;background:linear-gradient(white,blue)"></div>`}, nil)
	if err := b.cast(ctx, browserlink.Size{Width: 390, Height: 844, DPR: 2, Mobile: true}); err != nil {
		t.Fatal(err)
	}
	input := func(i browserlink.Input) {
		t.Helper()
		b.mu.Lock()
		i.Generation = b.generation
		b.mu.Unlock()
		if err := b.input(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	input(browserlink.Input{Kind: "touch", Phase: "start", X: 40, Y: 30})
	input(browserlink.Input{Kind: "touch", Phase: "end", X: 40, Y: 30})
	if got := eval("document.querySelector('button').textContent"); got != "Tapped" {
		t.Fatalf("tap: %q", got)
	}
	eval("document.querySelector('#entry').focus();''")
	input(browserlink.Input{Kind: "text", Text: "Phone 🦊"})
	input(browserlink.Input{Kind: "key", Key: "Backspace"})
	if got := eval("document.querySelector('#entry').value"); got != "Phone " {
		t.Fatalf("text/backspace: %q", got)
	}
	eval("window.motion=[];document.addEventListener('mousemove',e=>motion.push(e.buttons));''")
	input(browserlink.Input{Kind: "mouse", Phase: "move", Button: "none", X: 10, Y: 10})
	input(browserlink.Input{Kind: "mouse", Phase: "down", Button: "left", X: 10, Y: 10})
	input(browserlink.Input{Kind: "mouse", Phase: "move", Button: "left", X: 20, Y: 20})
	input(browserlink.Input{Kind: "mouse", Phase: "up", Button: "left", X: 20, Y: 20})
	if got := eval("JSON.stringify(motion)"); got != "[0,1]" {
		t.Fatalf("hover and drag buttons: %s", got)
	}
	// Native touch dispatch lets the page decide whether a finger drags a
	// control or scrolls. Converting every move into wheel loses that choice.
	input(browserlink.Input{Kind: "touch", Phase: "start", X: 25, Y: 180})
	for x := 45; x <= 245; x += 40 {
		input(browserlink.Input{Kind: "touch", Phase: "move", X: float64(x), Y: 180})
	}
	input(browserlink.Input{Kind: "touch", Phase: "end", X: 245, Y: 180})
	if got := eval("String(document.querySelector('#slider').valueAsNumber > 60)"); got != "true" {
		t.Fatalf("touch drag did not move the range slider: %s", got)
	}
	input(browserlink.Input{Kind: "touch", Phase: "start", X: 200, Y: 650})
	for y := 610; y >= 370; y -= 40 {
		input(browserlink.Input{Kind: "touch", Phase: "move", X: 200, Y: float64(y)})
	}
	input(browserlink.Input{Kind: "touch", Phase: "end", X: 200, Y: 370})
	swipeEnd := time.Now().Add(time.Second)
	for eval("String(scrollY > 100)") != "true" && time.Now().Before(swipeEnd) {
		time.Sleep(10 * time.Millisecond)
	}
	if eval("String(scrollY > 100)") != "true" {
		t.Fatal("a touch swipe did not scroll the page")
	}
	eval("scrollTo(0,0);''")
	input(browserlink.Input{Kind: "wheel", X: 200, Y: 500, DY: 300})
	end := time.Now().Add(3 * time.Second)
	var image *shot
	for time.Now().Before(end) {
		image = b.take()
		if image != nil && eval("String(scrollY > 100)") == "true" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if image == nil || len(image.data) < 100 {
		t.Fatal("the screencast produced no JPEG")
	}
	if eval("String(scrollY)") == "0" {
		t.Fatal("mouseWheel did not scroll")
	}
	// A slow viewer must receive the final static image, even after Chrome
	// has stopped producing frames. Its ACK is separate from Chrome's ACK.
	a, peer := net.Pipe()
	link := browserlink.New(peer)
	defer link.Close()
	viewCtx, stopView := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() {
		stopped <- serveViewer(
			viewCtx,
			b,
			browserlink.New(a),
			browserlink.Size{Width: 390, Height: 844, DPR: 2, Mobile: true},
		)
	}()
	nextFrame := func() browserlink.Frame {
		t.Helper()
		link.ReadDeadline(time.Now().Add(3 * time.Second))
		for {
			f, err := link.Read()
			if err != nil {
				t.Fatal(err)
			}
			if f.Type == "frame" {
				return f
			}
		}
	}
	first := nextFrame()
	eval("document.body.style.background='#20c040';document.body.replaceChildren();''")
	green := func(data []byte) bool {
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return false
		}
		r, g, bl, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
		return g > r*2 && g > bl*2
	}
	until := time.Now().Add(2 * time.Second)
	latest := false
	for time.Now().Before(until) {
		b.mu.Lock()
		latest = b.latest != nil && green(b.latest.data)
		b.mu.Unlock()
		if latest {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !latest {
		t.Fatal("the latest slot never received the final frame")
	}
	if err := link.Write(browserlink.Frame{Type: "ack", Sequence: first.Sequence}); err != nil {
		t.Fatal(err)
	}
	last := nextFrame()
	if last.Sequence != first.Sequence+1 || !green(last.Data) {
		t.Fatal("the stalled viewer got a stale frame")
	}
	stopView()
	link.Close()
	<-stopped
	// Closing the last tab leaves a usable blank tab, then switching still works.
	st, err := b.state(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input(browserlink.Input{Kind: "close-tab", Tab: st.Current})
	input(browserlink.Input{Kind: "new-tab"})
	st, err = b.state(ctx)
	if err != nil || len(st.Tabs) != 2 {
		t.Fatalf("tabs after close/new: %+v %v", st, err)
	}
	sid = b.session()
	parent := st.Current
	originalTabs := st.Tabs
	call("Runtime.evaluate", map[string]interface{}{"expression": "window.popup=window.open('about:blank')", "userGesture": true}, nil)
	var popup string
	popupEnd := time.Now().Add(time.Second)
	for time.Now().Before(popupEnd) {
		st, err = b.state(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, tab := range st.Tabs {
			known := false
			for _, old := range originalTabs {
				if old.ID == tab.ID {
					known = true
				}
			}
			if !known {
				popup = tab.ID
			}
		}
		if popup != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if popup == "" {
		t.Fatal("the script did not open a popup")
	}
	input(browserlink.Input{Kind: "select-tab", Tab: popup})
	sid = b.session()
	b.mu.Lock()
	popupGeneration := b.generation
	b.mu.Unlock()
	eval("window.close();''")
	for time.Now().Before(popupEnd) {
		st, err = b.state(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Current != popup && len(st.Tabs) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st.Current == popup || st.Current == "" {
		t.Fatal("a self-closed popup stayed selected")
	}
	// A manual selection would restart capture and hide a broken automatic resume.
	sid = b.session()
	eval("document.body.style.background='#20c040';document.body.replaceChildren();''")
	resumed := false
	resumeEnd := time.Now().Add(2 * time.Second)
	for time.Now().Before(resumeEnd) {
		frame := b.take()
		if frame != nil && frame.generation > popupGeneration && green(frame.data) {
			resumed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !resumed {
		t.Fatal("the remaining tab did not produce a new frame after the popup closed")
	}
	input(browserlink.Input{Kind: "select-tab", Tab: parent})
	sid = b.session()
	eval("document.body.innerHTML='<input id=\"file\" type=\"file\">';''")
	// Calling the file picker under interception must leave selection empty.
	input(browserlink.Input{Kind: "touch", Phase: "start", X: 20, Y: 18})
	input(browserlink.Input{Kind: "touch", Phase: "end", X: 20, Y: 18})
	select {
	case message := <-b.refused:
		if !strings.Contains(message, "uploads") {
			t.Fatal(message)
		}
	case <-time.After(time.Second):
		t.Fatal("the file chooser was not intercepted")
	}
	if eval("String(document.querySelector('#file').files.length)") != "0" {
		t.Fatal("a file reached the page")
	}
	file := filepath.Join(t.TempDir(), "private.html")
	file, _ = filepath.Abs(file)
	if err := os.WriteFile(file, []byte("<title>HOST SECRET</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	var nav struct {
		ErrorText string `json:"errorText"`
	}
	call("Page.navigate", map[string]string{"url": "file://" + file}, &nav)
	if !strings.Contains(nav.ErrorText, "BLOCKED_BY_CLIENT") {
		t.Fatalf("file navigation was not blocked: %+v", nav)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, "HOST SECRET")
	}))
	defer server.Close()
	call("Page.navigate", map[string]string{"url": server.URL}, &nav)
	if requests.Load() != 0 {
		t.Fatal("Chrome reached a loopback service through its proxy")
	}
	if eval("document.body.textContent") == "HOST SECRET" {
		t.Fatal("the local response reached Chrome")
	}
	call("Page.navigate", map[string]string{"url": "data:application/octet-stream;base64,dGVzdA=="}, nil)
	select {
	case message := <-b.refused:
		if !strings.Contains(message, "Downloads") {
			t.Fatal(message)
		}
	case <-time.After(time.Second):
		t.Fatal("the download was not canceled")
	}
	t.Run("ViewerIdentity", func(t *testing.T) { checkViewerIdentity(t, b, ctx) })
	call("Network.setCookies", map[string]interface{}{
		"cookies": []map[string]interface{}{
			{
				"name":    "panel_test",
				"value":   "kept",
				"url":     "https://example.com/",
				"expires": time.Now().Add(time.Hour).Unix(),
			},
		},
	}, nil)
	config := Config{Enabled: true, Program: b.cmd.Path, Root: filepath.Dir(b.cmd.Dir)}
	b.close()
	reopened, err := startChrome(ctx, config, filepath.Base(b.cmd.Dir))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	if err = reopened.ready(ctx, ""); err != nil {
		t.Fatal(err)
	}
	var cookies struct {
		Cookies []struct{ Name, Value string }
	}
	if err = reopened.call(
		ctx,
		reopened.session(),
		"Network.getCookies",
		map[string]interface{}{"urls": []string{"https://example.com/"}},
		&cookies,
	); err != nil {
		t.Fatal(err)
	}
	if len(cookies.Cookies) != 1 || cookies.Cookies[0].Value != "kept" {
		t.Fatalf("profile cookies did not survive reopening: %+v", cookies)
	}

}

func TestHolderHasOneControllerAndClosesAfterTheLastLeaves(t *testing.T) {
	program := os.Getenv("AACP_CHROME")
	if program == "" {
		var err error
		program, err = exec.LookPath("google-chrome")
		if err != nil {
			t.Skip("Chrome is not installed")
		}
	}
	runtime, err := os.MkdirTemp(os.TempDir(), "bh-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtime)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Enabled: true, Program: program, Root: root, Runtime: runtime}
	id := "b-0123456789abcdef"
	if err = Create(c, id, "Only one"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- hold(ctx, c, id, 50*time.Millisecond) }()
	var first *browserlink.Link
	until := time.NewTicker(10 * time.Millisecond)
	defer until.Stop()
	for first == nil {
		select {
		case err := <-ended:
			t.Fatalf("holder before attach: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-until.C:
			first, err = browserlink.Attach(ctx, Socket(c, id), id, browserlink.Size{Width: 390, Height: 844, DPR: 2, Mobile: true})
		}
	}
	defer first.Close()
	second, err := browserlink.Attach(ctx, Socket(c, id), id, browserlink.Size{Width: 390, Height: 844, DPR: 2, Mobile: true})
	if err == nil {
		second.Close()
		t.Fatal("two controllers were accepted")
	}
	if !strings.Contains(err.Error(), "controller") {
		t.Fatalf("second controller: %v", err)
	}
	first.Close()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("holder outlived its idle grace")
	}
	if _, err = os.Stat(Socket(c, id)); !os.IsNotExist(err) {
		t.Fatal("private socket outlived its holder")
	}
}

func TestBrowserStorageCannotResolveInsideServiceMount(t *testing.T) {
	base := t.TempDir()
	mount := filepath.Join(base, "service")
	if err := os.Mkdir(mount, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "xdg")
	if err := os.Symlink(mount, link); err != nil {
		t.Fatal(err)
	}
	c := Config{Root: filepath.Join(link, "not-made", "profiles"), Runtime: filepath.Join(base, "runtime")}
	if err := c.PrivateFrom(mount); err == nil {
		t.Fatal("an XDG symlink exposed future profiles to the service")
	}
	c.Root = filepath.Join(base, "profiles")
	if err := c.PrivateFrom(mount); err != nil {
		t.Fatal(err)
	}
	c.Runtime = filepath.Join(mount, "private")
	if err := c.PrivateFrom(mount); err == nil {
		t.Fatal("a holder socket was allowed inside the service mount")
	}
}

func TestBrowserSwitchAcceptsOnlyExplicitHostEnable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AACP_CHROME", "/bin/true")
	for _, value := range []string{"", "0", "true", "yes", "1"} {
		t.Setenv("AACP_BROWSER", value)
		if got := Load().Enabled; got != (value == "1") {
			t.Errorf("AACP_BROWSER=%q enabled=%v", value, got)
		}
	}
}
