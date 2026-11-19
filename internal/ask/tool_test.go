package ask

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/act"
	"aacpanel/internal/mcp"
)

func TestAskSendsTheBoundFormAndReturnsItsID(t *testing.T) {
	id := strings.Repeat("a", 32)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Conversation string `json:"conversation"`
			act.Input
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/acts" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("the act endpoint received no form")
		}
		if body.Conversation != "bound-conversation" || body.Kind != "ask" || body.Body.Source != "tool" || body.Blocking == nil || *body.Blocking || len(body.Body.Questions) != 2 || !body.Body.Questions[0].Multi || body.Body.Questions[0].Header != "Long header" || body.Body.Questions[1].Question != "Name?" {
			t.Errorf("form: %+v", body)
		}
		_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
	}))
	defer server.Close()
	tool := Tool(server.URL)
	bind := func() (mcp.Binding, error) { return mcp.Binding{SessionID: "bound-conversation"}, nil }
	text, failed := tool.Call(t.Context(), bind, json.RawMessage(`{"questions":[{"question":" Which checks? ","header":"Long header here","options":[{"label":"Unit","description":"Fast"},{"label":"Integration","description":"Full"}],"multiSelect":true},{"question":"Name?","header":"Name","options":[],"multiSelect":false}]}`))
	if failed || text != "Asked as "+id+"." || calls != 1 {
		t.Fatalf("reply: %q, failed %v, calls %d", text, failed, calls)
	}
	for _, raw := range []string{`{}`, `{"questions":[]}`, `{"questions":[{"question":" "}]}`, `{"questions":[{"question":"Name?","unexpected":true}]}`} {
		if _, failed := tool.Call(t.Context(), bind, json.RawMessage(raw)); !failed {
			t.Errorf("invalid ask accepted: %s", raw)
		}
	}
	if calls != 1 {
		t.Fatal("an invalid ask reached the panel")
	}
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"minItems":1`, `"maxItems":4`, `"minItems":0`, `"multiSelect"`, `"maxLength":200`, `"maxLength":12`, `"maxLength":40`, `"maxLength":120`} {
		if !strings.Contains(string(schema), field) {
			t.Errorf("schema lacks %s: %s", field, schema)
		}
	}
}
