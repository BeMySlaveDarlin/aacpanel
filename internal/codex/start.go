package codex

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"aacpanel/internal/mcp"
	"aacpanel/internal/stream"
)

// readyPoll is how often a wait for the link asks whether it is connected.
var readyPoll = 50 * time.Millisecond

// Begin is what a new thread starts with. A choice left empty is not sent,
// and the config.toml of the home decides it.
type Begin struct {
	CWD      string
	Model    string
	Approval string
	Sandbox  string
	// Effort is the thread's from its start, and goes with the first turn.
	Effort string
	// Message is the first message; without one no turn starts.
	Message string
	// Hold keeps the panel a client of the thread until it closes it.
	// Nothing else holds a thread the panel alone talks to, and the daemon
	// unloads a thread a while after its last client leaves.
	Hold bool
	// Name is given to the thread at its start. Naming writes the thread to
	// the disk — the daemon writes a thread at its first turn or its naming —
	// and codex in a terminal resumes only a thread written there. It is the
	// panel's name, shown as a rename is, until codex calls the thread
	// otherwise.
	Name string
	// Tools is the program of the panel's MCP server the thread gets, beside
	// the servers of its config.toml; empty gives the thread only those. A
	// home whose config.toml names the server already gets the same one.
	Tools string
	// Parent is the session that had the panel start the thread, by its
	// name; empty for a thread a person started.
	Parent string
}

// toolsConfig is the panel's MCP server as a thread's configuration names
// it, key by key: a key of its own overrides the same key of config.toml and
// leaves the rest of the server as config.toml has it.
func toolsConfig(program string) map[string]any {
	server := "mcp_servers." + mcp.ServerName
	return map[string]any{server + ".command": program, server + ".args": []string{mcp.Flag}}
}

// Up says whether the daemon of the home has its control socket: whether
// there is a daemon to connect to at all.
func (l *Link) Up() bool {
	_, err := filepath.EvalSymlinks(l.socket)
	return err == nil
}

// Home is the codex home the link is to.
func (l *Link) Home() string { return l.home }

// Ready waits until the link holds a connection to its daemon, dialling at
// once rather than at the next round.
func (l *Link) Ready(ctx context.Context) error {
	for {
		if _, err := l.client(); err == nil {
			return nil
		}
		select {
		case l.wake <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the codex daemon of %s did not take the panel's connection — the executor's log says why", l.home)
		case <-time.After(readyPoll):
		}
	}
}

