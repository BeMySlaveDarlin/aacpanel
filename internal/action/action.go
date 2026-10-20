// Package action describes the protocol between the service and the executor.
package action

import "slices"

// Kind is the kind of an action.
type Kind string

const (
	ContainerStart   Kind = "container.start"
	ContainerStop    Kind = "container.stop"
	ContainerRestart Kind = "container.restart"
	StackUp          Kind = "stack.up"
	StackDown        Kind = "stack.down"
	SessionOpen      Kind = "session.open"
	SessionResume    Kind = "session.resume"
	SessionClose     Kind = "session.close"
	// SessionRestart closes a session the gentle way and starts it again: as
	// its project from the map when one holds its directory, the host's main
	// session as it was otherwise. It starts with an empty context, or goes on
	// with the conversation it closed when the request names it to resume.
	SessionRestart Kind = "session.restart"
	SessionKill    Kind = "session.kill"
	SessionSend    Kind = "session.send"
	// SessionLetter sends a letter from one live session to another: to the
	// message socket of the recipient's claude, from the session that runs the
	// conversation the request names. It is a kind of its own rather than a
	// field of session.send: an executor that did not know the field would
	// take the letter for a message and type it in as the person's words.
	SessionLetter Kind = "session.letter"
	// SessionAnswer answers the question a session is currently standing on.
	SessionAnswer Kind = "session.answer"
	// SessionDismiss drops the question and returns the session to an ordinary conversation.
	SessionDismiss Kind = "session.dismiss"
	// SessionStop interrupts what the model is writing right now.
	SessionStop Kind = "session.stop"
	// SessionBackground moves a call that runs in the foreground — a shell
	// command or a subagent the turn waits on — to the background, as Ctrl+B
	// does in the terminal: the call answers at once, the turn goes on, and
	// the work goes on running beside it. It names one call, or none for every
	// call in the foreground.
	SessionBackground Kind = "session.background"
	// SessionEscape closes a screen the session put up on its own and gives the
	// composer back, so that what is typed next goes to the conversation and
	// not into a dialog nobody at the panel has seen.
	SessionEscape Kind = "session.escape"
	// SessionFile sends a file or a screenshot from the phone into a session.
	SessionFile Kind = "session.file"
	// SessionCommand sends a slash command into a session.
	SessionCommand Kind = "session.command"
	// SessionShell runs a command typed after "!" in a session on the stream,
	// as the composer of a terminal runs one: in the session's directory and
	// with no turn of the model, its output going into the conversation.
	SessionShell Kind = "session.shell"

	// SessionPermit answers a permission prompt.
	SessionPermit Kind = "session.permit"

	// SessionUnqueue takes back a message that waits in the queue of a session
	// on the stream, before the session reads it.
	SessionUnqueue Kind = "session.unqueue"

	// SessionSet changes one setting of a live session — its model, its effort
	// or its permission mode — picked from the list the session offers.
	SessionSet Kind = "session.set"

	// SessionMcp reconnects, enables or disables one MCP server of a session
	// on the stream.
	SessionMcp Kind = "session.mcp"

	// SessionRename gives a live session on the stream a new name.
	SessionRename Kind = "session.rename"

	// SessionRemote switches Remote Control of a live session on or off: the
	// bridge that makes it reachable from the Claude app and claude.ai.
	SessionRemote Kind = "session.remote"

	// SessionRole sets or clears the role of a live claude session in its
	// project, as the person gives it: the session is told it at its next start.
	SessionRole Kind = "session.role"

	// SessionIntent sets the messages a session opens and restarts with.
	SessionIntent Kind = "session.intent"

	// SessionParent stands a live session under another of its project, as
	// session_open stands the session it opens under the one that calls it.
	SessionParent Kind = "session.parent"

	// SessionDetach takes a live session from under the session it stands
	// under: it stands on its own, as a session a person opened, until it
	// is stood under another again.
	SessionDetach Kind = "session.detach"

	// SessionSwitch moves a live session between the console and the feed:
	// the same conversation is closed on one side and resumed on the other.
	SessionSwitch Kind = "session.switch"

	// WindowOpen opens a terminal window onto a live session on the host.
	WindowOpen Kind = "window.open"
	// TaskStop stops one background job of a session.
	TaskStop Kind = "task.stop"
	// AgentStop stops a subagent the session started.
	AgentStop Kind = "agent.stop"
	// ObserverStop stops an observer of the panel a session started, with its
	// command, by the id the executor keeps it under.
	ObserverStop Kind = "observer.stop"
	// AlarmCancel cancels an alarm of the panel a session set, by its id.
	AlarmCancel Kind = "alarm.cancel"
	// ObserverStart and AlarmSet record work a session already started over
	// the observers' private socket. They are deliberately absent from Kinds:
	// the service must never gain a way to start an arbitrary host command.
	ObserverStart Kind = "observer.start"
	AlarmSet      Kind = "alarm.set"

	// WindowClose closes the windows attached to a session on the host.
	WindowClose Kind = "window.close"

	// ProjectCreate creates a project directory on disk and grants it trust.
	ProjectCreate Kind = "project.create"
	// ProjectMove carries a project's Claude history between accounts with
	// an undo kept until the service commits the map.
	ProjectMove Kind = "project.move"

	// TermStart starts a terminal of the panel: the user's own shell in a
	// tmux session on the panel's socket, in a place from the map. The
	// target is the id the service gives it.
	TermStart Kind = "term.start"
	// TermClose kills a terminal of the panel with whatever runs in it.
	TermClose Kind = "term.close"
	// TmuxClose closes a foreign terminal found again by its opaque id.
	TmuxClose Kind = "tmux.close"
	// TermRename gives a terminal of the panel a name of the person's own in
	// place of the command it runs.
	TermRename Kind = "term.rename"
	// TermConsole opens a terminal window on the host attached to a terminal
	// of the panel.
	TermConsole Kind = "term.console"

	// SecretPut saves the notepad of credentials a session asked the person
	// for into a file only the owner reads, and tells the session its path.
	// The target is the session; the text is in the request and nowhere else.
	SecretPut Kind = "secret.put"
	// SecretDrop removes a secret from the host. The target is its name.
	SecretDrop Kind = "secret.drop"
)

