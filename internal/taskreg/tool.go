package taskreg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/mcp"
)

// Tools have no project or author argument: their binding is the authority
// for both, and the service checks the conversation against its live map.
func Tools(panel string) []mcp.Tool {
	text := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	number := map[string]any{"type": "integer", "minimum": 1, "description": "Task number within this project."}
	properties := map[string]any{
		"title": text("Task title."), "status": map[string]any{"type": "string", "enum": []string{"open", "deferred", "done", "cancelled"}},
		"priority":  map[string]any{"type": "string", "enum": []string{"HIGH", "MEDIUM", "LOW", ""}},
		"order":     map[string]any{"type": "number", "description": "Position within the priority group."},
		"tags":      map[string]any{"type": "array", "items": text("Tag.")},
		"blockedBy": map[string]any{"type": "array", "items": number},
		"external":  text("External issue URL or key; empty clears it."),
		"markdown":  text("Body markdown. Appends a section by default; replace=true replaces the entire body."),
		"section":   text("Heading of an appended section, Notes by default."),
		"replace":   map[string]any{"type": "boolean", "description": "Replace the entire body with markdown, including an empty body."},
	}
	schema := func(props map[string]any, required ...string) map[string]any {
		s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	editSchema := func(update bool) map[string]any {
		p := map[string]any{}
		for k, v := range properties {
			p[k] = v
		}
		if update {
			p["number"] = number
			return schema(p, "number")
		}
		return schema(p, "title")
	}
	specs := []struct {
		name, operation, title, description, instructions string
		schema                                            map[string]any
	}{
		{"task_list", "list", "List project tasks", "Lists this project's tasks. Filter by status, tags (all must match) or text in title/body. next=true returns open tasks whose blockers are done or cancelled, sorted by priority, position and number. Use task_show before editing or taking a task; an empty list means nothing matches.", "Use task_list; next=true selects unblocked work.", schema(map[string]any{"status": map[string]any{"type": "array", "items": properties["status"]}, "tags": properties["tags"], "text": text("Text in title or body."), "next": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500}})},
		{"task_show", "show", "Read a project task", "Reads a task by this project's number, with markdown, exact events and linked handoffs. Use before changing a task or choosing work. The number always belongs to this session's project; other projects cannot be selected.", "Use task_show before edits.", schema(map[string]any{"number": number}, "number")},
		{"task_create", "create", "Create a project task", "Creates a task in this session's project and assigns the next number atomically. Give title and optional status, priority, order, tags, blockedBy, external and markdown. Body markdown appends under Notes by default; use replace=true for a complete document. The event records this conversation and session name. Use task_list to check for existing work first.", "Use task_create for new work.", editSchema(false)},
		{"task_update", "update", "Update a project task", "Changes only supplied fields of this project's task. Status is open, deferred, done or cancelled; open reopens a closed task. Tags and blockedBy replace their arrays; empty arrays clear them. Markdown appends a section (Notes by default); replace=true replaces the whole body. Each effective edit records the author and exact time. Read with task_show first, then report what changed.", "Use task_update for fields, status and notes.", editSchema(true)},
		{"handoff_write", "handoff_write", "Write a project handoff", "Saves a title, markdown and next step for this project. Created, closed and touched tasks are derived from this conversation's events since its previous handoff; do not supply task lists. Use when saving progress or ending a session, after updating the tasks. The reply includes the key used by handoff_read.", "Use handoff_write to save progress and the next step.", schema(map[string]any{"title": text("Short handoff title."), "markdown": text("Progress, remaining work and useful context."), "next": text("The next concrete step.")}, "title", "markdown")},
		{"handoff_read", "handoff_read", "Read a project handoff", "Reads this project's latest handoff, or the one named by key. Contains its title, markdown, next step and recorded task groups. Use at session start or to recover saved context; no handoff is reported as an error, not invented context.", "Use handoff_read for saved context.", schema(map[string]any{"key": text("Handoff key; omitted reads the latest.")})},
	}
	out := make([]mcp.Tool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, mcp.Tool{Name: spec.name, Title: spec.title, Description: spec.description, Instructions: spec.instructions, InputSchema: spec.schema, Allowed: true,
			Call: func(ctx context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
				b, err := bind()
				if err != nil {
					return "The task registry caller is not known: " + err.Error(), true
				}
				if b.SessionID == "" {
					return "The task registry needs a bound conversation.", true
				}
				in := Request{Operation: spec.operation, Conversation: b.SessionID}
				if len(args) == 0 {
					args = json.RawMessage(`{}`)
				}
				decoder := json.NewDecoder(strings.NewReader(string(args)))
				decoder.DisallowUnknownFields()
				switch spec.operation {
				case "create":
					err = decoder.Decode(&in.Edit)
				case "update":
					var input struct {
						Number int64 `json:"number"`
						Edit
					}
					err = decoder.Decode(&input)
					in.Number = input.Number
					in.Edit = input.Edit
				case "list":
					var input struct {
						Status []string `json:"status"`
						Tags   []string `json:"tags"`
						Text   string   `json:"text"`
						Next   bool     `json:"next"`
						Limit  int      `json:"limit"`
					}
					err = decoder.Decode(&input)
					in.Status = input.Status
					in.Tags = input.Tags
					in.Text = input.Text
					in.Next = input.Next
					in.Limit = input.Limit
				case "show":
					var input struct {
						Number int64 `json:"number"`
					}
					err = decoder.Decode(&input)
					in.Number = input.Number
				case "handoff_write":
					err = decoder.Decode(&in.Handoff)
				case "handoff_read":
					var input struct {
						Key string `json:"key"`
					}
					err = decoder.Decode(&input)
					in.Key = input.Key
				}
				if err != nil {
					return "The task arguments were not parsed: " + err.Error(), true
				}
				wait, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				var reply json.RawMessage
				if err = Call(wait, panel, in, &reply); err != nil {
					return err.Error(), true
				}
				return fmt.Sprintf("%s", reply), false
			},
		})
	}
	return out
}
