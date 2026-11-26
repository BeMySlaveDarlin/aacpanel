package check

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The browser exercises the response contract checked against apiChatFilePage
// in cmd/aacpanel; these handlers replace only the collector and HTTP service.
const htmlFilePolicy = "sandbox allow-scripts allow-downloads; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'"

func htmlFileStorage(t *testing.T) string {
	t.Helper()
	shim, err := os.ReadFile("../../cmd/aacpanel/filepage-storage.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(shim)
}

func htmlFileAnswer(w http.ResponseWriter, body, shim string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", htmlFilePolicy)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte("<!doctype html><script>" + shim + "</script>" + strings.TrimPrefix(body, "<!doctype html>")))
}

func TestHTMLFileRunsOnlyInsideItsOpaqueOfflineFrame(t *testing.T) {
	shim := htmlFileStorage(t)
	for _, screen := range []struct{ name, size, pointer string }{{"phone", phoneScreen, phonePointer}, {"desktop", deskScreen, deskPointer}} {
		t.Run(screen.name, func(t *testing.T) {
			var requests atomic.Int32
			serve := map[string]http.Handler{
				"/html-storage-shim.js": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/javascript")
					_, _ = w.Write([]byte(shim))
				}),
				"/api/chat/file/page": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if shape := r.URL.Query().Get("path"); strings.HasPrefix(shape, "storage-") {
						body := `<ul id="tasks"></ul><script>
function checkStorage(store) {
    const fresh = store.length === 0 && store.getItem('review') === null;
    store.setItem('first', 17);
    const value = store.getItem('first') === '17';
    store.setItem('second', false);
    const indexed = store.length === 2 && store.key(0) === 'first' && store.key(1) === 'second' && store.key(2) === null;
    store.removeItem('first');
    const removed = store.getItem('first') === null && store.length === 1;
    store.named = 23;
    const property = store.named === '23' && store.getItem('named') === '23' && Object.keys(store).includes('named');
    delete store.named;
    const deleted = store.named === undefined && store.getItem('named') === null;
    store.clear();
    const cleared = store.length === 0 && store.key(0) === null && store.getItem('second') === null;
    store.setItem('review', JSON.stringify(['Review one', 'Review two']));
    return {fresh, value, indexed, removed, property, deleted, cleared};
}
const local = checkStorage(localStorage), session = checkStorage(sessionStorage);
for (const text of JSON.parse(localStorage.getItem('review'))) {
    const item = document.createElement('li');
    item.textContent = text;
    document.getElementById('tasks').append(item);
}
parent.postMessage({kind:'html-storage', local, session, compat:document.compatMode,
    drawn:document.querySelectorAll('#tasks li').length === 2}, '*');
</script>`
						switch shape {
						case "storage-complete":
							body = "<!doctype html><html><head><title>Review</title></head><body>" + body + "</body></html>"
						case "storage-head":
							body = "<head><title>Review</title></head><body>" + body + "</body>"
						}
						htmlFileAnswer(w, body, shim)
						return
					}
					body := `<!doctype html><meta charset="utf-8"><body><script>
document.body.append('LIST BUILT BY SCRIPT');
let cookieDenied = false, parentDenied = false;
try { document.cookie; } catch (err) { cookieDenied = err.name === 'SecurityError'; }
try { parent.document.cookie; } catch (err) { parentDenied = err.name === 'SecurityError'; }
const policy = document.permissionsPolicy || document.featurePolicy;
parent.postMessage({kind:'html-ready', cookieDenied, parentDenied, drawn:document.body.textContent.includes('LIST BUILT BY SCRIPT'),
    clipboardWrite:policy?.allowsFeature('clipboard-write') === true, clipboardRead:policy?.allowsFeature('clipboard-read') === true}, '*');
addEventListener('securitypolicyviolation', (event) => {
  if (event.blockedURI.includes('/frame-network-probe')) parent.postMessage({kind:'html-csp', directive:event.effectiveDirective}, '*');
});
fetch('/frame-network-probe').then(() => parent.postMessage({kind:'html-fetch', denied:false}, '*'))
  .catch(() => parent.postMessage({kind:'html-fetch', denied:true}, '*'));
</script>`
					htmlFileAnswer(w, body, shim)
				}),
				"/frame-network-probe": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.Header().Set("Access-Control-Allow-Origin", "*")
					w.WriteHeader(http.StatusNoContent)
				}),
			}
			var got struct {
				CookieDenied, ParentDenied, Drawn, Denied, ParentCookie, NativeKept bool
				ClipboardWrite, ClipboardRead                                       bool
				Origin, Directive, Sandbox, Src, Referrer, Allow                    string
				Storage                                                             []struct {
					Drawn          bool
					Compat         string
					Local, Session struct{ Fresh, Value, Indexed, Removed, Property, Deleted, Cleared bool }
				}
			}
			runFixtureServing(t, "htmlfile.html", screen.size, screen.pointer, serve, &got)
			if !got.Drawn || !got.CookieDenied || !got.ParentDenied || !got.ParentCookie || got.Origin != "null" {
				t.Errorf("the page did not run in its own opaque origin: %+v", got)
			}
			if !got.Denied || got.Directive != "connect-src" || requests.Load() != 0 {
				t.Errorf("network denial: %+v, requests=%d", got, requests.Load())
			}
			if got.Sandbox != "allow-scripts allow-downloads" || got.Allow != "clipboard-write" || got.Referrer != "no-referrer" || !strings.HasPrefix(got.Src, "/api/chat/file/page?") {
				t.Errorf("file frame: %+v", got)
			}
			if !got.ClipboardWrite || got.ClipboardRead {
				t.Errorf("the file needs clipboard writing without clipboard reading: write=%v, read=%v", got.ClipboardWrite, got.ClipboardRead)
			}
			if !got.NativeKept || len(got.Storage) != 4 {
				t.Errorf("native storage changed or document cases are missing: %+v", got)
			}
			for i, page := range got.Storage {
				if !page.Drawn || page.Compat != "CSS1Compat" {
					t.Errorf("document %d did not draw in standards mode: %+v", i, page)
				}
				for name, store := range map[string]struct{ Fresh, Value, Indexed, Removed, Property, Deleted, Cleared bool }{"local": page.Local, "session": page.Session} {
					if !store.Fresh || !store.Value || !store.Indexed || !store.Removed || !store.Property || !store.Deleted || !store.Cleared {
						t.Errorf("document %d %s storage did not behave like private memory: %+v", i, name, store)
					}
				}
			}
		})
	}
}

