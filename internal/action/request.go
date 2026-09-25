package action

import (
	"encoding/json"
	"slices"
)

// Request is what the service asks to be done.
type Request struct {
	ID string `json:"id"`

	Kind Kind `json:"kind"`

	Target string `json:"target"`

	Device string `json:"device,omitempty"`

	Resume string `json:"resume,omitempty"`

	Project *Project `json:"project,omitempty"`

	Permit *Permit `json:"permit,omitempty"`

	Text string `json:"text,omitempty"`

	Answer *Answer `json:"answer,omitempty"`

	Files []File `json:"files,omitempty"`

	Command *Command `json:"command,omitempty"`

	Work *Work `json:"work,omitempty"`

	Switch *Switch `json:"switch,omitempty"`

	// MessageID names a message sent to a session on the stream, so that it can
	// be taken back from the queue while it waits there.
	MessageID string `json:"messageId,omitempty"`

	// Setting is what session.set changes.
	Setting *Setting `json:"setting,omitempty"`

	// Mcp is what session.mcp does, and to which server.
	Mcp *McpChange `json:"mcp,omitempty"`

	Ask string `json:"ask,omitempty"`

	// Part names the screen a setup question asks for.
	Part string `json:"part,omitempty"`

	// History is the side chat so far, sent with a question aside.
	History []SideTurn `json:"history,omitempty"`
}

// Project is the project to open.
type Project struct {
	Path string `json:"path"`

	Session string `json:"session"`

	Launch json.RawMessage `json:"launch,omitempty"`

	ClaudeBin string `json:"claudeBin,omitempty"`

	ConfigDir string `json:"configDir,omitempty"`
}

// Answer is the reply to a pending session question.
type Answer struct {
	AskID string `json:"askId"`

	Picks [][]int `json:"picks"`

	Texts []string `json:"texts,omitempty"`

	// Notes are a person's words beside a pick, one per question: the model
	// reads them with the answer. Only a session on the stream takes them — a
	// terminal dialog has no field for them.
	Notes []string `json:"notes,omitempty"`
}

// File is a file sent from the phone.
type File struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// Command is a slash command for session.command.
type Command struct {
	Name string `json:"name"`
	Arg  string `json:"arg,omitempty"`
}

// Work is one background job of a session.
type Work struct {
	ID string `json:"id"`

	Line string `json:"line,omitempty"`
}

// Where session.switch moves a session.
const (
	// SwitchConsole is a terminal in tmux: every screen of claude is there.
	SwitchConsole = "console"
	// SwitchStream is the stream protocol under a holder: the feed answers it with structure.
	SwitchStream = "stream"
)

// Switch is where a live session moves. Force says the person was shown the
// background work that stops on the way and agreed to it: that work lives
// inside the process, and resuming the conversation does not bring it back.
type Switch struct {
	To string `json:"to"`

	Force bool `json:"force,omitempty"`
	// Window opens a terminal window on the host to the session once it is in
	// the console: a window is what a session in the feed does not have.
	Window bool `json:"window,omitempty"`
}

// Commands is what the panel can send as a slash command, and with which options.
var Commands = map[string][]string{
	"clear":    nil,
	"compact":  nil,
	"finalize": nil,
	"model":    {"default", "fable", "opus", "opus[1m]", "sonnet", "haiku"},
	"effort":   {"low", "medium", "high", "xhigh", "max", "ultracode"},
}

func commandNames() []string {
	out := make([]string, 0, len(Commands))
	for name := range Commands {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// AskKinds asks what this executor can do.
const AskKinds = "kinds"

// AskPermission asks which permission prompt a session is standing on.
const AskPermission = "permission"

// AskWindow asks whether a session has a terminal window open on the host.
const AskWindow = "window"

// AskModels asks what a session can be switched to: its models as claude
// lists them, and the mode and the effort it runs with.
const AskModels = "models"

// AskMcp asks a session on the stream about its MCP servers.
const AskMcp = "mcp"

// AskStatus asks a session about itself: its version and its account.
const AskStatus = "status"

// AskSetup asks a session for one of its read-only screens of settings: the
// part names which.
const AskSetup = "setup"
