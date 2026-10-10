package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Beyond an approval of a command or of a change to files, codex waits for a
// person on three requests: a grant of more permissions than the sandbox
// gives, the questions plan mode asks, and what an MCP server asks through
// codex — a form to fill, a page to open, a yes or a no. Each goes to every
// client of the thread, the first answer wins, and the others hear
// serverRequest/resolved. A question or a form with fields is answered with
// words; a grant, a page and a bare yes or no are a choice among a few.
const (
	methodPermissions = "item/permissions/requestApproval"
	// methodUserInput is experimental in the protocol: the daemon updates
	// itself, and a release may change it or drop it without a word. A
	// request of a shape the link does not read stays unanswered by the panel
	// and the thread shows it waits, as for any request the panel does not
	// know.
	methodUserInput   = "item/tool/requestUserInput"
	methodElicitation = "mcpServer/elicitation/request"
	flagUserInput     = "waitingOnUserInput"
)

// The modes of an elicitation the panel shows. A form names its fields with a
// schema; a page is an address the server asks the person to open; a
// verification is a challenge only codex itself can meet.
const (
	elicitForm       = "form"
	elicitURL        = "url"
	elicitVerify     = "openai/userVerification"
	elicitOpenAIForm = "openai/form"
	elicitOpenAI     = "openaiForm"
)

// Dismissed is what a model reads for a question a person put away from the
// panel to answer in the conversation — codex as the answer of every question,
// claude as the refusal of its question. The feed reads it back from a
// rollout, and the collector keeps the same words.
const Dismissed = "The person put the question away and will answer in the conversation."

// notePrefix marks the words a person adds beside a pick, the way codex's own
// clients send them: an answer is a list of strings, the labels picked first.
const notePrefix = "user_note: "

// Question is one question codex asks in plan mode, as the request names it.
type Question struct {
	ID       string   `json:"id"`
	Header   string   `json:"header"`
	Question string   `json:"question"`
	Other    bool     `json:"isOther"`
	Secret   bool     `json:"isSecret"`
	Options  []Option `json:"options"`
}

// Option is one option of a question.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Grants says the request asks for more permissions than the sandbox gives.
func (r Request) Grants() bool { return r.Method == methodPermissions }

// Elicits says the request is what an MCP server asks through codex.
func (r Request) Elicits() bool { return r.Method == methodElicitation }

// Asks says the request waits for a person's words rather than a choice
// among a few: a question of plan mode, or a form of an MCP server that has
// fields to fill.
func (r Request) Asks() bool {
	switch r.Method {
	case methodUserInput:
		return true
	case methodElicitation:
		fields, err := r.Fields()
		return err == nil && len(fields) > 0
	}
	return false
}

// Ask is a request that waits for a person's words, in the shape the panel
// keeps a question of claude's in: the questions with their options, and the
// id an answer names. A form of an MCP server is a question a field, with
// the server's name and its words above them.
type Ask struct {
	SessionID string        `json:"sessionId"`
	ToolUseID string        `json:"toolUseId"`
	CWD       string        `json:"cwd,omitempty"`
	At        time.Time     `json:"at"`
	Questions []AskQuestion `json:"questions"`
	Server    string        `json:"server,omitempty"`
	Message   string        `json:"message,omitempty"`
}

// AskQuestion is one question of an ask. Other says words of the person's own
// are taken beside the options — a question with no options takes only
// words; Secret says the words are not to be shown as they are typed; Required
// says a form does not go without it.
type AskQuestion struct {
	ID       string      `json:"id"`
	Text     string      `json:"text"`
	Header   string      `json:"header"`
	Multi    bool        `json:"multi"`
	Options  []AskOption `json:"options"`
	Other    bool        `json:"other"`
	Secret   bool        `json:"secret,omitempty"`
	Required bool        `json:"required,omitempty"`
}

// AskOption is one option of a question.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// How much of a question the panel carries: what an answer can address. A
// question of plan mode past them is still answered — with nothing, or with
// the note of a dismissal; a form past them is not shown as a form at all, for
// a field left out could be one the server requires.
const (
	askQuestions = 8
	askOptions   = 12
)

