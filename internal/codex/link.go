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
// notifications that carry it are turned off at the handshake, and the only
// words it holds are those a person decides on or reads to decide — the
// command or the change an approval asks about, the questions and the forms a
// thread waits on, the name and the goal of a thread, and the messages that
// wait in the panel's queue for a thread to be free.
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

// waitsForPerson are the requests of a daemon the link holds for a person.
var waitsForPerson = []string{methodCommand, methodFileChange, methodPermissions, methodUserInput, methodElicitation}

// quiet are the notifications a link turns off at the handshake. It reads the
// word that a request was answered, the status of a thread — a thread that
// turns free takes the next message of the panel's queue at once — its
// settings, its name and its goal, how full its context is and the rate limits
// of the account; none of them carries a word of the conversation beyond a
// name and a goal a person gives. Every other notification the
// protocol knows is off: most carry the conversation — items, their deltas,
// diffs, plans, the items of a finished turn — and the rest is traffic nobody
// reads. A notification a newer daemon adds arrives and is dropped unread.
var quiet = []string{
	"error", "thread/started", "thread/archived", "thread/deleted",
	"thread/unarchived", "thread/closed", "thread/reverted", "skills/changed",
	"thread/attachment/updated", "thread/prediction/updated",
	"thread/queue/changed", "project/changed", "thread/project/updated", "thread/environment/connected",
	"thread/environment/disconnected",
	"turn/started", "hook/started", "turn/completed", "hook/completed", "turn/diff/updated",
	"turn/plan/updated", "item/started", "item/autoApprovalReview/started",
	"item/autoApprovalReview/completed", "autoApprovalReview/strictReviewRequired", "item/completed",
	"rawResponseItem/completed", "rawResponse/completed", "item/agentMessage/delta", "item/plan/delta",
	"command/exec/outputDelta", "process/outputDelta", "process/exited",
	"item/commandExecution/outputDelta", "item/commandExecution/terminalInteraction",
	"item/fileChange/outputDelta", "item/fileChange/patchUpdated", "item/mcpToolCall/progress",
	"mcpServer/oauthLogin/completed", "mcpServer/startupStatus/updated",
	"mcpServer/event/stream/notification", "account/updated", "account/gatewayOAuth/changed",
	"app/list/updated", "remoteControl/status/changed",
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
// No text of the conversation is in it — not the preview of the thread — save
// what a person reads to decide or gave the thread: its name, its goal, and
// the question or the form it waits on. The file is the owner's alone.
type State struct {
	stream.Summary
	Agent     string `json:"agent"`
	CWD       string `json:"cwd"`
	Contour   string `json:"contour"`
	CodexHome string `json:"codexHome"`
	// Transcript is the rollout of the thread, as the daemon names it.
	Transcript string `json:"transcript"`
	// Plan says the thread plans rather than acts — codex's collaboration
	// mode, a thing of its own beside the permissions; absent while the
	// link has not been told.
	Plan *bool `json:"plan,omitempty"`
	// Context is how full the context of the thread is, as the daemon said
	// with the last request the link heard of; absent before that.
	Context *Context `json:"context,omitempty"`
	// Title is the name the panel gave the thread, while codex still calls the
	// thread by it; absent otherwise. A name codex made up by itself or was
	// given in its own terminal is not here: the protocol does not tell one
	// from a person's, and the row reads by the session of its project then,
	// as a claude session there does. The session keeps the name the panel
	// addresses it by.
	Title string `json:"title,omitempty"`
	// Goal is the goal of the thread; absent while it has none or the daemon
	// has not said.
	Goal *Goal `json:"goal,omitempty"`
	// Processes is how many background terminals of the thread run, as last
	// read; absent while it is not known.
	Processes *int `json:"processes,omitempty"`
	// Ask is the question or the form the thread waits on a person for, the
	// oldest first, in the shape the panel keeps a question of claude's in.
	Ask *Ask `json:"ask,omitempty"`
	// Terminal is the tmux session the panel started codex in on the thread,
	// empty for a thread no terminal of the panel holds.
	Terminal string `json:"terminal,omitempty"`
}

// Context is how full the context of a thread is, as the daemon said with the
// last request of a turn: the input of that request against the window of the
// model, the way the rollout counts it.
type Context struct {
	Tokens int64     `json:"tokens"`
	Window int64     `json:"window"`
	At     time.Time `json:"at"`
}

// Params are the params of a request that waits for a person, the part the
// panel shows. One struct reads every kind, each filling its own fields.
type Params struct {
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
	// Permissions are what a request for more permissions asks for, as it
	// came: the grant sends them back.
	Permissions json.RawMessage `json:"permissions"`
	// Questions are the questions of plan mode.
	Questions []Question `json:"questions"`
	Elicitation
}

// Request is a request of a daemon that waits for a person: an approval of a
// command or of a change to files, a grant of permissions, a question of plan
// mode, or what an MCP server asks.
type Request struct {
	ID     json.RawMessage
	Method string
	Params
	Since time.Time
}

// Key names the request across the panel: its id as the daemon gave it.
func (r Request) Key() string { return idKey(r.ID) }

// FileChange says the request is about a change to files rather than a command.
func (r Request) FileChange() bool { return r.Method == methodFileChange }

// Tool names the request the way the feed names the call: a command is Bash,
// a change to files is Edit, and a question or a form is the question of
// claude's, AskUserQuestion — what the row says a session waits for is read
// off these names.
func (r Request) Tool() string {
	switch {
	case r.Asks():
		return "AskUserQuestion"
	case r.FileChange():
		return "Edit"
	case r.Method == methodPermissions:
		return "Permissions"
	case r.Method == methodElicitation:
		return "MCP"
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
	Name      *string      `json:"name"`
	CreatedAt int64        `json:"createdAt"`
	UpdatedAt int64        `json:"updatedAt"`
	Status    threadStatus `json:"status"`
	Path      string       `json:"path"`
	CWD       string       `json:"cwd"`
}

type thread struct {
	info threadInfo
	// subscribed says the link is a client of the thread: it gets the
	// requests of the thread, and the daemon keeps the thread loaded for it.
	subscribed bool
	// ours says a turn the panel started runs: the link stays subscribed
	// until it ends, so an approval it asks for reaches the phone.
	ours bool
	// settings are what the daemon said of the thread's settings.
	settings settings
	// goal is the goal of the thread, goalKnown that the daemon said it.
	goal      *Goal
	goalKnown bool
	// processes is how many background terminals run, nil while not known;
	// extrasAt is when the goal and the terminals were last read, and
	// extrasSeen the time of the thread's last change then.
	processes  *int
	extrasAt   time.Time
	extrasSeen int64
	// terminal is the tmux session the panel started codex in on the thread.
	terminal string
	// given is the name the panel gave the thread, as kept on the disk; empty
	// while it gave none.
	given string
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

	// boxes are what the panel holds for threads until a turn takes it, the
	// messages of its queue among them; kept on the disk.
	boxes map[string]*outbox
	// usage is how full the context of each thread is, as last heard.
	usage map[string]Context
	// limits are the rate limits of the account, as last heard; limitsSaid
	// is the last failure to read them, logged once.
	limits     *Limits
	limitsSaid string
	// noGoals and noProcesses say the daemon of this connection knows no
	// goals or no background terminals, and is not asked them again.
	noGoals     bool
	noProcesses bool

	// held are the threads the panel started and stays a client of until it
	// closes them: nothing else holds such a thread, and the daemon unloads a
	// thread a while after its last client leaves. Each is marked on the disk
	// too, so an executor started again takes them back before the daemon
	// lets them go.
	held map[string]bool
	// closed are the threads the panel let go that the daemon has not unloaded
	// yet: they are no session any more, unless a turn runs in them again.
	// Each is kept with the turn the close interrupted.
	closed map[string]string

	// sub serialises what changes a subscription: the poll subscribing and
	// leaving, and a message starting a turn. A thread/unsubscribe crossing a
	// turn/start on the wire would take the panel off the turn it started, and
	// the approval that turn asks for would never reach the phone.
	sub sync.Mutex

	// terminals tells which threads codex runs in a tmux session of the
	// panel's; nil where nobody tells.
	terminals Terminals

	// wake asks Run to dial now rather than at the next round.
	wake chan struct{}
	// kick asks the connected link to read the daemon now: a thread turned
	// free, and a message of the queue waits for it.
	kick chan struct{}
}

// NewLink makes the link to the daemon of a codex home; Run keeps it.
func NewLink(home, contour string) *Link {
	return &Link{
		home: home, contour: contour, socket: SocketPath(home), holder: os.Getpid(),
		threads: map[string]*thread{}, pending: map[string][]Request{},
		held: map[string]bool{}, closed: map[string]string{}, wake: make(chan struct{}, 1),
		boxes: loadOutboxes(contour), usage: map[string]Context{}, kick: make(chan struct{}, 1),
	}
}

// Terminals tells which of the threads codex runs in a tmux session the panel
// started — codex resumed on the thread in a terminal — with the name of that
// session. The executor knows its terminals; the link asks at every poll.
type Terminals func(ctx context.Context, ids []string) (map[string]string, error)

// Links are the links of every codex home of the host.
type Links struct {
	list []*Link
	runs sync.WaitGroup
}

// Start removes the state files a stopped executor left behind and keeps a
// link to the daemon of every home until ctx ends; terminals, where given,
// tells which threads codex runs in a terminal of the panel.
func Start(ctx context.Context, homes []contours.CodexHome, terminals Terminals) *Links {
	Sweep()
	ls := &Links{}
	for _, h := range homes {
		l := NewLink(h.Dir, h.Contour)
		l.terminals = terminals
		ls.list = append(ls.list, l)
		ls.runs.Go(func() { l.Run(ctx) })
	}
	return ls
}

// Wait returns once every link has ended, its state files gone with it.
func (ls *Links) Wait() {
	ls.runs.Wait()
}

// Home is the link of a codex home, nil when the executor keeps none to it.
func (ls *Links) Home(dir string) *Link {
	if ls == nil {
		return nil
	}
	for _, l := range ls.list {
		if l.home == dir {
			return l
		}
	}
	return nil
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
		case <-l.wake:
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
	l.held = heldMarks(l.contour)
	// The daemon may have updated itself since the last connection: what the
	// old one did not know, the new one is asked again.
	l.noGoals, l.noProcesses = false, false
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
	var limitsAt time.Time
	for {
		if time.Since(limitsAt) >= limitsEvery {
			limitsAt = time.Now()
			l.readLimits(ctx, c)
		}
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
		case <-l.kick:
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
	// A thread started meanwhile is not in the list yet: what the panel keeps
	// of the unloaded ones is let go only while no thread is being started.
	l.sub.Lock()
	ids, err := loaded(ctx, c)
	if err == nil {
		l.unloaded(ids)
	}
	l.sub.Unlock()
	if err != nil {
		return err
	}
	// A failure to read the terminals leaves the threads as they were last
	// told, rather than moving every one out of its terminal for a round.
	var terms map[string]string
	termsErr := errors.New("nobody tells the terminals")
	if l.terminals != nil && len(ids) > 0 {
		terms, termsErr = l.terminals(ctx, ids)
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
		if l.letGo(ctx, c, id, info.Status.Type == "active") {
			continue
		}
		if !l.seen(info, asked) {
			continue
		}
		live[id] = true
		if termsErr == nil {
			l.set(id, func(t *thread) { t.terminal = terms[id] })
		}
		l.keep(ctx, c, id)
		l.drain(ctx, c, id)
		l.extras(ctx, c, id)
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
// answered or dropped with the turn, whatever the link heard of it. A thread
// the panel closed while it was being read is not taken back: what the daemon
// said of it is older than the close.
func (l *Link) seen(info threadInfo, asked time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, closed := l.closed[info.ID]; closed {
		return false
	}
	t := l.threads[info.ID]
	if t == nil {
		// The name the panel gave outlives the executor that gave it.
		t = &thread{given: named(info.ID)}
		l.threads[info.ID] = t
	}
	if info.Name == nil {
		// A rename the panel made is newer than a read that crossed it.
		info.Name = t.info.Name
	}
	t.info = info
	if t.info.Status.Type != "active" {
		l.pending[info.ID] = slices.DeleteFunc(l.pending[info.ID], func(r Request) bool {
			return r.Since.Before(asked)
		})
	}
	return true
}

// keep holds the link subscribed to a thread only while it is needed. The
// daemon unloads a thread a while after its last client leaves, and a link
// subscribed for good would keep every thread it ever saw loaded. So the link
// subscribes when the thread waits on an approval — the daemon then sends the
// request again, to the new client too — when a turn the panel started runs,
// or while messages of the panel's queue wait for the thread — the word that
// it turned free reaches only its clients — and leaves once the thread is
// free and nothing waits. A thread closed since it was seen is gone from the
// link, and there is nothing to keep.
func (l *Link) keep(ctx context.Context, c *conn, id string) {
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	t := l.threads[id]
	if t == nil {
		l.mu.Unlock()
		return
	}
	active := t.info.Status.Type == "active"
	waiting := slices.Contains(t.info.Status.Flags, flagApproval) || slices.Contains(t.info.Status.Flags, flagUserInput)
	asking := len(l.pending[id]) > 0
	subscribed, ours := t.subscribed, t.ours
	held := l.held[id]
	queued := l.queued(id) > 0
	l.mu.Unlock()

	switch {
	case held:
		// A thread the panel holds is left only when the panel closes it, and
		// joined again after the connection dropped.
		if !subscribed || (waiting && !asking) {
			l.join(ctx, c, id)
		}
	case !active && !asking && !queued:
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
		l.join(ctx, c, id)
	case !subscribed && (ours || asking || queued):
		l.join(ctx, c, id)
	}
}

// join makes the link a client of a thread: a resume of a loaded thread
// subscribes the one who asks, and brings what it waits on with it, and the
// settings of the thread with its answer.
func (l *Link) join(ctx context.Context, c *conn, id string) {
	var out settingsWire
	if within(ctx, c, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &out) == nil {
		l.set(id, func(t *thread) { t.subscribed, t.settings = true, out.read() })
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

// handle takes what the daemon sends on its own: the requests that wait for a
// person, the word that a request was answered, and the notifications the
// link reads. The other requests are left to the clients that know them —
// the TUI shows them, the panel does not answer them.
func (l *Link) handle(msg message) {
	switch {
	case len(msg.ID) == 0 && msg.Method == "thread/status/changed":
		var p struct {
			ThreadID string       `json:"threadId"`
			Status   threadStatus `json:"status"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.Status.Type == "" {
			return
		}
		// A thread that runs on after a question is answered says so with a
		// status of its own, and the row is busy again at once.
		if p.ThreadID != "" && p.Status.Type != "notLoaded" {
			l.set(p.ThreadID, func(t *thread) { t.info.Status = p.Status })
			l.save(p.ThreadID)
		}
		if p.Status.Type != "active" {
			l.nudge()
		}
	case len(msg.ID) == 0 && msg.Method == "thread/settings/updated":
		l.onSettings(msg.Params)
	case len(msg.ID) == 0 && msg.Method == "thread/tokenUsage/updated":
		l.onUsage(msg.Params)
	case len(msg.ID) == 0 && msg.Method == "account/rateLimits/updated":
		l.onLimits(msg.Params)
	case len(msg.ID) == 0 && msg.Method == "thread/name/updated":
		var p struct {
			ThreadID string  `json:"threadId"`
			Name     *string `json:"threadName"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.ThreadID == "" {
			return
		}
		name := ""
		if p.Name != nil {
			name = *p.Name
		}
		l.set(p.ThreadID, func(t *thread) { t.info.Name = &name })
		l.save(p.ThreadID)
	case len(msg.ID) == 0 && msg.Method == "thread/goal/updated":
		var p struct {
			ThreadID string `json:"threadId"`
			Goal     *Goal  `json:"goal"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.ThreadID != "" && p.Goal != nil {
			l.onGoal(p.ThreadID, p.Goal)
		}
	case len(msg.ID) == 0 && msg.Method == "thread/goal/cleared":
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.ThreadID != "" {
			l.onGoal(p.ThreadID, nil)
		}
	case len(msg.ID) > 0 && slices.Contains(waitsForPerson, msg.Method):
		var a Params
		if json.Unmarshal(msg.Params, &a) != nil || a.ThreadID == "" {
			return
		}
		r := Request{ID: msg.ID, Method: msg.Method, Params: a, Since: time.Now()}
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

// onSettings takes the settings of a thread another client — or the panel —
// changed: the daemon tells them whole to every client of the thread.
func (l *Link) onSettings(params json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		Settings struct {
			settingsWire
			Model  string  `json:"model"`
			Effort *string `json:"effort"`
		} `json:"threadSettings"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID == "" {
		return
	}
	l.set(p.ThreadID, func(t *thread) {
		t.settings = p.Settings.read()
		if p.Settings.Model != "" {
			t.info.Model = p.Settings.Model
		}
		if p.Settings.Effort != nil {
			t.info.Effort = *p.Settings.Effort
		}
	})
	l.save(p.ThreadID)
}

// onUsage takes how full the context of a thread is after a request of its
// turn.
func (l *Link) onUsage(params json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		Usage    struct {
			Last struct {
				Input int64 `json:"inputTokens"`
			} `json:"last"`
			Window *int64 `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID == "" {
		return
	}
	u := Context{Tokens: p.Usage.Last.Input, At: time.Now()}
	if p.Usage.Window != nil {
		u.Window = *p.Usage.Window
	}
	l.mu.Lock()
	l.usage[p.ThreadID] = u
	l.mu.Unlock()
	l.save(p.ThreadID)
}

// resolve forgets a request that was answered. The thread waits no more on
// what it asked: the flag the daemon showed for it goes with the last request
// of its kind, before any word of the daemon's on the status, which reaches
// only its clients and may come later.
func (l *Link) resolve(threadID, key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var gone []Request
	l.pending[threadID] = slices.DeleteFunc(l.pending[threadID], func(r Request) bool {
		if r.Key() == key {
			gone = append(gone, r)
			return true
		}
		return false
	})
	t := l.threads[threadID]
	if t == nil {
		return
	}
	for _, r := range gone {
		flag := r.flag()
		if slices.ContainsFunc(l.pending[threadID], func(o Request) bool { return o.flag() == flag }) {
			continue
		}
		t.info.Status.Flags = slices.DeleteFunc(slices.Clone(t.info.Status.Flags), func(f string) bool { return f == flag })
	}
}

// flag is the flag a thread shows while the request waits: a question of plan
// mode waits on the person's input, anything else on an approval.
func (r Request) flag() string {
	if r.Method == methodUserInput {
		return flagUserInput
	}
	return flagApproval
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
	var ask *Ask
	for _, r := range l.pending[t.info.ID] {
		waiting = append(waiting, r.Tool())
		if ask == nil {
			ask = r.Ask()
		}
	}
	// The thread waits on something the link holds no request for: one it
	// has not been sent yet, or one the panel does not answer. It waits on a
	// person all the same.
	if len(waiting) == 0 {
		waiting = append(waiting, t.info.Status.Flags...)
	}
	st := State{
		Summary: stream.Summary{
			Protocol: stream.Protocol, Name: SessionName(t.info.ID), SessionID: t.info.ID, Holder: l.holder,
			Started: time.Unix(t.info.CreatedAt, 0), Busy: t.info.Status.Type == "active",
			Model: t.info.Model, Effort: t.info.Effort, Mode: t.settings.mode(), Waiting: waiting,
			Queue: l.queued(t.info.ID),
		},
		Agent: Agent, CWD: t.info.CWD, Contour: l.contour, CodexHome: l.home, Transcript: t.info.Path,
	}
	if t.settings.planKnown {
		plan := t.settings.plan
		st.Plan = &plan
	}
	if u, ok := l.usage[t.info.ID]; ok {
		st.Context = &u
	}
	if t.info.Name != nil && t.given != "" && sameName(*t.info.Name, t.given) {
		st.Title = *t.info.Name
	}
	st.Goal, st.Processes, st.Ask, st.Terminal = t.goal, t.processes, ask, t.terminal
	return st
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
	delete(l.usage, id)
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

// Interrupt stops the turn a thread runs; false when none runs.
func (l *Link) Interrupt(ctx context.Context, threadID string) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	turn, err := interrupt(ctx, c, threadID)
	return turn != "", err
}

// Respond answers an approval of a thread with a decision, sent as it is.
func (l *Link) Respond(ctx context.Context, threadID, key string, decision json.RawMessage) error {
	return l.Reply(ctx, threadID, key, map[string]any{"decision": decision})
}

// Reply answers a request of a thread with the result its kind takes.
func (l *Link) Reply(ctx context.Context, threadID, key string, result any) error {
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
	if err := c.reply(ctx, id, result); err != nil {
		return err
	}
	l.resolve(threadID, key)
	l.save(threadID)
	return nil
}

// Model is one model of the catalogue a codex daemon lists: the name codex
// takes, the one a person reads, the efforts it takes and the one it starts at.
type Model struct {
	Model   string
	Name    string
	Efforts []string
	Effort  string
}

// Models lists the models codex offers, through the daemon of the contour's
// home, or of the first home that answers when no contour is named: the
// catalogue is codex's, and model/list asks no account, but a daemon serves
// the catalogue of its own release. The models codex keeps out of its own
// picker stay out of this list too: model/list leaves them out unless it is
// asked for them.
func (ls *Links) Models(ctx context.Context, contour string) ([]Model, error) {
	if ls == nil || len(ls.list) == 0 {
		return nil, errors.New("the executor knows no codex home")
	}
	if contour != "" {
		l := ls.Contour(contour)
		if l == nil {
			return nil, fmt.Errorf("contour %s has no codex home the executor keeps a link to", contour)
		}
		return l.models(ctx)
	}
	var last error
	for _, l := range ls.list {
		out, err := l.models(ctx)
		if err == nil {
			return out, nil
		}
		last = err
	}
	return nil, last
}

// Contour is the link of the codex home of a contour, nil when the executor
// keeps none.
func (ls *Links) Contour(name string) *Link {
	if ls == nil {
		return nil
	}
	for _, l := range ls.list {
		if l.contour == name {
			return l
		}
	}
	return nil
}

// Models lists the models the daemon of this home offers.
func (l *Link) Models(ctx context.Context) ([]Model, error) { return l.models(ctx) }

// Settings are the model, the effort and the mode a thread runs, as the link
// last heard them; the mode is empty while the daemon has not said it.
func (l *Link) Settings(threadID string) (model, effort, mode string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t := l.threads[threadID]; t != nil {
		return t.info.Model, t.info.Effort, t.settings.mode()
	}
	return "", "", ""
}

func (l *Link) models(ctx context.Context) ([]Model, error) {
	c, err := l.client()
	if err != nil {
		return nil, err
	}
	var out []Model
	cursor := ""
	for range 20 {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				Model       string `json:"model"`
				DisplayName string `json:"displayName"`
				Efforts     []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
				Effort string `json:"defaultReasoningEffort"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}
		if err := within(ctx, c, "model/list", params, &page); err != nil {
			return nil, fmt.Errorf("the codex daemon of %s did not list its models: %w", l.home, err)
		}
		for _, m := range page.Data {
			efforts := make([]string, 0, len(m.Efforts))
			for _, e := range m.Efforts {
				efforts = append(efforts, e.Effort)
			}
			out = append(out, Model{Model: m.Model, Name: m.DisplayName, Efforts: efforts, Effort: m.Effort})
		}
		if page.Next == "" || page.Next == cursor {
			break
		}
		cursor = page.Next
	}
	return out, nil
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
