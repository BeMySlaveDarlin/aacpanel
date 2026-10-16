package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"aacpanel/internal/action"
)

var (
	softWait  = 15 * time.Second
	pollEvery = time.Second
)

type agentProc struct {
	Session string
	Agent   int
	Konsole int
	Dir     string
	// Home marks the host's main session: the one running in the home directory.
	Home bool
}

// sessionClose ends a session the gentle way. A session closing itself names
// its conversation, and the close goes ahead only while the session still
// runs it: a late repeat of the call would otherwise close a session brought
// up under the name since.
func (e *Executor) sessionClose(ctx context.Context, target, conversation string) (string, error) {
	p, err := findAgent(target)
	if err != nil {
		return "", err
	}
	if conversation != "" {
		if err := runsConversation(target, conversation, "close"); err != nil {
			return "", err
		}
	}
	if s, err := findOneLiveSession(target); err == nil && onStream(s) {
		return e.closeGently(ctx, s, p, true)
	}
	return e.closeAgent(ctx, p)
}

// sessionRestart ends a session the gentle way and starts a new one in the
// same place, with an empty context or, where resume names the conversation
// the session runs, going on with it. The panel names the project the session
// belongs to, with its launch parameters from the map and the message after a
// restart as its first one: a project session started without them would come
// up in another setup — the console instead of the feed, another model,
// possibly another account. Without a project only the host's main session is
// restarted, since its launch is fixed: the home directory, the same name.
// A session restarting itself names its conversation, and the restart goes
// ahead only while the session still runs it: a late repeat of the call would
// otherwise close the session an earlier restart brought up under the name.
// A session going on with its conversation names the message it wrote itself
// to go on with as the text of the request, and that message comes first in
// the resumed conversation in place of the map's.
func (e *Executor) sessionRestart(ctx context.Context, req action.Request) (string, error) {
	target, resume := req.Target, req.Resume
	p, err := lookupAgent(target)
	if err != nil {
		return "", err
	}
	for _, named := range []string{resume, req.Conversation} {
		if named == "" {
			continue
		}
		if err := runsConversation(target, named, "restart"); err != nil {
			return "", err
		}
	}
	if req.Project != nil {
		return e.restartFromMap(ctx, target, p, req.Project, resume, req.Text)
	}
	if !p.Home {
		return "", fmt.Errorf(
			"session %s runs in %s, not in the home directory: a restart from scratch is for the host's "+
				"main session only. Other sessions are closed here and opened again from the projects map, "+
				"which carries their launch parameters",
			p.Session, p.Dir)
	}

	home := homeProject(p.Dir, p.Session)
	if home.Launch, err = goingOnLaunch(home.Launch, resume, req.Text); err != nil {
		return "", err
	}
	closed, err := e.closeAgent(ctx, p)
	if err != nil {
		return "", fmt.Errorf("the restart stopped at closing, nothing was started: %w", err)
	}

	rep, err := e.runLauncher(ctx, home, resume)
	if err != nil {
		return "", fmt.Errorf("%s; the new session did not start: %w", closed, err)
	}
	return closed + "; " + describeConsole(rep) + restartedWith(resume, ""), nil
}

// runsConversation refuses a restart or a close, as act names it, that names
// a conversation other than the one the session runs: going on with it would
// close one conversation and bring up another in its place, and a restart or
// a close asked for by it would close a session that conversation is no
// longer in.
func runsConversation(target, conversation, act string) error {
	s, err := findOneLiveSession(target)
	if err != nil {
		return fmt.Errorf("the conversation of session %s is not known, so the %s cannot tell it is %s: %w",
			target, act, conversation, err)
	}
	if s.SessionID != conversation {
		return fmt.Errorf("session %s runs conversation %s, not %s: a %s closes only the conversation "+
			"it names", target, s.SessionID, conversation, act)
	}
	return nil
}

