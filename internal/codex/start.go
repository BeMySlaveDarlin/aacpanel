package codex

import (
	"context"
	"encoding/json"
	"errors"
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
	// Allowed are the tools of that server the thread calls without asking
	// the person, by their names on the server; any other asks as codex asks
	// before a call of a tool of any server.
	Allowed []string
	// Parent is the session that had the panel start the thread, by its
	// name; empty for a thread a person started.
	Parent string
	Owner  stream.SessionOwner
	// Workspace overrides only this thread's write roots and network policy.
	Workspace     bool
	WritableRoots []string
}

// toolsConfig is the panel's MCP server as a thread's configuration names
// it, key by key: a key of its own overrides the same key of config.toml and
// leaves the rest of the server as config.toml has it. A tool allowed has its
// approval mode set to approve, which lets a call of it go without a request
// to the person: codex asks before any other call of a tool of a server, and
// under the policy never refuses it, and a checklist that asked before every
// update would not be kept. The mode is set a tool at a time, since the
// letter asks, as it does a claude session.
func toolsConfig(program string, allowed []string) map[string]any {
	server := "mcp_servers." + mcp.ServerName
	out := map[string]any{server + ".command": program, server + ".args": []string{mcp.Flag}}
	for _, tool := range allowed {
		out[server+".tools."+tool+".approval_mode"] = "approve"
	}
	return out
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
	if b.Workspace {
		config["sandbox_workspace_write.network_access"] = true
		if len(b.WritableRoots) > 0 {
			config["sandbox_workspace_write.writable_roots"] = b.WritableRoots
		}
	}
	if b.Effort != "" {
		config["model_reasoning_effort"] = b.Effort
	}
	if b.Tools != "" {
		maps.Copy(config, toolsConfig(b.Tools, b.Allowed))
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
	if b.Owner.Conversation != "" {
		if err := stream.KeepOwner(id, b.Owner); err != nil {
			return id, fmt.Errorf("the thread started, but its opening session was not recorded: %w", err)
		}
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
// alone talks to. A thread the panel closed before is its session again, with
// no parent: see disown. It returns the directory the thread runs in. A
// subagent's thread is part of its parent's turn and is not resumed: the panel
// leaves it at once.
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
	l.disown(threadID)
	if err := os.Remove(stream.OpenedPath(threadID)); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("the thread's opening session was not cleared: %w", err)
	}
	l.seen(out.Thread, time.Now())
	settled := out.read()
	l.set(threadID, func(t *thread) { t.subscribed, t.settings = true, settled })
	l.hold(threadID)
	l.save(threadID)
	return out.Thread.CWD, nil
}

// Reconnect starts every MCP server of a thread the panel holds anew, as its
// configuration names it now: the panel's own among them, the program it is
// after an update. A thread starts its servers once, when the daemon loads it,
// and nothing else in the protocol starts them again — a reload of the
// configuration restarts only a server whose configuration changed, and an
// update leaves the path of the panel's program as it was. So the thread is
// taken out of the daemon and back in: archived, which unloads it at once
// whoever else follows it, unarchived, and resumed with the panel's server
// beside the servers of its home, as it started. The conversation, the name
// and the goal are on the disk and stay; the permissions a thread started
// with are not, and the settings a resume read just before go back with
// thread/settings/update. tools and allowed are the panel's server as at the
// start: empty tools gives the thread the servers of its config.toml alone.
//
// What the unload would cut or cross is refused instead: a turn that runs —
// the archive interrupts it — background terminals, which it ends, the agents
// of the thread at work and a goal codex works towards on its own, whose work
// goes on in the thread, and a thread codex has not written to the disk yet,
// which no resume brings back. A thread the panel does not hold is another
// client's, codex's terminal or an app, and the archive would take it out
// from under that client. The price is a turn another client starts in the
// moment between the look and the archive: the archive interrupts it.
func (l *Link) Reconnect(ctx context.Context, threadID, tools string, allowed []string) error {
	c, err := l.client()
	if err != nil {
		return err
	}
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	held := l.held[threadID]
	l.mu.Unlock()
	if !held {
		return errors.New("the panel does not hold the thread — another client of the daemon does, codex's " +
			"terminal or an app — and starting its servers anew takes the thread out of the daemon from under it")
	}
	if err := l.unloadable(ctx, c, threadID); err != nil {
		return err
	}
	var before json.RawMessage
	if err := within(ctx, c, "thread/resume", map[string]any{"threadId": threadID, "excludeTurns": true}, &before); err != nil {
		return err
	}
	l.mu.Lock()
	l.moving[threadID] = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.moving, threadID)
		l.mu.Unlock()
	}()

	id := map[string]any{"threadId": threadID}
	if err := within(ctx, c, "thread/archive", id, nil); err != nil {
		return fmt.Errorf("codex did not take the thread out of the daemon: %w", err)
	}
	if err := within(ctx, c, "thread/unarchive", id, nil); err != nil {
		return fmt.Errorf("the thread went into codex's archive to start its servers anew and did not come out: %w "+
			"— it stays there, out of the daemon, until it is unarchived", err)
	}
	params := map[string]any{"threadId": threadID, "excludeTurns": true}
	if tools != "" {
		params["config"] = toolsConfig(tools, allowed)
	}
	var out struct {
		Thread threadInfo `json:"thread"`
		settingsWire
	}
	if err := within(ctx, c, "thread/resume", params, &out); err != nil {
		return fmt.Errorf("the thread is out of the daemon and did not come back in: %w — Resume from the archive "+
			"of the panel brings it back", err)
	}
	l.seen(out.Thread, time.Now())
	settled, lost := restore(ctx, c, threadID, before)
	if lost != nil {
		settled = out.read()
	}
	l.set(threadID, func(t *thread) { t.subscribed, t.settings = true, settled })
	l.save(threadID)
	return lost
}

