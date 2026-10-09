// Package codex keeps the panel's link to the codex app-server daemons of the
// contours.
//
// A codex conversation is a thread of a daemon that serves every client of
// its CODEX_HOME — a TUI started with --remote, an editor, the panel. The
// daemon is the holder here: it outlives the clients and keeps the turn and
// the requests that wait for a person, so the panel needs no process of its
// own beside a codex session. What the executor keeps is a connection to the
// daemon of each home and, per thread, the state file the collector reads to
// put the session on the map, in the shape of a claude holder's.
//
// The executor stays blind to the conversation the way it is with claude: the
// notifications that carry it are turned off at the handshake, the state file
// carries no text, and the only words it holds are those a person decides on —
// the command or the change an approval asks about.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/contours"
	"aacpanel/internal/stream"
)

// Agent is what a state file of a codex thread says it is.
const Agent = "codex"

// How often the threads of a daemon are read, and how long a link waits
// before it dials a daemon again after a failure or while none runs.
var (
	pollEvery   = 2 * time.Second
	redialEvery = 10 * time.Second
	callWait    = 15 * time.Second
)

const (
	methodCommand    = "item/commandExecution/requestApproval"
	methodFileChange = "item/fileChange/requestApproval"
	flagApproval     = "waitingOnApproval"
)

// quiet are the notifications a link turns off at the handshake. It reads one
// notification, serverRequest/resolved, and every other one the protocol
// knows is off: most carry the conversation — items, their deltas, diffs,
// plans, the items of a finished turn — and the rest is traffic nobody reads.
// A notification a newer daemon adds arrives and is dropped unread.
var quiet = []string{
	"error", "thread/started", "thread/status/changed", "thread/archived", "thread/deleted",
	"thread/unarchived", "thread/closed", "thread/reverted", "skills/changed", "thread/name/updated",
	"thread/attachment/updated", "thread/goal/updated", "thread/prediction/updated", "thread/goal/cleared",
	"thread/queue/changed", "project/changed", "thread/project/updated", "thread/environment/connected",
	"thread/environment/disconnected", "thread/settings/updated", "thread/tokenUsage/updated",
	"turn/started", "hook/started", "turn/completed", "hook/completed", "turn/diff/updated",
	"turn/plan/updated", "item/started", "item/autoApprovalReview/started",
	"item/autoApprovalReview/completed", "autoApprovalReview/strictReviewRequired", "item/completed",
	"rawResponseItem/completed", "rawResponse/completed", "item/agentMessage/delta", "item/plan/delta",
	"command/exec/outputDelta", "process/outputDelta", "process/exited",
	"item/commandExecution/outputDelta", "item/commandExecution/terminalInteraction",
	"item/fileChange/outputDelta", "item/fileChange/patchUpdated", "item/mcpToolCall/progress",
	"mcpServer/oauthLogin/completed", "mcpServer/startupStatus/updated",
	"mcpServer/event/stream/notification", "account/updated", "account/gatewayOAuth/changed",
	"account/rateLimits/updated", "app/list/updated", "remoteControl/status/changed",
	"externalAgentConfig/import/progress", "externalAgentConfig/import/completed", "fs/changed",
	"item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded", "item/reasoning/textDelta",
	"thread/compacted", "model/rerouted", "model/verification", "modelProvider/authRecoveryStarted",
	"modelProvider/authRecoveryCompleted", "turn/moderationMetadata", "model/safetyBuffering/updated",
	"warning", "guardianWarning", "deprecationNotice", "configWarning", "fuzzyFileSearch/sessionUpdated",
	"fuzzyFileSearch/sessionCompleted", "thread/realtime/started", "thread/realtime/itemAdded",
	"thread/realtime/item/started", "thread/realtime/item/transcript/delta",
	"thread/realtime/item/completed", "thread/realtime/transcript/delta",
	"thread/realtime/transcript/done", "thread/realtime/outputAudio/delta", "thread/realtime/sdp",
	"thread/realtime/error", "thread/realtime/closed", "windows/worldWritableWarning",
	"windowsSandbox/setupCompleted", "account/login/completed",
}

