package action

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"aacpanel/internal/checklist"
)

func reviewInput() ChecklistInput {
	return ChecklistInput{Checklist: ChecklistState{Items: []checklist.Item{{Text: "Fix the hook", Status: checklist.Active}}}, Log: "CALL Edit: main.go"}
}

func TestChecklistInput(t *testing.T) {
	base := Request{Ask: AskChecklist, Target: "work", Conversation: "11111111-1111-4111-8111-111111111111"}
	input := reviewInput()
	base.Checklist = &input
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.Checklist = nil },
		func(r *Request) { r.Conversation = "other" },
		func(r *Request) { r.Target = "" },
		func(r *Request) { r.Ask = AskQuestion; r.Text = "Publish?" },
		func(r *Request) { r.Text = "Publish?" },
		func(r *Request) { r.Part = "hooks" },
		func(r *Request) { r.History = []SideTurn{{Question: "Hi"}} },
		func(r *Request) { r.AlreadyAsked = []string{"Publish?"} },
		func(r *Request) { r.Checklist.Log = " " },
		func(r *Request) { r.Checklist.Log = strings.Repeat("é", ChecklistLogMax+1) },
		func(r *Request) { r.Checklist.Checklist.Items = nil },
		func(r *Request) { r.Checklist.Checklist.Items = make([]checklist.Item, checklist.MaxItems+1) },
		func(r *Request) { r.Checklist.Checklist.Items = []checklist.Item{{Text: "Step", Status: "waiting"}} },
		func(r *Request) {
			r.Checklist.Checklist.Items = []checklist.Item{{Text: " ", Status: checklist.Active}}
		},
		func(r *Request) {
			r.Checklist.Checklist.Items = []checklist.Item{{Text: strings.Repeat("é", checklist.MaxText+1), Status: checklist.Active}}
		},
		func(r *Request) { r.Checklist.Checklist.Note = strings.Repeat("é", checklist.MaxNote+1) },
	} {
		r := base
		input := reviewInput()
		r.Checklist = &input
		change(&r)
		if err := r.Validate(); err == nil {
			t.Fatalf("invalid review accepted: %+v", r)
		}
	}
	input.Log = strings.Repeat("é", ChecklistLogMax)
	if err := base.Validate(); err != nil {
		t.Fatalf("the character ceiling counted bytes: %v", err)
	}
	if err := (Request{Ask: AskChildren, Target: "work"}).Validate(); err == nil {
		t.Fatal("unbound child lookup was accepted")
	}
}

func TestChecklistVerdict(t *testing.T) {
	for _, text := range []string{`{"current":true,"confidence":0.7}`, `{"current":false,"confidence":0}`} {
		if _, err := ParseChecklistVerdict([]byte(text)); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
	for _, text := range []string{"", "not JSON", "null", "{}", `{"current":true}`, `{"confidence":0.9}`, `{"current":null,"confidence":0.9}`,
		`{"current":true,"confidence":null}`, `{"current":true,"confidence":-0.1}`, `{"current":true,"confidence":1.01}`,
		`{"current":"true","confidence":0.9}`, `{"current":true,"confidence":"0.9"}`, `{"current":true,"confidence":0.9,"extra":true}`,
		`{"current":true,"confidence":0.9} {}`} {
		if _, err := ParseChecklistVerdict([]byte(text)); err == nil {
			t.Errorf("unreadable verdict accepted: %s", text)
		}
	}
}

type checklistExec struct{ failed bool }

func (checklistExec) Execute(context.Context, Request) (string, error) {
	return "", errors.New("no actions")
}
func (e checklistExec) ChecklistCurrent(_ context.Context, target, conversation string, input ChecklistInput) (ChecklistVerdict, error) {
	if e.failed || target != "work" || conversation != "11111111-1111-4111-8111-111111111111" || !reflect.DeepEqual(input, reviewInput()) {
		return ChecklistVerdict{}, errors.New("invalid review")
	}
	return ChecklistVerdict{Current: true, Confidence: 0.7}, nil
}
func (checklistExec) Children(context.Context, string, string) ([]ChildSession, error) {
	return []ChildSession{{Key: "process-42-71", Conversation: "child"}}, nil
}

func TestChecklistSocket(t *testing.T) {
	client := serve(t, checklistExec{})
	verdict, err := client.ChecklistCurrent(t.Context(), "work", "11111111-1111-4111-8111-111111111111", reviewInput())
	if err != nil || !verdict.Current || verdict.Confidence != 0.7 {
		t.Fatalf("checklist verdict: %+v %v", verdict, err)
	}
	children, err := client.Children(t.Context(), "work", "11111111-1111-4111-8111-111111111111")
	if err != nil || len(children) != 1 || children[0].Key != "process-42-71" {
		t.Fatalf("child identities: %+v %v", children, err)
	}
	for _, client := range []*Client{serve(t, checklistExec{failed: true}), serve(t, openingExecutor{})} {
		if _, err := client.ChecklistCurrent(t.Context(), "work", "11111111-1111-4111-8111-111111111111", reviewInput()); err == nil {
			t.Fatal("an unavailable review became a verdict")
		}
	}
}
