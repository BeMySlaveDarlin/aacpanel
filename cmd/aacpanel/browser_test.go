package main

import (
	"aacpanel/internal/action"
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/auth"
	"aacpanel/internal/browserlink"
)

func TestBrowserRoutesRequireAuthenticationIndependentlyOfTerminal(t *testing.T) {
	s := &Server{auth: &auth.Service{}}
	mux := s.routes(s.publicGate())
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/browser"},
		{"GET", "/api/browser/stream"},
		{"GET", "/api/browser/frame"},
		{"POST", "/api/browser/input"},
		{"POST", "/api/browser/size"},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s bypassed auth: %d", route.method, route.path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.apiBrowserStream(w, httptest.NewRequest("GET", "/api/browser/stream?profile=b-0123456789abcdef&width=390&height=844&dpr=2", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled stream: %d", w.Code)
	}
}

func TestBrowserFramePullIsBinaryPrivateAndItsAck(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s := &Server{browsers: newBrowsers("")}
	v := &browserView{
		link:  browserlink.New(a),
		frame: browserlink.Frame{Sequence: 7, Data: []byte{255, 216, 1, 2, 255, 217}},
	}
	s.browsers.views["view"] = v
	ack := make(chan browserlink.Frame, 1)
	go func() {
		l := browserlink.New(b)
		l.ReadDeadline(time.Now().Add(time.Second))
		f, _ := l.Read()
		ack <- f
	}()
	w := httptest.NewRecorder()
	s.apiBrowserFrame(w, httptest.NewRequest("GET", "/api/browser/frame?id=view&sequence=7", nil))
	if w.Code != 200 ||
		w.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.Contains(w.Header().Get("Cache-Control"), "no-store") ||
		len(w.Body.Bytes()) != 6 {
		t.Fatalf("frame: %d %v %x", w.Code, w.Header(), w.Body.Bytes())
	}
	if f := <-ack; f.Type != "ack" || f.Sequence != 7 {
		t.Fatalf("pull did not acknowledge its frame: %+v", f)
	}
	w = httptest.NewRecorder()
	s.apiBrowserFrame(w, httptest.NewRequest("GET", "/api/browser/frame?id=view&sequence=7", nil))
	if w.Code != http.StatusGone {
		t.Fatal("frame was replayed")
	}
	r := httptest.NewRequest("POST", "/api/browser/input?id=view", strings.NewReader(`{"events":[{"kind":"text","text":"secret"}]}`))
	r = r.WithContext(context.WithValue(r.Context(), localKey{}, true))
	w = httptest.NewRecorder()
	s.apiBrowserInput(w, r)
	if w.Code != http.StatusGone {
		t.Fatal("another listener took over a controller")
	}
	r = httptest.NewRequest("POST", "/api/browser/input?id=view",
		strings.NewReader(`{"events":[{"kind":"Runtime.evaluate","text":"secret"}]}`))
	w = httptest.NewRecorder()
	s.apiBrowserInput(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatal("raw CDP reached the bridge")
	}
}

// EventSource uses a query token at the alternate address; routed fetch uses
// Bearer, while returning to the home address uses the same device's cookie.
// All three must retain one controller without admitting another device.
func TestBrowserControllerFollowsTheAuthenticatedDeviceAcrossAddresses(t *testing.T) {
	s := testServer(t, auth.SessionTTL{Idle: time.Hour, Absolute: time.Hour})
	s.exec, _ = startFakeExec(t, action.Response{OK: true, Browser: &browserlink.Status{Enabled: true}})
	path := socketPath(t, "browser.sock")
	ln, err := browserlink.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	s.browsers = newBrowsers(path)
	incoming := make(chan browserlink.Frame, 8)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		link := browserlink.New(conn)
		defer link.Close()
		if _, err := link.Read(); err != nil {
			return
		}
		link.Write(browserlink.Frame{Type: "ready", Version: browserlink.Protocol})
		link.Write(browserlink.Frame{Type: "frame", Sequence: 1, Data: []byte{255, 216, 255, 217}})
		for {
			frame, err := link.Read()
			if err != nil {
				return
			}
			incoming <- frame
		}
	}()
	server := httptest.NewServer(s.routes(s.publicGate()))
	defer server.Close()
	cookieRequest := withSession(t, s, "GET", "/api/session/token")
	token, err := s.auth.IssueBearer(cookieRequest)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL+
		"/api/browser/stream?profile=b-0123456789abcdef&width=390&height=844&dpr=2&token="+url.QueryEscape(token), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	ready := readEvent(t, reader)
	var view struct{ ID string }
	if ready.event != "ready" || json.Unmarshal([]byte(ready.data), &view) != nil || view.ID == "" {
		t.Fatalf("the alternate-address stream did not attach: %+v", ready)
	}
	if frame := readEvent(t, reader); frame.event != "frame" {
		t.Fatalf("no frame: %+v", frame)
	}
	for _, tc := range []struct {
		method, path, body, expected string
		cookie                       bool
	}{
		{
			"POST",
			"/api/browser/input?id=" + view.ID,
			`{"events":[{"kind":"text","text":"hello"}]}`,
			"input",
			false,
		},
		{"POST", "/api/browser/size?id=" + view.ID + "&width=400&height=800&dpr=1", "", "size", false},
		{"GET", "/api/browser/frame?id=" + view.ID + "&sequence=1", "", "ack", true},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.cookie {
			for _, cookie := range cookieRequest.Cookies() {
				r.AddCookie(cookie)
			}
		} else {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.routes(s.publicGate()).ServeHTTP(w, r)
		if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
			t.Fatalf("%s: %d %s", tc.expected, w.Code, w.Body)
		}
		select {
		case got := <-incoming:
			if got.Type != tc.expected {
				t.Fatalf("got %s, want %s", got.Type, tc.expected)
			}
		case <-ctx.Done():
			t.Fatal("the controller did not reach the holder")
		}
	}
	other := httptest.NewRecorder()
	s.auth.IssueDevice(other, false, 2)
	r := httptest.NewRequest("POST", "/api/browser/input?id="+view.ID, strings.NewReader(`{"events":[{"kind":"text","text":"intruder"}]}`))
	for _, cookie := range other.Result().Cookies() {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.routes(s.publicGate()).ServeHTTP(w, r)
	if w.Code != http.StatusGone {
		t.Fatalf("another device took the controller: %d", w.Code)
	}
}