// ErrAnswered is what an answer to a request finds when somebody was faster:
// a request goes to every client of the thread, and the first answer wins.
var ErrAnswered = errors.New("the request is answered already")

// SocketPath is the control socket of the daemon of a codex home.
func SocketPath(home string) string {
	return filepath.Join(home, "app-server-control", "app-server-control.sock")
}

// SessionName is what the panel calls a thread: the tail of its id. The head
// of a UUIDv7 is the time it was made, and threads started together share it.
func SessionName(threadID string) string {
	hex := strings.ReplaceAll(threadID, "-", "")
	if len(hex) > 8 {
		hex = hex[len(hex)-8:]
	}
	return "codex-" + hex
}

// State is the state file of a codex thread: the summary a claude holder
// writes, with what the collector needs to find the rollout and the contour.
// No text of the conversation is in it — not the preview of the thread, not
// its name.
type State struct {
	stream.Summary
	Agent     string `json:"agent"`
	CWD       string `json:"cwd"`
	Contour   string `json:"contour"`
	CodexHome string `json:"codexHome"`
	// Transcript is the rollout of the thread, as the daemon names it.
	Transcript string `json:"transcript"`
}

// Approval is what a request for an approval asks: the params of the request,
// the part the panel shows.
type Approval struct {
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	ItemID    string `json:"itemId"`
	Command   string `json:"command"`
	CWD       string `json:"cwd"`
	Reason    string `json:"reason"`
	GrantRoot string `json:"grantRoot"`
	Network   *struct {
		Host     string `json:"host"`
		Protocol string `json:"protocol"`
	} `json:"networkApprovalContext"`
	// Decisions are the answers the daemon takes, in its order; absent from
	// an older daemon and from a change to files.
	Offered []json.RawMessage `json:"availableDecisions"`
}

// Request is a request of a daemon that waits for a person: an approval of a
// command or of a change to files.
type Request struct {
	ID     json.RawMessage
	Method string
	Approval
	Since time.Time
}

// Key names the request across the panel: its id as the daemon gave it.
func (r Request) Key() string { return idKey(r.ID) }

// FileChange says the request is about a change to files rather than a command.
func (r Request) FileChange() bool { return r.Method == methodFileChange }

// Tool names the request the way the feed names the call: a command is Bash,
// a change to files is Edit.
func (r Request) Tool() string {
	if r.FileChange() {
		return "Edit"
	}
	return "Bash"
}

// Decisions are the answers the request takes. A request that names none
// takes the four every daemon knows.
func (r Request) Decisions() []json.RawMessage {
	if len(r.Offered) > 0 {
		return r.Offered
	}
	var out []json.RawMessage
	for _, d := range []string{"accept", "acceptForSession", "decline", "cancel"} {
		out = append(out, json.RawMessage(`"`+d+`"`))
	}
	return out
}

// idKey is a request id as a string: the daemon numbers its requests, and a
// string id is taken as it is.
func idKey(id json.RawMessage) string {
	var s string
	if json.Unmarshal(id, &s) == nil {
		return s
	}
	return string(bytes.TrimSpace(id))
}

