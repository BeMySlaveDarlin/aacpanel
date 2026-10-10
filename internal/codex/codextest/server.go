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
	// Approval, Reviewer and Profile are the permissions of the thread: when
	// codex asks, who answers, and its permission profile — empty for a
	// thread started with a sandbox rather than a profile, whose Sandbox, a
	// type of the sandbox policy, says it then. Plan says the thread is in
	// plan mode. Add fills in what a thread started from a default
	// config.toml has.
	Approval string
	Reviewer string
	Profile  string
	Sandbox  string
	Plan     bool
}

// Model is a model of the catalogue the daemon lists.
type Model struct {
	ID      string
	Model   string
	Name    string
	Efforts []string
	Default string
	Hidden  bool
}

// modelPage is how many models one answer of model/list holds: fewer than a
// catalogue, so a client that reads one page sees only part of it.
const modelPage = 2

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
	// quiet are the notifications the client turned off at initialize.
	quiet map[string]bool
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
	models    []Model
	nextID    int
	turnN     int
	startN    int
	// refuse are the methods the daemon does not know, as an older or a
	// newer release would not.
	refuse map[string]bool
	// limits is what account/rateLimits/read answers.
	limits map[string]any
	// lag leaves a thread as it was after turn/start: the daemon answers
	// turn/start before the thread turns active.
	lag bool
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
	s := &Server{Home: home, threads: map[string]*thread{}, clients: map[*client]bool{}, refuse: map[string]bool{}}
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

// Catalogue sets the models the daemon lists.
func (s *Server) Catalogue(models ...Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = models
}

// Lag makes the daemon answer turn/start before the thread turns active, as
// the real one does: the turn is in progress, and the status says nothing yet
// until the test sets it.
func (s *Server) Lag() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lag = true
}

// Refuse makes the daemon refuse a method as one it does not know.
func (s *Server) Refuse(method string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refuse[method] = true
}

// Limits sets the snapshot of the rate limits account/rateLimits/read
// answers with, in the shape of the protocol.
func (s *Server) Limits(snapshot map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = snapshot
}

// LimitsUpdate sends account/rateLimits/updated with a snapshot that may be
// sparse, as the daemon does after a turn. It goes to every client.
func (s *Server) LimitsUpdate(snapshot map[string]any) {
	s.mu.Lock()
	var all []*client
	for c := range s.clients {
		all = append(all, c)
	}
	s.mu.Unlock()
	for _, c := range all {
		c.send(map[string]any{"method": "account/rateLimits/updated", "params": map[string]any{"rateLimits": snapshot}})
	}
}

// Usage tells the clients of a thread how full its context is, as the daemon
// does after every request of a turn.
func (s *Server) Usage(id string, input, window int64) {
	s.mu.Lock()
	subs := clientsOf(s.threads[id])
	s.mu.Unlock()
	breakdown := map[string]any{"totalTokens": input + 5, "inputTokens": input, "cachedInputTokens": 0,
		"cacheWriteInputTokens": 0, "outputTokens": 5, "reasoningOutputTokens": 0}
	for _, c := range subs {
		c.send(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{
			"threadId": id, "turnId": "turn-x",
			"tokenUsage": map[string]any{"total": breakdown, "last": breakdown, "modelContextWindow": window}}})
	}
}

// Update changes the settings of a thread as another client does, and tells
// the clients of the thread.
func (s *Server) Update(id string, change func(*Thread)) {
	s.mu.Lock()
	t := s.threads[id]
	change(&t.Thread)
	subs, note := clientsOf(t), t.settingsUpdated()
	s.mu.Unlock()
	for _, c := range subs {
		c.send(note)
	}
}

// Settings returns the thread as the daemon holds it now.
func (s *Server) Settings(id string) Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threads[id].Thread
}

// Add puts a thread on the daemon.
func (s *Server) Add(th Thread) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if th.Status == "" {
		th.Status = "idle"
	}
	permissions(&th)
	if _, ok := s.threads[th.ID]; !ok {
		s.order = append(s.order, th.ID)
	}
	s.threads[th.ID] = &thread{Thread: th, items: map[string][]map[string]any{}, subs: map[*client]bool{}}
}