// Start starts a thread in the daemon and, with a first message, its first
// turn. It returns the id of the thread; a first message that did not go is
// an error beside a thread that did start.
func (l *Link) Start(ctx context.Context, b Begin) (string, error) {
	c, err := l.client()
	if err != nil {
		return "", err
	}
	params := map[string]any{"cwd": b.CWD}
	for key, value := range map[string]string{"model": b.Model, "approvalPolicy": b.Approval, "sandbox": b.Sandbox} {
		if value != "" {
			params[key] = value
		}
	}
	config := map[string]any{}
	if b.Effort != "" {
		config["model_reasoning_effort"] = b.Effort
	}
	if b.Tools != "" {
		maps.Copy(config, toolsConfig(b.Tools))
	}
	if len(config) > 0 {
		params["config"] = config
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	var out struct {
		Thread threadInfo `json:"thread"`
		settingsWire
	}
	if err := within(ctx, c, "thread/start", params, &out); err != nil {
		return "", err
	}
	id := out.Thread.ID
	if id == "" {
		return "", fmt.Errorf("the codex daemon of %s started a thread and did not name it", l.home)
	}
	l.seen(out.Thread, time.Now())
	// thread/start makes the caller a client of the thread, and its answer
	// says the settings the thread starts with; a thread starts out of plan
	// mode, which the answer does not say.
	settled := out.read()
	settled.planKnown = true
	l.set(id, func(t *thread) { t.subscribed, t.settings = true, settled })
	if b.Hold {
		l.hold(id)
	}
	if b.Parent != "" {
		l.adopt(id, b.Parent)
	}
	l.save(id)
	if b.Name != "" {
		if err := within(ctx, c, "thread/name/set", map[string]any{"threadId": id, "name": b.Name}, nil); err != nil {
			return id, fmt.Errorf("the thread was not named, and codex in a terminal cannot resume it: %w", err)
		}
		l.give(id, b.Name)
	}
	if b.Message == "" {
		return id, nil
	}
	turn := map[string]any{"threadId": id, "input": textInput(b.Message)}
	if b.Effort != "" {
		turn["effort"] = b.Effort
	}
	if err := within(ctx, c, "turn/start", turn, nil); err != nil {
		return id, fmt.Errorf("the first message did not go: %w", err)
	}
	l.set(id, func(t *thread) { t.ours = true })
	return id, nil
}

// Resume joins a thread the daemon has on the disk and holds it, as the panel
// holds a thread it started: the daemon loads a thread a client resumes and
// keeps it while the client stays, and nothing else holds a thread the panel
// alone talks to. A thread the panel closed before is its session again. It
// returns the directory the thread runs in. A subagent's thread is part of its
// parent's turn and is not resumed: the panel leaves it at once.
func (l *Link) Resume(ctx context.Context, threadID string) (string, error) {
	c, err := l.client()
	if err != nil {
		return "", err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	var out struct {
		Thread threadInfo `json:"thread"`
		settingsWire
	}
	if err := within(ctx, c, "thread/resume", map[string]any{"threadId": threadID, "excludeTurns": true}, &out); err != nil {
		return "", err
	}
	if out.Thread.ID != threadID {
		return "", fmt.Errorf("the codex daemon of %s resumed thread %q in place of %s", l.home, out.Thread.ID, threadID)
	}
	if out.Thread.Parent != "" {
		_ = within(ctx, c, "thread/unsubscribe", map[string]any{"threadId": threadID}, nil)
		return "", fmt.Errorf("thread %s is a subagent's, part of the turn of thread %s: it is not resumed on its own",
			threadID, out.Thread.Parent)
	}
	l.mu.Lock()
	delete(l.closed, threadID)
	l.mu.Unlock()
	l.seen(out.Thread, time.Now())
	settled := out.read()
	l.set(threadID, func(t *thread) { t.subscribed, t.settings = true, settled })
	l.hold(threadID)
	l.save(threadID)
	return out.Thread.CWD, nil
}

// Close lets a thread go: the turn it runs is interrupted, and the panel
// stops holding it and leaves it. The daemon unloads the thread once no
// client is left; its rollout stays, and codex resume brings it back. It
// reports whether a turn was interrupted.
func (l *Link) Close(ctx context.Context, threadID string) (bool, error) {
	c, err := l.client()
	if err != nil {
		return false, err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	turn, err := interrupt(ctx, c, threadID)
	if err != nil {
		return false, err
	}
	l.unhold(threadID)
	if err := within(ctx, c, "thread/unsubscribe", map[string]any{"threadId": threadID}, nil); err != nil {
		return turn != "", err
	}
	l.mu.Lock()
	l.closed[threadID] = turn
	l.remove(threadID)
	delete(l.boxes, threadID)
	l.keepOutbox(threadID)
	l.mu.Unlock()
	return turn != "", nil
}

// interrupt stops the turn a thread runs and returns its id, empty when none
// runs.
func interrupt(ctx context.Context, c *conn, threadID string) (string, error) {
	info, err := read(ctx, c, threadID)
	if err != nil {
		return "", err
	}
	if info.Status.Type != "active" {
		return "", nil
	}
	turn, err := runningTurn(ctx, c, threadID)
	if err != nil || turn == "" {
		return "", err
	}
	err = within(ctx, c, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turn}, nil)
	// A review runs an inner turn under the root turn the thread lists, and
	// the daemon interrupts only the inner one, naming it in its refusal.
	if m := innerTurn.FindStringSubmatch(errText(err)); m != nil && refused(err) {
		turn = m[1]
		err = within(ctx, c, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turn}, nil)
	}
	if err != nil {
		return "", err
	}
	return turn, nil
}

// innerTurn reads the turn the daemon interrupts out of its refusal of
// another one.
var innerTurn = regexp.MustCompile(`expected active turn id \S+ but found (\S+)`)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// letGo says a thread the panel closed is no session of its own any more: it
// stays out of the panel until the daemon unloads it, unless another client
// starts a turn in it — the turn interrupted at the close is not that.
func (l *Link) letGo(ctx context.Context, c *conn, id string, active bool) bool {
	l.mu.Lock()
	stopped, closed := l.closed[id]
	l.mu.Unlock()
	if !closed {
		return false
	}
	if !active {
		return true
	}
	if turn, err := runningTurn(ctx, c, id); err != nil || turn == "" || turn == stopped {
		return true
	}
	l.mu.Lock()
	delete(l.closed, id)
	l.mu.Unlock()
	return false
}

// unloaded forgets what the link kept of threads the daemon no longer has
// loaded: one the panel closed is gone, and one it held was let go past it —
// the daemon was started again, or no executor ran for a while. What waited
// in the panel's queue of such a thread goes with it: the link stays a client
// of a thread while its queue holds anything, so only a daemon started again
// or an executor down past the daemon's patience gets here, and a message
// sent into a thread the daemon let go is not the panel's to bring it back
// for.
func (l *Link) unloaded(ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.closed {
		if !slices.Contains(ids, id) {
			delete(l.closed, id)
		}
	}
	for id := range l.held {
		if !slices.Contains(ids, id) {
			delete(l.held, id)
			_ = os.Remove(heldPath(l.contour, id))
		}
	}
	for id, b := range l.boxes {
		if slices.Contains(ids, id) {
			continue
		}
		if len(b.Queue) > 0 {
			log.Printf("codex %s: thread %s is no longer loaded, and %d messages of the panel's queue go with it",
				l.home, id, len(b.Queue))
		}
		delete(l.boxes, id)
		l.keepOutbox(id)
	}
}

// heldDir keeps a mark per thread the panel holds, by the contour of its
// home: the threads outlive an executor, and the one started after it has to
// know which of them are the panel's to take back.
func heldDir(contour string) string {
	return filepath.Join(stream.Dir(), "codex-held", contour)
}

func heldPath(contour, id string) string { return filepath.Join(heldDir(contour), id) }

// heldMarks are the threads of a contour the panel holds, as marked.
func heldMarks(contour string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(heldDir(contour))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out[e.Name()] = true
		}
	}
	return out
}

// hold marks a thread the panel holds. A mark not written leaves the thread
// held only until the connection drops: the link takes back the marked ones.
func (l *Link) hold(id string) {
	l.mu.Lock()
	l.held[id] = true
	l.mu.Unlock()
	err := os.MkdirAll(heldDir(l.contour), 0o700)
	if err == nil {
		err = os.WriteFile(heldPath(l.contour, id), nil, 0o600)
	}
	if err != nil {
		log.Printf("codex %s: thread %s is held, and its mark was not written: %v", l.home, id, err)
	}
}

func (l *Link) unhold(id string) {
	l.mu.Lock()
	delete(l.held, id)
	l.mu.Unlock()
	_ = os.Remove(heldPath(l.contour, id))
}

func textInput(text string) []any {
	return []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}
}
