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
		{"task_list", "list", "List project tasks", "Lists this project's task summaries and total. Filter by status, tags (all must match) or text in title/body. next=true returns open tasks whose blockers are done or cancelled, sorted by priority, position and number. Use task_show before editing or taking a task; an empty list means nothing matches.", "Use task_list; next=true selects unblocked work.", schema(map[string]any{"status": map[string]any{"type": "array", "items": properties["status"]}, "tags": properties["tags"], "text": text("Text in title or body."), "next": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 500}})},
		{"task_show", "show", "Read a project task", "Reads a task by this project's number, with markdown, event summaries and handoff keys. Use before changing a task or choosing work. The number always belongs to this session's project; other projects cannot be selected.", "Use task_show before edits.", schema(map[string]any{"number": number}, "number")},
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
				text, err := toolReply(spec.operation, reply)
				if err != nil {
					return "The task registry reply was not parsed: " + err.Error(), true
				}
				return text, false
			},
		})
	}
	return out
}

// Tool replies keep documents behind explicit reads. The HTTP and CLI replies
// retain source records for imports and scripts, which repeat those documents.
type toolTaskRow struct {
	Number    int64      `json:"number"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Priority  string     `json:"priority,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	BlockedBy []int64    `json:"blockedBy,omitempty"`
	External  string     `json:"external,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

func taskRow(task Task) toolTaskRow {
	return toolTaskRow{Number: task.Number, Title: task.Title, Status: task.Status,
		Priority: task.Priority, Tags: task.Tags, BlockedBy: task.BlockedBy,
		External: taskExternal(task), UpdatedAt: task.UpdatedAt}
}

func taskExternal(task Task) string {
	if task.ExternalURL != "" {
		return task.ExternalURL
	}
	var text string
	if json.Unmarshal(task.External, &text) == nil {
		return text
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(task.External, &fields) != nil {
		return ""
	}
	for _, key := range []string{"url", "key", "number", "id"} {
		// A native task id belongs to a conversation, not an external issue.
		if key == "id" && fields["session"] != nil {
			continue
		}
		if json.Unmarshal(fields[key], &text) == nil && text != "" {
			return text
		}
	}
	return ""
}

type toolEvent struct {
	Kind      string                     `json:"kind"`
	At        time.Time                  `json:"at"`
	ActorName string                     `json:"actorName,omitempty"`
	Origin    string                     `json:"origin,omitempty"`
	From      string                     `json:"from,omitempty"`
	To        string                     `json:"to,omitempty"`
	Status    string                     `json:"status,omitempty"`
	Changes   map[string]json.RawMessage `json:"changes,omitempty"`
}

// An event whose payload has another shape, as an imported one may, keeps
// its kind, time and author: one such event does not cost the whole read.
func eventSummary(event Event) toolEvent {
	out := toolEvent{Kind: event.Kind, At: event.At, ActorName: event.ActorName, Origin: event.Origin}
	var payload struct {
		From    string                     `json:"from"`
		To      string                     `json:"to"`
		Status  string                     `json:"status"`
		Changes map[string]json.RawMessage `json:"changes"`
	}
	if len(event.Payload) > 0 && json.Unmarshal(event.Payload, &payload) != nil {
		return out
	}
	out.From, out.To, out.Status = payload.From, payload.To, payload.Status
	out.Changes = map[string]json.RawMessage{}
	for _, key := range []string{"title", "status", "priority", "order", "tags", "blockedBy", "external"} {
		if value, ok := payload.Changes[key]; ok {
			out.Changes[key] = value
		}
	}
	return out
}

type toolHandoffRef struct {
	Key         string     `json:"key"`
	Title       string     `json:"title"`
	At          *time.Time `json:"at,omitempty"`
	SessionName string     `json:"sessionName,omitempty"`
}

func handoffRef(handoff Handoff) toolHandoffRef {
	return toolHandoffRef{Key: handoff.Key, Title: handoff.Title, At: handoff.At, SessionName: handoff.SessionName}
}

func toolReply(operation string, raw json.RawMessage) (string, error) {
	var out any
	switch operation {
	case "list":
		var reply struct {
			Tasks  []Task `json:"tasks"`
			Total  int    `json:"total"`
			Limit  int    `json:"limit"`
			Offset int    `json:"offset"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", err
		}
		page := struct {
			Tasks  []toolTaskRow `json:"tasks"`
			Total  int           `json:"total"`
			Limit  int           `json:"limit,omitempty"`
			Offset int           `json:"offset,omitempty"`
		}{Tasks: make([]toolTaskRow, 0, len(reply.Tasks)), Total: reply.Total}
		for _, task := range reply.Tasks {
			page.Tasks = append(page.Tasks, taskRow(task))
		}
		if reply.Total > len(reply.Tasks) || reply.Offset > 0 {
			page.Limit, page.Offset = reply.Limit, reply.Offset
		}
		out = page
	case "create", "update":
		var reply Task
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", err
		}
		out = taskRow(reply)
	case "show":
		var reply Detail
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", err
		}
		detail := struct {
			Task struct {
				toolTaskRow
				Markdown string `json:"markdown"`
			} `json:"task"`
			Events   []toolEvent      `json:"events"`
			Handoffs []toolHandoffRef `json:"handoffs"`
		}{Events: make([]toolEvent, 0, len(reply.Events)), Handoffs: make([]toolHandoffRef, 0, len(reply.Handoffs))}
		detail.Task.toolTaskRow, detail.Task.Markdown = taskRow(reply.Task), reply.Task.Markdown
		for _, event := range reply.Events {
			summary := eventSummary(event)
			detail.Events = append(detail.Events, summary)
		}
		for _, handoff := range reply.Handoffs {
			detail.Handoffs = append(detail.Handoffs, handoffRef(handoff))
		}
		out = detail
	case "handoff_read", "handoff_write":
		var reply Handoff
		if err := json.Unmarshal(raw, &reply); err != nil {
			return "", err
		}
		var tasks map[string][]int64
		if len(reply.Tasks) > 0 {
			if err := json.Unmarshal(reply.Tasks, &tasks); err != nil {
				return "", err
			}
		}
		out = struct {
			toolHandoffRef
			Next     string             `json:"next,omitempty"`
			Markdown string             `json:"markdown"`
			Tasks    map[string][]int64 `json:"tasks"`
		}{toolHandoffRef: handoffRef(reply), Next: reply.Next, Markdown: Markdown(reply.Body), Tasks: tasks}
	default:
		return "", fmt.Errorf("unknown task tool operation %q", operation)
	}
	text, err := json.Marshal(out)
	return string(text), err
}
