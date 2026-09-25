package stream

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// How long claude may take to answer the handshake. It reads its
	// settings, plugins and MCP servers first, and a cold start with a few of
	// them is seconds.
	initWait = 90 * time.Second
	// How long claude may take to bring Remote Control up: it registers the
	// session with claude.ai before it answers.
	remoteWait = 60 * time.Second
	// How long a control request may wait for its answer.
	controlWait = 30 * time.Second
	// A question aside is answered by the model, and thinking takes as long
	// as it takes; the panel's own wait for the executor ends a little later.
	sideWait = 110 * time.Second
	// How long a closed session gets to finish its turn and write its
	// transcript before it is asked to stop harder.
	closeWait = 20 * time.Second
	// The largest request the socket reads. A message is text a person typed.
	maxRequest = 1 << 20
	// What of claude's own complaints is kept for the log when it dies at
	// start. Past the first minute it is nothing: claude is running, and what
	// it says after that is not the holder's to keep.
	stderrKeep = 4 << 10
	startGrace = time.Minute
)

// Holder keeps one claude session on the stream protocol.
type Holder struct {
	spec  Spec
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex

	mu    sync.Mutex
	state State
	// One writer of the state file at a time. Two writing it at once through
	// one temporary file leave whichever renamed last — not the newest — and a
	// collector reading it would see a question already gone or not yet there.
	saveMu  sync.Mutex
	waiters map[string]chan json.RawMessage
	seq     int
	closed  bool

	stderr *tail
	log    *os.File
	// Where this holder's files are, fixed at its start: a write that comes
	// late — an answer arriving as the session ends — goes where the holder
	// began, or nowhere, never wherever the environment points by then.
	statePath string
	// Set when the holder has cleaned up: nothing is written after that, or
	// a state file would outlive the session it describes.
	finished bool
	// A session that ended cleanly leaves no log behind: the log is for the
	// launch that failed, read by the launcher to say why.
	clean bool
}

// Run starts claude under a holder and keeps it until claude ends. It returns
// when the session is over.
func Run(ctx context.Context, spec Spec) error {
	if err := checkSpec(spec); err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("the directory of the stream sessions was not made: %w", err)
	}
	ln, err := listen(spec.SessionID)
	if err != nil {
		return err
	}
	logFile, _ := os.OpenFile(LogPath(spec.SessionID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)

	h := &Holder{
		spec:      spec,
		waiters:   map[string]chan json.RawMessage{},
		stderr:    &tail{limit: stderrKeep},
		log:       logFile,
		statePath: StatePath(spec.SessionID),
		state: State{
			Protocol:  Protocol,
			Name:      spec.Name,
			SessionID: spec.SessionID,
			Holder:    os.Getpid(),
			Started:   time.Now(),
			Pending:   []Pending{},
			Queue:     []Queued{},
			Tasks:     []Task{},
		},
	}
	defer h.cleanup(ln)

	if err := h.start(); err != nil {
		h.logf("claude did not start: %v", err)
		return err
	}
	h.logf("claude started, pid %d", h.cmd.Process.Pid)
	h.saveSummary()

	go h.serve(ln)
	go h.handshake()

	exit := make(chan error, 1)
	go func() { exit <- h.cmd.Wait() }()

	select {
	case err := <-exit:
		h.exited(err)
		return nil
	case <-ctx.Done():
		h.closeStdin()
		select {
		case err := <-exit:
			h.exited(err)
		case <-time.After(closeWait):
			_ = h.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case err := <-exit:
				h.exited(err)
			case <-time.After(5 * time.Second):
				_ = h.cmd.Process.Kill()
				h.exited(<-exit)
			}
		}
		return nil
	}
}

func checkSpec(spec Spec) error {
	switch {
	case !uuidLike(spec.SessionID):
		return fmt.Errorf("the session id %q is not a uuid", spec.SessionID)
	case strings.TrimSpace(spec.Name) == "":
		return errors.New("the session has no name")
	case !filepath.IsAbs(spec.Dir):
		return fmt.Errorf("the project directory %q is not absolute", spec.Dir)
	case len(spec.Argv) == 0:
		return errors.New("there is no command to start")
	}
	return nil
}

