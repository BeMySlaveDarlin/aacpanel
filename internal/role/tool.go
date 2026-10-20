package role

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/mcp"
)

// ToolName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__role.
const ToolName = "role"

// Instructions is the tool's line in the server's word to every session that
// has it.
const Instructions = "At the start, read role and set it if assigned but missing; update it when the person " +
	"or opener changes this session's lasting responsibility."

// Description is the tool's own word to the model.
const Description = "Reads or sets which work of this repository belongs to this session, shown in the panel " +
	"and told to the session at its start and with the prompt after a change. Call without role to read it; " +
	"role with at most 500 characters sets it, and an empty string clears it. At the start, read the role " +
	"before choosing work from shared notes; set an assigned role if missing, and update it when the person " +
	"or opener changes the responsibility or the work changes for good. Continue work after the call. " +
	"Do not replace an assigned role with a task title or progress: use checklist for steps. The role is " +
	"kept by account, repository and session name, surviving restart, reopen under that name and a move into " +
	"a worktree. Only the main conversation calls this, never its subagents."

// Tool is the role tool of the panel's server, keeping the roles in dir with
// the times now gives. It is allowed: a role is the session's word about
// itself, and the person sees it in the panel and changes it there.
func Tool(dir string, now func() time.Time) mcp.Tool {
	return mcp.Tool{
		Name:         ToolName,
		Title:        "Role",
		Description:  Description,
		InputSchema:  InputSchema(),
		Instructions: Instructions,
		Standing:     func(b mcp.Binding) string { return standing(Read(dir, b.Place, b.Name)) },
		Allowed:      true,
		Call: func(_ context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return call(dir, now, bind, args)
		},
	}
}

// standing is what the instructions add when the session has a role: a
// session started again under its name learns at once which work in the
// directory is its, before it picks up a note another session left there.
// Without a role it adds nothing.
func standing(r *Role) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf("This session's role, set %s: “%s”. Other sessions may share the directory under other roles: "+
		"notes, handoffs and task picks there may be theirs; go on with this role's.", setBy(r.By), r.Role)
}

// setBy says who set a role, in the words the model reads it in.
func setBy(by string) string {
	switch {
	case by == BySession:
		return "by itself"
	case by == ByPerson:
		return "by the person"
	case strings.HasPrefix(by, byOpener+" "):
		return "by its opener " + strings.TrimPrefix(by, byOpener+" ")
	}
	return "by its opener"
}

// InputSchema is what the tool takes, as JSON Schema.
func InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{"type": "string", "maxLength": MaxText,
				"description": "The role of this session, in a sentence or two. Left out, the call reads the role and " +
					"changes nothing; empty, it clears the role."},
		},
		"additionalProperties": false,
	}
}

// call reads the role of the session of the claude, found anew on every call,
// or sets it.
func call(dir string, now func() time.Time, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Role *string `json:"role"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "The role was not kept: the arguments are not the role tool's (" + err.Error() + ").", true
		}
	}
	b, err := bind()
	if err != nil {
		if args.Role == nil {
			return "The role was not read: " + err.Error(), true
		}
		return "The role was not kept: " + err.Error(), true
	}
	if args.Role == nil {
		r := Read(dir, b.Place, b.Name)
		if r == nil {
			return "This session has no role.", false
		}
		return fmt.Sprintf("This session's role, set %s at %s: “%s”", setBy(r.By), r.At, r.Role), false
	}
	kept, err := Keep(dir, b.Place, b.Name, *args.Role, BySession, now())
	if err != nil {
		return "The role was not kept: " + err.Error() + ".", true
	}
	if kept == nil {
		return "The role is cleared.", false
	}
	return "The role is kept: the person sees it in the panel, and this session is told it with its next prompt.", false
}
