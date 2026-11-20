package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"aacpanel/internal/action"
	"aacpanel/internal/question"
)

type questionExecutor struct {
	calls        atomic.Int32
	yes          bool
	failed       bool
	action       string
	started      chan struct{}
	release      <-chan struct{}
	alreadyAsked []string
	noSummary    bool
}

func (q *questionExecutor) Execute(context.Context, action.Request) (string, error) {
	return "", errors.New("no actions")
}
func (q *questionExecutor) Question(ctx context.Context, target, conversation, text string, alreadyAsked []string) (question.Verdict, error) {
	q.calls.Add(1)
	if !slices.Equal(alreadyAsked, q.alreadyAsked) {
		return question.Verdict{}, fmt.Errorf("open questions: %v; want %v", alreadyAsked, q.alreadyAsked)
	}
	if target != "sentinel" || conversation != requestConversation || text != "Publish? The checks passed." {
		return question.Verdict{}, errors.New("wrong candidate")
	}
	if q.failed {
		return question.Verdict{}, errors.New("classifier refused")
	}
	if q.started != nil {
		q.started <- struct{}{}
	}
	if q.release != nil {
		select {
		case <-q.release:
		case <-ctx.Done():
			return question.Verdict{}, ctx.Err()
		}
	}
	v := question.Verdict{Questions: []question.Question{}, Action: q.action}
	if q.yes {
		v.Questions = publishQuestions()
	}
	if (q.yes || q.action != "") && !q.noSummary {
		v.What = "Publish?"
	}
	return v, nil
}

func publishQuestions() []question.Question {
	return []question.Question{{Question: "Publish?", Header: "Release", Options: []question.Option{{Label: "Publish"}, {Label: "Wait"}}}}
}
