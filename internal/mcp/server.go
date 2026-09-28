package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Protocols are the versions of MCP the server speaks, newest first. It uses
// nothing a version changed — tools, their list and their call — so it
// answers in the version the client asked for when it knows it.
var Protocols = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Server answers MCP over stdio for one claude process: JSON-RPC 2.0, one
// message a line.
type Server struct {
	// Lead is the server's own first sentence in its word to the session;
	// the line of every tool goes on from it.
	Lead string
	// Tools are the tools the server lists and calls, in the order it lists
	// them.
	Tools []Tool
	// Bind finds where the claude this server serves works.
	Bind Bind
}

// Allowed are the tools of the server the launcher allows, by the names
// claude gives them.
func (s *Server) Allowed() []string {
	var out []string
	for _, t := range s.Tools {
		if t.Allowed {
			out = append(out, Qualified(t.Name))
		}
	}
	return out
}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeNoMethod       = -32601
	codeInvalidParams  = -32602
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type reply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// maxLine bounds one message. The arguments of a call are the bulk of one: a
// checklist at its limits is some ten kilobytes, a brief at the collector's
// ceiling four megabytes. A longer line ends the server and takes every tool
// from the session, so the bound stands above the largest call a tool takes.
const maxLine = 8 << 20

// Serve answers messages from in until it ends. ctx is handed to every call
// of a tool.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 64<<10), maxLine)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}
		if r := s.answer(ctx, line); r != nil {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	}
	return scan.Err()
}

// answer returns the reply to one message, or nil where none is owed: a
// notification, or the answer of the client to a request of ours.
func (s *Server) answer(ctx context.Context, line []byte) *reply {
	if !json.Valid(line) {
		return failed(nil, codeParse, "the message is not JSON")
	}
	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		return failed(nil, codeInvalidRequest, "a message is one JSON object")
	}
	if len(m.ID) == 0 || m.Method == "" {
		return nil
	}
	switch m.Method {
	case "initialize":
		return done(m.ID, s.initialize(m.Params))
	case "ping":
		return done(m.ID, struct{}{})
	case "tools/list":
		return done(m.ID, map[string]any{"tools": s.list()})
	case "tools/call":
		return s.call(ctx, m.ID, m.Params)
	}
	return failed(m.ID, codeNoMethod, "the server has no method "+m.Method)
}

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var asked struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &asked)
	version := Protocols[0]
	if slices.Contains(Protocols, asked.ProtocolVersion) {
		version = asked.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": ServerName, "title": "aacpanel", "version": "1"},
		"instructions":    s.instructions(),
	}
}

// instructions are the server's word to the session: its lead, then the line
// of every tool, each on a line of its own, with what the place already holds
// for it. A place not known yet is said nothing of: every call finds it again.
func (s *Server) instructions() string {
	b, err := s.Bind()
	lines := []string{s.Lead}
	for _, t := range s.Tools {
		line := []string{t.Instructions}
		if err == nil && t.Standing != nil {
			line = append(line, t.Standing(b))
		}
		lines = append(lines, strings.Join(slices.DeleteFunc(line, empty), " "))
	}
	return strings.Join(slices.DeleteFunc(lines, empty), "\n")
}

func empty(s string) bool { return s == "" }

func (s *Server) list() []any {
	out := make([]any, 0, len(s.Tools))
	for _, t := range s.Tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"title":       t.Title,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return out
}

// call runs the tool named in the call. What the tool reports comes back as
// its own result, error or not; only a call of a tool the server does not
// have is an error of the protocol.
func (s *Server) call(ctx context.Context, id json.RawMessage, params json.RawMessage) *reply {
	var c struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &c); err != nil {
		return failed(id, codeInvalidParams, "the call is not parsed: "+err.Error())
	}
	at := slices.IndexFunc(s.Tools, func(t Tool) bool { return t.Name == c.Name })
	if at < 0 {
		return failed(id, codeInvalidParams, fmt.Sprintf("the server has no tool %q", c.Name))
	}
	text, isError := s.Tools[at].Call(ctx, s.Bind, c.Arguments)
	return done(id, toolText(text, isError))
}

func toolText(text string, isError bool) map[string]any {
	out := map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
	if isError {
		out["isError"] = true
	}
	return out
}

func done(id json.RawMessage, result any) *reply {
	return &reply{JSONRPC: "2.0", ID: id, Result: result}
}

func failed(id json.RawMessage, code int, text string) *reply {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &reply{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: text}}
}
