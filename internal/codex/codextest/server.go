// Package codextest is a codex app-server daemon for tests: WebSocket on the
// unix socket of a codex home, the JSON-RPC of the daemon without the
// "jsonrpc" field, and threads a test sets up and drives. It answers the way
// codex 0.162 answered the probes; what it does not know it refuses.
package codextest

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Thread is a thread of the daemon as a test sets it up.
type Thread struct {
	ID      string
	CWD     string
	Path    string
	Model   string
	Effort  string
	Parent  string
	Created int64
	// Status is idle, active, systemError or notLoaded.
	Status string
	Flags  []string
}

// Call is a request a client made, as it came.
type Call struct {
	Method string
	Params json.RawMessage
}

// Answer is a reply of a client to a request of the server.
type Answer struct {
	ID     int
	Result json.RawMessage
}

type turn struct {
	id     string
	status string
}

type thread struct {
	Thread
	turns []turn
	items map[string][]map[string]any
	subs  map[*client]bool
}

type approval struct {
	id     int
	thread string
	method string
	params map[string]any
}

type client struct {
	ws *websocket.Conn
}

// Server is the fake daemon.
type Server struct {
	// Home is the codex home the daemon serves.
	Home string

	ln  net.Listener
	srv *http.Server

	mu        sync.Mutex
	threads   map[string]*thread
	order     []string
	clients   map[*client]bool
	calls     []Call
	answers   []Answer
	approvals []*approval
	nextID    int
	turnN     int
}

// New starts a daemon in a home of its own. The home is short on purpose: a
// unix socket takes a path of 108 bytes at most, and a test's own temporary
// directory is longer than that once the socket's place is added.
func New(t testing.TB) *Server {
	t.Helper()
	home, err := os.MkdirTemp("", "cx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	s := &Server{Home: home, threads: map[string]*thread{}, clients: map[*client]bool{}}
	s.Start(t)
	t.Cleanup(s.Close)
	return s
}

// Socket is where the daemon listens.
func (s *Server) Socket() string {
	return filepath.Join(s.Home, "app-server-control", "app-server-control.sock")
}

// Start listens on the socket: once at New, and again after Close for a
// daemon that comes back.
func (s *Server) Start(t testing.TB) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(s.Socket()), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(s.Socket())
	ln, err := net.Listen("unix", s.Socket())
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.ln = ln
	s.srv = &http.Server{Handler: http.HandlerFunc(s.accept), ReadHeaderTimeout: 5 * time.Second}
	srv := s.srv
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
}

// Close stops the daemon: the socket goes and every client is dropped.
func (s *Server) Close() {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	clients := s.clients
	s.clients = map[*client]bool{}
	for _, t := range s.threads {
		t.subs = map[*client]bool{}
	}
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
	for c := range clients {
		_ = c.ws.CloseNow()
	}
	_ = os.Remove(s.Socket())
}

// Add puts a thread on the daemon.
func (s *Server) Add(th Thread) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if th.Status == "" {
		th.Status = "idle"
	}
	if _, ok := s.threads[th.ID]; !ok {
		s.order = append(s.order, th.ID)
	}
	s.threads[th.ID] = &thread{Thread: th, items: map[string][]map[string]any{}, subs: map[*client]bool{}}
}

// Set changes the status of a thread. A thread that is not active has no
// turn in progress: the one that ran is completed.
func (s *Server) Set(id, status string, flags ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[id]
	t.Status, t.Flags = status, flags
	if last := t.last(); last != nil && status != "active" && last.status == "inProgress" {
		last.status = "completed"
	}
}

// Running gives a thread a turn in progress, as one started by another client.
func (s *Server) Running(id, turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[id]
	t.turns = append(t.turns, turn{id: turnID, status: "inProgress"})
	t.Status = "active"
}

// Unload takes a thread off the list of the loaded ones, as the daemon does a
// while after its last client leaves.
func (s *Server) Unload(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[id]
	t.Status, t.Flags, t.subs = "notLoaded", nil, map[*client]bool{}
}

// Drop forgets the clients of a thread without a word to them: a client that
// believes it follows the thread gets nothing of it any more.
func (s *Server) Drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[id].subs = map[*client]bool{}
}