// AskID is what the panel names a question by: the item of the call that asks
// it — the id the rollout names the call by, so the feed closes the card of
// the question it answered — or the id of the request where there is none.
// The answer goes to the daemon by the id of the request either way.
func (r Request) AskID() string {
	if r.ItemID != "" && strings.Trim(strings.ToLower(r.ItemID), "abcdefghijklmnopqrstuvwxyz0123456789-_") == "" {
		return r.ItemID
	}
	return r.Key()
}

// Ask is the request as the panel shows it, nil for a request that is not a
// question or a form.
func (r Request) Ask() *Ask {
	if !r.Asks() {
		return nil
	}
	a := &Ask{SessionID: r.ThreadID, ToolUseID: r.AskID(), CWD: r.CWD, At: r.Since, Questions: []AskQuestion{}}
	if r.Method == methodElicitation {
		a.Server, a.Message = r.Server, r.Message
		fields, _ := r.Fields()
		for _, f := range fields {
			a.Questions = append(a.Questions, f.question())
		}
	} else {
		for _, q := range r.Questions {
			aq := AskQuestion{ID: q.ID, Text: q.Question, Header: q.Header, Options: []AskOption{},
				Other: q.Other || len(q.Options) == 0, Secret: q.Secret}
			for _, o := range q.Options {
				aq.Options = append(aq.Options, AskOption{Label: o.Label, Description: o.Description})
			}
			a.Questions = append(a.Questions, aq)
		}
	}
	if len(a.Questions) > askQuestions {
		a.Questions = a.Questions[:askQuestions]
	}
	for i := range a.Questions {
		if len(a.Questions[i].Options) > askOptions {
			a.Questions[i].Options = a.Questions[i].Options[:askOptions]
		}
	}
	return a
}

// Answers is the reply to a question of plan mode: the labels picked, or the
// words of the person's own, and a note beside a pick, by the id of each
// question. A question nobody answered gets an empty answer.
func (r Request) Answers(picks [][]int, texts, notes []string) (map[string]any, error) {
	out := map[string]any{}
	for i, q := range r.Questions {
		var said []string
		if i < len(picks) {
			for _, n := range picks[i] {
				if n < 1 || n > len(q.Options) {
					return nil, fmt.Errorf("question %d has no option %d", i+1, n)
				}
				said = append(said, q.Options[n-1].Label)
			}
		}
		if i < len(texts) && strings.TrimSpace(texts[i]) != "" {
			said = append(said, texts[i])
		}
		if i < len(notes) && strings.TrimSpace(notes[i]) != "" {
			said = append(said, notePrefix+notes[i])
		}
		if said == nil {
			said = []string{}
		}
		out[q.ID] = map[string]any{"answers": said}
	}
	return map[string]any{"answers": out}, nil
}

// Dismiss is the reply that puts a question of plan mode away: every question
// answered with the note that the person will answer in the conversation, so
// the model goes on knowing why it got no pick.
func (r Request) Dismiss() map[string]any {
	out := map[string]any{}
	for _, q := range r.Questions {
		out[q.ID] = map[string]any{"answers": []string{notePrefix + Dismissed}}
	}
	return map[string]any{"answers": out}
}

// Grant is the reply to a request for more permissions: the permissions it
// asks for, held for the turn or for the session. What the request leaves
// null is not granted.
func (r Request) Grant(scope string) map[string]any {
	granted := map[string]any{}
	var asked map[string]json.RawMessage
	_ = json.Unmarshal(r.Permissions, &asked)
	for _, key := range []string{"network", "fileSystem"} {
		if v := asked[key]; len(v) > 0 && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			granted[key] = v
		}
	}
	return map[string]any{"permissions": granted, "scope": scope}
}

// Deny is the reply that grants none of what a request for permissions asks:
// the turn goes on in the sandbox it had.
func (r Request) Deny() map[string]any {
	return map[string]any{"permissions": map[string]any{}, "scope": "turn"}
}

// Asked is what a request for permissions asks for, in lines a person reads:
// the network, and the places to read and to write.
func (r Request) Asked() []string {
	var p struct {
		Network *struct {
			Enabled *bool `json:"enabled"`
		} `json:"network"`
		FileSystem *struct {
			Read    []string `json:"read"`
			Write   []string `json:"write"`
			Entries []struct {
				Path   json.RawMessage `json:"path"`
				Access string          `json:"access"`
			} `json:"entries"`
		} `json:"fileSystem"`
	}
	_ = json.Unmarshal(r.Permissions, &p)
	var out []string
	if p.Network != nil && p.Network.Enabled != nil && *p.Network.Enabled {
		out = append(out, "network access")
	}
	if fs := p.FileSystem; fs != nil {
		for _, path := range fs.Read {
			out = append(out, "read "+path)
		}
		for _, path := range fs.Write {
			out = append(out, "write "+path)
		}
		for _, e := range fs.Entries {
			out = append(out, strings.TrimSpace(e.Access+" "+entryPath(e.Path)))
		}
	}
	return out
}

