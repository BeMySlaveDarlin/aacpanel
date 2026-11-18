package request

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"aacpanel/internal/mcp"
)

func TestRequestValidatesChainOperations(t *testing.T) {
	blocking := false
	valid := Input{Title: strings.Repeat("界", TitleMax), Steps: []Step{{Who: "you", Text: strings.Repeat("界", TextMax), Command: strings.Repeat("x", CommandMax)}}, Blocking: &blocking}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]Input{
		"empty": {}, "no steps": {Title: "Do it", Blocking: &blocking},
		"no blocking":   {Title: "Do it", Steps: valid.Steps},
		"long title":    {Title: valid.Title + "x", Steps: valid.Steps, Blocking: &blocking},
		"agent first":   {Title: "Run", Steps: []Step{{Who: "agent", Text: "Check"}}, Blocking: &blocking},
		"agent command": {Title: "Run", Steps: []Step{{Who: "you", Text: "Run"}, {Who: "agent", Text: "Check", Command: "test"}}, Blocking: &blocking},
		"long text":     {Title: "Run", Steps: []Step{{Who: "you", Text: strings.Repeat("x", TextMax+1)}}, Blocking: &blocking},
		"long command":  {Title: "Run", Steps: []Step{{Who: "you", Text: "Run", Command: strings.Repeat("x", CommandMax+1)}}, Blocking: &blocking},
		"too many":      {Title: "Run", Steps: make([]Step, StepsMax+1), Blocking: &blocking},
		"unknown who":   {Title: "Run", Steps: []Step{{Who: "you", Text: "Run"}, {Who: "other", Text: "Check"}}, Blocking: &blocking},
		"id alone":      {ID: strings.Repeat("a", 32)}, "bad id": {ID: "../other", Withdraw: true},
		"mixed create":     {ID: strings.Repeat("a", 32), Withdraw: true, Title: "Other"},
		"mixed operations": {ID: strings.Repeat("a", 32), Withdraw: true, Reopen: 1},
		"no outcome":       {ID: strings.Repeat("a", 32), Step: 1},
		"bad outcome":      {ID: strings.Repeat("a", 32), Step: 1, Outcome: "cant"},
		"long note":        {ID: strings.Repeat("a", 32), Step: 1, Outcome: "done", Note: strings.Repeat("x", NoteMax+1)},
		"negative reopen":  {ID: strings.Repeat("a", 32), Reopen: -1},
	} {
		if err := input.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for _, input := range []Input{{ID: strings.Repeat("a", 32), Withdraw: true}, {ID: strings.Repeat("a", 32), Reopen: 1}, {ID: strings.Repeat("a", 32), Step: 2, Outcome: "done", Note: "Checked"}, {ID: strings.Repeat("a", 32), Step: 2, Outcome: "failed"}} {
		if err := input.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRequestTakesItsConversationOnlyFromTheCaller(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var got []map[string]any
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/acts" || r.Method != http.MethodPost {
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
		text, failed := tool.Call(context.Background(), bind, json.RawMessage(`{"title":"Restart the router","steps":[{"who":"you","text":"Run the command"}],"blocking":true}`))
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
		if body["withdraw"] != true {
			content, ok := body["body"].(map[string]any)
			if body["kind"] != "do" || !ok || len(content["steps"].([]any)) != 1 || content["steps"].([]any)[0].(map[string]any)["text"] != "Run the command" || body["blocking"] != true {
				t.Errorf("act payload: %+v", body)
			}
		}
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

// The feed recognises the whole reply, so a change to its writer must agree
// with the samples the browser reads before it can reach a transcript.
func TestTheRequestReplyIsTheOneTheFeedReads(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "check", "testdata", "panel-said.json"))
	if err != nil {
		t.Fatal(err)
	}
	var samples struct {
		Request []struct {
			Input struct {
				Title  string `json:"title"`
				ID     string `json:"id"`
				Answer string `json:"answer"`
				Note   string `json:"note"`
			} `json:"input"`
			Text string `json:"text"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatalf("the samples do not parse: %v", err)
	}
	if len(samples.Request) == 0 {
		t.Fatal("no samples of a request reply: the check compares nothing")
	}
	for _, s := range samples.Request {
		r := Record{ID: s.Input.ID, Title: s.Input.Title}
		if got := Reply(r, s.Input.Answer, s.Input.Note); got != s.Text {
			t.Errorf("the request %q is answered as\n%q\nand the feed reads\n%q", r.Title, got, s.Text)
		}
	}
}

func TestChainSummaryCarriesOnlyUsefulOutput(t *testing.T) {
	start := time.Unix(100, 0)
	end := start.Add(31 * time.Second)
	zero, three := 0, 3
	steps := []Step{{Who: "you", Text: "Login"}, {Who: "agent", Text: "Check access"}, {Who: "you", Text: "Deploy"}}
	outs := []*Output{{State: "done", Exit: &zero, Started: &start, Ended: &end, Tail: "old output", Log: "/logs/1"}, {State: "done", Note: "Access confirmed"}, {State: "failed", Exit: &three, Tail: "deploy error", Log: "/logs/3"}}
	text := Summary(strings.Repeat("a", 32), "Release", steps, outs, 3, 1, "failed")
	for _, want := range []string{"The chain “Release”", "2 agent", "Access confirmed", "exit 3", "deploy error", "Full log: /logs/3", "reopen, withdraw"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "old output") || strings.Contains(text, "1 you") {
		t.Fatal(text)
	}
	text = Summary("id", "Release", steps, outs, 2, 0, "open")
	if !strings.Contains(text, "Step 2 is yours") || !strings.Contains(text, "request {id:") {
		t.Fatal(text)
	}
	if got := Tail(strings.Repeat("line\n", 50)); len(strings.Split(got, "\n")) != 40 {
		t.Fatal(got)
	}
	if got := Tail(strings.Repeat("界", 4100)); utf8.RuneCountInString(got) != 4000 {
		t.Fatal("tail exceeds its character bound")
	}
}

func TestRequestSchemaAndOperationReplies(t *testing.T) {
	id := strings.Repeat("a", 32)
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["conversation"] != "caller" {
			t.Error(body)
		}
		message := "Step 2 closed. Step 3 is the person's — end the turn."
		if body["reopen"] != nil {
			message = "Step 1 is the person's — end the turn."
		}
		json.NewEncoder(w).Encode(map[string]string{"id": id, "message": message})
	}))
	defer panel.Close()
	tool := Tool(panel.URL)
	properties := tool.InputSchema["properties"].(map[string]any)
	if properties["steps"].(map[string]any)["type"] != "array" || len(tool.InputSchema["oneOf"].([]any)) != 3 {
		t.Fatal(tool.InputSchema)
	}
	bind := func() (mcp.Binding, error) { return mcp.Binding{SessionID: "caller"}, nil }
	for _, raw := range []string{`{"id":"` + id + `","step":2,"outcome":"done","note":"Checked"}`, `{"id":"` + id + `","reopen":1}`} {
		text, failed := tool.Call(t.Context(), bind, json.RawMessage(raw))
		if failed || !strings.Contains(text, "end the turn") {
			t.Fatal(text)
		}
	}
}