// listen takes the socket of a conversation. A socket left by a holder that
// died is taken over; one a live holder answers on is not — two holders of one
// conversation would write into one transcript from two processes.
func listen(sessionID string) (net.Listener, error) {
	path := SocketPath(sessionID)
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("the conversation %s is already held by a live session", sessionID)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("the socket of the session was not opened: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("the socket of the session was not closed to others: %w", err)
	}
	return ln, nil
}

func (h *Holder) start() error {
	cmd := exec.Command(h.spec.Argv[0], h.spec.Argv[1:]...)
	cmd.Dir = h.spec.Dir
	cmd.Stderr = h.stderr
	// Its own process group: a signal meant for the holder is not a signal to
	// the session, and the session ends by its stdin being closed.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	h.cmd, h.stdin = cmd, stdin
	h.mu.Lock()
	h.state.PID = cmd.Process.Pid
	h.mu.Unlock()
	go h.read(stdout)
	return nil
}

// handshake asks claude what it is and, once it has answered, sends the
// first message. A message sent before the handshake is read all the same,
// but a session that failed to start would then take a message nobody knows
// was lost.
func (h *Holder) handshake() {
	resp, err := h.control(context.Background(), "initialize", nil, initWait)
	if err != nil {
		h.logf("claude did not answer the handshake: %v", err)
		return
	}
	var body struct {
		Response json.RawMessage `json:"response"`
	}
	_ = json.Unmarshal(resp, &body)
	var init struct {
		Mode string `json:"current_permission_mode"`
	}
	_ = json.Unmarshal(body.Response, &init)
	h.mu.Lock()
	h.state.Init = body.Response
	if init.Mode != "" {
		h.state.Mode = init.Mode
		if h.state.StartMode == "" {
			h.state.StartMode = init.Mode
		}
	}
	h.mu.Unlock()
	h.saveSummary()
	if h.spec.RemoteControl {
		if _, err := h.control(context.Background(), "remote_control", map[string]any{"enabled": true}, remoteWait); err != nil {
			h.logf("remote control was not switched on: %v", err)
		}
	}
	if strings.TrimSpace(h.spec.Intent) != "" {
		if _, err := h.send(h.spec.Intent, ""); err != nil {
			h.logf("the opening message was not sent: %v", err)
		}
	}
}

// ------------------------------------------------------------ reading claude

type event struct {
	Type           string          `json:"type"`
	Subtype        string          `json:"subtype"`
	RequestID      string          `json:"request_id"`
	Request        json.RawMessage `json:"request"`
	Response       json.RawMessage `json:"response"`
	UUID           string          `json:"uuid"`
	Model          string          `json:"model"`
	PermissionMode string          `json:"permissionMode"`
	Tasks          []Task          `json:"tasks"`
	Message        json.RawMessage `json:"message"`
	Status         json.RawMessage `json:"status"`
	CompactResult  string          `json:"compact_result"`
	CommandUUID    string          `json:"command_uuid"`
	State          string          `json:"state"`
}

type toolAsk struct {
	Subtype     string          `json:"subtype"`
	ToolName    string          `json:"tool_name"`
	ToolUseID   string          `json:"tool_use_id"`
	Input       json.RawMessage `json:"input"`
	Description string          `json:"description"`
	Reason      string          `json:"decision_reason"`
	Suggestions json.RawMessage `json:"permission_suggestions"`
}

// read follows what claude writes. Everything that is not the state of the
// session or a request to it is dropped as it passes: the conversation is the
// transcript's, and the holder keeps no copy.
func (h *Holder) read(stdout io.Reader) {
	r := bufio.NewReaderSize(stdout, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var ev event
			if json.Unmarshal(line, &ev) == nil {
				h.handle(ev)
			}
		}
		if err != nil {
			return
		}
	}
}

func (h *Holder) handle(ev event) {
	switch ev.Type {
	case "control_request":
		h.onRequest(ev)
	case "control_response":
		h.onResponse(ev)
	case "control_cancel_request":
		h.dropPending(ev.RequestID)
	case "user":
		h.onUser(ev)
	case "command_lifecycle":
		h.onCommand(ev)
	case "system":
		h.onSystem(ev)
	case "result":
		h.mu.Lock()
		h.state.Busy = false
		// A turn that ended waits for nobody: a request left from it was
		// answered or withdrawn with the turn.
		h.state.Pending = []Pending{}
		// Nor does it compact: a compaction interrupted ends with the turn,
		// and claude says nothing else about it.
		h.state.Compacting = nil
		h.mu.Unlock()
		h.saveSummary()
	}
}

