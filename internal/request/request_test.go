package request

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

func TestRequestValidatesBothOperations(t *testing.T) {
	blocking := false
	valid := Input{Title: strings.Repeat("界", TitleMax), Steps: strings.Repeat("界", StepsMax), Blocking: &blocking}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]Input{
		"empty": {}, "no steps": {Title: "Do it", Blocking: &blocking},
		"no blocking": {Title: "Do it", Steps: "Run it"},
		"long title":  {Title: valid.Title + "x", Steps: "Run", Blocking: &blocking},
		"long steps":  {Title: "Run", Steps: valid.Steps + "x", Blocking: &blocking},
		"id alone":    {ID: strings.Repeat("a", 32)}, "bad id": {ID: "../other", Withdraw: true},
		"mixed": {ID: strings.Repeat("a", 32), Withdraw: true, Title: "Other"},
	} {
		if err := input.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := (Input{ID: strings.Repeat("a", 32), Withdraw: true}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRequestTakesItsConversationOnlyFromTheCaller(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var got []map[string]any
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/requests" || r.Method != http.MethodPost {
			t.Errorf("wrong destination: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		got = append(got, body)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}))
	defer panel.Close()
	tool := Tool(panel.URL)
	for _, codex := range []bool{false, true} {
		caller := mcp.Binding{SessionID: "caller-conversation", Name: "caller-name", Codex: codex}
		bind := func() (mcp.Binding, error) { return caller, nil }
		text, failed := tool.Call(context.Background(), bind, json.RawMessage(`{"title":"Restart the router","steps":"Run the command","blocking":true}`))
		if failed || !strings.HasPrefix(text, "Requested as "+id+".") || !strings.Contains(text, "End the turn") {
			t.Fatalf("request: %s", text)
		}
		text, failed = tool.Call(context.Background(), bind, json.RawMessage(`{"id":"`+id+`","withdraw":true}`))
		if failed || !strings.HasPrefix(text, "Withdrawn request") {
			t.Fatalf("withdraw: %s", text)
		}
		for _, raw := range []string{`{"title":"Run","steps":"Do","blocking":false,"conversation":"someone else"}`, `{"title":"Run","steps":"Do","blocking":false,"session":"someone else"}`} {
			if _, failed := tool.Call(context.Background(), bind, json.RawMessage(raw)); !failed {
				t.Fatal("the model supplied its sender")
			}
		}
	}
	if len(got) != 4 {
		t.Fatalf("%d calls, want 4", len(got))
	}
	for _, body := range got {
		if body["conversation"] != "caller-conversation" || body["session"] != nil {
			t.Errorf("caller changed: %v", body)
		}
	}
	if _, failed := tool.Call(context.Background(), func() (mcp.Binding, error) { return mcp.Binding{}, errors.New("no caller") }, json.RawMessage(`{"title":"Run","steps":"Do","blocking":false}`)); !failed {
		t.Fatal("an unbound request succeeded")
	}
}

func TestTheRequestReplyNamesTheTaskAndThePersonsAnswer(t *testing.T) {
	r := Record{ID: strings.Repeat("b", 32), Title: "Restart the router"}
	for _, answer := range []string{"done", "cant"} {
		text := Reply(r, answer, "The router is online.")
		word := "Done"
		if answer == "cant" {
			word = "Can't"
		}
		if !strings.Contains(text, "The person answered your request") || !strings.Contains(text, r.ID) || !strings.Contains(text, r.Title) || !strings.Contains(text, word) || !strings.Contains(text, "The router is online.") {
			t.Errorf("reply: %s", text)
		}
	}
	if ValidateAnswer("skip", "") == nil || ValidateAnswer("done", strings.Repeat("x", NoteMax+1)) == nil {
		t.Fatal("invalid answer accepted")
	}
}
