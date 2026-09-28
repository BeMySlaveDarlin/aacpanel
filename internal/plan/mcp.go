package plan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"
)

// The server and its one tool, as a session sees them: the model calls
// mcp__aacpanel__plan, and a permission rule names it the same way.
const (
	ServerName = "aacpanel"
	ToolName   = "plan"
	// Qualified is the name claude gives the tool: the launcher allows it by
	// this name.
	Qualified = "mcp__" + ServerName + "__" + ToolName
	// Flag starts the executor as this server: the launcher names it in the
	// MCP configuration of every session it starts.
	Flag = "-plan"
)

// Instructions go into the system prompt of every session that has the
// server. A tool of a server is often deferred — the model sees its name and
// not its description — so what the plan is for is said here as well.
const Instructions = "The panel shows the person this session's plan on their phone and desk. " +
	"When the work has several steps, keep it with the plan tool: the whole list every time, " +
	"updated when a step starts or ends and when the plan changes. A short task needs no plan."

// Description is the tool's own word to the model.
const Description = "The plan of the current work, shown to the person in the panel on their phone and desk; " +
	"the terminal does not show it. Send the whole list every time, in order, each step with its status: " +
	"pending, active (being worked on now; one at a time), done, or dropped (no longer needed, kept for the record). " +
	"Update it when a step starts or ends and when the plan changes. Whether to keep a plan and what makes a step " +
	"is yours to decide: a question or a one-step task needs none. An empty list clears the plan. " +
	"note is an optional short line about the plan as a whole, such as what it waits on. " +
	"The plan belongs to the main conversation: a subagent does not call this."

// InputSchema is what the tool takes, as JSON Schema.
func InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":        "array",
				"description": "Every step of the plan, in order.",
				"maxItems":    MaxItems,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text":   map[string]any{"type": "string", "minLength": 1, "maxLength": MaxText, "description": "The step, in a few words."},
						"status": map[string]any{"type": "string", "enum": Statuses},
					},
					"required":             []string{"text", "status"},
					"additionalProperties": false,
				},
			},
			"note": map[string]any{"type": "string", "maxLength": MaxNote, "description": "One short line about the plan as a whole."},
		},
		"required":             []string{"items"},
		"additionalProperties": false,
	}
}

// Protocols are the versions of MCP the server speaks, newest first. It uses
// nothing a version changed — tools, their list and their call — so it
// answers in the version the client asked for when it knows it.
var Protocols = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Server answers MCP over stdio for one claude process: JSON-RPC 2.0, one
// message a line.
type Server struct {
	// Dir is where the plans lie.
	Dir string
	// Session names the conversation of the claude this server serves, read
	// anew on every call: /clear starts another conversation in the same
	// process.
	Session func() (id string, pid int, err error)
	// Now is the clock of the times a plan carries.
	Now func() time.Time
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

// maxLine bounds one message. A plan at its limits is some ten kilobytes.
const maxLine = 1 << 20

// Serve answers messages from in until it ends.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 64<<10), maxLine)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}
		if r := s.answer(line); r != nil {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	}
	return scan.Err()
}

// answer returns the reply to one message, or nil where none is owed: a
// notification, or the answer of the client to a request of ours.
func (s *Server) answer(line []byte) *reply {
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
		return done(m.ID, map[string]any{"tools": []any{tool()}})
	case "tools/call":
		return s.call(m.ID, m.Params)
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
		"instructions":    Instructions,
	}
}

func tool() map[string]any {
	return map[string]any{
		"name":        ToolName,
		"title":       "Plan",
		"description": Description,
		"inputSchema": InputSchema(),
	}
}

// call runs the tool. What the model got wrong or what the host failed at
// comes back as the tool's own error, which the model reads; only a call of
// a tool the server does not have is an error of the protocol.
func (s *Server) call(id json.RawMessage, params json.RawMessage) *reply {
	var c struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &c); err != nil {
		return failed(id, codeInvalidParams, "the call is not parsed: "+err.Error())
	}
	if c.Name != ToolName {
		return failed(id, codeInvalidParams, fmt.Sprintf("the server has no tool %q", c.Name))
	}
	var args struct {
		Items []Item `json:"items"`
		Note  string `json:"note"`
	}
	if len(c.Arguments) > 0 {
		if err := json.Unmarshal(c.Arguments, &args); err != nil {
			return done(id, toolText("The plan was not kept: the arguments are not the plan's ("+err.Error()+").", true))
		}
	}
	if args.Items == nil {
		return done(id, toolText("The plan was not kept: items is required — the whole list, or an empty one to clear the plan.", true))
	}
	session, pid, err := s.Session()
	if err != nil {
		return done(id, toolText("The plan was not kept: "+err.Error(), true))
	}
	kept, err := Keep(s.Dir, session, pid, args.Items, args.Note, s.Now())
	if err != nil {
		var refused Refusal
		if errors.As(err, &refused) {
			return done(id, toolText("The plan was not kept: "+refused.Why+".", true))
		}
		return done(id, toolText("The plan was not kept: "+err.Error(), true))
	}
	return done(id, toolText(summary(kept), false))
}

// summary is what the model hears back: short, since it is read on every
// update of the plan.
func summary(p *Plan) string {
	if p == nil {
		return "The plan is cleared."
	}
	finished := 0
	for _, it := range p.Items {
		if it.Status == Done || it.Status == Dropped {
			finished++
		}
	}
	return fmt.Sprintf("The plan is kept: %d of %d steps finished.", finished, len(p.Items))
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
