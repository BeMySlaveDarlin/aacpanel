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

	Ask string `json:"ask,omitempty"`
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

// Commands is what the panel can send as a slash command, and with which options.
var Commands = map[string][]string{
	"clear":    nil,
	"compact":  nil,
	"finalize": nil,
	"model":    {"default", "fable", "opus", "opus[1m]", "sonnet", "haiku"},
	"effort":   {"low", "medium", "high", "xhigh", "max"},
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