func (h *Holder) onRequest(ev event) {
	var ask toolAsk
	_ = json.Unmarshal(ev.Request, &ask)
	if ask.Subtype != "can_use_tool" {
		// Nothing else is registered by the panel: no hooks of its own, no
		// MCP servers inside the session. A request it cannot answer is
		// refused at once rather than left to hang the turn.
		h.write(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "error", "request_id": ev.RequestID,
			"error": fmt.Sprintf("the panel does not answer %s requests", ask.Subtype),
		}})
		return
	}
	h.mu.Lock()
	h.state.Pending = append(h.state.Pending, Pending{
		RequestID: ev.RequestID, Tool: ask.ToolName, ToolUseID: ask.ToolUseID, Input: ask.Input,
		Description: ask.Description, Reason: ask.Reason, Suggestions: ask.Suggestions, Since: time.Now(),
	})
	h.mu.Unlock()
	h.saveSummary()
}

func (h *Holder) onResponse(ev event) {
	var head struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(ev.Response, &head)
	h.mu.Lock()
	ch := h.waiters[head.RequestID]
	delete(h.waiters, head.RequestID)
	h.mu.Unlock()
	if ch != nil {
		ch <- ev.Response
	}
}

// onUser notices that a message sent while claude was busy has been taken up:
// claude echoes a message back when it reads it.
func (h *Holder) onUser(ev event) {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(ev.Message, &msg)
	var text string
	_ = json.Unmarshal(msg.Content, &text)
	if h.take(ev.UUID, text) {
		h.saveSummary()
	}
}

// onCommand notices that claude has taken up a message by the lifecycle it
// reports for it. A slash command claude runs itself — /cost, or /plugins it
// turns down — is never echoed back as a user message, and without this word
// it would stay in the queue for good and stand in the way of a switch.
//
// "started" is also where a turn begins that nobody sent a message for right
// now: a message left in the queue when the turn before it ended, a scheduled
// prompt, a turn resumed — claude starts them itself. The session is busy from
// that word to the result, or a switch would take it for free and cut the
// answer off.
func (h *Holder) onCommand(ev event) {
	switch ev.State {
	case "started":
		h.mu.Lock()
		h.state.Busy = true
		h.mu.Unlock()
		h.take(ev.CommandUUID, "")
		h.saveSummary()
	case "completed":
		if h.take(ev.CommandUUID, "") {
			h.saveSummary()
		}
	}
}

// take removes a message claude has read from the queue, found by its id or,
// for an echo that lost it, by its text, and says whether it was there.
func (h *Holder) take(id, text string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, q := range h.state.Queue {
		if (id != "" && q.UUID == id) || (text != "" && q.Text == text) {
			h.state.Queue = append(h.state.Queue[:i], h.state.Queue[i+1:]...)
			return true
		}
	}
	return false
}

