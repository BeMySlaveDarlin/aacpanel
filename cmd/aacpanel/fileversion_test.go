package main

import (
	"encoding/base64"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"aacpanel/internal/chat"
)

// A phone keeps its downloads by name and opens the copy it already has
// instead of fetching a file under the same name again. So a file is handed
// over under a name that carries the minute of its last change — whether it
// is shown in place or saved: a copy saved under the bare name would be the
// one the next "open" stumbles on.
func TestAFileIsHandedOverUnderTheMinuteOfItsLastChange(t *testing.T) {
	cases := []struct {
		name, query, way string
	}{
		{"shown in place", "&inline=1", "inline"},
		{"saved", "", "attachment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent := startAgent(t, map[string]any{
				"ok": true, "kind": "raw", "name": "cv.pdf", "media": "application/pdf",
				"mtime": "2026-09-13T17:50:46+07:00",
				"data":  base64.StdEncoding.EncodeToString([]byte("%PDF-")), "size": 5,
			})
			srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

			w := httptest.NewRecorder()
			srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
				"/api/chat/file/download?session=sentinel&path=out/cv.pdf"+c.query, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("response %d: %s", w.Code, w.Body.String())
			}
			head := w.Header().Get("Content-Disposition")
			way, params, err := mime.ParseMediaType(head)
			if err != nil {
				t.Fatalf("the header did not parse: %v (%q)", err, head)
			}
			if way != c.way {
				t.Errorf("the file came as %q, expected %q", way, c.way)
			}
			const want = "cv 2026-09-13 17-50.pdf"
			if params["filename"] != want {
				t.Errorf("the file is named %q instead of %q — a phone that has a copy under "+
					"that name opens the copy and never asks for the file", params["filename"], want)
			}
			if plain := quotedName(t, head); plain != want {
				t.Errorf("the plain name is %q instead of %q — a browser that reads the quoted "+
					"name only gets a name with no version in it", plain, want)
			}
		})
	}
}

func TestTwoVersionsOfAFileAreHandedOverUnderTwoNames(t *testing.T) {
	names := map[string]string{}
	for _, mtime := range []string{"2026-09-13T17:46:03+07:00", "2026-09-13T17:50:46+07:00"} {
		agent := startAgent(t, map[string]any{
			"ok": true, "kind": "raw", "name": "cv.pdf", "media": "application/pdf",
			"mtime": mtime,
			"data":  base64.StdEncoding.EncodeToString([]byte("%PDF-")), "size": 5,
		})
		srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

		w := httptest.NewRecorder()
		srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
			"/api/chat/file/download?session=sentinel&path=out/cv.pdf&inline=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
		_, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
		if err != nil {
			t.Fatalf("the header did not parse: %v", err)
		}
		names[mtime] = params["filename"]
	}
	if names["2026-09-13T17:46:03+07:00"] == names["2026-09-13T17:50:46+07:00"] {
		t.Errorf("both versions are handed over as %q — the phone has no way to tell the "+
			"fresh one from the copy it saved", names["2026-09-13T17:50:46+07:00"])
	}
}

// A collector that does not say when the file changed is an older one, and
// the file keeps its bare name: a name with no version is still the file.
func TestAFileWithNoTimeOfChangeKeepsItsName(t *testing.T) {
	agent := startAgent(t, map[string]any{
		"ok": true, "kind": "raw", "name": "cv.pdf", "media": "application/pdf",
		"data": base64.StdEncoding.EncodeToString([]byte("%PDF-")), "size": 5,
	})
	srv := &Server{host: hostWith(t, liveSnapshot), chat: chat.New(agent.path)}

	w := httptest.NewRecorder()
	srv.apiChatDownload(w, httptest.NewRequest(http.MethodGet,
		"/api/chat/file/download?session=sentinel&path=out/cv.pdf&inline=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	_, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("the header did not parse: %v", err)
	}
	if params["filename"] != "cv.pdf" {
		t.Errorf("the file is named %q instead of cv.pdf — with no time of change there is "+
			"nothing to stamp it with", params["filename"])
	}
}

func TestTheVersionStampGoesBeforeTheExtension(t *testing.T) {
	const at = "2026-09-13T17:50:46+07:00"
	cases := []struct {
		name, mtime, want string
	}{
		{"cv.pdf", at, "cv 2026-09-13 17-50.pdf"},
		{"aziz-muzafarov-cv.pdf", at, "aziz-muzafarov-cv 2026-09-13 17-50.pdf"},
		{"archive.tar.gz", at, "archive.tar 2026-09-13 17-50.gz"},
		{"Makefile", at, "Makefile 2026-09-13 17-50"},
		{".env", at, ".env 2026-09-13 17-50"},
		{"résumé.md", at, "résumé 2026-09-13 17-50.md"},
		{"the time is the clock of the host, not of the panel", "2026-09-13T10:50:46Z", "the time is the clock of the host, not of the panel 2026-09-13 10-50"},
		{"cv.pdf", "", "cv.pdf"},
		{"cv.pdf", "yesterday", "cv.pdf"},
		{"", at, ""},
	}
	for _, c := range cases {
		if got := versionedName(c.name, c.mtime); got != c.want {
			t.Errorf("versionedName(%q, %q) = %q, expected %q", c.name, c.mtime, got, c.want)
		}
	}
}
