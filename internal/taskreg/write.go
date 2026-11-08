package taskreg

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const FilesWriter = "cctm"
const PanelWriter = "panel"

type Actor struct {
	Conversation string `json:"conversation"`
	Name         string `json:"name"`
}

// Edit distinguishes an omitted field from an explicit empty value. Markdown
// appends as a named section unless Replace is requested, including an empty body.
type Edit struct {
	Title     *string   `json:"title,omitempty"`
	Status    *string   `json:"status,omitempty"`
	Priority  *string   `json:"priority,omitempty"`
	Order     *float64  `json:"order,omitempty"`
	Tags      *[]string `json:"tags,omitempty"`
	BlockedBy *[]int64  `json:"blockedBy,omitempty"`
	External  *string   `json:"external,omitempty"`
	Markdown  *string   `json:"markdown,omitempty"`
	Section   string    `json:"section,omitempty"`
	Replace   bool      `json:"replace,omitempty"`
}

func (e Edit) Validate(create bool) error {
	if (create && e.Title == nil) || (e.Title != nil && strings.TrimSpace(*e.Title) == "") {
		return fmt.Errorf("a task needs a title")
	}
	if e.Status != nil && !slices.Contains([]string{"open", "deferred", "done", "cancelled"}, *e.Status) {
		return fmt.Errorf("unknown task status %q", *e.Status)
	}
	if e.Priority != nil && !slices.Contains([]string{"", "HIGH", "MEDIUM", "LOW"}, *e.Priority) {
		return fmt.Errorf("priority must be HIGH, MEDIUM, LOW or empty")
	}
	if (e.Replace || e.Section != "") && e.Markdown == nil {
		return fmt.Errorf("a body operation needs markdown")
	}
	if e.Replace && e.Section != "" {
		return fmt.Errorf("replacement cannot name an appended section")
	}
	if strings.ContainsAny(e.Section, "\r\n") {
		return fmt.Errorf("a section title must fit one line")
	}
	if e.BlockedBy != nil {
		for _, n := range *e.BlockedBy {
			if n <= 0 {
				return fmt.Errorf("blocked-by numbers must be positive")
			}
		}
	}
	return nil
}

func (e Edit) Apply(t *Task) map[string]any {
	changes := map[string]any{}
	for _, field := range []struct {
		name   string
		value  *string
		target *string
	}{
		{"title", e.Title, &t.Title}, {"status", e.Status, &t.Status}, {"priority", e.Priority, &t.Priority},
	} {
		if field.value != nil && *field.target != *field.value {
			changes[field.name] = map[string]string{"from": *field.target, "to": *field.value}
			*field.target = *field.value
		}
	}
	if e.Order != nil && (t.Order == nil || *t.Order != *e.Order) {
		t.Order = e.Order
		changes["order"] = e.Order
	}
	if e.Tags != nil && !slices.Equal(t.Tags, *e.Tags) {
		t.Tags = append([]string{}, (*e.Tags)...)
		changes["tags"] = t.Tags
	}
	if e.BlockedBy != nil && !slices.Equal(t.BlockedBy, *e.BlockedBy) {
		t.BlockedBy = append([]int64{}, (*e.BlockedBy)...)
		changes["blockedBy"] = t.BlockedBy
	}
	if e.External != nil {
		raw, _ := json.Marshal(*e.External)
		if string(t.External) != string(raw) {
			t.External = raw
			changes["external"] = *e.External
		}
	}
	if e.Markdown != nil {
		body := *e.Markdown
		if !e.Replace {
			section := e.Section
			if section == "" {
				section = "Notes"
			}
			body = strings.TrimSpace(t.Markdown + "\n\n## " + section + "\n\n" + body)
		}
		if t.Markdown != body {
			t.Markdown = body
			t.Body, _ = json.Marshal(body)
			changes["body"] = body
		}
	}
	var source struct {
		Refs struct {
			Jira string `json:"jira"`
		} `json:"refs"`
	}
	_ = json.Unmarshal(t.Source, &source)
	t.ExternalRefs, t.ExternalURL = external(strings.Join(append([]string{t.Title, source.Refs.Jira}, t.Tags...), " "), t.External)
	return changes
}

type HandoffInput struct {
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
	Next     string `json:"next"`
}

// Request is shared by the local CLI and MCP tools; the service resolves an
// agent's project and author from Conversation instead of trusting caller fields.
type Request struct {
	Operation    string       `json:"operation"`
	Project      int          `json:"project,omitempty"`
	Conversation string       `json:"conversation,omitempty"`
	CWD          string       `json:"cwd,omitempty"`
	Number       int64        `json:"number,omitempty"`
	Key          string       `json:"key,omitempty"`
	Edit         Edit         `json:"edit,omitempty"`
	Handoff      HandoffInput `json:"handoff,omitempty"`
	Status       []string     `json:"status,omitempty"`
	Tags         []string     `json:"tags,omitempty"`
	Text         string       `json:"text,omitempty"`
	Next         bool         `json:"next,omitempty"`
	Limit        int          `json:"limit,omitempty"`
}

type Project struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Writer string `json:"writer"`
}
