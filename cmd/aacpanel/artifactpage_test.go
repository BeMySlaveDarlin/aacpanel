package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/chat"
)

// The name of a page arrives in a path, and that path is what the collector is
// asked for. A walk out of the shelf has to end at the panel.
func TestAPageNameThatIsNotOneIsRefused(t *testing.T) {
	for _, bad := range []string{"../secret", "AB12", "a b", "", strings.Repeat("a", 65), "a/b", "-a"} {
		if pageIDRE.MatchString(bad) {
			t.Errorf("%q is taken for the name of a page", bad)
		}
	}
	for _, good := range []string{"ab12cd34", "f-0123456789abcdef", "a"} {
		if !pageIDRE.MatchString(good) {
			t.Errorf("%q is a name and was refused", good)
		}
	}
}

func pageReply(html string) map[string]any {
	return map[string]any{
		"ok": true,
		"page": map[string]any{
			"id":    "ab12cd34",
			"title": "The roadmap",
			"file":  "roadmap.html",
			"url":   "https://claude.ai/public/artifacts/ab12cd34",
			"html":  html,
		},
	}
}

// A kept copy is somebody else's code served from the panel's own address. The
// answer carries the sandbox itself, so the rule holds when that address is
// opened outside any frame — the case no attribute on a frame covers.
func TestAKeptPageIsServedSandboxed(t *testing.T) {
	page := "<!doctype html><script>fetch('/api/sessions')</script>"
	agent := startAgent(t, pageReply(page))
	srv := &Server{chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/artifacts/ab12cd34/page", nil)
	r.SetPathValue("id", "ab12cd34")
	srv.apiPage(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	policy := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "sandbox") {
		t.Fatalf("the answer carries no sandbox: %q", policy)
	}
	if strings.Contains(policy, "allow-same-origin") {
		t.Errorf("the sandbox hands back the origin, and with it the session cookie: %q", policy)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("the answer may be sniffed into another type: %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("a page is served as %q", got)
	}
	if w.Body.String() != page {
		t.Error("the page is changed on the way out: the panel does not rewrite foreign markup, " +
			"safety does not rest on that")
	}
	if req := <-agent.got; req.Page != "ab12cd34" {
		t.Errorf("the collector was asked for %q", req.Page)
	}
}

// A name the route would refuse never becomes a question to the collector.
func TestAPageIsNotAskedForUnderANameThatIsNotOne(t *testing.T) {
	agent := startAgent(t, pageReply("<p>hi"))
	srv := &Server{chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/artifacts/x/page", nil)
	r.SetPathValue("id", "../secret")
	srv.apiPage(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	select {
	case req := <-agent.got:
		t.Errorf("the collector was asked anyway, for %q", req.Page)
	default:
	}
}

// The shelf is a list of cards, and a card is what the screen draws before
// anything is opened. Carrying the documents would make listing twenty pages a
// download of twenty pages.
func TestTheShelfOfPagesCarriesNoDocuments(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true,
		"pages": []map[string]any{
			{"id": "ab12cd34", "title": "The roadmap", "file": "roadmap.html", "bytes": 4096},
		},
	})
	srv := &Server{chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiPages(w, httptest.NewRequest(http.MethodGet, "/api/artifacts", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"html"`) {
		t.Errorf("the list carries the documents themselves: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "The roadmap") {
		t.Errorf("the list says nothing about the page: %s", w.Body.String())
	}
}
