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
	TitleMax = 120
	StepsMax = 2000
	NoteMax  = 2000
)

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// ValidID reports whether an id is one the service makes for a request.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Input either leaves a task or withdraws one; the caller is bound separately.
type Input struct {
	Title    string `json:"title,omitempty"`
	Steps    string `json:"steps,omitempty"`
	Blocking *bool  `json:"blocking,omitempty"`
	ID       string `json:"id,omitempty"`
	Withdraw bool   `json:"withdraw,omitempty"`
}

// Validate refuses mixed operations rather than guessing which the model meant.
func (i Input) Validate() error {
	if i.Withdraw || i.ID != "" {
		if !i.Withdraw || !ValidID(i.ID) || i.Title != "" || i.Steps != "" || i.Blocking != nil {
			return errors.New("withdraw needs only an id and withdraw: true")
		}
		return nil
	}
	if strings.TrimSpace(i.Title) == "" || utf8.RuneCountInString(i.Title) > TitleMax {
		return errors.New("title needs 1 to 120 characters")
	}
	if strings.TrimSpace(i.Steps) == "" || utf8.RuneCountInString(i.Steps) > StepsMax {
		return errors.New("steps needs 1 to 2000 characters")
	}
	if i.Blocking == nil {
		return errors.New("blocking must say whether the session waits for this task")
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
		text += "\n\nTheir note:\n" + note
	}
	return text
}
