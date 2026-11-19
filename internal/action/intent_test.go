package action

import (
	"context"
	"strings"
	"testing"

	"aacpanel/internal/sessionintent"
)

func TestSessionIntentAcceptsOnlyItsMessagesAndQuickPhrases(t *testing.T) {
	for _, values := range []sessionintent.Values{
		{"intent": "Queue"}, {"restartIntent": ""}, {"intent": "", "restartIntent": "Continue"},
		{"intent": strings.Repeat("🙂", IntentMax)},
		{"quick": ""}, {"quick": "Go on\n\nStatus"},
		{"quick": strings.Repeat(strings.Repeat("🙂", 200)+"\n", 5)},
	} {
		if err := (Request{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: values}).Validate(); err != nil {
			t.Errorf("%v: %v", values, err)
		}
	}
	for _, r := range []Request{
		{ID: "a1", Kind: SessionIntent, Target: "lead"},
		{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: sessionintent.Values{}},
		{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: sessionintent.Values{"model": "opus"}},
		{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: sessionintent.Values{"intent": "a\ab"}},
		{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: sessionintent.Values{"intent": strings.Repeat("é", IntentMax+1)}},
		{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: sessionintent.Values{"quick": strings.Repeat("go\n", 6)}},
		{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: sessionintent.Values{"quick": strings.Repeat("🙂", 201)}},
		{ID: "a1", Kind: SessionIntent, Target: "lead", Intents: sessionintent.Values{"quick": "go\a"}},
		{ID: "a1", Kind: SessionStop, Target: "lead", Intents: sessionintent.Values{"intent": ""}},
		{Ask: AskIntent},
		{Ask: AskIntent, Target: "lead", Intents: sessionintent.Values{"intent": ""}},
	} {
		if err := r.Validate(); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
}

type intentExecutor struct{ Executor }

func (intentExecutor) Intents(context.Context, string) (sessionintent.Values, error) {
	return sessionintent.Values{"restartIntent": "Continue"}, nil
}

func TestSessionMessagesCrossTheExecutorSocket(t *testing.T) {
	client := serve(t, intentExecutor{okExecutor("ok")})
	values, err := client.Intents(t.Context(), "lead")
	if err != nil || values["restartIntent"] != "Continue" {
		t.Fatalf("the messages arrived as %v, %v", values, err)
	}
	client = serve(t, okExecutor("ok"))
	if _, err := client.Intents(t.Context(), "lead"); err == nil {
		t.Fatal("an executor without the question answered it")
	}
}
