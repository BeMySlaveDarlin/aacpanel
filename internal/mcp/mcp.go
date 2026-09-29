// Package mcp is the panel's MCP server: the executor serves it over stdio to
// the claude that started it, and every tool the panel gives a session is one
// of its tools. The server speaks the protocol — the handshake, the list of
// tools and their calls — and finds where the claude it serves works; what a
// tool does is the tool's own.
package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
)

// The server as a session sees it: the model calls a tool of it as
// mcp__aacpanel__<tool>, and a permission rule names it the same way.
const (
	ServerName = "aacpanel"
	// Flag starts the executor as this server: the launcher names it in the
	// MCP configuration of every session it starts.
	Flag = "-mcp"
)

// Qualified is the name claude gives a tool of the server: the launcher
// allows a tool by this name.
func Qualified(tool string) string {
	return "mcp__" + ServerName + "__" + tool
}

// Place is where a session works: the config directory of its account and
// the directory claude runs in. It outlives a conversation: a restart starts
// another conversation in the same place, or goes on with the old one, and
// not always under the old id.
type Place struct {
	ConfigDir string
	Dir       string
}

// Clean puts a place in the one form every reader names it by: both paths
// absolute and cleaned, or no place at all.
func (p Place) Clean() (Place, bool) {
	if !filepath.IsAbs(p.ConfigDir) || !filepath.IsAbs(p.Dir) {
		return Place{}, false
	}
	return Place{ConfigDir: filepath.Clean(p.ConfigDir), Dir: filepath.Clean(p.Dir)}, true
}

// Binding is what the server learns of the claude it serves: where it
// works, the name of the session, the conversation it is in — empty while
// claude has not written the file of itself yet — and its process.
type Binding struct {
	Place Place
	// Name is the name the session runs under, empty for a session started
	// without one. Sessions of one place are told apart by it, and a restart
	// under the same name — the same conversation or another — is the same
	// session going on.
	Name      string
	SessionID string
	PID       int
}

// Bind finds where the claude the server serves works and the conversation it
// is in. It is asked anew on every call: /clear starts another conversation in
// the same process.
type Bind func() (Binding, error)

// Tool is one tool of the server: how it is listed, what it adds to the
// server's word to the session, and what a call of it does.
type Tool struct {
	// Name is the tool's name on the server; claude calls it Qualified(Name).
	Name  string
	Title string
	// Description is the tool's own word to the model.
	Description string
	// InputSchema is what the tool takes, as JSON Schema.
	InputSchema map[string]any
	// Instructions is the tool's line in the server's word to the session,
	// which goes into the system prompt of every session that has the
	// server. A tool of a server is often deferred — the model sees its name
	// and not its description — so what the tool is for is said here as well.
	Instructions string
	// Standing, where the tool has it, adds to the tool's line what the place
	// already holds for it. It is asked at the handshake and only for a place
	// known then: a place not known yet is said nothing of.
	Standing func(Binding) string
	// Allowed is a tool the launcher allows, so a call of it never waits on
	// the person; a tool without it asks as claude asks of any tool.
	Allowed bool
	// Call runs the tool with the arguments the model sent. bind finds where
	// the claude works, for the tool that needs it; a tool says itself what
	// it could not do when the place is not known. What the model got wrong
	// and what the host failed at come back as the text with isError set:
	// the model reads it and calls again.
	Call func(ctx context.Context, bind Bind, args json.RawMessage) (text string, isError bool)
}
