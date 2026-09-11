package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalPanelRefusesForeignPages(t *testing.T) {
	for _, c := range []struct {
		name    string
		host    string
		origin  string
		site    string
		method  string
		path    string
		mode    string
		dest    string
		allowed bool
		why     string
	}{
		{
			name: "our own page", host: "localhost:8777",
			origin: "http://localhost:8777", site: "same-origin", allowed: true,
		},
		{
			name: "the address was typed by hand", host: "localhost:8777",
			site: "none", allowed: true,
		},
		{
			name: "by the loopback address", host: "127.0.0.1:8777",
			origin: "http://127.0.0.1:8777", site: "same-origin", allowed: true,
		},
		{
			name: "curl from the machine", host: "127.0.0.1:8777", allowed: true,
			why: "there are no headers at all — this is not a browser, and the owner of the machine has a shell anyway",
		},
		{
			name: "a foreign site sends a fetch", host: "localhost:8777",
			origin: "https://evil.example", site: "cross-site",
			why: "exactly the case the door is there for",
		},
		{
			name: "a foreign site without Sec-Fetch-Site", host: "localhost:8777",
			origin: "https://evil.example",
			why:    "a browser without the header still sends Origin",
		},
		{
			name: "a subdomain of our own domain", host: "localhost:8777",
			origin: "http://panel.example", site: "same-site",
			why: "same-site is not ours: a subdomain here is just as foreign",
		},
		{
			name: "DNS rebinding", host: "rebind.example:8777",
			site: "none",
			why:  "the browser came to the loopback, but Host gives away a foreign name",
		},
		{
			name: "a foreign port", host: "localhost:9999",
			origin: "http://localhost:9999", site: "same-origin",
			why: "the panel is not the only thing living on the loopback",
		},
		{
			name: "our own origin, a foreign host", host: "rebind.example:8777",
			origin: "http://localhost:8777", site: "same-origin",
			why: "both have to be checked: either one alone can be forged",
		},
		{
			name: "an https origin", host: "localhost:8777",
			origin: "https://localhost:8777", site: "same-origin",
			why: "the local panel lives over http, and an https origin does not come here from it",
		},

		{
			name: "a whole-page navigation from the panel on the domain", host: "127.0.0.1:8777",
			site: "cross-site", method: http.MethodGet, path: "/app", mode: "navigate", dest: "document",
			allowed: true,
		},
		{
			name: "a navigation from a foreign site passes too", host: "127.0.0.1:8777",
			site: "cross-site", method: http.MethodGet, path: "/app", mode: "navigate", dest: "document",
			allowed: true,
			why:     "this is a person following a link: the document is drawn on our own origin and is not visible to the foreign page",
		},
		{
			name: "a navigation through the service worker of the local origin", host: "127.0.0.1:8777",
			site: "cross-site", method: http.MethodGet, path: "/app", mode: "navigate", dest: "empty",
			allowed: true,
			why:     "the worker re-requests the navigation itself, and Chrome gives it Dest: empty (measured)",
		},
		{
			name: "a foreign site embeds the panel in a frame", host: "127.0.0.1:8777",
			site: "cross-site", method: http.MethodGet, path: "/app", mode: "navigate", dest: "iframe",
			why: "the panel without a login inside a foreign page is clickjacking",
		},
		{
			name: "a form from a foreign site", host: "127.0.0.1:8777",
			origin: "https://evil.example", site: "cross-site", method: http.MethodPost, path: "/api/term/input",
			mode: "navigate", dest: "document",
			why: "navigate/document happens for a POST form too, while a command travels as fetch",
		},
		{
			name: "a navigation with DNS rebinding", host: "rebind.example:8777",
			site: "cross-site", method: http.MethodGet, path: "/app", mode: "navigate", dest: "document",
			why: "a navigation does not cancel the Host check",
		},
		{
			name: "a probe from the panel address", host: "127.0.0.1:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodGet, path: "/probe", mode: "cors",
			allowed: true,
		},
		{
			name: "a probe from the panel address without Sec-Fetch-Site", host: "127.0.0.1:8777",
			origin: "https://panel.example", method: http.MethodGet, path: "/probe",
			allowed: true,
			why:     "the Origin from the map decides on its own, not in a pair with a header an old browser may not have",
		},
		{
			name: "a probe from a foreign origin", host: "127.0.0.1:8777",
			origin: "https://evil.example", site: "cross-site", method: http.MethodGet, path: "/probe", mode: "cors",
			why: "the probe is open only to the addresses from the panel map",
		},
		{
			name: "a probe from the panel address, but through DNS rebinding", host: "rebind.example:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodGet, path: "/probe", mode: "cors",
			why: "the Host is checked for the probe too",
		},

		{
			name: "data from the panel address", host: "127.0.0.1:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodGet, path: "/api/tree", mode: "cors",
			allowed: true,
		},
		{
			name: "an action from the panel address", host: "127.0.0.1:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodPost, path: "/api/actions", mode: "cors",
			allowed: true,
			why:     "this is the panel itself: the browser does not let Origin be forged, and the address stands in the map",
		},
		{
			name: "the terminal from the panel address", host: "127.0.0.1:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodPost, path: "/api/term/input", mode: "cors",
			allowed: true,
			why:     "the price is named: a page on the domain, in the browser of the machine itself, gets the terminal too",
		},
		{
			name: "an API preflight from the panel address", host: "127.0.0.1:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodOptions, path: "/api/actions", mode: "cors",
			allowed: true,
			why:     "without a preflight the browser sends neither a POST with json nor an Authorization header",
		},
		{
			name: "data from a foreign origin", host: "127.0.0.1:8777",
			origin: "https://evil.example", site: "cross-site", method: http.MethodGet, path: "/api/tree", mode: "cors",
			why: "the exception is an Origin from the map, not every cross-site on /api/",
		},
		{
			name: "a preflight from a foreign origin", host: "127.0.0.1:8777",
			origin: "https://evil.example", site: "cross-site", method: http.MethodOptions, path: "/api/actions", mode: "cors",
			why: "a foreign site does not even get an answer to a preflight",
		},
		{
			name: "a subdomain of our own domain on the API", host: "127.0.0.1:8777",
			origin: "https://sub.panel.example", site: "same-site", method: http.MethodGet, path: "/api/tree", mode: "cors",
			why: "the map is matched literally, and a subdomain does not stand in it",
		},
		{
			name: "a page disguised as the API from the panel address", host: "127.0.0.1:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodGet, path: "/app", mode: "cors",
			why: "the exception is /api/, /dist/ and /probe, not everything for this origin",
		},

		{
			name: "the panel code from the panel address", host: "127.0.0.1:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodGet, path: "/dist/bundle.js", mode: "cors",
			allowed: true,
			why:     "this is the same built front the local panel gives to everyone who comes",
		},
		{
			name: "code from a foreign origin", host: "127.0.0.1:8777",
			origin: "https://evil.example", site: "cross-site", method: http.MethodGet, path: "/dist/bundle.js", mode: "cors",
			why: "the exception is an Origin from the map, not every cross-site on /dist/",
		},
		{
			name: "code from the panel address, but through DNS rebinding", host: "rebind.example:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodGet, path: "/dist/bundle.js", mode: "cors",
			why: "the Host is checked for the code too",
		},
		{
			name: "the API from the panel address, but through DNS rebinding", host: "rebind.example:8777",
			origin: "https://panel.example", site: "cross-site", method: http.MethodGet, path: "/api/tree", mode: "cors",
			why: "the Host is checked for the API too",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var reached bool
			panel := func(origin string) bool { return origin == "https://panel.example" }
			h := localOnly("8777", panel, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
			}))

			method, path := http.MethodPost, "/api/term/in"
			if c.method != "" {
				method = c.method
			}
			if c.path != "" {
				path = c.path
			}
			r := httptest.NewRequest(method, "http://"+c.host+path, strings.NewReader("rm -rf\r"))
			r.Host = c.host
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if c.site != "" {
				r.Header.Set("Sec-Fetch-Site", c.site)
			}
			if c.mode != "" {
				r.Header.Set("Sec-Fetch-Mode", c.mode)
			}
			if c.dest != "" {
				r.Header.Set("Sec-Fetch-Dest", c.dest)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if c.allowed {
				if !reached {
					t.Errorf("our own request did not pass (%d): %s", w.Code, w.Body.String())
				}
				return
			}
			if reached {
				t.Errorf("a foreign request reached the handler — %s", c.why)
			}
			if w.Code != http.StatusForbidden {
				t.Errorf("status %d instead of 403", w.Code)
			}
			if w.Body.Len() == 0 {
				t.Error("a refusal without a reason: on the local panel it reads as a breakage")
			}
		})
	}
}

func TestLocalPortParsing(t *testing.T) {
	for _, c := range []struct {
		addr string
		want string
		bad  bool
	}{
		{addr: ":8777", want: "8777"},
		{addr: "127.0.0.1:8777", want: "8777"},
		{addr: "0.0.0.0:9000", want: "9000"},
		{addr: "8777", bad: true},
		{addr: "", bad: true},
	} {
		got, err := portOf(c.addr)
		if c.bad {
			if err == nil {
				t.Errorf("the address %q was accepted as %q", c.addr, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("the address %q was not parsed: %v", c.addr, err)
			continue
		}
		if got != c.want {
			t.Errorf("port %q was taken from %q instead of %q", c.addr, got, c.want)
		}
	}
}

func TestLocalPanelRefusesFrames(t *testing.T) {
	srv, err := (&Server{}).localServer(":8777")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8777/healthz", nil)
	r.Host = "127.0.0.1:8777"
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz on the local panel: %d", rec.Code)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("the response of the local panel does not forbid framing: X-Frame-Options=%q CSP=%q",
			rec.Header().Get("X-Frame-Options"), rec.Header().Get("Content-Security-Policy"))
	}
}