// restartedWith says what the new session started with.
func restartedWith(resume, more string) string {
	if resume != "" {
		return ", going on with conversation " + resume + more
	}
	return " with an empty context" + more
}

func (e *Executor) restartFromMap(ctx context.Context, target string, p agentProc, want *action.Project, resume, intent string) (string, error) {
	next, err := chooseProject(target, want)
	if err != nil {
		return "", err
	}
	s, err := findOneLiveSession(target)
	held := err == nil && onStream(s)
	var closed, afresh string
	// A conversation on the stream nobody has said a word in has no
	// transcript to go on with: the session starts anew rather than close and
	// bring nothing up.
	if held && resume != "" {
		if st, err := streamState(ctx, s); err == nil && !st.Said {
			resume, afresh = "", "; nothing had been said in the conversation, so it started anew"
		}
	}
	if next.Launch, err = goingOnLaunch(next.Launch, resume, intent); err != nil {
		return "", err
	}
	if held {
		closed, err = e.closeGently(ctx, s, p, true)
	} else {
		closed, err = e.closeAgent(ctx, p)
	}
	if err != nil {
		return "", fmt.Errorf("the restart stopped at closing, nothing was started: %w", err)
	}
	rep, err := e.runLauncher(ctx, next, resume)
	if err != nil {
		return "", fmt.Errorf("%s; the new session did not start: %w", closed, err)
	}
	return closed + "; " + describeConsole(rep) + restartedWith(resume, " and the project's parameters") + afresh, nil
}

// goingOnLaunch is the launch a restart brings the session up with: with the
// message the session wrote itself as the first one where it goes on with its
// conversation, in place of the map's message after a restart. A session that
// starts anew opens with the map's: the words were written for a conversation
// the new one does not have. The message rides as the opening message of any
// start does — an argument of claude in a terminal, the first message after
// the handshake on the stream.
func goingOnLaunch(raw json.RawMessage, resume, intent string) (json.RawMessage, error) {
	if resume == "" || intent == "" {
		return raw, nil
	}
	launch := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &launch); err != nil {
			return nil, fmt.Errorf("the launch parameters of the project were not read, nothing was closed: %w", err)
		}
	}
	launch["intent"] = intent
	return json.Marshal(launch)
}

// closeAgent ends a session with a signal and waits for its transcript. A
// tmux session the launcher started goes with it, or under remain-on-exit the
// terminal would hang there empty. Any other loses only claude: one typed into
// a shell — of the person's own tmux session or of a terminal of the panel —
// leaves the shell where it was, since the session is the person's.
func (e *Executor) closeAgent(ctx context.Context, p agentProc) (string, error) {
	var launched tmuxPane
	if pane, err := tmuxPaneFor(ctx, p.Agent); err == nil && pane.launched(ctx) {
		launched = pane
	}

	if p.Konsole > 0 {
		_ = e.signal(p.Konsole, syscall.SIGTERM)
	}
	if err := e.signal(p.Agent, syscall.SIGTERM); err != nil {
		return "", fmt.Errorf("could not send TERM to agent %d of session %s: %w", p.Agent, p.Session, err)
	}

	if e.waitGone(ctx, p.Agent, e.softWait()) {
		_ = launched.killSession(ctx)
		return fmt.Sprintf("session %s closed gracefully, the transcript is complete", p.Session), nil
	}
	return "", fmt.Errorf(
		"session %s did not close within %s: agent %d is still alive. "+
			"Only kill -9 is left — it can leave the transcript incomplete, and /resume will not bring it back",
		p.Session, e.softWait(), p.Agent)
}

