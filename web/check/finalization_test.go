package check

import (
	"strings"
	"testing"
)

func TestCloseOffersSavingByDefaultAndImmediateInTheSameSheet(t *testing.T) {
	var got struct {
		Prepared []struct {
			Choices []struct {
				Text   string
				Picked bool
			}
			Sent []struct {
				Kind, Target string
				Params       map[string]any
			}
		}
	}
	runFixture(t, "closesheet.html", &got)
	if len(got.Prepared) != 2 {
		t.Fatalf("prepared closes: %+v", got.Prepared)
	}
	for i, c := range got.Prepared {
		if len(c.Choices) != 2 || !c.Choices[0].Picked || c.Choices[1].Picked || !strings.HasPrefix(c.Choices[0].Text, "Save and close") || !strings.HasPrefix(c.Choices[1].Text, "Close immediately") {
			t.Fatalf("choices: %+v", c.Choices)
		}
		if len(c.Sent) != 1 || c.Sent[0].Kind != "session.close" || c.Sent[0].Target != "prepared" || c.Sent[0].Params["prepare"] != (i == 0) || c.Sent[0].Params["conversation"] != "c2" {
			t.Fatalf("close: %+v", c.Sent)
		}
	}
}

func TestWrapUpUsesThePanelCycleFromItsCommandsRow(t *testing.T) {
	var got struct {
		WrapUpConfirm string
		WrapUpSent    []struct {
			Kind, Target string
			Params       map[string]any
		}
		Error string
	}
	runFixture(t, "commandsheet.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	if got.WrapUpConfirm != "Wrap up helios?" || len(got.WrapUpSent) != 1 || got.WrapUpSent[0].Kind != "session.finalize" || len(got.WrapUpSent[0].Params) != 0 {
		t.Fatalf("wrap up: %+v", got)
	}
}

func TestWrapUpIsAPanelActionAndNoLongerASlashCommand(t *testing.T) {
	groups := runModuleJS(t, "src/screens/chat/commands.js", "panelCommands", [][]any{{map[string]any{"stream": true}}})
	found := false
	for _, group := range groups[0].([]any) {
		for _, raw := range group.(map[string]any)["rows"].([]any) {
			row := raw.(map[string]any)
			if row["line"] != "Wrap up" {
				continue
			}
			found = true
			if row["kind"] != "finalize" || row["action"] != "session.finalize" {
				t.Fatalf("wrap up sends a slash command: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("the commands list lost Wrap up")
	}
	for _, value := range runModuleJS(t, "src/actions/registry.js", "parseCommand", [][]any{{"/finalize", true}, {"/finalize", false}}) {
		row, _ := value.(map[string]any)
		if row["ready"] != false || row["refused"] != true {
			t.Fatalf("/finalize can still be sent: %+v", value)
		}
	}
}

func TestFinalizedIsTheQuietSessionState(t *testing.T) {
	calls := [][]any{}
	for _, extra := range []map[string]any{
		{}, {"checklist": map[string]any{"items": []any{map[string]any{"text": "Save", "status": "done"}}}},
		{"status": "busy"}, {"ask": map[string]any{"header": "Next task"}}, {"work": map[string]any{"tasks": 1}},
	} {
		row := map[string]any{"status": "idle", "finalizedAt": "2026-01-01T00:00:00Z"}
		for k, v := range extra {
			row[k] = v
		}
		calls = append(calls, []any{row})
	}
	got := runModuleJS(t, "src/screens/sessions/state.js", "stateOf", calls)
	for i, value := range got {
		row := value.(map[string]any)
		if (row["word"] == "finalized") != (i < 2) {
			t.Fatalf("case %d: %+v", i, row)
		}
	}
}
