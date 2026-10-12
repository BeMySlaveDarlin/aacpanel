package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aacpanel/internal/browserlink"
)

func TestBrowserLanguageFollowsTheHostLocale(t *testing.T) {
	for _, tc := range []struct{ all, lang, want string }{
		{"", "ru_RU.UTF-8", "ru-RU,ru,en"},
		{"de_DE.UTF-8", "ru_RU.UTF-8", "de-DE,de,en"},
		{"", "en_GB.UTF-8", "en-GB,en"},
		{"", "fr", "fr,en"},
		{"", "sr_RS.UTF-8@latin", "sr-RS,sr,en"},
		{"C.UTF-8", "ru_RU.UTF-8", "en-US,en"},
		{"", "POSIX", "en-US,en"},
		{"", "", "en-US,en"},
	} {
		t.Run(tc.all+"/"+tc.lang, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.all)
			t.Setenv("LANG", tc.lang)
			if got := hostLanguage(); got != tc.want {
				t.Fatalf("Accept-Language = %q, want %q", got, tc.want)
			}
		})
	}
}

// Reuse the input/guard test's Chrome. Only this disposable context bypasses
// the public-address proxy so a real local server can see the first headers;
// the original profile still exercises the production network boundary.
func checkViewerIdentity(t *testing.T, b *chrome, ctx context.Context) {
	t.Helper()
	type brand struct{ Brand, Version string }
	type observed struct {
		Path, UA, Language, Platform string
		Webdriver                    *bool
		Width, Height                int
		DPR                          float64
		Data                         struct {
			Mobile                           bool
			Platform, PlatformVersion, Model string
			Brands, FullVersionList          []brand
		}
		Headers http.Header
	}
	reports := make(chan observed, 16)
	errors := make(chan error, 1)
	var mu sync.Mutex
	headers := make(map[string]http.Header)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var got observed
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				select {
				case errors <- err:
				default:
				}
				return
			}
			mu.Lock()
			got.Headers = headers[got.Path]
			mu.Unlock()
			reports <- got
			return
		}
		mu.Lock()
		headers[r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, `<meta name="viewport" content="width=device-width"><script>
(async () => {
  const report = {path:location.pathname, ua:navigator.userAgent,
    language:navigator.language, platform:navigator.platform, webdriver:navigator.webdriver,
    width:innerWidth, height:innerHeight, dpr:devicePixelRatio,
    data:await navigator.userAgentData.getHighEntropyValues(['platformVersion','model','fullVersionList'])};
  await fetch('/report', {method:'POST', body:JSON.stringify(report)});
})();
</script>`)
	}))
	defer server.Close()
	call := func(sid, method string, params, out interface{}) {
		t.Helper()
		if err := b.call(ctx, sid, method, params, out); err != nil {
			t.Fatal(err)
		}
	}
	var version struct{ Product string }
	call("", "Browser.getVersion", struct{}{}, &version)
	_, fullVersion, _ := strings.Cut(version.Product, "/")
	major, _, _ := strings.Cut(fullVersion, ".")
	phone := browserlink.Size{Width: 390, Height: 844, DPR: 2, Mobile: true}
	desktop := browserlink.Size{Width: 1280, Height: 800, DPR: 1}
	check := func(path string, size browserlink.Size) {
		t.Helper()
		var got observed
		select {
		case got = <-reports:
		case err := <-errors:
			t.Fatal(err)
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: the page did not report its initial identity", path)
		}
		if got.Path != path {
			t.Fatalf("report path = %q, want %q", got.Path, path)
		}
		platform, navigatorPlatform, mobile := "Linux", "Linux x86_64", "?0"
		if size.Mobile {
			platform, navigatorPlatform, mobile = "Android", "Linux armv8l", "?1"
		}
		if strings.Contains(got.UA, "Headless") || !strings.Contains(got.UA, "Chrome/"+fullVersion+" ") ||
			strings.Contains(got.UA, " Mobile ") != size.Mobile || !strings.Contains(got.UA, platform) {
			t.Errorf("%s: UA = %q for mobile=%v", path, got.UA, size.Mobile)
		}
		if got.Headers.Get("User-Agent") != got.UA || got.Headers.Get("Sec-CH-UA-Mobile") != mobile ||
			got.Headers.Get("Sec-CH-UA-Platform") != `"`+platform+`"` {
			t.Errorf("%s: first navigation headers = %v", path, got.Headers)
		}
		if got.Data.Mobile != size.Mobile || got.Data.Platform != platform || got.Platform != navigatorPlatform {
			t.Errorf("%s: navigator identity = %+v, platform=%q", path, got.Data, got.Platform)
		}
		for _, name := range []string{"Chromium", "Google Chrome"} {
			found, full := false, false
			for _, brand := range got.Data.Brands {
				found = found || brand.Brand == name && brand.Version == major
			}
			for _, brand := range got.Data.FullVersionList {
				full = full || brand.Brand == name && brand.Version == fullVersion
			}
			if !found || !full || !strings.Contains(got.Headers.Get("Sec-CH-UA"), `"`+name+`";v="`+major+`"`) {
				t.Errorf("%s: %s version differs from %s: %+v, headers=%v", path, name, fullVersion, got.Data, got.Headers)
			}
		}
		if size.Mobile && (got.Data.Model != "Pixel 7" || got.Data.PlatformVersion != "13.0.0") {
			t.Errorf("%s: mobile model/version = %+v", path, got.Data)
		}
		if got.Webdriver == nil || *got.Webdriver {
			t.Errorf("%s: navigator.webdriver must exist and equal false", path)
		}
		if got.Width != size.Width || got.Height != size.Height || got.DPR != size.DPR {
			t.Errorf("%s: initial viewport = %dx%d @ %v, want %+v", path, got.Width, got.Height, got.DPR, size)
		}
		if got.Language != "ru-RU" || got.Headers.Get("Accept-Language") != "ru-RU,ru;q=0.9,en;q=0.8" {
			t.Errorf("%s: language = %q, header = %q", path, got.Language, got.Headers.Get("Accept-Language"))
		}
	}
	var browserContext struct{ BrowserContextID string }
	call("", "Target.createBrowserContext", map[string]string{"proxyServer": "direct://"}, &browserContext)
	defer call("", "Target.disposeBrowserContext", map[string]string{"browserContextId": browserContext.BrowserContextID}, nil)
	resize := func(size browserlink.Size) {
		t.Helper()
		if err := b.cast(ctx, size); err != nil {
			t.Fatal(err)
		}
	}
	resize(phone)
	var target struct{ TargetID string }
	// Like the new-tab input, start blank and wait for the holder before
	// navigating. createTarget with a URL can load before auto-attachment.
	call("", "Target.createTarget", map[string]string{
		"url": "about:blank", "browserContextId": browserContext.BrowserContextID,
	}, &target)
	if err := b.ready(ctx, target.TargetID); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	sid := b.tabs[target.TargetID].id
	b.mu.Unlock()
	call(sid, "Page.navigate", map[string]string{"url": server.URL + "/phone-tab"}, nil)
	check("/phone-tab", phone)
	call(sid, "Runtime.evaluate", map[string]interface{}{
		"expression": fmt.Sprintf("window.identityPopup=window.open(%q)", server.URL+"/phone-popup"), "userGesture": true,
	}, nil)
	check("/phone-popup", phone)
	resize(desktop)
	call(sid, "Runtime.evaluate", map[string]string{
		"expression": fmt.Sprintf("identityPopup.location=%q", server.URL+"/desktop-popup"),
	}, nil)
	check("/desktop-popup", desktop)
	call(sid, "Page.navigate", map[string]string{"url": server.URL + "/desktop-background"}, nil)
	check("/desktop-background", desktop)
	resize(phone)
	call(sid, "Page.navigate", map[string]string{"url": server.URL + "/phone-again"}, nil)
	check("/phone-again", phone)
}