// Unsettled is a reconnect whose servers started anew and whose settings did
// not come back as they were: the thread runs as its config.toml says until
// they are set again.
type Unsettled struct{ Err error }

func (u Unsettled) Error() string {
	return "the settings of the thread did not come back as they were, and it runs as its config.toml says " +
		"until they are set again: " + u.Err.Error()
}

func (u Unsettled) Unwrap() error { return u.Err }

// unloadable says what taking a thread out of the daemon would cut, nil when
// nothing.
func (l *Link) unloadable(ctx context.Context, c *conn, threadID string) error {
	busy := errors.New("a turn runs, and taking the thread out of the daemon interrupts it: start its servers " +
		"anew once the turn is over")
	info, err := read(ctx, c, threadID)
	if err != nil {
		return err
	}
	if info.Status.Type == "active" {
		return busy
	}
	// The status lags behind a turn just started; the list of turns does not.
	_, status, err := lastTurn(ctx, c, threadID)
	switch {
	case unwritten(err):
		return errors.New("codex has not written the thread to its disk yet — its first message does — and a " +
			"thread taken out of the daemon before that does not come back")
	case err != nil:
		return err
	case status == "inProgress":
		return busy
	}
	var goal struct {
		Goal *Goal `json:"goal"`
	}
	err = within(ctx, c, "thread/goal/get", map[string]any{"threadId": threadID}, &goal)
	switch {
	case err != nil && !unknownMethod(err):
		return err
	case goal.Goal != nil && goal.Goal.Status == GoalActive:
		return errors.New("codex works towards the goal of the thread and starts its next turn by itself: pause " +
			"the goal first")
	}
	running, err := processes(ctx, c, threadID)
	switch {
	case err != nil && !unknownMethod(err):
		return err
	case len(running) > 0:
		return fmt.Errorf("%d background terminals of the thread run, and taking the thread out of the daemon "+
			"ends them: stop them first, or wait for them", len(running))
	}
	ids, err := loaded(ctx, c)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == threadID {
			continue
		}
		agent, err := read(ctx, c, id)
		if err == nil && agent.Parent == threadID && agent.Status.Type == "active" {
			return errors.New("agents of the thread are at work: start its servers anew once they are done")
		}
	}
	return nil
}

// standing is what a thread runs with, as a resume of it says, kept the way
// the daemon said it to be given back the same way.
type standing struct {
	Model    string          `json:"model"`
	Effort   *string         `json:"reasoningEffort"`
	Approval json.RawMessage `json:"approvalPolicy"`
	Reviewer string          `json:"approvalsReviewer"`
	Sandbox  json.RawMessage `json:"sandbox"`
	Profile  *struct {
		ID string `json:"id"`
	} `json:"activePermissionProfile"`
	Collaboration json.RawMessage `json:"collaborationMode"`
	Tier          *string         `json:"serviceTier"`
}

// restore gives a thread back the settings a resume of it read before, and
// returns them as the panel keeps them. The permission profile goes back in
// place of the sandbox, which the daemon takes only one at a time. Plan mode
// goes back with its own words; the default mode is the one a resumed thread
// is in, and naming it again would add its words to the next turn.
func restore(ctx context.Context, c *conn, threadID string, before json.RawMessage) (settings, error) {
	var was standing
	var wire settingsWire
	if err := json.Unmarshal(before, &was); err != nil {
		return settings{}, Unsettled{err}
	}
	if err := json.Unmarshal(before, &wire); err != nil {
		return settings{}, Unsettled{err}
	}
	params := map[string]any{"threadId": threadID}
	if was.Model != "" {
		params["model"] = was.Model
	}
	if was.Effort != nil {
		params["effort"] = *was.Effort
	}
	if len(was.Approval) > 0 && string(was.Approval) != "null" {
		params["approvalPolicy"] = was.Approval
	}
	if was.Reviewer != "" {
		params["approvalsReviewer"] = was.Reviewer
	}
	if was.Profile != nil && was.Profile.ID != "" {
		params["permissions"] = was.Profile.ID
	} else if len(was.Sandbox) > 0 && string(was.Sandbox) != "null" {
		params["sandboxPolicy"] = was.Sandbox
	}
	if wire.Collaboration != nil && wire.Collaboration.Mode != "" && wire.Collaboration.Mode != "default" {
		params["collaborationMode"] = was.Collaboration
	}
	if was.Tier != nil {
		params["serviceTier"] = *was.Tier
	}
	if err := within(ctx, c, "thread/settings/update", params, nil); err != nil {
		return settings{}, Unsettled{experimental(err)}
	}
	return wire.read(), nil
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
