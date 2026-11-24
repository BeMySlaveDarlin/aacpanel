package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"aacpanel/internal/act"
	"aacpanel/internal/notify"
	personrequest "aacpanel/internal/request"
	"aacpanel/internal/store"
)

// Existing MCP servers keep the request endpoint until they reconnect.
func (s *Server) apiRequest(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.host == nil {
		http.Error(w, "acts need the database and the host snapshot", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Conversation string `json:"conversation"`
		Title        string `json:"title"`
		Steps        string `json:"steps"`
		Blocking     *bool  `json:"blocking"`
		ID           string `json:"id"`
		Withdraw     bool   `json:"withdraw"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil {
		http.Error(w, "the request was not parsed", http.StatusBadRequest)
		return
	}
	input := act.Input{Kind: "do", Title: body.Title, Body: act.Body{Steps: body.Steps}, Blocking: body.Blocking, ID: body.ID, Withdraw: body.Withdraw}
	if input.Withdraw {
		input.Kind = ""
	}
	if err := input.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.createAct(w, r, body.Conversation, input)
}

func (s *Server) apiAct(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.host == nil {
		http.Error(w, "acts need the database and the host snapshot", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Conversation string `json:"conversation"`
		act.Input
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil {
		http.Error(w, "the act was not parsed", http.StatusBadRequest)
		return
	}
	body.Input.Clean()
	if err := body.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.ID == "" && body.Kind != "do" && body.Kind != "secret" {
		http.Error(w, "only do and secret acts can be created here", http.StatusBadRequest)
		return
	}
	if body.Kind == "do" && body.Body.Chain == nil {
		http.Error(w, "steps must be a chain", http.StatusBadRequest)
		return
	}
	s.createAct(w, r, body.Conversation, body.Input)
}

// The caller supplies a conversation, never a target session chosen by the model.
func (s *Server) createAct(w http.ResponseWriter, r *http.Request, conversation string, input act.Input) {
	if s.db == nil || s.host == nil {
		http.Error(w, "acts need the database and the host snapshot", http.StatusServiceUnavailable)
		return
	}
	live, ok := s.host.LiveSessionOf(conversation)
	if !ok || live.Outside || live.Name == "" {
		http.Error(w, "the caller has no live session the panel can answer", http.StatusConflict)
		return
	}
	if input.ID != "" {
		s.operateChain(w, r, live.SessionID, live.Name, input)
		return
	}
	row, err := s.db.CreateAct(r.Context(), live.SessionID, live.Name, input)
	if err != nil {
		actError(w, err)
		return
	}
	// secret_ask calls the phone after it has a durable act.
	from, _ := row.CommandWindow()
	if row.Kind == "do" && s.push != nil && from == "" {
		s.push.Send(notify.Requested(notify.Session{ID: live.SessionID, Name: live.Name, CWD: live.CWD}, row.ID, row.Title))
	}
	writeJSON(w, map[string]string{"id": row.ID})
}

func actError(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, store.ErrActClosed) {
		code = http.StatusConflict
	}
	http.Error(w, err.Error(), code)
}
func (s *Server) apiActs(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "acts need the database", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("session")
	if !uuidRE.MatchString(id) {
		http.Error(w, "the conversation id does not look like an id", http.StatusBadRequest)
		return
	}
	rows, err := s.db.ActsOf(r.Context(), id)
	if err != nil {
		actError(w, err)
		return
	}
	if rows == nil {
		rows = []act.Record{}
	}
	writeJSON(w, map[string]any{"acts": rows})
}

type actReply struct {
	lease        *store.ActAnswer
	answer, note string
	ctx          context.Context
	cancel       context.CancelFunc
}

func (a *actReply) close() { a.lease.Close(a.ctx); a.cancel() }
func (s *Server) prepareActAnswer(r *http.Request, target string, params map[string]any) (*actReply, error) {
	id, _ := params["act"].(string)
	if !personrequest.ValidID(id) {
		return nil, errors.New("the act id is not valid")
	}
	if s.db == nil || s.host == nil {
		return nil, errors.New("acts need the database and the host snapshot")
	}
	// Closing a page must not unlock a row while the executor still takes it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout+5*time.Second)
	lease, err := s.db.LockAct(ctx, id)
	if err != nil {
		cancel()
		return nil, err
	}
	live, ok := s.host.LiveSessionOf(lease.Record.Conversation)
	if !ok || live.Outside || live.Name != target {
		lease.Close(ctx)
		cancel()
		return nil, errors.New("the act belongs to another conversation or its session has ended")
	}
	if lease.Record.Kind == "ask" {
		lease.Close(ctx)
		cancel()
		return nil, store.ErrActClosed
	}
	answer, _ := params["answer"].(string)
	note, _ := params["note"].(string)
	return &actReply{lease: lease, answer: answer, note: note, ctx: ctx, cancel: cancel}, nil
}

func requestMessageID(id string) string { return act.MessageID(id) }
