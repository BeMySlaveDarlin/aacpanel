package main

import (
	"context"
	"errors"
	"testing"

	"aacpanel/internal/action"
)

func TestAuditedForwardsTheQuestion(t *testing.T) {
	want := &action.Permission{Tool: "Bash command", Options: []action.PermOption{{N: 1, Text: "Yes"}}}
	a := audited{next: askingExec{perm: want}}

	asker, ok := any(a).(action.Asker)
	if !ok {
		t.Fatal("the journal wrapper does not implement action.Asker — the server will answer \"cannot do it\" " +
			"while the parsing works")
	}
	got, err := asker.Permission(t.Context(), "aacpanel")
	if err != nil {
		t.Fatalf("the question did not pass through the wrapper: %v", err)
	}
	if got == nil || got.Tool != want.Tool {
		t.Errorf("the wrapper returned %+v, the dialog of the executor was expected", got)
	}
}

func TestAuditedSaysWhenExecutorCannotAsk(t *testing.T) {
	asker, ok := any(audited{next: muteExec{}}).(action.Asker)
	if !ok {
		t.Fatal("the journal wrapper does not implement action.Asker")
	}
	if _, err := asker.Permission(t.Context(), "aacpanel"); err == nil {
		t.Error("an executor unable to read dialogs said nothing instead of refusing")
	}
}

func TestAuditedForwardsWhatTheHostCanDo(t *testing.T) {
	able, ok := any(audited{next: ableExec{kinds: []action.Kind{action.ContainerStop}}}).(action.Capable)
	if !ok {
		t.Fatal("the journal wrapper does not implement action.Capable — the panel gets the full list " +
			"on a machine where half of the actions cannot be carried out")
	}
	got := able.Kinds()
	if len(got) != 1 || got[0] != action.ContainerStop {
		t.Errorf("the wrapper returned %v, the list of the executor was expected", got)
	}
}

func TestAuditedKeepsFullListForOlderExecutor(t *testing.T) {
	able, ok := any(audited{next: muteExec{}}).(action.Capable)
	if !ok {
		t.Fatal("the journal wrapper does not implement action.Capable")
	}
	if got := able.Kinds(); len(got) != len(action.Kinds) {
		t.Errorf("an executor with no Capable got %d actions out of %d", len(got), len(action.Kinds))
	}
}

type ableExec struct{ kinds []action.Kind }

func (e ableExec) Execute(context.Context, action.Request) (string, error) { return "", nil }
func (e ableExec) Kinds() []action.Kind                                    { return e.kinds }

type askingExec struct{ perm *action.Permission }

func (e askingExec) Execute(context.Context, action.Request) (string, error) { return "", nil }
func (e askingExec) Permission(context.Context, string) (*action.Permission, error) {
	return e.perm, nil
}

type muteExec struct{}

func (muteExec) Execute(context.Context, action.Request) (string, error) {
	return "", errors.New("cannot do it")
}
