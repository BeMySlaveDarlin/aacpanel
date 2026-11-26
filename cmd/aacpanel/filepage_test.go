package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/chat"
)

func htmlRange(body string, size, offset int) map[string]any {
	return map[string]any{"ok": true, "kind": "raw", "name": "review.html",
		"size": size, "offset": offset, "data": base64.StdEncoding.EncodeToString([]byte(body))}
}

func TestHTMLFilePageServesTheWholeFileWithItsOwnSandbox(t *testing.T) {
	body := "<!doctype html><script>document.body.append('drawn')</script>" + strings.Repeat(" ", downloadChunk)
	agent := startAgentSeq(t, htmlRange(body[:downloadChunk], len(body), 0), htmlRange(body[downloadChunk:], len(body), downloadChunk))
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w := httptest.NewRecorder()
	srv.apiChatFilePage(w, httptest.NewRequest(http.MethodGet, "/api/chat/file/page?session=sentinel&path=review.html", nil))
	wantBody := "<!doctype html><script>" + filePageStorage + "</script>" + strings.TrimPrefix(body, "<!doctype html>")
	if w.Code != http.StatusOK || w.Body.String() != wantBody {
		t.Fatalf("page response %d with %d bytes, expected %d bytes with storage before the page", w.Code, w.Body.Len(), len(wantBody))
	}
	for key, want := range map[string]string{
		"Content-Type": "text/html; charset=utf-8", "X-Content-Type-Options": "nosniff",
		"Referrer-Policy": "no-referrer", "Cache-Control": "no-store", "Content-Length": strconv.Itoa(len(wantBody)),
		"Content-Security-Policy": "sandbox allow-scripts allow-downloads; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'",
	} {
		if got := w.Header().Get(key); got != want {
			t.Errorf("%s = %q, expected %q", key, got, want)
		}
	}
	for _, offset := range []int64{0, downloadChunk} {
		got := <-agent.got
		if got.Raw != "review.html" || got.Session != "567f4d24-cd5f-48fa-bdc1-04c89d203494" || got.Offset != offset || got.Bytes != downloadChunk {
			t.Errorf("file range: %+v, expected the viewer's session/path at offset %d", got, offset)
		}
	}
}

func TestHTMLFilePageRejectsIncompleteAndUnsupportedFiles(t *testing.T) {
	for _, c := range []struct {
		name   string
		reply  map[string]any
		status int
	}{
		{"too large", htmlRange("x", (8<<20)+1, 0), http.StatusRequestEntityTooLarge},
		{"truncated", htmlRange("short", 20, 0), http.StatusBadGateway},
		{"not a page", map[string]any{"ok": true, "kind": "raw", "name": "image.png", "media": "image/png", "size": 1, "data": "eA=="}, http.StatusUnsupportedMediaType},
		{"not decoded", map[string]any{"ok": true, "kind": "raw", "name": "review.html", "size": 1, "data": "%%%"}, http.StatusBadGateway},
		{"old collector", map[string]any{"ok": true, "kind": "text", "text": "page"}, http.StatusBadGateway},
	} {
		t.Run(c.name, func(t *testing.T) {
			agent := startAgent(t, c.reply)
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
			w := httptest.NewRecorder()
			srv.apiChatFilePage(w, httptest.NewRequest(http.MethodGet, "/api/chat/file/page?session=sentinel&path=review.html", nil))
			if w.Code != c.status {
				t.Errorf("response %d: %s, expected %d", w.Code, w.Body.String(), c.status)
			}
		})
	}
}

func TestHTMLFilePageAcceptsTheCollectorsTextReplyWithoutAMediaType(t *testing.T) {
	for _, name := range []string{"review.html", "review.HTM", "review.xhtml"} {
		t.Run(name, func(t *testing.T) {
			reply := htmlRange("<p>page</p>", 11, 0)
			reply["name"] = name
			agent := startAgent(t, reply)
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
			w := httptest.NewRecorder()
			srv.apiChatFilePage(w, httptest.NewRequest(http.MethodGet, "/api/chat/file/page?session=sentinel&path="+name, nil))
			if w.Code != http.StatusOK || w.Body.String() != "<!doctype html><script>"+filePageStorage+"</script><p>page</p>" {
				t.Errorf("the collector's text reply for %s returned %d: %s", name, w.Code, w.Body.String())
			}
		})
	}
}

func TestHTMLFilePageKeepsTheCollectorPathCheckAndSessionGate(t *testing.T) {
	const path = "../private.html"
	const refusal = "the file was not opened"
	agent := startAgent(t, map[string]any{"ok": false, "error": refusal})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w := httptest.NewRecorder()
	srv.apiChatFilePage(w, httptest.NewRequest(http.MethodGet, "/api/chat/file/page?session=sentinel&path="+url.QueryEscape(path), nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), refusal) {
		t.Fatalf("a refused path returned %d: %s", w.Code, w.Body.String())
	}
	if got := <-agent.got; got.Raw != path || got.Session != "567f4d24-cd5f-48fa-bdc1-04c89d203494" {
		t.Errorf("the path check did not go through the collector: %+v", got)
	}
	deny := func(http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "sign in", http.StatusUnauthorized) })
	}
	w = httptest.NewRecorder()
	srv.routes(gate{page: deny, stream: deny}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/chat/file/page?session=sentinel&path=review.html", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("the file page bypassed the session gate: %d", w.Code)
	}
	select {
	case got := <-agent.got:
		t.Errorf("an unauthenticated page still asked the collector: %+v", got)
	default:
	}
}

func TestHTMLFilePageHeadKeepsThePolicyWithoutABody(t *testing.T) {
	agent := startAgent(t, htmlRange("<p>page</p>", 11, 0))
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w := httptest.NewRecorder()
	srv.apiChatFilePage(w, httptest.NewRequest(http.MethodHead, "/api/chat/file/page?session=sentinel&path=review.html", nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Content-Security-Policy") != filePageCSP || w.Header().Get("Content-Length") != strconv.Itoa(len(filePageDocument([]byte("<p>page</p>")))) {
		t.Errorf("HEAD response %d, body %q, policy %q", w.Code, w.Body.String(), w.Header().Get("Content-Security-Policy"))
	}
}

func TestHTMLFilePagePutsStorageBeforeEveryDocumentShape(t *testing.T) {
	for _, c := range []struct{ name, source, tail string }{
		{"complete", "<!doctype html><html><head><title>Page</title></head><body>Page</body></html>", "<html><head><title>Page</title></head><body>Page</body></html>"},
		{"head", "<head><title>Page</title></head><body>Page</body>", "<head><title>Page</title></head><body>Page</body>"},
		{"fragment", "<p>Page</p>", "<p>Page</p>"},
		{"leading comment", "\ufeff \n<!-- Page -->\n<!DOCTYPE HTML PUBLIC \"legacy\"><html><body>Page</body></html>", "\ufeff \n<!-- Page -->\n<html><body>Page</body></html>"},
		{"doctype in script", "<script>const source = '<!doctype html>';</script>", "<script>const source = '<!doctype html>';</script>"},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := "<!doctype html><script>" + filePageStorage + "</script>" + c.tail
			if got := string(filePageDocument([]byte(c.source))); got != want {
				t.Errorf("storage wrapper changed the document content or order for %s", c.name)
			}
		})
	}
}
