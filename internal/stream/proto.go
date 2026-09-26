// Package stream holds a claude session that speaks the stream protocol.
//
// A session in tmux is a terminal: text goes in as keystrokes and a dialog is
// read off the screen. A session on the stream protocol is `claude -p` with
// stream-json on both pipes: a message is a line of JSON, a question and a
// permission are requests with an id, an answer is a reply to that id. Its
// pipes need a process that outlives the executor and the panel, the way the
// tmux server does for a terminal — that is the holder.
//
// The holder is the parent of claude and reads everything claude writes. It
// keeps nothing of the conversation and passes nothing of it on: its socket
// answers with the requests that wait for a person and with the state of the
// session, and its state file carries no text at all. The feed is still read
// off the transcript by the collector.
package stream

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Protocol is the version of the socket protocol. A holder outlives the
// executor that started it, so an executor may be talking to an older one.
const Protocol = 1

const dirName = "aacpanel-stream"

// Dir is where the holders keep their sockets and state files. It is not the
// directory of the executor's socket: that one is mounted into the panel's
// container, and a holder's socket there would let the service talk to a
// session past the closed list of actions.
func Dir() string {
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		return filepath.Join(base, dirName)
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", dirName, os.Getuid()))
}

// keptDir is where the files that outlive a holder lie: what the transcript
// does not say about a conversation, kept so the archive of it says the same.
func keptDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, dirName)
}

// WithdrawnPath keeps the fingerprints of the messages a person took back from
// the queue of one conversation. The transcript cannot tell a message taken
// back from one that was read — claude writes the same record for both — and
// the feed would show it as sent. It holds no words: a fingerprint is a hash
// of the message, not the message.
func WithdrawnPath(sessionID string) string {
	return filepath.Join(keptDir(), "withdrawn", sessionID+".json")
}

// PermitsPath keeps the answers a person gave to the permissions of one
// conversation. The transcript has the call and its result and nothing of the
// question between them — on the stream it is a request and a reply that
// never reach the file — and the feed would show a call nobody was asked
// about. It holds no words: what the call was about is in the transcript.
func PermitsPath(sessionID string) string {
	return filepath.Join(keptDir(), "permits", sessionID+".json")
}

// Permit is an answer to a permission, by the call it was given for.
type Permit struct {
	Use  string `json:"use"`
	Tool string `json:"tool"`
	// allow or deny.
	Decision string `json:"decision"`
	// Allowed and not asked again: the answer carried rules for the session.
	Lasting bool `json:"lasting,omitempty"`
}

// Fingerprint names a message without keeping its words.
func Fingerprint(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:16])
}

// SocketPath is the socket of the holder of one conversation.
func SocketPath(sessionID string) string { return filepath.Join(Dir(), sessionID+".sock") }

// StatePath is the state file of the holder of one conversation.
func StatePath(sessionID string) string { return filepath.Join(Dir(), sessionID+".json") }

// LogPath is the holder's own log: its start and its end, never the conversation.
func LogPath(sessionID string) string { return filepath.Join(Dir(), sessionID+".log") }

// Spec is what a holder is started with.
type Spec struct {
	Name      string   `json:"name"`
	Dir       string   `json:"dir"`
	SessionID string   `json:"sessionId"`
	Argv      []string `json:"argv"`
	// The first message, sent once claude has answered the handshake. A
	// session in a terminal gets it as an argument; on the stream it is a
	// message like any other.
	Intent string `json:"intent,omitempty"`
	// RemoteControl switches Remote Control on once claude has answered the
	// handshake. A terminal takes it as an argument; on the stream it is a
	// request.
	RemoteControl bool `json:"remoteControl,omitempty"`
	// Resumed says the session goes on with a conversation already on the
	// disk, rather than starting one.
	Resumed bool `json:"resumed,omitempty"`
	// Launched is what the launcher started the session with — the project,
	// its launch parameters and its contour. The session is started again from
	// it where the panel that knows the project is not there to ask.
	Launched json.RawMessage `json:"launched,omitempty"`
}

// Ops of the socket.
const (
	OpState   = "state"
	OpSend    = "send"
	OpRespond = "respond"
	OpControl = "control"
	OpClose   = "close"
)

// Request is one question to a holder.
type Request struct {
	Op        string          `json:"op"`
	Text      string          `json:"text,omitempty"`
	UUID      string          `json:"uuid,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	Response  json.RawMessage `json:"response,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	Fields    map[string]any  `json:"fields,omitempty"`
}

