package main

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const filePageCap = 8 << 20

const filePageCSP = "sandbox allow-scripts allow-downloads; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'"

//go:embed filepage-storage.js
var filePageStorage string

var filePageDoctype = regexp.MustCompile(`(?is)^(\x{FEFF}?\s*(?:<!--.*?-->\s*)*)<!doctype\b[^>]*>`)

func filePageDocument(body []byte) []byte {
	// A script before the original doctype would put a complete document in
	// quirks mode. Keep a doctype first, then storage before any page code.
	prefix := "<!doctype html><script>" + filePageStorage + "</script>"
	return append([]byte(prefix), filePageDoctype.ReplaceAll(body, []byte("${1}"))...)
}

// The response carries its own sandbox so opening the address outside the
// viewer cannot give a project file the panel's origin or network access.
func (s *Server) apiChatFilePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", filePageCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	target, err := s.chatTarget(r, r.URL.Query().Get("session"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "a file path is required", http.StatusBadRequest)
		return
	}
	// RawFile applies the same collector-side path check as downloading; the
	// web service never opens the host path itself.
	first, err := s.chat.RawFile(r.Context(), target, path, 0, downloadChunk)
	if err != nil {
		chatFail(w, err)
		return
	}
	if first.Kind != "raw" {
		http.Error(w, "the collector on this host does not hand over files by ranges", http.StatusBadGateway)
		return
	}
	if first.Size > filePageCap {
		http.Error(w, fmt.Sprintf("the page is too large to draw: %d bytes against %d", first.Size, filePageCap), http.StatusRequestEntityTooLarge)
		return
	}
	// Raw replies name every file but only media files carry a MIME type.
	// Match the same HTML extensions the viewer uses to choose its page view.
	switch strings.ToLower(filepath.Ext(first.Name)) {
	case ".html", ".htm", ".xhtml":
	default:
		http.Error(w, "the file is not an HTML page", http.StatusUnsupportedMediaType)
		return
	}
	head, err := base64.StdEncoding.DecodeString(first.Data)
	if err != nil {
		http.Error(w, "the collector sent a page that does not decode", http.StatusBadGateway)
		return
	}
	stream := &fileRanges{ctx: r.Context(), chat: s.chat, target: target, path: path, size: first.Size, at: first.Offset, raw: head}
	body, err := io.ReadAll(io.LimitReader(stream, filePageCap+1))
	if err != nil || int64(len(body)) != first.Size {
		http.Error(w, "the collector did not hand over the whole page", http.StatusBadGateway)
		return
	}
	body = filePageDocument(body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
