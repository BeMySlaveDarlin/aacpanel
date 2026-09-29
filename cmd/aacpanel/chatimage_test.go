package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aacpanel/internal/chat"
)

// imageAsked runs one request for a picture against a collector that answers
// with the picture, and returns the answer and what the collector was asked.
func imageAsked(t *testing.T, query string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	agent := startAgent(t, map[string]any{
		"ok": true, "media": "image/png", "data": base64.StdEncoding.EncodeToString([]byte("\x89PNG")),
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}
	w := httptest.NewRecorder()
	srv.apiChatImage(w, httptest.NewRequest(http.MethodGet, "/api/chat/image?session=sentinel"+query, nil))
	if w.Code != http.StatusOK {
		return w, nil
	}
	var req struct {
		Image map[string]any `json:"image"`
	}
	if err := json.Unmarshal(<-agent.raw, &req); err != nil {
		t.Fatalf("the request to the collector did not parse: %v", err)
	}
	return w, req.Image
}

// A picture the panel sent is not in the transcript: the collector is asked
// for it by its name and reads it from the directory the executor keeps it in.
func TestAPictureThePanelSentIsAskedForByItsName(t *testing.T) {
	w, asked := imageAsked(t, "&upload=20260920-100000-ab12cd-shot.png")
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s — a picture by its name needs no place in the transcript", w.Code, w.Body.String())
	}
	if asked["upload"] != "20260920-100000-ab12cd-shot.png" {
		t.Errorf("the collector was asked %v: without the name it looks in the transcript", asked)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("the picture came as %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q — the browser is free to take the bytes for a page", got)
	}
	if !bytes.Equal(w.Body.Bytes(), []byte("\x89PNG")) {
		t.Errorf("the bytes came out as %q", w.Body.Bytes())
	}
}

// A picture a call returned lies inside the result of the call: the place in
// the result travels beside the record and the block, and a pasted picture is
// asked for the way it always was.
func TestAPictureACallReturnedIsAskedForByItsPlaceInTheResult(t *testing.T) {
	_, asked := imageAsked(t, "&pos=64&i=1&part=0")
	if asked["pos"] != float64(64) || asked["index"] != float64(1) || asked["part"] != float64(0) {
		t.Errorf("the collector was asked %v: the picture at place 0 of the result at block 1 of 64", asked)
	}

	_, asked = imageAsked(t, "&pos=64&i=1")
	if _, ok := asked["part"]; ok {
		t.Errorf("a pasted picture was asked for with a place in a result: %v", asked)
	}
	if _, ok := asked["upload"]; ok {
		t.Errorf("a pasted picture was asked for by a name: %v", asked)
	}

	w, _ := imageAsked(t, "&pos=64&i=1&part=x")
	if w.Code != http.StatusBadRequest {
		t.Errorf("a place that is not a number got %d: %s", w.Code, w.Body.String())
	}
}
