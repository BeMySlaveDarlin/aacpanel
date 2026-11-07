package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type automaticActionKey struct{}

// An automatic action enters the same handler as a press on the panel. The
// request stays in process; no local listener or host capability is needed.
func automaticRequest(ctx context.Context) *http.Request {
	r, _ := http.NewRequestWithContext(context.WithValue(ctx, automaticActionKey{}, "reboot recovery"), http.MethodPost, "http://panel/api/actions", nil)
	return r
}

type actionReply struct {
	header http.Header
	status int
	bytes.Buffer
}

func (r *actionReply) Header() http.Header    { return r.header }
func (r *actionReply) WriteHeader(status int) { r.status = status }
func (r *actionReply) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.Buffer.Write(p)
}

func (s *Server) automaticAction(ctx context.Context, kind, target string, params map[string]any) error {
	raw, err := json.Marshal(map[string]any{"kind": kind, "target": target, "params": params})
	if err != nil {
		return err
	}
	r := automaticRequest(ctx)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	w := &actionReply{header: make(http.Header)}
	s.runAction(w, r, true)
	if w.status != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(w.Bytes(), &failure) == nil && failure.Error != "" {
			return fmt.Errorf("%s", failure.Error)
		}
		return fmt.Errorf("%s", strings.TrimSpace(w.String()))
	}
	var result struct {
		Logged bool `json:"logged"`
	}
	if err := json.Unmarshal(w.Bytes(), &result); err != nil {
		return err
	}
	if !result.Logged {
		return fmt.Errorf("the automatic action was not journaled")
	}
	return nil
}