func (e *Executor) sessionKill(ctx context.Context, target string) (string, error) {
	p, err := findAgent(target)
	if err != nil {
		return "", err
	}

	if p.Konsole > 0 {
		_ = e.signal(p.Konsole, syscall.SIGKILL)
	}
	if err := e.signal(p.Agent, syscall.SIGKILL); err != nil {
		return "", fmt.Errorf("could not send KILL to agent %d of session %s: %w", p.Agent, p.Session, err)
	}
	if e.waitGone(ctx, p.Agent, 5*time.Second) {
		return fmt.Sprintf("session %s killed; the transcript may have stayed incomplete", p.Session), nil
	}
	return "", fmt.Errorf("session %s: agent %d survived KILL — look on the host", p.Session, p.Agent)
}

func (e *Executor) waitGone(ctx context.Context, pid int, limit time.Duration) bool {
	// A process that has ended stays in the table until its parent collects
	// it, and a signal still finds it there: the parent of an agent is a tmux
	// server or a holder, and a busy one collects late. It is gone all the
	// same — it wrote what it had to write before it ended.
	alive := func() bool { return e.signal(pid, 0) == nil && !exited(pid) }

	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if !alive() {
			return true
		}
		select {
		case <-ctx.Done():
			return !alive()
		case <-time.After(e.pollEvery()):
		}
	}
	return !alive()
}

func (e *Executor) signal(pid int, sig syscall.Signal) error {
	if sig != 0 && isAncestor(pid) {
		return fmt.Errorf("refusing to send %v to process %d: it is the executor itself or its ancestor", sig, pid)
	}
	if e.sendSignal != nil {
		return e.sendSignal(pid, sig)
	}
	return syscall.Kill(pid, sig)
}

func (e *Executor) softWait() time.Duration {
	if e.soft > 0 {
		return e.soft
	}
	return softWait
}

func (e *Executor) pollEvery() time.Duration {
	if e.poll > 0 {
		return e.poll
	}
	return pollEvery
}

// findAgent finds a live session by name for the actions that end it. The
// host's main session is not among them: the panel does not close it, just as
// it does not stop its own container.
func findAgent(target string) (agentProc, error) {
	p, err := lookupAgent(target)
	if err != nil {
		return agentProc{}, err
	}
	if p.Home {
		return agentProc{}, fmt.Errorf(
			"session %s runs in the home directory — it is the host's main session, and it is not closed from here",
			p.Session)
	}
	return p, nil
}

func lookupAgent(target string) (agentProc, error) {
	if strings.TrimSpace(target) == "" {
		return agentProc{}, fmt.Errorf("the session name is empty")
	}
	home, _ := os.UserHomeDir()

	pids, err := claudePIDs()
	if err != nil {
		return agentProc{}, err
	}

	var known []string
	for _, pid := range pids {
		name, ok := sessionName(pid)
		if !ok {
			continue
		}
		known = append(known, name)
		if name != target {
			continue
		}

		dir := procCwd(pid)
		if dir == "" {
			dir = sessionFileCwd(pid)
		}
		if isAncestor(pid) {
			return agentProc{}, fmt.Errorf(
				"session %s is the one the executor itself runs in: closing it would leave it unable to report the result",
				name)
		}
		return agentProc{
			Session: name, Agent: pid, Konsole: konsoleOf(pid), Dir: dir,
			Home: home != "" && dir == strings.TrimRight(home, "/"),
		}, nil
	}

	if len(known) == 0 {
		return agentProc{}, fmt.Errorf("there is no session %q: not a single named claude session is running", target)
	}
	return agentProc{}, fmt.Errorf("there is no session %q among the running ones: %s", target, strings.Join(known, ", "))
}

// sessionFileCwd is the working directory claude wrote into the file of its
// live session. The cwd link under /proc is refused to a reader in a user
// namespace below the one of the process, and the sandbox of the executor's
// unit runs it in a namespace of its own: read there, every session would be
// in no directory at all.
func sessionFileCwd(pid int) string {
	for _, dir := range sessionsDirs() {
		if s, ok := readSessionFile(dir, pid); ok {
			return strings.TrimRight(s.CWD, "/")
		}
	}
	return ""
}
