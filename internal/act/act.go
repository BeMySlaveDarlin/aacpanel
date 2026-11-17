// Package act describes an outstanding turn for the person.
package act

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/action"
	"aacpanel/internal/request"
)

type Body struct {
	Steps    string   `json:"steps,omitempty"`
	Name     string   `json:"name,omitempty"`
	Template string   `json:"template,omitempty"`
	Source   string   `json:"source,omitempty"`
	Message  string   `json:"message,omitempty"`
	What     string   `json:"what,omitempty"`
	Answers  []string `json:"answers,omitempty"`
}

type Answer struct {
	Outcome string `json:"outcome,omitempty"`
	Note    string `json:"note,omitempty"`
	Path    string `json:"path,omitempty"`
}

type Input struct {
	Kind     string `json:"kind,omitempty"`
	Title    string `json:"title,omitempty"`
	Body     Body   `json:"body,omitempty"`
	Blocking *bool  `json:"blocking,omitempty"`
	ID       string `json:"id,omitempty"`
	Withdraw bool   `json:"withdraw,omitempty"`
}

func (i Input) Validate() error {
	if i.Withdraw || i.ID != "" {
		if !i.Withdraw || !request.ValidID(i.ID) || i.Kind != "" || i.Title != "" || i.Blocking != nil || (i.Body.Steps != "" || i.Body.Name != "" || i.Body.Template != "" || i.Body.Source != "" || i.Body.Message != "" || i.Body.What != "" || len(i.Body.Answers) > 0) {
			return errors.New("withdraw needs only an id and withdraw: true")
		}
		return nil
	}
	if strings.TrimSpace(i.Title) == "" || utf8.RuneCountInString(i.Title) > request.TitleMax {
		return errors.New("title needs 1 to 120 characters")
	}
	if i.Blocking == nil {
		return errors.New("blocking must say whether the session waits for this act")
	}
	switch i.Kind {
	case "do":
		return (request.Input{Title: i.Title, Steps: i.Body.Steps, Blocking: i.Blocking}).Validate()
	case "secret":
		if !action.SecretName(i.Body.Name) {
			return errors.New("the secret name is not valid")
		}
		if len(i.Body.Template) > 8192 {
			return errors.New("the secret template is longer than 8192 bytes")
		}
	case "ask", "call":
	default:
		return errors.New("the act kind is not valid")
	}
	return nil
}

type Record struct {
	ID           string     `json:"id"`
	Conversation string     `json:"conversation"`
	Session      string     `json:"session"`
	Kind         string     `json:"kind"`
	Blocking     bool       `json:"blocking"`
	Title        string     `json:"title"`
	Body         Body       `json:"body"`
	State        string     `json:"state"`
	Created      time.Time  `json:"created"`
	Closed       *time.Time `json:"closed,omitempty"`
	Answer       *Answer    `json:"answer,omitempty"`
	AnsweredBy   string     `json:"answeredBy"`
}

func (r Record) Open() bool { return r.State == "open" }

func (r Record) ValidateAnswer(answer, note string) error {
	if utf8.RuneCountInString(note) > request.NoteMax {
		return errors.New("the note is longer than 2000 characters")
	}
	if r.Kind == "do" {
		return request.ValidateAnswer(answer, note)
	}
	if r.Kind == "secret" && answer == "decline" {
		return nil
	}
	return errors.New("the answer does not apply to this act")
}

func (r Record) Reply(answer, note string) string {
	if r.Kind == "do" {
		return request.Reply(request.Record{ID: r.ID, Title: r.Title}, answer, note)
	}
	text := "The person won't give the secret \"" + r.Body.Name + "\" (act " + r.ID + ")."
	if note = strings.TrimSpace(note); note != "" {
		text += "\n\nTheir reason:\n" + note
	}
	return text + "\n\nDo not ask for it again; go on without it or say what it blocks."
}

// MessageID preserves the act's identity in transports that deduplicate messages.
func MessageID(id string) string {
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}