// Change is one file of a change codex asks to make.
type Change struct {
	Path string `json:"path"`
	Kind struct {
		Type     string `json:"type"`
		MovePath string `json:"move_path"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

type threadStatus struct {
	Type  string   `json:"type"`
	Flags []string `json:"activeFlags"`
}

// threadInfo is what thread/read says of a thread, the part the panel keeps.
type threadInfo struct {
	ID        string       `json:"id"`
	Parent    string       `json:"parentThreadId"`
	Model     string       `json:"model"`
	Effort    string       `json:"reasoningEffort"`
	CreatedAt int64        `json:"createdAt"`
	Status    threadStatus `json:"status"`
	Path      string       `json:"path"`
	CWD       string       `json:"cwd"`
}

type thread struct {
	info threadInfo
	// subscribed says the link is a client of the thread: it gets the
	// requests of the thread, and the daemon keeps the thread loaded for it.
	subscribed bool
	// ours says a turn the panel started or steered runs: the link stays
	// subscribed until it ends, so an approval it asks for reaches the phone.
	ours bool
	// written is the state last written, without its time.
	written []byte
}

// Link is the connection to the daemon of one codex home.
type Link struct {
	home    string
	contour string
	socket  string
	holder  int

	mu      sync.Mutex
	conn    *conn
	threads map[string]*thread
	pending map[string][]Request
	said    string

	// sub serialises what changes a subscription: the poll subscribing and
	// leaving, and a message starting a turn. A thread/unsubscribe crossing a
	// turn/start on the wire would take the panel off the turn it started, and
	// the approval that turn asks for would never reach the phone.
	sub sync.Mutex
}

// NewLink makes the link to the daemon of a codex home; Run keeps it.
func NewLink(home, contour string) *Link {
	return &Link{
		home: home, contour: contour, socket: SocketPath(home), holder: os.Getpid(),
		threads: map[string]*thread{}, pending: map[string][]Request{},
	}
}

// Links are the links of every codex home of the host.
type Links struct {
	list []*Link
	runs sync.WaitGroup
}

// Start removes the state files a stopped executor left behind and keeps a
// link to the daemon of every home until ctx ends.
func Start(ctx context.Context, homes []contours.CodexHome) *Links {
	Sweep()
	ls := &Links{}
	for _, h := range homes {
		l := NewLink(h.Dir, h.Contour)
		ls.list = append(ls.list, l)
		ls.runs.Go(func() { l.Run(ctx) })
	}
	return ls
}

// Wait returns once every link has ended, its state files gone with it.
func (ls *Links) Wait() {
	ls.runs.Wait()
}

// Thread is a thread of a daemon, as the panel names it.
type Thread struct {
	Link *Link
	ID   string
	Name string
}

// Find returns the threads the panel calls by the name.
func (ls *Links) Find(name string) []Thread {
	if ls == nil {
		return nil
	}
	var out []Thread
	for _, l := range ls.list {
		l.mu.Lock()
		for id := range l.threads {
			if SessionName(id) == name {
				out = append(out, Thread{Link: l, ID: id, Name: name})
			}
		}
		l.mu.Unlock()
	}
	return out
}

// Sweep removes the state files of codex threads whose executor is gone. A
// file outlives an executor stopped hard, and the threads it names may have
// ended since; the executor that runs now writes its own.
func Sweep() {
	entries, err := os.ReadDir(stream.Dir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(stream.Dir(), e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file struct {
			Agent  string `json:"agent"`
			Holder int    `json:"holder"`
		}
		if json.Unmarshal(raw, &file) != nil || file.Agent != Agent {
			continue
		}
		if file.Holder <= 0 || !alive(file.Holder) {
			_ = os.Remove(path)
		}
	}
}

func alive(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

// Run keeps the link until ctx ends: it dials the daemon when its socket is
// there, reads its threads while the connection lasts, and dials again a
// while after it drops. The state files of the link go with the connection —
// whatever the daemon ran is not known without it.
func (l *Link) Run(ctx context.Context) {
	defer l.forget()
	for {
		l.report(l.serve(ctx))
		l.forget()
		select {
		case <-ctx.Done():
			return
		case <-time.After(redialEvery):
		}
	}
}

// report logs a change of what the link has to say, not every attempt.
func (l *Link) report(err error) {
	said := ""
	if err != nil {
		said = err.Error()
	}
	if said == l.said {
		return
	}
	l.said = said
	if err != nil {
		log.Printf("codex %s: %v", l.home, err)
	}
}

func (l *Link) serve(ctx context.Context) error {
	// The socket in the home is a link to the one codex keeps under /tmp, and
	// it is dialled where it really lies: the address of a unix socket holds
	// 108 bytes, and a home deep enough in the tree passes them, which is why
	// codex keeps the socket itself on a short path.
	socket, err := filepath.EvalSymlinks(l.socket)
	if err != nil {
		return nil
	}
	dctx, cancel := context.WithTimeout(ctx, callWait)
	c, err := dial(dctx, socket, l.handle)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("the daemon did not take the connection: %w", err)
	}
	defer c.close()
	if err := initialize(ctx, c); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	l.mu.Lock()
	l.conn = c
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.conn = nil
		l.mu.Unlock()
	}()
	if l.said != "connected" {
		l.said = "connected"
		log.Printf("codex %s: connected to its daemon", l.home)
	}

	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		if err := l.poll(ctx, c); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-c.closed:
			return c.lost()
		case <-tick.C:
		}
	}
}

func initialize(ctx context.Context, c *conn) error {
	params := map[string]any{
		"clientInfo": map[string]any{"name": "aacpanel", "title": "aacpanel", "version": "1"},
		"capabilities": map[string]any{
			"experimentalApi": true, "requestAttestation": false, "optOutNotificationMethods": quiet,
		},
	}
	if err := within(ctx, c, "initialize", params, nil); err != nil {
		return err
	}
	return c.notify(ctx, "initialized")
}

// within is a call bounded in time: a daemon that stops answering holds the
// poll, and with it every action over its threads, no longer than that.
func within(ctx context.Context, c *conn, method string, params, out any) error {
	ctx, cancel := context.WithTimeout(ctx, callWait)
	defer cancel()
	return c.call(ctx, method, params, out)
}

// poll reads the loaded threads of the daemon and brings the state files and
// the subscriptions to them. A subagent's thread is part of its parent's turn
// and is not a session of its own.
func (l *Link) poll(ctx context.Context, c *conn) error {
	ids, err := loaded(ctx, c)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, id := range ids {
		asked := time.Now()
		info, err := read(ctx, c, id)
		if err != nil {
			if c.down() {
				return c.lost()
			}
			continue
		}
		if info.Parent != "" || info.Status.Type == "notLoaded" || info.Status.Type == "" {
			continue
		}
		live[id] = true
		l.seen(info, asked)
		l.keep(ctx, c, id)
		l.save(id)
	}
	l.drop(live)
	return nil
}

func loaded(ctx context.Context, c *conn) ([]string, error) {
	var ids []string
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []string `json:"data"`
			Next string   `json:"nextCursor"`
		}
		if err := within(ctx, c, "thread/loaded/list", params, &page); err != nil {
			return nil, err
		}
		ids = append(ids, page.Data...)
		if page.Next == "" || page.Next == cursor {
			return ids, nil
		}
		cursor = page.Next
	}
}

func read(ctx context.Context, c *conn, id string) (threadInfo, error) {
	var out struct {
		Thread threadInfo `json:"thread"`
	}
	err := within(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &out)
	return out.Thread, err
}

// seen takes what the daemon said of a thread. A thread whose turn is over
// waits on nothing: a request that came before the question was asked was
// answered or dropped with the turn, whatever the link heard of it.
func (l *Link) seen(info threadInfo, asked time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.threads[info.ID]
	if t == nil {
		t = &thread{}
		l.threads[info.ID] = t
	}
	t.info = info
	if info.Status.Type != "active" {
		l.pending[info.ID] = slices.DeleteFunc(l.pending[info.ID], func(r Request) bool {
			return r.Since.Before(asked)
		})
	}
}

// keep holds the link subscribed to a thread only while it is needed. The
// daemon unloads a thread a while after its last client leaves, and a link
// subscribed for good would keep every thread it ever saw loaded. So the link
// subscribes when the thread waits on an approval — the daemon then sends the
// request again, to the new client too — or when a turn the panel started
// runs, and leaves once the thread is free and nothing waits.
func (l *Link) keep(ctx context.Context, c *conn, id string) {
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	t := l.threads[id]
	active := t.info.Status.Type == "active"
	waiting := slices.Contains(t.info.Status.Flags, flagApproval)
	asking := len(l.pending[id]) > 0
	subscribed, ours := t.subscribed, t.ours
	l.mu.Unlock()

	switch {
	case !active && !asking:
		if !subscribed {
			l.set(id, func(t *thread) { t.ours = false })
			return
		}
		// The state was read before the lock, and a message sent since may
		// have started a turn: the thread is asked again before the link
		// leaves it. A turn the panel has just started may not show even
		// then — the daemon answers turn/start before the thread turns active.
		if info, err := read(ctx, c, id); err != nil || info.Status.Type == "active" {
			return
		}
		if ours {
			if running, _ := runningTurn(ctx, c, id); running != "" {
				return
			}
		}
		if subscribed && within(ctx, c, "thread/unsubscribe", map[string]any{"threadId": id}, nil) != nil {
			return
		}
		l.set(id, func(t *thread) { t.subscribed, t.ours = false, false })
	case waiting && !asking:
		// A thread waits and the link holds no request of it: whatever the
		// link believes of its subscription, the daemon has not counted it in
		// — a turn/start that went into a running turn subscribes nobody. A
		// resume of a running thread joins it again and brings the waiting
		// request with it.
		if within(ctx, c, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, nil) == nil {
			l.set(id, func(t *thread) { t.subscribed = true })
		}
	case !subscribed && (ours || asking):
		if within(ctx, c, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, nil) == nil {
			l.set(id, func(t *thread) { t.subscribed = true })
		}
	}
}

func (l *Link) set(id string, change func(*thread)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t := l.threads[id]; t != nil {
		change(t)
	}
}

// runningTurn is the turn of a thread in progress, empty when none runs. The
// items of the turn are not asked for.
func runningTurn(ctx context.Context, c *conn, id string) (string, error) {
	var out struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	params := map[string]any{"threadId": id, "limit": 1, "itemsView": "notLoaded"}
	if err := within(ctx, c, "thread/turns/list", params, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 || out.Data[0].Status != "inProgress" {
		return "", nil
	}
	return out.Data[0].ID, nil
}

// handle takes what the daemon sends on its own: the requests for an approval
// and the word that a request was answered. The other requests are left to the
// clients that know them — the TUI shows them, the panel does not answer them.
func (l *Link) handle(msg message) {
	switch {
	case len(msg.ID) > 0 && (msg.Method == methodCommand || msg.Method == methodFileChange):
		var a Approval
		if json.Unmarshal(msg.Params, &a) != nil || a.ThreadID == "" {
			return
		}
		r := Request{ID: msg.ID, Method: msg.Method, Approval: a, Since: time.Now()}
		l.mu.Lock()
		list := slices.DeleteFunc(l.pending[a.ThreadID], func(p Request) bool { return p.Key() == r.Key() })
		l.pending[a.ThreadID] = append(list, r)
		l.mu.Unlock()
		l.save(a.ThreadID)
	case len(msg.ID) == 0 && msg.Method == "serverRequest/resolved":
		var p struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		l.resolve(p.ThreadID, idKey(p.RequestID))
		l.save(p.ThreadID)
	}
}

func (l *Link) resolve(threadID, key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending[threadID] = slices.DeleteFunc(l.pending[threadID], func(r Request) bool { return r.Key() == key })
}

// save writes the state file of a thread when what it says has changed.
func (l *Link) save(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.threads[id]
	if t == nil {
		return
	}
	st := l.state(t)
	body, err := json.Marshal(st)
	if err != nil || bytes.Equal(body, t.written) {
		return
	}
	st.Updated = time.Now()
	full, err := json.Marshal(st)
	if err != nil {
		return
	}
	if write(stream.StatePath(id), full) == nil {
		t.written = body
	}
}

func (l *Link) state(t *thread) State {
	waiting := []string{}
	for _, r := range l.pending[t.info.ID] {
		waiting = append(waiting, r.Tool())
	}
	// The thread waits on something the link holds no request for: one it
	// has not been sent yet, or one the panel does not answer. It waits on a
	// person all the same.
	if len(waiting) == 0 {
		waiting = append(waiting, t.info.Status.Flags...)
	}
	return State{
		Summary: stream.Summary{
			Protocol: stream.Protocol, Name: SessionName(t.info.ID), SessionID: t.info.ID, Holder: l.holder,
			Started: time.Unix(t.info.CreatedAt, 0), Busy: t.info.Status.Type == "active",
			Model: t.info.Model, Effort: t.info.Effort, Waiting: waiting,
		},
		Agent: Agent, CWD: t.info.CWD, Contour: l.contour, CodexHome: l.home, Transcript: t.info.Path,
	}
}

func write(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// drop forgets the threads the daemon no longer has loaded, and the requests
// of threads that are not on the map — a subagent's thread asks too.
func (l *Link) drop(live map[string]bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.threads {
		if !live[id] {
			l.remove(id)
		}
	}
	for id := range l.pending {
		if !live[id] {
			delete(l.pending, id)
		}
	}
}

// forget drops every thread of the link: the connection is gone, and with it
// what the link knew.
func (l *Link) forget() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.threads {
		l.remove(id)
	}
	l.pending = map[string][]Request{}
}

func (l *Link) remove(id string) {
	_ = os.Remove(stream.StatePath(id))
	delete(l.threads, id)
	delete(l.pending, id)
}

// ------------------------------------------------------------ what the executor asks

func (l *Link) client() (*conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil || l.conn.down() {
		return nil, fmt.Errorf("the codex daemon of %s is not connected", l.home)
	}
	return l.conn, nil
}

// Pending returns the requests of a thread that wait for a person, the oldest first.
func (l *Link) Pending(threadID string) []Request {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.pending[threadID])
}

// Send gives a thread a message. A free thread starts a turn with it; a busy
// one takes it into the turn that runs, the way a person types into a working
// TUI. It reports whether the message went into a running turn.
func (l *Link) Send(ctx context.Context, threadID, text, messageID string) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	params := map[string]any{
		"threadId": threadID,
		"input":    []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}},
	}
	if messageID != "" {
		params["clientUserMessageId"] = messageID
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	info, err := read(ctx, c, threadID)
	if err != nil {
		return false, err
	}
	if info.Status.Type == "active" {
		turn, err := runningTurn(ctx, c, threadID)
		if err != nil {
			return false, err
		}
		if turn != "" {
			params["expectedTurnId"] = turn
			if err := within(ctx, c, "turn/steer", params, nil); err != nil {
				return false, err
			}
			l.set(threadID, func(t *thread) { t.ours = true })
			return true, nil
		}
	}
	if err := within(ctx, c, "turn/start", params, nil); err != nil {
		return false, err
	}
	// turn/start makes the caller a client of the thread.
	l.set(threadID, func(t *thread) { t.ours, t.subscribed = true, true })
	return false, nil
}

// Interrupt stops the turn a thread runs; false when none runs.
func (l *Link) Interrupt(ctx context.Context, threadID string) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	info, err := read(ctx, c, threadID)
	if err != nil {
		return false, err
	}
	if info.Status.Type != "active" {
		return false, nil
	}
	turn, err := runningTurn(ctx, c, threadID)
	if err != nil || turn == "" {
		return false, err
	}
	if err := within(ctx, c, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turn}, nil); err != nil {
		return false, err
	}
	return true, nil
}

// Respond answers a request of a thread with a decision, sent as it is.
func (l *Link) Respond(ctx context.Context, threadID, key string, decision json.RawMessage) error {
	c, err := l.client()
	if err != nil {
		return err
	}
	var id json.RawMessage
	for _, r := range l.Pending(threadID) {
		if r.Key() == key {
			id = r.ID
		}
	}
	if id == nil {
		return ErrAnswered
	}
	if err := c.reply(ctx, id, map[string]any{"decision": decision}); err != nil {
		return err
	}
	l.resolve(threadID, key)
	l.save(threadID)
	return nil
}

// Changes reads the change to files an approval asks about: the request names
// the item, and the item holds the change.
func (l *Link) Changes(ctx context.Context, threadID, turnID, itemID string) ([]Change, error) {
	c, err := l.client()
	if err != nil {
		return nil, err
	}
	cursor := ""
	for range 20 {
		params := map[string]any{"threadId": threadID, "turnId": turnID, "sortDirection": "desc"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				Item struct {
					Type    string   `json:"type"`
					ID      string   `json:"id"`
					Changes []Change `json:"changes"`
				} `json:"item"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}
		if err := within(ctx, c, "thread/items/list", params, &page); err != nil {
			return nil, err
		}
		for _, e := range page.Data {
			if e.Item.ID == itemID && e.Item.Type == "fileChange" {
				return e.Item.Changes, nil
			}
		}
		if page.Next == "" || page.Next == cursor {
			break
		}
		cursor = page.Next
	}
	return nil, fmt.Errorf("codex does not show the change it asks about")
}
