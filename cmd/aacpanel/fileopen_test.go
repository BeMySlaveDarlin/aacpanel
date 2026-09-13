package main

import (
	"bytes"
	"encoding/base64"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/chat"
)

func TestAFileAskedForInPlaceIsShownWhenItIsAPDF(t *testing.T) {
	raw := []byte("%PDF-1.4\n1 0 obj<<>>endobj\n%%EOF\n")
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": "cv.pdf", "media": "application/pdf",
		"data": base64.StdEncoding.EncodeToString(raw), "size": len(raw),
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=out/cv.pdf&inline=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}

	head := w.Header().Get("Content-Disposition")
	way, params, err := mime.ParseMediaType(head)
	if err != nil {
		t.Fatalf("the header did not parse: %v (%q)", err, head)
	}
	if way != "inline" {
		t.Errorf("the answer came as %q: the phone saves the file instead of handing it "+
			"to its viewer, and the tap on \"open\" ends in the download shade", way)
	}
	if params["filename"] != "cv.pdf" {
		t.Errorf("the file is shown as %q instead of cv.pdf — the viewer names its tab by the route", params["filename"])
	}
	if got := w.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("the type of the file is %q — the phone has no idea what opens it", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q — the browser is free to guess a type of its own", got)
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(raw)) {
		t.Errorf("Content-Length %q with %d bytes of the file", got, len(raw))
	}
	if !bytes.Equal(w.Body.Bytes(), raw) {
		t.Errorf("the bytes came out as %x instead of %x — base64 reached the viewer instead of the file",
			w.Body.Bytes(), raw)
	}

	req := <-agent.got
	if req.Raw != "out/cv.pdf" || req.File != "" {
		t.Errorf("the file was asked for as raw=%q file=%q — a file shown in place has to cross "+
			"the same way a saved one does, by ranges of bytes", req.Raw, req.File)
	}
}

func TestAFileAskedForInPlaceIsSavedWhenTheBrowserWouldRunIt(t *testing.T) {
	cases := []struct {
		name, file, media string
	}{
		{"a page", "index.html", "text/html"},
		{"a picture in SVG", "logo.svg", "image/svg+xml"},
		{"a file of no known type", "core.bin", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent := startAgent(t, map[string]any{
				"ok": true, "kind": "raw", "name": c.file, "media": c.media,
				"data": base64.StdEncoding.EncodeToString([]byte("<script>1</script>")), "size": 18,
			})
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

			w := httptest.NewRecorder()
			srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
				"/api/chat/file/download?session=sentinel&path="+c.file+"&inline=1", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("response %d: %s", w.Code, w.Body.String())
			}
			way, _, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
			if err != nil {
				t.Fatalf("the header did not parse: %v", err)
			}
			if way != "attachment" {
				t.Errorf("%s from a project directory came as %q — shown in place it runs "+
					"with the cookies of the panel", c.name, way)
			}
		})
	}
}

func TestAPDFIsSavedUnlessAskedForInPlace(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": "cv.pdf", "media": "application/pdf",
		"data": base64.StdEncoding.EncodeToString([]byte("%PDF-")), "size": 5,
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=cv.pdf", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	way, _, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("the header did not parse: %v", err)
	}
	if way != "attachment" {
		t.Errorf("the save link got the file as %q — the icon meant to put the file on the "+
			"phone opens it instead", way)
	}
}

func TestAFileAskedForInPlaceGoesThroughTheSamePathCheck(t *testing.T) {
	const refusal = "the file was not opened: it either does not exist, or lies outside the " +
		"directory of this conversation"
	agent := startAgent(t, map[string]any{"ok": false, "error": refusal})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	const climb = "../../../etc/passwd"
	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&inline=1&path="+url.QueryEscape(climb), nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), refusal) {
		t.Errorf("the refusal of the collector did not reach the phone: %q", w.Body.String())
	}
	if head := w.Header().Get("Content-Disposition"); head != "" {
		t.Errorf("a refused file is still offered to the viewer: %q", head)
	}

	req := <-agent.got
	if req.Raw != climb {
		t.Errorf("the path reached the collector as %q instead of %q — the flag that asks for "+
			"the file in place must not change what is held against the directory", req.Raw, climb)
	}
	if req.Session != "567f4d24-cd5f-48fa-bdc1-04c89d203494" {
		t.Errorf("the file was asked for in conversation %q", req.Session)
	}
}
