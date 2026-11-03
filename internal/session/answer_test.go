package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

func TestAParentAnswersTheChildsQuestionByOptionLabelsAndOwnWords(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			_, _ = w.Write([]byte(`{"state":{"ask":{"toolUseId":"q","questions":[{"text":"Choose","multi":true,"options":[{"label":"First"},{"label":"Second"}]},{"text":"Explain","options":[]}]}}}`))
			return
		}
		if r.URL.Path != "/api/actions" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		requests = append(requests, request)
		_, _ = w.Write([]byte(`{"detail":"question answered"}`))
	}))
	t.Cleanup(server.Close)
	tool := Answer(Host{Panel: server.URL})
	if !tool.Allowed || !strings.Contains(tool.Instructions, AnswerName) {
		t.Fatal("the scoped answer tool is not available to the parent without another question")
	}
	for _, bind := range []mcp.Bind{bound(mine), func() (mcp.Binding, error) {
		return mcp.Binding{SessionID: mine, Codex: true}, nil
	}} {
		said, failed := tool.Call(t.Context(), bind, []byte(`{"name":"child","ask":"q","answers":[{"picks":["Second","First"]},{"text":"Use the saved plan"}]}`))
		if failed || said != "Answered: question answered" {
			t.Fatalf("the answer failed: %s", said)
		}
	}
	if len(requests) != 2 {
		t.Fatalf("the panel received %d answers", len(requests))
	}
	for _, request := range requests {
		params := request["params"].(map[string]any)
		if request["kind"] != "session.answer" || request["target"] != "child" || params["caller"] != mine ||
			params["ask"] != "q" {
			t.Fatalf("the answer lost its child or caller: %v", request)
		}
		picks, _ := json.Marshal(params["picks"])
		texts, _ := json.Marshal(params["texts"])
		if string(picks) != `[[2,1],[]]` || string(texts) != `["","Use the saved plan"]` {
			t.Fatalf("labels or own words were changed: %s %s", picks, texts)
		}
	}
}

func TestAParentAnswerRefusesPermissionsFormsAndStaleQuestions(t *testing.T) {
	var response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Error("an invalid answer reached the action path")
		}
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	tool := Answer(Host{Panel: server.URL})
	for _, reply := range []string{
		`{"state":{}}`,
		`{"state":{"ask":{"toolUseId":"changed","questions":[{"text":"Choose"}]}}}`,
		`{"state":{"ask":{"toolUseId":"q","server":"tracker","questions":[{"text":"Sign in"}]}}}`,
		`{"state":{"ask":{"toolUseId":"q","async":true,"questions":[{"text":"Choose"}]}}}`,
	} {
		response = reply
		if said, failed := tool.Call(t.Context(), bound(mine), []byte(`{"name":"child","ask":"q","answers":[{"text":"yes"}]}`)); !failed {
			t.Fatalf("a non-question was answered: %s", said)
		}
	}
	response = `{"state":{"ask":{"toolUseId":"q","questions":[{"text":"Choose","options":[{"label":"Yes"}]}]}}}`
	for _, args := range []string{
		`{"name":"child","ask":"q","answers":[{"picks":["Unknown"]}]}`,
		`{"name":"child","ask":"q","answers":[{"picks":["Yes","Yes"]}]}`,
		`{"name":"child","ask":"q","answers":[]}`,
		`{"name":"child","ask":"q","answers":[{"picks":["Yes"],"text":"Also"}]}`,
	} {
		if said, failed := tool.Call(t.Context(), bound(mine), []byte(args)); !failed {
			t.Fatalf("an invalid answer was sent: %s", said)
		}
	}
	response = `{"state":{"ask":{"toolUseId":"q","questions":[{"text":"Choose","options":[{"label":"Yes"},{"label":"Yes"}]}]}}}`
	if said, failed := tool.Call(t.Context(), bound(mine), []byte(`{"name":"child","ask":"q","answers":[{"picks":["Yes"]}]}`)); !failed {
		t.Fatalf("an ambiguous label selected an arbitrary option: %s", said)
	}
}
