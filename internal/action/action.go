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
	// SessionRestart closes the host's main session and starts it again in the
	// same directory with an empty context.
	SessionRestart Kind = "session.restart"
	SessionKill    Kind = "session.kill"
	SessionSend    Kind = "session.send"
	// SessionAnswer answers the question a session is currently standing on.
	SessionAnswer Kind = "session.answer"
	// SessionDismiss drops the question and returns the session to an ordinary conversation.
	SessionDismiss Kind = "session.dismiss"
	// SessionStop interrupts what the model is writing right now.
	SessionStop Kind = "session.stop"
	// SessionEscape closes a screen the session put up on its own and gives the
	// composer back, so that what is typed next goes to the conversation and
	// not into a dialog nobody at the panel has seen.
	SessionEscape Kind = "session.escape"
	// SessionFile sends a file or a screenshot from the phone into a session.
	SessionFile Kind = "session.file"
	// SessionCommand sends a slash command into a session.
	SessionCommand Kind = "session.command"

	// SessionPermit answers a permission prompt.
	SessionPermit Kind = "session.permit"

	// SessionUnqueue takes back a message that waits in the queue of a session
	// on the stream, before the session reads it.
	SessionUnqueue Kind = "session.unqueue"

	// SessionSet changes one setting of a live session — its model, its effort
	// or its permission mode — picked from the list the session offers.
	SessionSet Kind = "session.set"

	// SessionSwitch moves a live session between the console and the feed:
	// the same conversation is closed on one side and resumed on the other.
	SessionSwitch Kind = "session.switch"

	// WindowOpen opens a terminal window onto a live session on the host.
	WindowOpen Kind = "window.open"
	// TaskStop stops one background job of a session.
	TaskStop Kind = "task.stop"
	// AgentStop stops a subagent the session started.
	AgentStop Kind = "agent.stop"

	// WindowClose closes the windows attached to a session on the host.
	WindowClose Kind = "window.close"

	// ProjectCreate creates a project directory on disk and grants it trust.
	ProjectCreate Kind = "project.create"
)

// Kinds is the full list of what the executor can do.
var Kinds = []Kind{
	ContainerStart, ContainerStop, ContainerRestart, StackUp, StackDown,
	SessionOpen, SessionResume, SessionClose, SessionRestart, SessionKill, SessionSend,
	SessionAnswer, SessionDismiss, SessionStop, SessionEscape, SessionFile, SessionCommand,
	SessionPermit, SessionSwitch, SessionUnqueue, SessionSet, TaskStop, AgentStop, WindowOpen, WindowClose,
	ProjectCreate,
}

// Valid reports whether the executor knows this kind of action.
func Valid(k Kind) bool { return slices.Contains(Kinds, k) }
