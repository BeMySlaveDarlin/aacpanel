package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// A picture past what the viewer reads whole is drawn from its address, and
// the address answers with a picture to draw: anything but SVG, which is a
// page and stays a file to save.
func TestAPictureAskedForInPlaceIsShown(t *testing.T) {
	cases := []struct{ file, media string }{
		{"shot.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"board.webp", "image/webp"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			raw := []byte{0x89, 'P', 'N', 'G'}
			agent := startAgent(t, map[string]any{
				"ok": true, "kind": "raw", "name": c.file, "media": c.media,
				"data": base64.StdEncoding.EncodeToString(raw), "size": len(raw),
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
			if way != "inline" {
				t.Errorf("%s came as %q — a picture opened on its own is saved instead of shown", c.file, way)
			}
			if got := w.Header().Get("Content-Type"); got != c.media {
				t.Errorf("the type of %s is %q, expected %q", c.file, got, c.media)
			}
		})
	}
}

// The cap on a file handed over whole holds for a picture shown in place: it
// is fetched whole, as a save is.
func TestAPictureOverTheCapIsRefusedInPlace(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": "scan.png", "media": "image/png",
		"size": downloadCap + 1, "data": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=scan.png&inline=1", nil))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response %d: %q — a picture past the cap is handed over whole", w.Code, w.Body.String())
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
	const refusal = "the file was not opened: it either does not exist, or is not a file " +
		"that can be read, or is a secret"
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

// fileAgent is a collector holding one file. It hands the file over by ranges
// no longer than its step, as the real one caps a range, and keeps where each
// range was asked for.
type fileAgent struct {
	path  string
	mu    sync.Mutex
	asked []int64
}

func startFileAgent(t *testing.T, name, media string, body []byte, step int) *fileAgent {
	t.Helper()
	a := &fileAgent{path: socketPath(t, "chat.sock")}
	ln, err := net.Listen("unix", a.path)
	if err != nil {
		t.Fatalf("the agent socket: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go a.serve(conn, name, media, body, step)
		}
	}()
	return a
}

func (a *fileAgent) serve(conn net.Conn, name, media string, body []byte, step int) {
	defer conn.Close()
	var req chat.Req
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	a.mu.Lock()
	a.asked = append(a.asked, req.Offset)
	a.mu.Unlock()
	size := int64(len(body))
	start := min(req.Offset, size)
	end := min(start+int64(min(req.Bytes, step)), size)
	reply := map[string]any{"ok": true, "kind": "raw", "name": name, "media": media,
		"size": size, "offset": start, "mtime": "2026-09-13T17:50:46+07:00",
		"data": base64.StdEncoding.EncodeToString(body[start:end])}
	if end < size {
		reply["next"] = end
	}
	_ = json.NewEncoder(conn).Encode(reply)
}

func (a *fileAgent) offsets() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.asked)
}

func playedFile() []byte {
	body := make([]byte, 40)
	for i := range body {
		body[i] = byte(i)
	}
	return body
}

// A player asks for the part it plays and gets that part: the bytes of the
// range, cut from as many ranges of the collector as it takes, and not a byte
// read past it. Without that a phone plays nothing it has no bytes of in hand
// — no song over the size the viewer reads whole, no seek into a clip.
func TestASoundOrAClipIsPlayedByTheRangeThePlayerAsksFor(t *testing.T) {
	cases := []struct{ name, file, media string }{
		{"a sound", "song.mp3", "audio/mpeg"},
		{"a clip", "clip.mp4", "video/mp4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := playedFile()
			agent := startFileAgent(t, c.file, c.media, body, 8)
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

			req := httptest.NewRequest(http.MethodGet,
				"/api/chat/file/download?session=sentinel&path=media/"+c.file+"&inline=1", nil)
			req.Header.Set("Range", "bytes=10-29")
			w := httptest.NewRecorder()
			srv.apiChatDownload(w, req)
			if w.Code != http.StatusPartialContent {
				t.Fatalf("response %d to a range: %q — the player gets the whole file or nothing, "+
					"and a seek starts the file over", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Content-Range"); got != "bytes 10-29/40" {
				t.Errorf("Content-Range %q, expected bytes 10-29/40", got)
			}
			if got := w.Header().Get("Accept-Ranges"); got != "bytes" {
				t.Errorf("Accept-Ranges %q — the player is not told it may seek", got)
			}
			if got := w.Header().Get("Content-Length"); got != "20" {
				t.Errorf("Content-Length %q for a range of 20 bytes", got)
			}
			if !bytes.Equal(w.Body.Bytes(), body[10:30]) {
				t.Errorf("the range came as %v instead of %v", w.Body.Bytes(), body[10:30])
			}
			way, _, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
			if err != nil {
				t.Fatalf("the header did not parse: %v", err)
			}
			if way != "inline" {
				t.Errorf("%s came as %q — the player is handed a file to save", c.name, way)
			}
			if got, want := agent.offsets(), []int64{0, 10, 18, 26}; !slices.Equal(got, want) {
				t.Errorf("the collector was asked at %v, expected %v: the head that names the "+
					"file, then the ranges the player asked for and nothing past them", got, want)
			}
		})
	}
}

// Without a range the file comes whole, as a save takes it.
func TestASoundAskedForWithoutARangeComesWhole(t *testing.T) {
	body := playedFile()
	agent := startFileAgent(t, "song.mp3", "audio/mpeg", body, 8)
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=song.mp3&inline=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), body) {
		t.Errorf("the file came as %v instead of %v", w.Body.Bytes(), body)
	}
	if got := w.Header().Get("Content-Length"); got != "40" {
		t.Errorf("Content-Length %q against 40 bytes of the file", got)
	}
	if got, want := agent.offsets(), []int64{0, 8, 16, 24, 32}; !slices.Equal(got, want) {
		t.Errorf("the collector was asked at %v, expected %v", got, want)
	}
}

// The cap on what the panel hands over is a cap on a file saved whole: a
// film played in place crosses the socket as fast as it plays.
func TestAFilmOverTheCapIsPlayed(t *testing.T) {
	head := []byte("ftypisom")
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": "film.mp4", "media": "video/mp4",
		"size": downloadCap + 1, "offset": 0, "next": len(head),
		"data": base64.StdEncoding.EncodeToString(head),
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	req := httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=film.mp4&inline=1", nil)
	req.Header.Set("Range", "bytes=0-7")
	w := httptest.NewRecorder()
	srv.apiChatDownload(w, req)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("response %d: %q — a film past the size the panel saves does not play",
			w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), head) {
		t.Errorf("the head of the film came as %q instead of %q", w.Body.Bytes(), head)
	}
	if got, want := w.Header().Get("Content-Range"), fmt.Sprintf("bytes 0-7/%d", downloadCap+1); got != want {
		t.Errorf("Content-Range %q, expected %q", got, want)
	}
}
