// Package heavy is the queue of heavy runs on the host: a build or a test run
// of a session waits for its turn at the executor, and runs in a scope of its
// own in the agents' slice, held to the memory it asked for, once the
// machine has room for it.
//
// The command is aacpanel-exec -heavy [-mem SIZE] [-name TEXT] -- COMMAND
// [ARGS...]. To whoever runs it, it is the command itself, only later: the
// waiting client is the process the command becomes, so its input, output,
// directory, environment, signals and exit are the command's own, and the
// queue speaks only on stderr.
package heavy

import (
	"path/filepath"
	"time"
)

// Flag is the word that makes aacpanel-exec a client of the queue.
const Flag = "-heavy"

// Prefix begins every line the client writes.
const Prefix = "aacpanel-exec -heavy: "

// UnitPrefix names the scope of every run, as aacp-heavy-7.scope.
const UnitPrefix = "aacp-heavy-"

// Socket is where the queue listens, next to the executor's own socket.
func Socket(actionSocket string) string {
	return filepath.Join(filepath.Dir(actionSocket), "heavy.sock")
}

// Ops of the socket.
const (
	OpRun  = "run"
	OpView = "view"
)

// Request is one call on the queue's socket: a run that asks for its turn,
// or a look at the queue.
type Request struct {
	Op   string `json:"op"`
	Name string `json:"name,omitempty"`
	Mem  string `json:"mem,omitempty"`
	Dir  string `json:"dir,omitempty"`
}

// The states a reply to a run carries. A waiting run gets lines while it
// waits, and one of the other three once: admitted, it runs in its scope;
// unheld, it runs outside the queue, which could not start the scope; refused,
// it would wait for ever and does not run.
const (
	StateWaiting  = "waiting"
	StateAdmitted = "admitted"
	StateUnheld   = "unheld"
	StateRefused  = "refused"
)

// Reply is one line of the queue to a run, or the answer to a look.
type Reply struct {
	State string `json:"state,omitempty"`
	Line  string `json:"line,omitempty"`
	Error string `json:"error,omitempty"`
	View  *View  `json:"view,omitempty"`
}

// View is the queue as a screen shows it: the runs going and the runs
// waiting in their order, and what keeps the first of them waiting.
type View struct {
	Slots   int           `json:"slots"`
	Running []RunningView `json:"running"`
	Waiting []WaitingView `json:"waiting"`
	Head    string        `json:"head"`
}

// RunningView is one run going; Taken is what it holds besides the cache of
// files. A run the executor admitted before its last start is known by its
// scope alone: its name, directory and start were kept by the executor that
// admitted it.
type RunningView struct {
	Unit  string `json:"unit"`
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Ask   uint64 `json:"ask"`
	Taken uint64 `json:"taken"`
	Since int64  `json:"since,omitempty"`
}

// WaitingView is one run waiting, at its place in the queue counted from one.
type WaitingView struct {
	Place int    `json:"place"`
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Ask   uint64 `json:"ask"`
	Since int64  `json:"since"`
}

// How the queue keeps time. It looks at the host this often and at once
// when a run leaves, and tells a waiting run where it stands every sayEvery.
var (
	lookEvery = 2 * time.Second
	sayEvery  = 15 * time.Second
)