// entryPath words the path of an entry of a file system grant: a path as it
// is, or the special place codex names by a kind.
func entryPath(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		for _, key := range []string{"path", "value", "kind", "type"} {
			if v, ok := obj[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return string(raw)
}

// Elicitation is the part of an MCP server's request a person reads: which
// server asks, in which mode, its words, and what a mode adds — the schema of
// a form, the address of a page, the title of a verification.
type Elicitation struct {
	Server      string          `json:"serverName"`
	Mode        string          `json:"mode"`
	Message     string          `json:"message"`
	Schema      json.RawMessage `json:"requestedSchema"`
	URL         string          `json:"url"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
}

// Verification says the request asks for a check only codex itself can make.
func (r Request) Verification() bool {
	return r.Method == methodElicitation && r.Mode == elicitVerify
}

// Elicit is the reply to an MCP server: accept with what was filled, or
// decline, or cancel, with no content.
func Elicit(action string, content map[string]any) map[string]any {
	var c any
	if action == "accept" && content != nil {
		c = content
	}
	return map[string]any{"action": action, "content": c, "_meta": nil}
}

// Field is one field of a form an MCP server asks to fill, in the order the
// schema names them.
type Field struct {
	Name        string
	Title       string
	Description string
	// Type is string, number, integer, boolean, or array for several picks.
	Type     string
	Format   string
	Options  []FieldOption
	Required bool
}

// FieldOption is a value a field takes, with the words it is shown by.
type FieldOption struct {
	Value string
	Label string
}

func (f Field) question() AskQuestion {
	q := AskQuestion{ID: f.Name, Text: f.Title, Header: f.Name, Options: []AskOption{}, Required: f.Required,
		Multi: f.Type == "array"}
	if q.Text == "" {
		q.Text = f.Name
	}
	for _, o := range f.choices() {
		q.Options = append(q.Options, AskOption{Label: o.Label})
	}
	if f.Description != "" && f.Description != q.Text {
		q.Text += " — " + f.Description
	}
	q.Other = len(q.Options) == 0
	return q
}

// choices are what a field offers to pick from: its values, or yes and no.
func (f Field) choices() []FieldOption {
	if f.Type == "boolean" {
		return []FieldOption{{Value: "true", Label: "Yes"}, {Value: "false", Label: "No"}}
	}
	return f.Options
}

// Fields reads the form of an elicitation, in the order of its schema. A
// schema that is not a flat object of the kinds MCP names is not a form the
// panel can show, and the error says so.
func (r Request) Fields() ([]Field, error) {
	if r.Method != methodElicitation {
		return nil, nil
	}
	switch r.Mode {
	case elicitForm, elicitOpenAIForm, elicitOpenAI:
	default:
		return nil, nil
	}
	var schema struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if len(r.Schema) == 0 || json.Unmarshal(r.Schema, &schema) != nil {
		return nil, fmt.Errorf("the form of %s does not read", r.Server)
	}
	names, props, err := ordered(schema.Properties)
	if err != nil {
		return nil, fmt.Errorf("the form of %s does not read: %w", r.Server, err)
	}
	if len(names) > askQuestions {
		return nil, fmt.Errorf("the form of %s has %d fields, and the panel shows %d", r.Server, len(names), askQuestions)
	}
	var out []Field
	for i, name := range names {
		f, err := field(name, props[i])
		if err != nil {
			return nil, fmt.Errorf("the form of %s: %w", r.Server, err)
		}
		if len(f.Options) > askOptions {
			return nil, fmt.Errorf("field %s of the form of %s offers %d values, and the panel shows %d", name,
				r.Server, len(f.Options), askOptions)
		}
		f.Required = slices.Contains(schema.Required, name)
		out = append(out, f)
	}
	return out, nil
}

// Known says the request of an MCP server is in a mode the panel knows how to
// answer: a form, or a page to open. A mode of another release is refused
// rather than accepted blind.
func (r Request) Known() bool {
	switch r.Mode {
	case elicitForm, elicitOpenAIForm, elicitOpenAI, elicitURL:
		return true
	}
	return false
}

// ordered reads the members of a JSON object in the order they are written:
// a form shows its fields in the order the server named them.
func ordered(raw json.RawMessage) ([]string, []json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, fmt.Errorf("the fields are not an object")
	}
	var names []string
	var values []json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		name, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		names, values = append(names, name), append(values, v)
	}
	return names, values, nil
}

func field(name string, raw json.RawMessage) (Field, error) {
	var p struct {
		Type        string   `json:"type"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Format      string   `json:"format"`
		Enum        []string `json:"enum"`
		EnumNames   []string `json:"enumNames"`
		OneOf       []struct {
			Const string `json:"const"`
			Title string `json:"title"`
		} `json:"oneOf"`
		Items *struct {
			Enum  []string `json:"enum"`
			AnyOf []struct {
				Const string `json:"const"`
				Title string `json:"title"`
			} `json:"anyOf"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return Field{}, fmt.Errorf("field %s does not read: %w", name, err)
	}
	f := Field{Name: name, Title: p.Title, Description: p.Description, Type: p.Type, Format: p.Format}
	switch p.Type {
	case "string", "array":
	case "number", "integer", "boolean":
		return f, nil
	default:
		return Field{}, fmt.Errorf("field %s is of a kind the panel does not fill: %q", name, p.Type)
	}
	values, labels := p.Enum, p.EnumNames
	for _, o := range p.OneOf {
		values, labels = append(values, o.Const), append(labels, o.Title)
	}
	if p.Items != nil {
		values = append(values, p.Items.Enum...)
		for _, o := range p.Items.AnyOf {
			values, labels = append(values, o.Const), append(labels, o.Title)
		}
	}
	if p.Type == "array" && len(values) == 0 {
		return Field{}, fmt.Errorf("field %s is a list with nothing to pick from", name)
	}
	for i, v := range values {
		label := v
		if i < len(labels) && labels[i] != "" {
			label = labels[i]
		}
		f.Options = append(f.Options, FieldOption{Value: v, Label: label})
	}
	return f, nil
}

// Content is what a person filled in a form, as the server takes it: a value
// of the kind each field is of. A required field left empty stops it.
func (r Request) Content(picks [][]int, texts []string) (map[string]any, error) {
	fields, err := r.Fields()
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for i, f := range fields {
		var picked []int
		if i < len(picks) {
			picked = picks[i]
		}
		text := ""
		if i < len(texts) {
			text = strings.TrimSpace(texts[i])
		}
		v, err := f.value(picked, text)
		if err != nil {
			return nil, err
		}
		if v == nil {
			if f.Required {
				return nil, fmt.Errorf("%s is required, and nothing was given", f.label())
			}
			continue
		}
		out[f.Name] = v
	}
	return out, nil
}

func (f Field) label() string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

// value is what a field takes from a pick or words, nil when it got neither.
func (f Field) value(picks []int, text string) (any, error) {
	choices := f.choices()
	var chosen []string
	for _, n := range picks {
		if n < 1 || n > len(choices) {
			return nil, fmt.Errorf("%s has no option %d", f.label(), n)
		}
		chosen = append(chosen, choices[n-1].Value)
	}
	switch {
	case f.Type == "boolean" && len(chosen) == 1:
		return chosen[0] == "true", nil
	case f.Type == "array" && len(chosen) > 0:
		return chosen, nil
	case len(chosen) == 1:
		return chosen[0], nil
	case len(chosen) > 1:
		return nil, fmt.Errorf("%s takes one option, and %d were picked", f.label(), len(chosen))
	case text == "":
		return nil, nil
	}
	switch f.Type {
	case "number":
		n, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, fmt.Errorf("%s takes a number, not %q", f.label(), text)
		}
		return n, nil
	case "integer":
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s takes a whole number, not %q", f.label(), text)
		}
		return n, nil
	case "boolean", "array":
		return nil, fmt.Errorf("%s is answered with a pick, not with words", f.label())
	}
	if len(f.Options) > 0 {
		return nil, fmt.Errorf("%s is answered with a pick, not with words", f.label())
	}
	return text, nil
}