func (h *Holder) onSystem(ev event) {
	h.mu.Lock()
	switch ev.Subtype {
	case "init":
		if ev.Model != "" {
			h.state.Model = ev.Model
		}
		if ev.PermissionMode != "" {
			h.state.Mode = ev.PermissionMode
		}
	case "status":
		if ev.PermissionMode != "" {
			h.state.Mode = ev.PermissionMode
		}
		h.compacting(ev)
	case "compact_boundary":
		h.state.Compacting = nil
	case "background_tasks_changed":
		h.state.Tasks = append([]Task{}, ev.Tasks...)
	default:
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.saveSummary()
}

// compacting follows a compaction by the status claude reports: "compacting"
// when it starts and again every half a minute while it runs, anything else
// once it is over. The start is kept from the first report — the repeats would
// set the clock back. A status that carries a mode and no outcome of a
// compaction reports a change of mode, not the end of one. Called under h.mu.
func (h *Holder) compacting(ev event) {
	var status *string
	if len(ev.Status) == 0 || json.Unmarshal(ev.Status, &status) != nil {
		return
	}
	if status != nil && *status == "compacting" {
		if h.state.Compacting == nil {
			now := time.Now()
			h.state.Compacting = &now
		}
		return
	}
	if ev.PermissionMode != "" && ev.CompactResult == "" {
		return
	}
	h.state.Compacting = nil
}

func (h *Holder) dropPending(requestID string) {
	h.mu.Lock()
	for i, p := range h.state.Pending {
		if p.RequestID == requestID {
			h.state.Pending = append(h.state.Pending[:i], h.state.Pending[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
	h.saveSummary()
}

// ------------------------------------------------------------ writing claude

func (h *Holder) write(obj any) error {
	line, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.closed {
		return errors.New("the session is closing and takes no more input")
	}
	_, err = h.stdin.Write(append(line, '\n'))
	return err
}

// clearing are the commands that start a conversation under a new id. The
// holder keeps its conversation by id — its socket, its state file, the panel
// finding the session — so a clear would leave a session nobody holds.
var clearing = map[string]bool{"/clear": true, "/reset": true, "/new": true}

func (h *Holder) send(text, id string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("the message is empty")
	}
	if fields := strings.Fields(text); clearing[fields[0]] {
		return "", fmt.Errorf("%s starts a conversation under a new id, and the session would drop off the panel: "+
			"close it and open a new one instead", fields[0])
	}
	if id == "" {
		id = newUUID()
	} else if !uuidLike(id) {
		return "", fmt.Errorf("the message id %q is not a uuid", id)
	}
	msg := map[string]any{
		"type": "user", "session_id": h.spec.SessionID, "parent_tool_use_id": nil, "uuid": id,
		"message": map[string]any{"role": "user", "content": text},
	}
	h.mu.Lock()
	h.state.Queue = append(h.state.Queue, Queued{UUID: id, Text: text, Since: time.Now()})
	h.state.Busy = true
	h.mu.Unlock()
	if err := h.write(msg); err != nil {
		h.mu.Lock()
		h.state.Queue = h.state.Queue[:len(h.state.Queue)-1]
		h.mu.Unlock()
		return "", err
	}
	h.picked(text)
	h.saveSummary()
	return id, nil
}

// cancelled reads whether claude took a message back from its queue.
func cancelled(resp json.RawMessage) bool {
	var body struct {
		Response struct {
			Cancelled bool `json:"cancelled"`
		} `json:"response"`
	}
	return json.Unmarshal(resp, &body) == nil && body.Response.Cancelled
}

// unqueue forgets a message claude took back: it will never be read, and a
// queue that still held it would stand in the way of a switch for good.
func (h *Holder) unqueue(id string) {
	h.mu.Lock()
	kept := h.state.Queue[:0]
	var gone []string
	for _, q := range h.state.Queue {
		if q.UUID != id {
			kept = append(kept, q)
			continue
		}
		gone = append(gone, q.Text)
	}
	h.state.Queue = kept
	h.mu.Unlock()
	h.saveSummary()
	for _, text := range gone {
		if err := withdraw(h.spec.SessionID, text); err != nil {
			h.logf("a message taken back was not marked as such: %v", err)
		}
	}
}

// withdraw adds the fingerprint of a message taken back to the list of its
// conversation. A message sent twice and taken back once is listed once: the
// feed marks only as many of its copies as were taken back.
func withdraw(sessionID, text string) error {
	return rewrite(WithdrawnPath(sessionID), func(list []string) []string {
		return append(list, Fingerprint(text))
	})
}

// keptMu keeps the files that outlive the holder to one writer at a time: two
// answers given at once would each write the list back without the other.
var keptMu sync.Mutex

// rewrite reads a list kept in a file, changes it and puts it back whole, so a
// reader never finds half of it.
func rewrite[T any](path string, change func([]T) []T) error {
	keptMu.Lock()
	defer keptMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var list []T
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &list)
	}
	body, err := json.Marshal(change(list))
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// picked remembers a model or an effort chosen by a message: a slash command
// is a message on the stream, whether the panel sent it or a person typed it.
func (h *Holder) picked(text string) {
	fields := strings.Fields(text)
	if len(fields) != 2 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch fields[0] {
	case "/model":
		h.state.Picked = fields[1]
	case "/effort":
		h.state.Effort = fields[1]
	}
}

func (h *Holder) respond(requestID string, response json.RawMessage) error {
	if len(response) == 0 || !json.Valid(response) {
		return errors.New("the answer is not JSON")
	}
	h.mu.Lock()
	var asked *Pending
	for _, p := range h.state.Pending {
		if p.RequestID == requestID {
			asked = &p
			break
		}
	}
	h.mu.Unlock()
	if asked == nil {
		return fmt.Errorf("the session is not asking %q — it was answered or withdrawn", requestID)
	}
	// The answer is on the disk before claude has it: the collector parses
	// a record once, and a result read before its permit stays without one.
	permit, kept := permitOf(*asked, response)
	if kept {
		if err := h.keepPermit(permit); err != nil {
			h.logf("an answer to a permission was not kept for the feed: %v", err)
			kept = false
		}
	}
	err := h.write(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": requestID, "response": response,
	}})
	if err != nil {
		if kept {
			h.dropPermit(permit.Use)
		}
		return err
	}
	h.dropPending(requestID)
	return nil
}

// permitOf reads the answer to a permission the feed shows. A question is
// not one: its answer is in the transcript, and the feed has a card for it.
func permitOf(p Pending, response json.RawMessage) (Permit, bool) {
	if p.Tool == "AskUserQuestion" || p.ToolUseID == "" {
		return Permit{}, false
	}
	var answer struct {
		Behavior string            `json:"behavior"`
		Rules    []json.RawMessage `json:"updatedPermissions"`
	}
	if json.Unmarshal(response, &answer) != nil || (answer.Behavior != "allow" && answer.Behavior != "deny") {
		return Permit{}, false
	}
	return Permit{
		Use: p.ToolUseID, Tool: p.Tool, Decision: answer.Behavior,
		Lasting: answer.Behavior == "allow" && len(answer.Rules) > 0,
	}, true
}

func (h *Holder) keepPermit(p Permit) error {
	return rewrite(PermitsPath(h.spec.SessionID), func(list []Permit) []Permit {
		return append(list, p)
	})
}

// dropPermit takes back an answer claude never got.
func (h *Holder) dropPermit(use string) {
	err := rewrite(PermitsPath(h.spec.SessionID), func(list []Permit) []Permit {
		kept := list[:0]
		for _, p := range list {
			if p.Use != use {
				kept = append(kept, p)
			}
		}
		return kept
	})
	if err != nil {
		h.logf("an answer claude never got is still kept for the feed: %v", err)
	}
}

func (h *Holder) control(ctx context.Context, subtype string, fields map[string]any, wait time.Duration) (json.RawMessage, error) {
	h.mu.Lock()
	h.seq++
	id := fmt.Sprintf("panel-%d", h.seq)
	ch := make(chan json.RawMessage, 1)
	h.waiters[id] = ch
	h.mu.Unlock()

	req := map[string]any{"subtype": subtype}
	for k, v := range fields {
		if k != "subtype" {
			req[k] = v
		}
	}
	if err := h.write(map[string]any{"type": "control_request", "request_id": id, "request": req}); err != nil {
		h.forget(id)
		return nil, err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("claude ended before it answered %s", subtype)
		}
		var head struct {
			Subtype string `json:"subtype"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(resp, &head)
		if head.Subtype == "error" {
			return resp, fmt.Errorf("claude refused %s: %s", subtype, head.Error)
		}
		return resp, nil
	case <-time.After(wait):
		h.forget(id)
		return nil, fmt.Errorf("claude did not answer %s in %s", subtype, wait)
	case <-ctx.Done():
		h.forget(id)
		return nil, ctx.Err()
	}
}

func (h *Holder) forget(id string) {
	h.mu.Lock()
	delete(h.waiters, id)
	h.mu.Unlock()
}

func (h *Holder) closeStdin() {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if !h.closed {
		h.closed = true
		_ = h.stdin.Close()
	}
}

// ------------------------------------------------------------ the socket

func (h *Holder) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go h.answer(conn)
	}
}

func (h *Holder) answer(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(controlWait + 5*time.Second))
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Reply{Error: "the request was not parsed: " + err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(h.do(req))
}

func (h *Holder) do(req Request) Reply {
	switch req.Op {
	case OpState:
		s := h.snapshot()
		return Reply{OK: true, State: &s}
	case OpSend:
		id, err := h.send(req.Text, req.UUID)
		if err != nil {
			return Reply{Error: err.Error()}
		}
		return Reply{OK: true, UUID: id}
	case OpRespond:
		if err := h.respond(req.RequestID, req.Response); err != nil {
			return Reply{Error: err.Error()}
		}
		return Reply{OK: true}
	case OpControl:
		if !Controls[req.Subtype] {
			return Reply{Error: fmt.Sprintf("%q is not a request the panel passes on", req.Subtype)}
		}
		if err := vetSettings(req.Subtype, req.Fields); err != nil {
			return Reply{Error: err.Error()}
		}
		resp, err := h.control(context.Background(), req.Subtype, req.Fields, replyWait(req.Subtype))
		resp = trimAnswer(req.Subtype, resp)
		if err != nil {
			return Reply{Error: err.Error(), Response: resp}
		}
		if req.Subtype == "apply_flag_settings" {
			if err := h.flagsApplied(context.Background(), req.Fields); err != nil {
				return Reply{Error: err.Error()}
			}
		}
		if model, ok := req.Fields["model"].(string); ok && req.Subtype == "set_model" {
			h.picked("/model " + model)
			h.saveSummary()
		}
		// claude reports a new mode by an event of its own, but a mode the
		// person has just been told was set is not left to arrive later.
		if mode, ok := req.Fields["mode"].(string); ok && req.Subtype == "set_permission_mode" {
			h.mu.Lock()
			h.state.Mode = mode
			h.mu.Unlock()
			h.saveSummary()
		}
		if id, ok := req.Fields["message_uuid"].(string); ok && req.Subtype == "cancel_async_message" && cancelled(resp) {
			h.unqueue(id)
		}
		return Reply{OK: true, Response: resp}
	case OpClose:
		h.closeStdin()
		return Reply{OK: true}
	}
	return Reply{Error: fmt.Sprintf("there is no such operation: %q", req.Op)}
}

func (h *Holder) snapshot() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.state
	s.Pending = append([]Pending{}, h.state.Pending...)
	s.Queue = append([]Queued{}, h.state.Queue...)
	s.Tasks = append([]Task{}, h.state.Tasks...)
	return s
}

// ------------------------------------------------------------ the state file

func (h *Holder) saveSummary() {
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	if h.finished {
		return
	}
	s := h.snapshot()
	sum := Summary{
		Protocol: s.Protocol, Name: s.Name, SessionID: s.SessionID, PID: s.PID, Holder: s.Holder,
		Started: s.Started, Busy: s.Busy, Model: s.Model, Mode: s.Mode, Effort: s.Effort, Waiting: []string{},
		Queue: len(s.Queue), Tasks: len(s.Tasks), Compacting: s.Compacting, Updated: time.Now(),
	}
	for _, p := range s.Pending {
		sum.Waiting = append(sum.Waiting, p.Tool)
	}
	body, err := json.Marshal(sum)
	if err != nil {
		return
	}
	path := h.statePath
	tmp := path + ".tmp"
	if os.WriteFile(tmp, body, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func (h *Holder) exited(err error) {
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	h.logf("claude ended, exit %d", code)
	failed := failedStart(code, time.Since(h.state.Started))
	h.clean = !failed
	if failed {
		if said := strings.TrimSpace(h.stderr.String()); said != "" {
			h.logf("what it said on the way out:\n%s", said)
		}
	}
	h.mu.Lock()
	for id, ch := range h.waiters {
		close(ch)
		delete(h.waiters, id)
	}
	h.mu.Unlock()
}

// failedStart says whether a session ended as a launch that did not take: an
// error within its first minute. Anything else is a session that lived — the
// panel closes one with a signal, and its exit code says so — and leaves no
// log behind.
func failedStart(code int, lived time.Duration) bool {
	return code != 0 && lived < startGrace
}

func (h *Holder) cleanup(ln net.Listener) {
	h.saveMu.Lock()
	h.finished = true
	h.saveMu.Unlock()
	_ = ln.Close()
	_ = os.Remove(SocketPath(h.spec.SessionID))
	_ = os.Remove(h.statePath)
	if h.log != nil {
		_ = h.log.Close()
	}
	if h.clean {
		_ = os.Remove(LogPath(h.spec.SessionID))
	}
}

func (h *Holder) logf(format string, args ...any) {
	if h.log == nil {
		return
	}
	fmt.Fprintf(h.log, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

// tail keeps the last bytes written to it.
type tail struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// ------------------------------------------------------------ ids

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewSessionID returns an id for a new conversation.
func NewSessionID() string { return newUUID() }

func uuidLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
	}
	return true
}
