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

// Every question the server asks goes through the wrapper, and the server finds
// out whether one is answered by what the wrapper implements: a question it
// forgets to pass on is answered "cannot" on a host whose executor can.
func TestAuditedPassesOnEveryQuestion(t *testing.T) {
	var a any = audited{next: modelsExec{}}
	for name, ok := range map[string]bool{
		"Asker":       func() bool { _, ok := a.(action.Asker); return ok }(),
		"WindowAsker": func() bool { _, ok := a.(action.WindowAsker); return ok }(),
		"ModelsAsker": func() bool { _, ok := a.(action.ModelsAsker); return ok }(),
		"McpAsker":    func() bool { _, ok := a.(action.McpAsker); return ok }(),
		"StatusAsker": func() bool { _, ok := a.(action.StatusAsker); return ok }(),
		"Capable":     func() bool { _, ok := a.(action.Capable); return ok }(),
	} {
		if !ok {
			t.Errorf("the journal wrapper does not implement action.%s", name)
		}
	}
	got, err := a.(action.ModelsAsker).Models(t.Context(), "aacpanel")
	if err != nil || got == nil || got.Transport != "stream" {
		t.Errorf("the question about models came back as %+v, %v", got, err)
	}
	if _, err := any(audited{next: muteExec{}}).(action.ModelsAsker).Models(t.Context(), "aacpanel"); err == nil {
		t.Error("an executor that does not know models said nothing instead of refusing")
	}
	mcp, err := a.(action.McpAsker).Mcp(t.Context(), "aacpanel")
	if err != nil || mcp == nil || len(mcp.Servers) != 1 {
		t.Errorf("the question about MCP came back as %+v, %v", mcp, err)
	}
	if _, err := any(audited{next: muteExec{}}).(action.McpAsker).Mcp(t.Context(), "aacpanel"); err == nil {
		t.Error("an executor that does not know MCP said nothing instead of refusing")
	}
	status, err := a.(action.StatusAsker).Status(t.Context(), "aacpanel")
	if err != nil || status == nil || status.Version != "2.1.282" {
		t.Errorf("the question about the session came back as %+v, %v", status, err)
	}
	if _, err := any(audited{next: muteExec{}}).(action.StatusAsker).Status(t.Context(), "aacpanel"); err == nil {
		t.Error("an executor that does not know the session said nothing instead of refusing")
	}
}

type modelsExec struct{ muteExec }

func (modelsExec) Models(context.Context, string) (*action.Models, error) {
	return &action.Models{Transport: "stream"}, nil
}

func (modelsExec) Status(context.Context, string) (*action.Status, error) {
	return &action.Status{Transport: "stream", Version: "2.1.282"}, nil
}

func (modelsExec) Mcp(context.Context, string) (*action.Mcp, error) {
	return &action.Mcp{Transport: "stream", Servers: []action.McpServer{{Name: "docker", Status: "connected"}}}, nil
}

type keepingExec struct {
	muteExec
	got *[]action.Guard
}

func (k keepingExec) KeepGuards(_ context.Context, guards []action.Guard) error {
	*k.got = guards
	return nil
}

// The guards reach the executor through the journal wrapper: without it the
// server answers "does not keep" while the host file goes stale unseen.
func TestAuditedForwardsTheGuards(t *testing.T) {
	var got []action.Guard
	keeper, ok := any(audited{next: keepingExec{got: &got}}).(action.GuardKeeper)
	if !ok {
		t.Fatal("the journal wrapper does not implement action.GuardKeeper")
	}
	want := []action.Guard{{Path: "/opt/x", Cap: 80, Restart: true}}
	if err := keeper.KeepGuards(t.Context(), want); err != nil || len(got) != 1 || got[0] != want[0] {
		t.Errorf("the guards reached the executor as %v, %v", got, err)
	}
	if err := any(audited{next: muteExec{}}).(action.GuardKeeper).KeepGuards(t.Context(), want); err == nil {
		t.Error("an executor that keeps no guards said nothing instead of refusing")
	}
}
