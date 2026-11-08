package taskreg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

func TestRegistryToolsSendOnlyTheirBoundConversation(t *testing.T) {
	requests := []Request{}
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		requests = append(requests, in)
		w.Write([]byte(`{"saved":true}`))
	}))
	t.Cleanup(panel.Close)
	tools := Tools(panel.URL)
	args := []string{`{"status":["open"],"tags":["backend"],"text":"Work","next":true,"limit":3}`, `{"number":4}`, `{"title":"Work","markdown":"Body"}`, `{"number":4,"status":"done","markdown":"Finished","section":"Result"}`, `{"title":"Saved","markdown":"Progress","next":"Review"}`, `{"key":"saved-key"}`}
	bind := func() (mcp.Binding, error) { return mcp.Binding{SessionID: "bound-conversation", Name: "worker"}, nil }
	for i, tool := range tools {
		if text, failed := tool.Call(t.Context(), bind, json.RawMessage(args[i])); failed || !strings.Contains(text, "saved") {
			t.Fatalf("%s: %s %v", tool.Name, text, failed)
		}
		in := requests[len(requests)-1]
		if in.Conversation != "bound-conversation" || in.Project != 0 || in.CWD != "" {
			t.Fatalf("caller identity: %+v", in)
		}
		if _, failed := tool.Call(t.Context(), bind, json.RawMessage(`{"project":99}`)); !failed {
			t.Fatalf("%s accepted a project supplied by the model", tool.Name)
		}
	}
	if len(requests) != 6 || requests[0].Operation != "list" || !requests[0].Next || requests[1].Number != 4 || requests[2].Edit.Markdown == nil || requests[3].Edit.Status == nil || requests[4].Handoff.Next != "Review" || requests[5].Key != "saved-key" {
		t.Fatalf("tool payloads: %+v", requests)
	}
}
