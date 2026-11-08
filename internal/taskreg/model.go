// Package taskreg describes project tasks and reads file registries without writing to their source.
package taskreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// File keeps the original bytes, including fields the panel does not understand.
type File struct {
	Name  string `json:"name"`
	Raw   string `json:"raw"`
	Error string `json:"error,omitempty"`
}

// Snapshot is a complete listing assembled from the collector's bounded pages.
// An unreadable directory cannot prove that any of its tasks disappeared.
type Snapshot struct {
	Root             string          `json:"root"`
	Present          bool            `json:"present"`
	Meta             json.RawMessage `json:"meta"`
	TasksComplete    bool            `json:"tasksComplete"`
	HandoffsComplete bool            `json:"handoffsComplete"`
	Files            []File          `json:"files"`
	Errors           []string        `json:"errors"`
	Next             string          `json:"next,omitempty"`
}

type Task struct {
	ProjectID     int             `json:"projectId"`
	Project       string          `json:"project"`
	ContourID     int             `json:"contourId"`
	Contour       string          `json:"contour"`
	Number        int64           `json:"number"`
	Title         string          `json:"title"`
	Status        string          `json:"status"`
	Priority      string          `json:"priority"`
	Order         *float64        `json:"order"`
	Tags          []string        `json:"tags"`
	BlockedBy     []int64         `json:"blockedBy"`
	External      json.RawMessage `json:"external"`
	ExternalRefs  []string        `json:"externalRefs"`
	ExternalURL   string          `json:"externalUrl"`
	CreatedAt     *time.Time      `json:"createdAt"`
	UpdatedAt     *time.Time      `json:"updatedAt"`
	ClosedAt      *time.Time      `json:"closedAt"`
	CreatedBy     string          `json:"createdBy"`
	ClosedBy      string          `json:"closedBy"`
	TouchedBy     []string        `json:"touchedBy"`
	Session       string          `json:"session"`
	Body          json.RawMessage `json:"body"`
	Markdown      string          `json:"markdown"`
	Source        json.RawMessage `json:"source"`
	ImportedAt    time.Time       `json:"importedAt"`
	DeletedAt     *time.Time      `json:"deletedAt"`
	LastEventAt   *time.Time      `json:"lastEventAt"`
	LastActor     string          `json:"lastActor"`
	LastActorName string          `json:"lastActorName"`
	InWork        bool            `json:"inWork"`
	People        []Person        `json:"people"`
}

type Event struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	At        time.Time       `json:"at"`
	Precision string          `json:"precision"`
	Actor     string          `json:"actor"`
	ActorName string          `json:"actorName"`
	Origin    string          `json:"origin"`
	Payload   json.RawMessage `json:"payload"`
}

type Handoff struct {
	SessionName string          `json:"sessionName"`
	Next        string          `json:"next"`
	File        string          `json:"file"`
	Key         string          `json:"key"`
	Session     string          `json:"session"`
	Kind        string          `json:"kind"`
	At          *time.Time      `json:"at"`
	Date        string          `json:"date"`
	Title       string          `json:"title"`
	CWD         string          `json:"cwd"`
	Git         json.RawMessage `json:"git"`
	Tasks       json.RawMessage `json:"tasks"`
	TaskNumbers []int64         `json:"taskNumbers"`
	Body        json.RawMessage `json:"body"`
	Source      json.RawMessage `json:"source"`
	DeletedAt   *time.Time      `json:"deletedAt"`
}

type Detail struct {
	Task     Task      `json:"task"`
	Events   []Event   `json:"events"`
	Handoffs []Handoff `json:"handoffs"`
}

func rawOrNull(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("null")
	}
	return v
}

