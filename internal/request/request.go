// Package request describes a task the session leaves for the person's hands.
package request

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// The limits keep a request readable on the phone without cutting its steps.
const (
	TitleMax   = 120
	StepsMax   = 10
	TextMax    = 500
	CommandMax = 2000
	NoteMax    = 2000
)

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// ValidID reports whether an id is one the service makes for a request.
func ValidID(id string) bool { return idPattern.MatchString(id) }

type Step struct {
	Who     string `json:"who"`
	Text    string `json:"text"`
	Command string `json:"command,omitempty"`
}

type Output struct {
	State   string     `json:"state"`
	Term    string     `json:"term,omitempty"`
	Exit    *int       `json:"exit,omitempty"`
	Started *time.Time `json:"started,omitempty"`
	Ended   *time.Time `json:"ended,omitempty"`
	Tail    string     `json:"tail,omitempty"`
	Log     string     `json:"log,omitempty"`
	Note    string     `json:"note,omitempty"`
}

// A saved terminal reservation is already a launch in progress; pending steps
// without a terminal still await a person's Run after being reopened.
func (o *Output) Running() bool {
	return o != nil && (o.State == "running" || (o.State == "pending" && o.Term != ""))
}

// Input admits one operation, so a malformed report cannot create another chain.
type Input struct {
	Title    string `json:"title,omitempty"`
	Steps    []Step `json:"steps,omitempty"`
	Blocking *bool  `json:"blocking,omitempty"`
	ID       string `json:"id,omitempty"`
	Withdraw bool   `json:"withdraw,omitempty"`
	Reopen   int    `json:"reopen,omitempty"`
	Step     int    `json:"step,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Note     string `json:"note,omitempty"`
}

func (i Input) Validate() error {
	if i.ID != "" || i.Withdraw || i.Reopen != 0 || i.Step != 0 || i.Outcome != "" || i.Note != "" {
		if !ValidID(i.ID) || i.Title != "" || i.Steps != nil || i.Blocking != nil {
			return errors.New("a chain operation needs only its id and operation fields")
		}
		if i.Withdraw && i.Reopen == 0 && i.Step == 0 && i.Outcome == "" && i.Note == "" {
			return nil
		}
		if !i.Withdraw && i.Reopen >= 1 && i.Reopen <= StepsMax && i.Step == 0 && i.Outcome == "" && i.Note == "" {
			return nil
		}
		if !i.Withdraw && i.Reopen == 0 && i.Step >= 1 && i.Step <= StepsMax && (i.Outcome == "done" || i.Outcome == "failed") && utf8.RuneCountInString(i.Note) <= NoteMax {
			return nil
		}
		return errors.New("give withdraw, reopen, or step with outcome done or failed and an optional note")
	}
	if strings.TrimSpace(i.Title) == "" || utf8.RuneCountInString(i.Title) > TitleMax {
		return errors.New("title needs 1 to 120 characters")
	}
	if i.Blocking == nil {
		return errors.New("blocking must say whether the session waits for this task")
	}
	return ValidateSteps(i.Steps)
}

func ValidateSteps(steps []Step) error {
	if len(steps) < 1 || len(steps) > StepsMax {
		return errors.New("steps needs 1 to 10 steps")
	}
	if steps[0].Who != "you" {
		return errors.New("the first step belongs to the person")
	}
	for _, s := range steps {
		if s.Who != "you" && s.Who != "agent" {
			return errors.New("who must be you or agent")
		}
		if strings.TrimSpace(s.Text) == "" || utf8.RuneCountInString(s.Text) > TextMax {
			return errors.New("step text needs 1 to 500 characters")
		}
		if strings.IndexByte(s.Command, 0) >= 0 || utf8.RuneCountInString(s.Command) > CommandMax || (s.Who == "agent" && s.Command != "") || (s.Command != "" && strings.TrimSpace(s.Command) == "") {
			return errors.New("command needs 1 to 2000 characters and belongs only to the person")
		}
	}
	return nil
}

// Record is the service's copy of a request and the person's answer to it.
type Record struct {
	ID           string     `json:"id"`
	Conversation string     `json:"conversation"`
	Session      string     `json:"session"`
	Title        string     `json:"title"`
	Steps        string     `json:"steps"`
	Blocking     bool       `json:"blocking"`
	Created      time.Time  `json:"created"`
	Answered     *time.Time `json:"answered,omitempty"`
	Withdrawn    *time.Time `json:"withdrawn,omitempty"`
	Answer       string     `json:"answer,omitempty"`
	Note         string     `json:"note,omitempty"`
}

// Open reports whether the request still waits for the person.
func (r Record) Open() bool { return r.Answered == nil && r.Withdrawn == nil }

// ValidateAnswer checks what the person sends, before any message is delivered.
func ValidateAnswer(answer, note string) error {
	if answer != "done" && answer != "cant" {
		return errors.New("answer must be done or cant")
	}
	if utf8.RuneCountInString(note) > NoteMax {
		return errors.New("the note is longer than 2000 characters")
	}
	return nil
}

// Reply tells the session whose answer this is, even when other messages arrived.
func Reply(r Record, answer, note string) string {
	word := "Done"
	if answer == "cant" {
		word = "Can't"
	}
	text := "The person answered your request “" + r.Title + "” (" + r.ID + "): " + word + "."
	if note = strings.TrimSpace(note); note != "" {
		label := "note"
		if answer == "cant" {
			label = "reason"
		}
		text += "\n\nTheir " + label + ":\n" + note
	}
	return text
}
