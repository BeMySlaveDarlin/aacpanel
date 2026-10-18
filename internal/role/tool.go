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
const Instructions = "Set this session's role with the role tool when the person or the session that opened it " +
	"gives one, or its work changes for good."

// Description is the tool's own word to the model.
const Description = "The role of this session in its project: a short text that tells it from the other " +
	"sessions working in the same repository, shown to the person in the panel and told to the session before " +
	"its first word and again with the prompt after it changes. It is kept by the account, the repository and " +
	"the session's name, so it " +
	"outlives a restart, a close and an open under the same name, and a move into a worktree. " +
	"Without role the call reads it; a text sets it, at most 500 characters; an empty text clears it. " +
	"Set it when the person or the session that opened this one assigns a role, or when the work of this " +
	"session changes for good — not for each task. The role belongs to the main conversation: a subagent does " +
	"not call this."

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