// Set changes the status of a thread and tells its clients. A thread that is
// not active has no turn in progress: the one that ran is completed.
func (s *Server) Set(id, status string, flags ...string) {
	s.mu.Lock()
	t := s.threads[id]
	t.Status, t.Flags = status, flags
	if last := t.last(); last != nil && status != "active" && last.status == "inProgress" {
		last.status = "completed"
	}
	subs, note := clientsOf(t), t.statusChanged()
	s.mu.Unlock()
	for _, c := range subs {
		c.send(note)
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

// Starting gives a thread a turn in progress that has not made it active yet,
// as a turn another client has just started: the daemon answers turn/start
// before the thread turns active.
func (s *Server) Starting(id, turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[id]
	t.turns = append(t.turns, turn{id: turnID, status: "inProgress"})
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

// send writes a message to the client, unless it is a notification the
// client turned off.
func (c *client) send(msg map[string]any) {
	if _, request := msg["id"]; !request && c.quiet[fmt.Sprint(msg["method"])] {
		return
	}
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
	c := &client{ws: ws, quiet: map[string]bool{}}
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
		if msg.Method == "initialize" {
			var p struct {
				Capabilities struct {
					Quiet []string `json:"optOutNotificationMethods"`
				} `json:"capabilities"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			for _, m := range p.Capabilities.Quiet {
				c.quiet[m] = true
			}
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
			if to, ok := m["to"].([]*client); ok {
				delete(m, "to")
				for _, other := range to {
					other.send(m)
				}
				continue
			}
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
		Cursor         string `json:"cursor"`
		IncludeHidden  bool   `json:"includeHidden"`
	}
	_ = json.Unmarshal(raw, &p)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse[method] {
		return nil, nil, "Invalid request: unknown variant `" + method + "`, expected one of `initialize`, `thread/start`"
	}
	if method == "account/rateLimits/read" {
		if s.limits == nil {
			return nil, nil, "codex account authentication required to read rate limits"
		}
		return map[string]any{"rateLimits": s.limits, "rateLimitsByLimitId": nil, "ordinaryUsageAllowed": true}, nil, ""
	}
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
	if method == "model/list" {
		return s.modelList(p.Cursor, p.IncludeHidden), nil, ""
	}
	if method == "thread/start" {
		return s.start(c, raw), nil, ""
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
		answer := t.settingsAnswer()
		answer["thread"] = t.json()
		answer["collaborationMode"] = t.collaboration()
		return answer, after, ""
	case "thread/unsubscribe":
		delete(t.subs, c)
		return map[string]any{"status": "unsubscribed"}, nil, ""
	case "thread/name/set":
		return map[string]any{}, nil, ""
	case "turn/start":
		var q struct {
			Model  string `json:"model"`
			Effort string `json:"effort"`
		}
		_ = json.Unmarshal(raw, &q)
		if q.Model != "" {
			t.Model = q.Model
		}
		if q.Effort != "" {
			t.Effort = q.Effort
		}
		t.subs[c] = true
		s.turnN++
		id := fmt.Sprintf("turn-%d", s.turnN)
		t.turns = append(t.turns, turn{id: id, status: "inProgress"})
		answer := map[string]any{"turn": map[string]any{"id": id, "items": []any{}, "itemsView": "notLoaded",
			"status": "inProgress"}}
		if s.lag {
			return answer, nil, ""
		}
		t.Status = "active"
		note := t.statusChanged()
		note["to"] = clientsOf(t)
		return answer, []map[string]any{note}, ""
	case "thread/settings/update":
		t.apply(raw)
		note := t.settingsUpdated()
		note["to"] = clientsOf(t)
		return map[string]any{}, []map[string]any{note}, ""
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

// start makes a thread the way thread/start does: in the directory asked,
// with the model and the effort asked or the home's own, idle and with the
// caller as its client. Its id ends in the number of the thread.
func (s *Server) start(c *client, raw json.RawMessage) map[string]any {
	var p struct {
		CWD      string `json:"cwd"`
		Model    string `json:"model"`
		Approval string `json:"approvalPolicy"`
		Sandbox  string `json:"sandbox"`
		Config   struct {
			Effort string `json:"model_reasoning_effort"`
		} `json:"config"`
	}
	_ = json.Unmarshal(raw, &p)
	s.startN++
	th := Thread{ID: fmt.Sprintf("019a2000-0000-7000-8000-0000beef%04x", s.startN), CWD: p.CWD, Model: p.Model,
		Effort: p.Config.Effort, Status: "idle", Created: time.Now().Unix(), Approval: p.Approval,
		Sandbox: legacySandbox[p.Sandbox]}
	if th.Model == "" {
		th.Model = "gpt-home"
	}
	permissions(&th)
	s.order = append(s.order, th.ID)
	t := &thread{Thread: th, items: map[string][]map[string]any{}, subs: map[*client]bool{c: true}}
	s.threads[th.ID] = t
	answer := t.settingsAnswer()
	answer["thread"] = t.json()
	answer["cwd"] = th.CWD
	return answer
}

// legacySandbox is the type of the sandbox policy a sandbox mode of
// thread/start stands for.
var legacySandbox = map[string]string{"read-only": "readOnly", "workspace-write": "workspaceWrite",
	"danger-full-access": "dangerFullAccess"}

// profileSandbox is the sandbox policy a permission profile of codex's own
// comes down to.
var profileSandbox = map[string]string{":read-only": "readOnly", ":workspace": "workspaceWrite",
	":danger-full-access": "dangerFullAccess"}

// permissions fills in what a thread started from a default config.toml has:
// it asks on request, the person answers, and it writes in its workspace.
func permissions(th *Thread) {
	if th.Approval == "" {
		th.Approval = "on-request"
	}
	if th.Reviewer == "" {
		th.Reviewer = "user"
	}
	if th.Profile == "" && th.Sandbox == "" {
		th.Sandbox = "workspaceWrite"
	}
}

// sandbox is the policy of the thread: its profile's, or the one it started with.
func (t *thread) sandbox() map[string]any {
	if t.Profile != "" {
		return map[string]any{"type": profileSandbox[t.Profile]}
	}
	return map[string]any{"type": t.Sandbox}
}

func (t *thread) profile() any {
	if t.Profile == "" {
		return nil
	}
	return map[string]any{"id": t.Profile, "extends": nil}
}

func (t *thread) collaboration() map[string]any {
	mode := "default"
	if t.Plan {
		mode = "plan"
	}
	return map[string]any{"mode": mode, "settings": map[string]any{"model": t.Model,
		"reasoning_effort": nullable(t.Effort), "developer_instructions": "the built-in words of the mode"}}
}

// settingsAnswer is what the answers of thread/start and thread/resume say of
// the settings: the start says no collaboration mode.
func (t *thread) settingsAnswer() map[string]any {
	return map[string]any{"model": t.Model, "modelProvider": "openai", "approvalPolicy": t.Approval,
		"approvalsReviewer": t.Reviewer, "sandbox": t.sandbox(), "activePermissionProfile": t.profile(),
		"reasoningEffort": nullable(t.Effort)}
}

// settingsUpdated is the notification of the thread's settings, whole.
func (t *thread) settingsUpdated() map[string]any {
	return map[string]any{"method": "thread/settings/updated", "params": map[string]any{"threadId": t.ID,
		"threadSettings": map[string]any{"cwd": t.CWD, "approvalPolicy": t.Approval, "approvalsReviewer": t.Reviewer,
			"sandboxPolicy": t.sandbox(), "activePermissionProfile": t.profile(), "model": t.Model,
			"modelProvider": "openai", "effort": nullable(t.Effort), "collaborationMode": t.collaboration()}}}
}

func (t *thread) statusChanged() map[string]any {
	status := map[string]any{"type": t.Status}
	if t.Status == "active" {
		status["activeFlags"] = append([]string{}, t.Flags...)
	}
	return map[string]any{"method": "thread/status/changed", "params": map[string]any{"threadId": t.ID,
		"status": status}}
}

// apply takes thread/settings/update the way the daemon does: whatever is
// named, with no check of a model or an effort — the daemon takes any name.
// A collaboration mode brings its model and effort with it.
func (t *thread) apply(raw json.RawMessage) {
	var p struct {
		Model       *string `json:"model"`
		Effort      *string `json:"effort"`
		Permissions *string `json:"permissions"`
		Approval    *string `json:"approvalPolicy"`
		Reviewer    *string `json:"approvalsReviewer"`
		Mode        *struct {
			Mode     string `json:"mode"`
			Settings struct {
				Model  string  `json:"model"`
				Effort *string `json:"reasoning_effort"`
			} `json:"settings"`
		} `json:"collaborationMode"`
	}
	_ = json.Unmarshal(raw, &p)
	for to, from := range map[*string]*string{&t.Model: p.Model, &t.Effort: p.Effort, &t.Profile: p.Permissions,
		&t.Approval: p.Approval, &t.Reviewer: p.Reviewer} {
		if from != nil {
			*to = *from
		}
	}
	if p.Mode != nil {
		t.Plan = p.Mode.Mode == "plan"
		t.Model = p.Mode.Settings.Model
		t.Effort = ""
		if p.Mode.Settings.Effort != nil {
			t.Effort = *p.Mode.Settings.Effort
		}
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// modelList is a page of the catalogue as model/list gives it, the cursor
// being where the page starts. A hidden model is listed only when asked for.
func (s *Server) modelList(cursor string, hidden bool) map[string]any {
	var shown []Model
	for _, m := range s.models {
		if hidden || !m.Hidden {
			shown = append(shown, m)
		}
	}
	from := 0
	if cursor != "" {
		_, _ = fmt.Sscanf(cursor, "at-%d", &from)
	}
	data := []any{}
	var next any
	for i := from; i < len(shown); i++ {
		if len(data) == modelPage {
			next = fmt.Sprintf("at-%d", i)
			break
		}
		m := shown[i]
		efforts := []any{}
		for _, e := range m.Efforts {
			efforts = append(efforts, map[string]any{"reasoningEffort": e, "description": "thinks " + e})
		}
		data = append(data, map[string]any{
			"id": m.ID, "model": m.Model, "displayName": m.Name, "description": "a model of the catalogue",
			"hidden": m.Hidden, "isDefault": i == 0, "supportedReasoningEfforts": efforts,
			"defaultReasoningEffort": m.Default, "inputModalities": []string{"text", "image"},
		})
	}
	return map[string]any{"data": data, "nextCursor": next}
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
	return map[string]any{
		"id": t.ID, "sessionId": t.ID, "parentThreadId": nullable(t.Parent),
		"preview": "the first words of the conversation", "name": "a title made of the conversation",
		"ephemeral": false, "modelProvider": "openai", "model": nullable(t.Model),
		"reasoningEffort": nullable(t.Effort), "createdAt": t.Created, "updatedAt": t.Created,
		"status": status, "path": nullable(t.Path), "cwd": t.CWD, "cliVersion": "0.162.0",
		"source": "vscode", "turns": []any{},
	}
}