// ParseTask keeps the legacy spelling in Source while normalizing the state
// used by queries. The legacy closed state means the task is complete.
func ParseTask(raw string) (Task, error) {
	var d struct {
		ID        int64           `json:"id"`
		Title     string          `json:"title"`
		Status    string          `json:"status"`
		Priority  string          `json:"priority"`
		Order     *float64        `json:"order"`
		Tags      []string        `json:"tags"`
		BlockedBy []int64         `json:"blockedBy"`
		External  json.RawMessage `json:"external"`
		Refs      json.RawMessage `json:"refs"`
		Body      json.RawMessage `json:"body"`
		Created   string          `json:"created"`
		Updated   string          `json:"updated"`
		Closed    string          `json:"closed"`
		CreatedBy string          `json:"created_by"`
		ClosedBy  string          `json:"closed_by"`
		TouchedBy []string        `json:"touched_by"`
		Session   string          `json:"session"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Task{}, err
	}
	if d.Status == "closed" {
		d.Status = "done"
	}
	if d.ID <= 0 || strings.TrimSpace(d.Title) == "" || !slices.Contains([]string{"open", "deferred", "done", "cancelled"}, d.Status) {
		return Task{}, fmt.Errorf("the task needs a positive number, title and known status")
	}
	t := Task{Number: d.ID, Title: d.Title, Status: d.Status, Priority: d.Priority, Order: d.Order,
		Tags: append([]string{}, d.Tags...), BlockedBy: append([]int64{}, d.BlockedBy...),
		External: rawOrNull(d.External), Body: rawOrNull(d.Body), Source: json.RawMessage(raw),
		CreatedBy: d.CreatedBy, ClosedBy: d.ClosedBy, TouchedBy: append([]string{}, d.TouchedBy...), Session: d.Session}
	var err error
	if t.CreatedAt, _, err = Date(d.Created); err != nil {
		return Task{}, err
	}
	if t.UpdatedAt, _, err = Date(d.Updated); err != nil {
		return Task{}, err
	}
	if t.ClosedAt, _, err = Date(d.Closed); err != nil {
		return Task{}, err
	}
	t.Markdown = Markdown(t.Body)
	var refs struct {
		Jira string `json:"jira"`
	}
	_ = json.Unmarshal(d.Refs, &refs)
	t.ExternalRefs, t.ExternalURL = external(strings.Join(append([]string{d.Title, refs.Jira}, d.Tags...), " "), t.External)
	return t, nil
}

// Date retains the difference between a source date and a timed event.
func Date(s string) (*time.Time, string, error) {
	if s == "" {
		return nil, "", nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			precision := "instant"
			if layout == time.DateOnly {
				precision = "date"
			}
			return &t, precision, nil
		}
	}
	return nil, "", fmt.Errorf("the source date is neither an ISO date nor an RFC3339 timestamp")
}

var webURL = regexp.MustCompile(`https?://[^\s<>"\x60]+`)

func external(title string, raw json.RawMessage) ([]string, string) {
	text := title
	var value any
	_ = json.Unmarshal(raw, &value)
	switch v := value.(type) {
	case string:
		text += " " + v
	case map[string]any:
		// cctm's {session,id} is a native task binding, not an issue link.
		for _, key := range []string{"url", "number", "key"} {
			if s, ok := v[key].(string); ok {
				text += " " + s
			}
		}
		if _, native := v["session"]; !native {
			if s, ok := v["id"].(string); ok {
				text += " " + s
			}
		}
	}
	return IssueNumbers(text), strings.TrimRight(webURL.FindString(text), ".,;)")
}

var sections = []string{"description", "context", "precondition", "plan", "verify", "measure", "result", "notes", "files"}

// Markdown gives mixed string, list and object sections a stable presentation.
// Unknown sections remain visible after the known sections, in lexical order.
func Markdown(raw json.RawMessage) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return strings.TrimSpace(render(raw))
	}
	keys := append([]string{}, sections...)
	extra := []string{}
	for k := range obj {
		if !slices.Contains(sections, k) {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	keys = append(keys, extra...)
	parts := []string{}
	for _, k := range keys {
		if text := strings.TrimSpace(render(obj[k])); text != "" {
			title := k
			if slices.Contains(sections, k) {
				title = strings.ToUpper(k[:1]) + k[1:]
			}
			parts = append(parts, "## "+title+"\n\n"+text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func render(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		parts := []string{}
		for _, v := range list {
			if s := render(v); s != "" {
				parts = append(parts, "- "+strings.ReplaceAll(s, "\n", "\n  "))
			}
		}
		return strings.Join(parts, "\n")
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := []string{}
		for _, k := range keys {
			parts = append(parts, "**"+k+"**\n\n"+render(obj[k]))
		}
		return strings.Join(parts, "\n\n")
	}
	return string(raw)
}

func ParseHandoff(name, raw string) (Handoff, error) {
	var d struct {
		Key     string          `json:"key"`
		Session string          `json:"session"`
		Kind    string          `json:"kind"`
		TS      float64         `json:"ts"`
		Date    string          `json:"date"`
		Title   string          `json:"title"`
		CWD     string          `json:"cwd"`
		Git     json.RawMessage `json:"git"`
		Tasks   json.RawMessage `json:"tasks"`
		Body    json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Handoff{}, err
	}
	if d.Key == "" {
		return Handoff{}, fmt.Errorf("the handoff has no key")
	}
	h := Handoff{File: name, Key: d.Key, Session: d.Session, Kind: d.Kind, Date: d.Date, Title: d.Title, CWD: d.CWD,
		Git: rawOrNull(d.Git), Tasks: rawOrNull(d.Tasks), Body: rawOrNull(d.Body), Source: json.RawMessage(raw), TaskNumbers: []int64{}}
	if d.TS != 0 {
		at := time.Unix(int64(d.TS), int64((d.TS-float64(int64(d.TS)))*1e9)).UTC()
		h.At = &at
	} else {
		var err error
		h.At, _, err = Date(d.Date)
		if err != nil {
			return Handoff{}, err
		}
	}
	var groups map[string][]int64
	if len(d.Tasks) > 0 && string(d.Tasks) != "null" {
		if err := json.Unmarshal(d.Tasks, &groups); err != nil {
			return Handoff{}, fmt.Errorf("the handoff task groups are invalid: %w", err)
		}
	}
	for _, numbers := range groups {
		h.TaskNumbers = append(h.TaskNumbers, numbers...)
	}
	slices.Sort(h.TaskNumbers)
	h.TaskNumbers = slices.Compact(h.TaskNumbers)
	return h, nil
}
