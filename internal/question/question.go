// Package question defines the form shared by classified and explicit asks.
package question

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type Question struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Multi    bool     `json:"multi"`
	Options  []Option `json:"options"`
}

type Verdict struct {
	Questions []Question `json:"questions"`
	Action    string     `json:"action"`
	What      string     `json:"what"`
}

func Line(s string, limit int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	return strings.TrimSpace(string(r[:min(len(r), limit)]))
}

func Clean(questions []Question) []Question {
	out := []Question{}
	for _, q := range questions {
		q.Question, q.Header = Line(q.Question, 200), Line(q.Header, 12)
		if q.Question == "" {
			continue
		}
		options := []Option{}
		for _, o := range q.Options {
			o.Label, o.Description = Line(o.Label, 40), Line(o.Description, 120)
			if o.Label == "" || slices.ContainsFunc(options, func(v Option) bool { return v.Label == o.Label }) {
				continue
			}
			options = append(options, o)
			if len(options) == 4 {
				break
			}
		}
		q.Options = options
		out = append(out, q)
		if len(out) == 4 {
			break
		}
	}
	return out
}

func ParseVerdict(raw []byte) (Verdict, error) {
	var in struct {
		Questions *[]Question `json:"questions"`
		Action    *string     `json:"action"`
		What      *string     `json:"what"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Questions == nil || in.Action == nil || in.What == nil {
		return Verdict{}, errors.New("the question classifier returned an invalid verdict")
	}
	v := Verdict{Questions: Clean(*in.Questions), Action: Line(*in.Action, 140), What: Line(*in.What, 140)}
	if len(v.Questions) == 0 && v.Action == "" {
		v.What = ""
	}
	if (len(v.Questions) > 0 || v.Action != "") && v.What == "" {
		return Verdict{}, errors.New("the question classifier did not name what is asked")
	}
	return v, nil
}

type Answer struct {
	Picks []string `json:"picks"`
	Text  string   `json:"text"`
}

func ValidateAnswers(questions []Question, answers []Answer) error {
	if len(questions) == 0 || len(answers) != len(questions) {
		return errors.New("give one answer per question")
	}
	for i, a := range answers {
		q := questions[i]
		if len(a.Picks) == 0 && strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("question %d needs an answer", i+1)
		}
		if !q.Multi && len(a.Picks) > 1 {
			return fmt.Errorf("question %d allows one option", i+1)
		}
		for j, pick := range a.Picks {
			if slices.Contains(a.Picks[:j], pick) || !slices.ContainsFunc(q.Options, func(o Option) bool { return o.Label == pick }) {
				return fmt.Errorf("question %d has an invalid option", i+1)
			}
		}
	}
	return nil
}