// Item adds an item to a turn of a thread.
func (s *Server) Item(threadID, turnID string, item map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[threadID]
	t.items[turnID] = append(t.items[turnID], item)
}

// Ask makes a request for an approval in a thread and sends it to every
// client of the thread. It returns the id of the request.
func (s *Server) Ask(threadID, method string, params map[string]any) int {
	s.mu.Lock()
	t := s.threads[threadID]
	s.nextID++
	a := &approval{id: s.nextID, thread: threadID, method: method, params: params}
	a.params["threadId"] = threadID
	s.approvals = append(s.approvals, a)
	t.Status = "active"
	if !slices.Contains(t.Flags, "waitingOnApproval") {
		t.Flags = append(t.Flags, "waitingOnApproval")
	}
	subs := clientsOf(t)
	s.mu.Unlock()
	for _, c := range subs {
		c.send(map[string]any{"id": a.id, "method": a.method, "params": a.params})
	}
	return a.id
}

// Resolve answers a request as another client did: it is gone, and the
// clients of the thread are told so.
func (s *Server) Resolve(id int) {
	s.mu.Lock()
	subs, thread := s.resolve(id)
	s.mu.Unlock()
	for _, c := range subs {
		c.send(map[string]any{"method": "serverRequest/resolved",
			"params": map[string]any{"threadId": thread, "requestId": id}})
	}
}

func (s *Server) resolve(id int) ([]*client, string) {
	i := slices.IndexFunc(s.approvals, func(a *approval) bool { return a.id == id })
	if i < 0 {
		return nil, ""
	}
	a := s.approvals[i]
	s.approvals = slices.Delete(s.approvals, i, i+1)
	t := s.threads[a.thread]
	if !slices.ContainsFunc(s.approvals, func(o *approval) bool { return o.thread == a.thread }) {
		t.Flags = slices.DeleteFunc(t.Flags, func(f string) bool { return f == "waitingOnApproval" })
	}
	return clientsOf(t), a.thread
}

// Calls returns the params of the requests clients made with the method.
func (s *Server) Calls(method string) []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []json.RawMessage
	for _, c := range s.calls {
		if c.Method == method {
			out = append(out, c.Params)
		}
	}
	return out
}

// Answers returns the replies clients sent to requests of the server.
func (s *Server) Answers() []Answer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.answers)
}

// Subscribed says whether a client is subscribed to a thread.
func (s *Server) Subscribed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.threads[id].subs) > 0
}

func clientsOf(t *thread) []*client {
	var out []*client
	for c := range t.subs {
		out = append(out, c)
	}
	return out
}