// Agent updates have fixed programs and sources: the request carries neither.
const (
	CodexUpdate  Kind = "codex.update"
	ClaudeUpdate Kind = "claude.update"
)

// Kinds is the full list of what the executor can do.
var Kinds = []Kind{
	ContainerStart, ContainerStop, ContainerRestart, StackUp, StackDown, CodexUpdate, ClaudeUpdate,
	SessionOpen, SessionResume, SessionClose, SessionRestart, SessionKill, SessionSend, SessionLetter,
	SessionAnswer, SessionDismiss, SessionStop, SessionBackground, SessionEscape, SessionFile, SessionCommand, SessionShell,
	SessionPermit, SessionSwitch, SessionUnqueue, SessionSet, SessionMcp, SessionRename, SessionRemote, SessionRole, SessionParent, SessionDetach, TaskStop, AgentStop, ObserverStop, AlarmCancel, WindowOpen,
	WindowClose, ProjectCreate, ProjectMove, TermStart, TermClose, TermRename, TermConsole, TmuxClose, SecretPut, SecretDrop, SessionIntent,
}

// Valid reports whether the executor knows this kind of action.
func Valid(k Kind) bool { return slices.Contains(Kinds, k) }

// RecordOnly identifies facts the service journals without calling the executor.
func RecordOnly(k Kind) bool { return k == ObserverStart || k == AlarmSet }

// WorkRecord is what a session reports after its private socket call succeeds.
// From is the calling conversation, as on a letter; the target is its name.
type WorkRecord struct {
	From    string `json:"from"`
	Name    string `json:"name"`
	Command string `json:"command,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	At      string `json:"at,omitempty"`
	Every   string `json:"every,omitempty"`
}

// Params keeps the record in the same shape as the other action parameters.
func (r WorkRecord) Params() map[string]any {
	p := map[string]any{"from": r.From, "name": r.Name}
	for key, value := range map[string]string{"command": r.Command, "cwd": r.Cwd, "at": r.At, "every": r.Every} {
		if value != "" {
			p[key] = value
		}
	}
	return p
}