func TestHTMLFileStartsABlobDownload(t *testing.T) {
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no Chrome on this machine")
	}
	fixture, err := os.ReadFile("fixtures/htmldownload.html")
	if err != nil {
		t.Fatal(err)
	}
	shim := htmlFileStorage(t)
	dir := t.TempDir()
	open := func() (json.RawMessage, error) {
		mux := http.NewServeMux()
		mux.Handle("/", http.FileServer(http.Dir(webDir)))
		mux.HandleFunc("/fixture.html", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fixture)
		})
		mux.HandleFunc("/api/chat/file/page", func(w http.ResponseWriter, r *http.Request) {
			htmlFileAnswer(w, `<a id="save" download="html-review.txt">Save the list</a><script>
document.getElementById('save').href = URL.createObjectURL(new Blob(['Review one\nReview two\n'], {type:'text/plain'}));
parent.postMessage({kind:'html-download-ready'}, '*');
</script>`, shim)
		})
		server := httptest.NewServer(mux)
		defer server.Close()
		b, err := sharedBrowser(chrome, phonePointer)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		session, closeTab, err := b.open(ctx, server.URL+"/fixture.html", phoneScreen)
		if err != nil {
			return nil, err
		}
		defer closeTab()
		if _, err := b.wait(ctx, session, server.URL+"/fixture.html"); err != nil {
			return nil, err
		}
		raw, err := b.call(ctx, session, "Target.getTargetInfo", map[string]any{})
		if err != nil {
			return nil, err
		}
		var info struct {
			TargetInfo struct{ TargetID, BrowserContextID string }
		}
		if err := json.Unmarshal(raw, &info); err != nil {
			return nil, err
		}
		if _, err := b.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "allowAndName", "browserContextId": info.TargetInfo.BrowserContextID, "downloadPath": dir, "eventsEnabled": true}); err != nil {
			return nil, err
		}
		begun := make(chan json.RawMessage, 1)
		b.mu.Lock()
		if b.downloads == nil {
			b.downloads = map[string]chan json.RawMessage{}
		}
		b.downloads["html-review.txt"] = begun
		b.mu.Unlock()
		defer func() { b.mu.Lock(); delete(b.downloads, "html-review.txt"); b.mu.Unlock() }()
		raw, err = b.call(ctx, "", "Target.getTargets", map[string]any{})
		if err != nil {
			return nil, err
		}
		var targets struct {
			TargetInfos []struct{ TargetID, Type, ParentID, URL string }
		}
		if err := json.Unmarshal(raw, &targets); err != nil {
			return nil, err
		}
		frameID := ""
		for _, target := range targets.TargetInfos {
			if target.Type == "iframe" && target.ParentID == info.TargetInfo.TargetID && strings.HasPrefix(target.URL, server.URL+"/api/chat/file/page?") {
				frameID = target.TargetID
			}
		}
		if frameID == "" {
			return nil, fmt.Errorf("the sandboxed file has no frame target")
		}
		raw, err = b.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": frameID, "flatten": true})
		if err != nil {
			return nil, err
		}
		var attached struct{ SessionID string }
		if err := json.Unmarshal(raw, &attached); err != nil {
			return nil, err
		}
		raw, err = b.call(ctx, attached.SessionID, "Runtime.evaluate", map[string]any{"expression": "document.getElementById('save').click()", "userGesture": true})
		if err != nil {
			return nil, err
		}
		var evaluated struct{ ExceptionDetails json.RawMessage }
		if err := json.Unmarshal(raw, &evaluated); err != nil {
			return nil, err
		}
		if len(evaluated.ExceptionDetails) != 0 {
			return nil, fmt.Errorf("the download click threw: %s", evaluated.ExceptionDetails)
		}
		select {
		case event := <-begun:
			return event, nil
		case <-ctx.Done():
			return nil, fmt.Errorf("the sandboxed blob link did not start a download: %w", ctx.Err())
		}
	}
	run := lineUp(t, false, open)
	parallel(t)
	raw, err := run.wait()
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ URL, SuggestedFilename, FrameID string }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.SuggestedFilename != "html-review.txt" || !strings.HasPrefix(got.URL, "blob:null/") || got.FrameID == "" {
		t.Errorf("the browser did not begin the file's blob download: %+v", got)
	}
}

func runPageFileFixture(t *testing.T, wide bool, into any) {
	t.Helper()
	line := "<p>" + strings.Repeat("a line of the page ", 50) + "</p>\n"
	body := `<p id="first">THE START</p>` + strings.Repeat(line, 3200) + `<p id="last">THE END</p>`
	shim := htmlFileStorage(t)
	serve := map[string]http.Handler{"/api/chat/file/page": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { htmlFileAnswer(w, body, shim) })}
	size, pointer := phoneScreen, phonePointer
	if wide {
		size, pointer = deskScreen, deskPointer
	}
	runFixtureServing(t, "pagefile.html", size, pointer, serve, into)
}
