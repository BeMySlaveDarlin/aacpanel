// Package act describes an outstanding turn for the person.
package act

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/action"
	"aacpanel/internal/question"
	"aacpanel/internal/request"
)

type Body struct {
	Steps     string              `json:"-"`
	Chain     []request.Step      `json:"-"`
	Name      string              `json:"name,omitempty"`
	Template  string              `json:"template,omitempty"`
	Source    string              `json:"source,omitempty"`
	Message   string              `json:"message,omitempty"`
	What      string              `json:"what,omitempty"`
	Questions []question.Question `json:"questions,omitempty"`
	Action    string              `json:"action,omitempty"`
}

// Stored Markdown and structured steps share one wire key. Keeping the two
// representations explicit lets archived acts retain their original rendering.
func (b Body) MarshalJSON() ([]byte, error) {
	type fields Body
	var steps any
	if b.Chain != nil {
		steps = b.Chain
	} else if b.Steps != "" {
		steps = b.Steps
	}
	return json.Marshal(struct {
		fields
		Steps any `json:"steps,omitempty"`
	}{fields(b), steps})
}
func (b *Body) UnmarshalJSON(data []byte) error {
	type fields Body
	var raw struct {
		fields
		Steps json.RawMessage `json:"steps"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&raw); err != nil {
		return err
	}
	*b = Body(raw.fields)
	if len(raw.Steps) == 0 || string(raw.Steps) == "null" {
		return nil
	}
	if raw.Steps[0] == '"' {
		return json.Unmarshal(raw.Steps, &b.Steps)
	}
	d = json.NewDecoder(bytes.NewReader(raw.Steps))
	d.DisallowUnknownFields()
	return d.Decode(&b.Chain)
}

type Answer struct {
	Answers []question.Answer `json:"answers,omitempty"`
	Outcome string            `json:"outcome,omitempty"`
	Note    string            `json:"note,omitempty"`
	Path    string            `json:"path,omitempty"`
}

type Input struct {
	Kind     string `json:"kind,omitempty"`
	Title    string `json:"title,omitempty"`
	Body     Body   `json:"body,omitempty"`
	Blocking *bool  `json:"blocking,omitempty"`
	ID       string `json:"id,omitempty"`
	Withdraw bool   `json:"withdraw,omitempty"`
	Reopen   int    `json:"reopen,omitempty"`
	Step     int    `json:"step,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Note     string `json:"note,omitempty"`
}

func (i Input) Validate() error {
	if i.Withdraw || i.ID != "" || i.Reopen != 0 || i.Step != 0 || i.Outcome != "" || i.Note != "" {
		if i.Kind != "" || i.Title != "" || i.Blocking != nil || i.Body.Steps != "" || i.Body.Chain != nil || i.Body.Name != "" || i.Body.Template != "" || i.Body.Source != "" || i.Body.Message != "" || i.Body.What != "" || len(i.Body.Questions) > 0 || i.Body.Action != "" {
			return errors.New("a chain operation carries no act body")
		}
		return (request.Input{ID: i.ID, Withdraw: i.Withdraw, Reopen: i.Reopen, Step: i.Step, Outcome: i.Outcome, Note: i.Note}).Validate()
	}
	if strings.TrimSpace(i.Title) == "" || utf8.RuneCountInString(i.Title) > request.TitleMax {
		return errors.New("title needs 1 to 120 characters")
	}
	if i.Blocking == nil {
		return errors.New("blocking must say whether the session waits for this act")
	}
	switch i.Kind {
	case "do":
		if i.Body.Chain != nil {
			return request.ValidateSteps(i.Body.Chain)
		}
		if strings.TrimSpace(i.Body.Steps) == "" || utf8.RuneCountInString(i.Body.Steps) > 2000 {
			return errors.New("steps needs 1 to 2000 characters")
		}
	case "secret":
		if !action.SecretName(i.Body.Name) {
			return errors.New("the secret name is not valid")
		}
		if len(i.Body.Template) > 8192 {
			return errors.New("the secret template is longer than 8192 bytes")
		}
	case "ask":
		if *i.Blocking {
			return errors.New("ask does not block the session")
		}
		if i.Body.Source != "tool" && i.Body.Source != "prose" {
			return errors.New("ask source must be tool or prose")
		}
		if len(question.Clean(i.Body.Questions)) == 0 {
			return errors.New("ask needs 1 to 4 questions")
		}
	case "call":
	default:
		return errors.New("the act kind is not valid")
	}
	return nil
}

type Record struct {
	ID           string            `json:"id"`
	Conversation string            `json:"conversation"`
	Session      string            `json:"session"`
	Kind         string            `json:"kind"`
	Blocking     bool              `json:"blocking"`
	Title        string            `json:"title"`
	Body         Body              `json:"body"`
	State        string            `json:"state"`
	Created      time.Time         `json:"created"`
	Closed       *time.Time        `json:"closed,omitempty"`
	Answer       *Answer           `json:"answer,omitempty"`
	AnsweredBy   string            `json:"answeredBy"`
	Step         *int              `json:"step,omitempty"`
	Outputs      []*request.Output `json:"outputs,omitempty"`
	Notified     int               `json:"-"`
	Notice       *Notice           `json:"-"`
}

func (r Record) Open() bool { return r.State == "open" || r.State == "failed" }

func (r Record) ValidateAnswer(answer, note string, answers ...question.Answer) error {
	if utf8.RuneCountInString(note) > request.NoteMax {
		return errors.New("the note is longer than 2000 characters")
	}
	if r.Kind == "ask" {
		// Put away, a question carries no answer: the person will answer in
		// the conversation.
		if answer == "decline" {
			if len(answers) > 0 {
				return errors.New("a question put away carries no answers")
			}
			return nil
		}
		return question.ValidateAnswers(r.Body.Questions, answers)
	}
	if r.Kind == "do" {
		return request.ValidateAnswer(answer, note)
	}
	if r.Kind == "secret" && answer == "decline" {
		return nil
	}
	return errors.New("the answer does not apply to this act")
}

func (r Record) Reply(answer, note string, answers ...question.Answer) string {
	if r.Kind == "ask" {
		who := "your ask"
		if r.Body.Source == "prose" {
			who = "the questions of your last message"
		}
		if answer == "decline" {
			text := "The person put " + who + " (" + r.ID + ") away and will answer in the conversation:"
			for i, q := range r.Body.Questions {
				text += fmt.Sprintf("\n%d. %s", i+1, q.Question)
			}
			return text
		}
		text := "The person answered " + who + " (" + r.ID + "):"
		for i, a := range answers {
			reply := strings.Join(a.Picks, ", ")
			if words := strings.TrimSpace(a.Text); words != "" {
				if reply != "" {
					reply += "; "
				}
				reply += words
			}
			text += fmt.Sprintf("\n%d. %s\n   %s", i+1, r.Body.Questions[i].Question, reply)
		}
		return text
	}
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

func (i *Input) Clean() {
	if i.Kind != "ask" {
		return
	}
	i.Body.Questions = question.Clean(i.Body.Questions)
	i.Body.What, i.Body.Action = question.Line(i.Body.What, 140), question.Line(i.Body.Action, 140)
	if i.Body.What == "" && len(i.Body.Questions) > 0 {
		i.Body.What = question.Line(i.Body.Questions[0].Question, 140)
	}
	i.Title = question.Line(i.Title, 120)
}