// Reply is what a holder answers.
type Reply struct {
	OK       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	State    *State          `json:"state,omitempty"`
	UUID     string          `json:"uuid,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
}

// Pending is a request of claude that waits for a person: a permission for a
// tool, a question, a plan to approve. The input of the tool is what the
// person decides on — the command, the edit, the questions — and it is what a
// terminal would show in its dialog.
type Pending struct {
	RequestID   string          `json:"requestId"`
	Tool        string          `json:"tool"`
	ToolUseID   string          `json:"toolUseId,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Description string          `json:"description,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	Suggestions json.RawMessage `json:"suggestions,omitempty"`
	Since       time.Time       `json:"since"`
}

// Queued is a message sent while claude was busy and not yet taken up.
type Queued struct {
	UUID  string    `json:"uuid"`
	Text  string    `json:"text"`
	Since time.Time `json:"since"`
}

// Task is a background task claude runs.
type Task struct {
	ID          string `json:"task_id"`
	Type        string `json:"task_type,omitempty"`
	Description string `json:"description,omitempty"`
}

// State is what a holder knows about its session.
type State struct {
	Protocol  int       `json:"protocol"`
	Name      string    `json:"name"`
	SessionID string    `json:"sessionId"`
	PID       int       `json:"pid"`
	Holder    int       `json:"holder"`
	Started   time.Time `json:"started"`
	Busy      bool      `json:"busy"`
	Model     string    `json:"model,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	// StartMode is the mode claude reported at the handshake. A mode equal to
	// it is the one the launch parameters gave, and a switch does not carry it:
	// the other side is started with the same parameters.
	StartMode string `json:"startMode,omitempty"`
	// Picked is the model as a person chose it since the start — "opus[1m]",
	// not the id claude resolves it to, which loses the context window — and
	// Effort the effort chosen since the start. A switch starts the other side
	// with them; empty is what the launch parameters gave.
	Picked  string    `json:"picked,omitempty"`
	Effort  string    `json:"effort,omitempty"`
	Pending []Pending `json:"pending"`
	Queue   []Queued  `json:"queue"`
	Tasks   []Task    `json:"tasks"`
	// Compacting is when a compaction of the conversation started, while it
	// runs. Claude reports the start and the end and nothing between: how far
	// it has got is not known to anybody.
	Compacting *time.Time      `json:"compacting,omitempty"`
	Init       json.RawMessage `json:"init,omitempty"`
	// Launched is what the session was started with; see Spec.
	Launched json.RawMessage `json:"launched,omitempty"`
	// Said is whether the conversation is on the disk to be resumed: it was
	// resumed, or a message went to claude. claude writes no transcript for
	// a conversation nobody has said a word in.
	Said bool `json:"said,omitempty"`
}

// Summary is the state file: what the collector reads to place the session on
// the map. No text of the conversation is in it — the names of the tools that
// wait and the counts, nothing a person wrote or claude answered.
type Summary struct {
	Protocol  int       `json:"protocol"`
	Name      string    `json:"name"`
	SessionID string    `json:"sessionId"`
	PID       int       `json:"pid"`
	Holder    int       `json:"holder"`
	Started   time.Time `json:"started"`
	Busy      bool      `json:"busy"`
	Model     string    `json:"model,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	Effort    string    `json:"effort,omitempty"`
	Waiting   []string  `json:"waiting"`
	Queue     int       `json:"queue"`
	Tasks     int       `json:"tasks"`
	// Compacting is when a compaction started, while it runs.
	Compacting *time.Time `json:"compacting,omitempty"`
	Updated    time.Time  `json:"updated"`
}

// Controls are the control requests a holder passes on to claude. The list
// is closed for the same reason the list of actions is: a request not on it
// is not forwarded, whoever asks.
var Controls = map[string]bool{
	"interrupt":               true,
	"set_model":               true,
	"set_permission_mode":     true,
	"set_max_thinking_tokens": true,
	"cancel_async_message":    true,
	"list_models":             true,
	"get_context_usage":       true,
	"get_usage":               true,
	"get_session_cost":        true,
	"mcp_status":              true,
	"mcp_toggle":              true,
	"mcp_reconnect":           true,
	"stop_task":               true,
	"apply_flag_settings":     true,
	"update_settings":         true,
	"get_settings":            true,
	"side_question":           true,
	"get_hooks_listing":       true,
	"get_memory_dialog":       true,
	"get_skills_dialog":       true,
	"rename_session":          true,
	"remote_control":          true,
}