func (c *client) send(msg map[string]any) {
	body, _ := json.Marshal(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.ws.Write(ctx, websocket.MessageText, body)
}

func (s *Server) accept(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(-1)
	c := &client{ws: ws}
	s.mu.Lock()
	if s.srv == nil {
		s.mu.Unlock()
		_ = ws.CloseNow()
		return
	}
	s.clients[c] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		for _, t := range s.threads {
			delete(t.subs, c)
		}
		s.mu.Unlock()
		_ = ws.CloseNow()
	}()
	for {
		_, data, err := ws.Read(context.Background())
		if err != nil {
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Method == "" {
			s.answered(msg.ID, msg.Result)
			continue
		}
		s.mu.Lock()
		s.calls = append(s.calls, Call{Method: msg.Method, Params: msg.Params})
		s.mu.Unlock()
		if len(msg.ID) == 0 {
			continue
		}
		result, after, fail := s.serve(c, msg.Method, msg.Params)
		reply := map[string]any{"id": msg.ID}
		if fail != "" {
			reply["error"] = map[string]any{"code": -32600, "message": fail}
		} else {
			reply["result"] = result
		}
		c.send(reply)
		for _, m := range after {
			c.send(m)
		}
	}
}

func (s *Server) answered(rawID, result json.RawMessage) {
	var id int
	if json.Unmarshal(rawID, &id) != nil {
		return
	}
	s.mu.Lock()
	s.answers = append(s.answers, Answer{ID: id, Result: result})
	subs, thread := s.resolve(id)
	s.mu.Unlock()
	for _, c := range subs {
		c.send(map[string]any{"method": "serverRequest/resolved",
			"params": map[string]any{"threadId": thread, "requestId": id}})
	}
}

// serve answers one request of a client: the result, the messages that go to
// the client after it, or the refusal.
func (s *Server) serve(c *client, method string, raw json.RawMessage) (any, []map[string]any, string) {
	var p struct {
		ThreadID       string `json:"threadId"`
		TurnID         string `json:"turnId"`
		ExpectedTurnID string `json:"expectedTurnId"`
	}
	_ = json.Unmarshal(raw, &p)
	s.mu.Lock()
	defer s.mu.Unlock()
	if method == "initialize" {
		return map[string]any{"userAgent": "fake/0.162.0", "codexHome": s.Home,
			"platformFamily": "unix", "platformOs": "linux"}, nil, ""
	}
	if method == "thread/loaded/list" {
		data := []string{}
		for _, id := range s.order {
			if s.threads[id].Status != "notLoaded" {
				data = append(data, id)
			}
		}
		return map[string]any{"data": data, "nextCursor": nil}, nil, ""
	}
	t := s.threads[p.ThreadID]
	if t == nil {
		return nil, nil, "thread not found: " + p.ThreadID
	}
	switch method {
	case "thread/read":
		return map[string]any{"thread": t.json()}, nil, ""
	case "thread/resume":
		if t.Status == "notLoaded" {
			t.Status = "idle"
		}
		t.subs[c] = true
		var after []map[string]any
		for _, a := range s.approvals {
			if a.thread == t.ID {
				after = append(after, map[string]any{"id": a.id, "method": a.method, "params": a.params})
			}
		}
		return map[string]any{"thread": t.json(), "model": t.Model}, after, ""
	case "thread/unsubscribe":
		delete(t.subs, c)
		return map[string]any{"status": "unsubscribed"}, nil, ""
	case "turn/start":
		t.subs[c] = true
		s.turnN++
		id := fmt.Sprintf("turn-%d", s.turnN)
		t.turns = append(t.turns, turn{id: id, status: "inProgress"})
		t.Status = "active"
		return map[string]any{"turn": map[string]any{"id": id, "items": []any{}, "itemsView": "notLoaded",
			"status": "inProgress"}}, nil, ""
	case "turn/steer":
		last := t.last()
		if last == nil || last.status != "inProgress" || last.id != p.ExpectedTurnID {
			return nil, nil, "expectedTurnId does not match the active turn"
		}
		return map[string]any{"turnId": last.id}, nil, ""
	case "turn/interrupt":
		last := t.last()
		if last == nil || last.id != p.TurnID {
			return nil, nil, "no such turn"
		}
		last.status = "interrupted"
		t.Status, t.Flags = "idle", nil
		return map[string]any{}, nil, ""
	case "thread/turns/list":
		data := []any{}
		if last := t.last(); last != nil {
			data = append(data, map[string]any{"id": last.id, "items": []any{}, "itemsView": "notLoaded",
				"status": last.status})
		}
		return map[string]any{"data": data, "nextCursor": nil}, nil, ""
	case "thread/items/list":
		data := []any{}
		for _, item := range t.items[p.TurnID] {
			data = append(data, map[string]any{"turnId": p.TurnID, "item": item,
				"startedAtMs": nil, "completedAtMs": nil})
		}
		return map[string]any{"data": data, "nextCursor": nil, "backwardsCursor": nil}, nil, ""
	}
	return nil, nil, "method not found: " + method
}

func (t *thread) last() *turn {
	if len(t.turns) == 0 {
		return nil
	}
	return &t.turns[len(t.turns)-1]
}

// json is the thread as thread/read gives it, with the fields a client must
// not keep: the preview of the thread is its first message.
func (t *thread) json() map[string]any {
	status := map[string]any{"type": t.Status}
	if t.Status == "active" {
		status["activeFlags"] = append([]string{}, t.Flags...)
	}
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	return map[string]any{
		"id": t.ID, "sessionId": t.ID, "parentThreadId": nullable(t.Parent),
		"preview": "the first words of the conversation", "name": "a title made of the conversation",
		"ephemeral": false, "modelProvider": "openai", "model": nullable(t.Model),
		"reasoningEffort": nullable(t.Effort), "createdAt": t.Created, "updatedAt": t.Created,
		"status": status, "path": nullable(t.Path), "cwd": t.CWD, "cliVersion": "0.162.0",
		"source": "vscode", "turns": []any{},
	}
}
