package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/notify"
	personrequest "aacpanel/internal/request"
	"aacpanel/internal/store"
)

// Only the local listener accepts a model's task. The MCP server supplies
// the conversation from its binding; the snapshot supplies its current name.
func (s *Server) apiRequest(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.host == nil {
		http.Error(w, "requests need the database and the host snapshot", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Conversation string `json:"conversation"`
		personrequest.Input
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil {
		http.Error(w, "the request was not parsed", http.StatusBadRequest)
		return
	}
	if err := body.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	live, ok := s.host.LiveSessionOf(body.Conversation)
	if !ok || live.Outside || live.Name == "" {
		http.Error(w, "the caller has no live session the panel can answer", http.StatusConflict)
		return
	}
	if body.Withdraw {
		if err := s.db.WithdrawRequest(r.Context(), live.SessionID, body.ID); err != nil {
			requestError(w, err)
			return
		}
		writeJSON(w, map[string]string{"id": body.ID})
		return
	}
	row, err := s.db.CreateRequest(r.Context(), live.SessionID, live.Name, body.Input)
	if err != nil {
		requestError(w, err)
		return
	}
	if s.push != nil {
		s.push.Send(notify.Requested(notify.Session{ID: live.SessionID, Name: live.Name, CWD: live.CWD}, row.ID, row.Title))
	}
	writeJSON(w, map[string]string{"id": row.ID})
}

func requestError(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, store.ErrRequestClosed) {
		code = http.StatusConflict
	}
	http.Error(w, err.Error(), code)
}

func (s *Server) apiRequests(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "requests need the database", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("session")
	if !uuidRE.MatchString(id) {
		http.Error(w, "the conversation id does not look like an id", http.StatusBadRequest)
		return
	}
	rows, err := s.db.Requests(r.Context(), []string{id})
	if err != nil {
		requestError(w, err)
		return
	}
	if rows == nil {
		rows = []personrequest.Record{}
	}
	writeJSON(w, map[string]any{"requests": rows})
}

type requestReply struct {
	lease        *store.RequestAnswer
	answer, note string
	ctx          context.Context
	cancel       context.CancelFunc
}

func (a *requestReply) close() { a.lease.Close(a.ctx); a.cancel() }

func (s *Server) prepareRequestAnswer(r *http.Request, target string, params map[string]any) (*requestReply, error) {
	id, _ := params["request"].(string)
	answer, _ := params["answer"].(string)
	note, _ := params["note"].(string)
	if !personrequest.ValidID(id) {
		return nil, errors.New("the request id is not valid")
	}
	if err := personrequest.ValidateAnswer(answer, note); err != nil {
		return nil, err
	}
	if s.db == nil || s.host == nil {
		return nil, errors.New("requests need the database and the host snapshot")
	}
	// Closing the page must not release the row while the executor still
	// takes the message: a second device would then send another answer.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout+5*time.Second)
	lease, err := s.db.LockRequest(ctx, id)
	if err != nil {
		cancel()
		return nil, err
	}
	live, ok := s.host.LiveSessionOf(lease.Record.Conversation)
	if !ok || live.Outside || live.Name != target {
		lease.Close(ctx)
		cancel()
		return nil, errors.New("the request belongs to another conversation or its session has ended")
	}
	return &requestReply{lease: lease, answer: answer, note: strings.TrimSpace(note), ctx: ctx, cancel: cancel}, nil
}

// A retry keeps its identity in transports whose queues know message ids.
func requestMessageID(id string) string {
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}
