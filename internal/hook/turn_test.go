package hook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTurnReadsTheFinalTextAndBlocksQuestionsBeforeManualActions(t *testing.T) {
	dir, proc, config := inPlace(t)
	t.Setenv(CodexHomeEnv, "")
	t.Setenv(PIDEnv, "42")
	fakeProcess(t, proc, 42, 1, "71", []string{"claude", "--name", "work"})
	writeJSON(t, filepath.Join(config, "sessions", "42.json"), map[string]any{"pid": 42, "procStart": "71", "sessionId": "conversation", "name": "work"})
	path := filepath.Join(dir, "transcript.jsonl")
	put := func(text string) {
		writeFile(t, path, `{"type":"user","message":{"content":"go"}}`+"\n"+
			`{"type":"assistant","uuid":"old","message":{"content":[{"type":"text","text":"Old?"}]}}`+"\n"+
			`{"type":"assistant","uuid":"side","isSidechain":true,"message":{"content":[{"type":"text","text":"Other agent?"}]}}`+"\n"+
			`{"type":"assistant","uuid":"last","message":{"content":[{"type":"text","text":`+string(mustJSON(t, text))+`}]}}`+"\n"+
			`{"type":"assistant","uuid":"tool","message":{"content":[{"type":"tool_use","name":"Bash"}]}}`+"\n")
	}
	put("Please run this command")
	calls := 0
	mode := "block"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var got map[string]string
		if r.Method != http.MethodPost || r.URL.Path != "/api/turn" || json.NewDecoder(r.Body).Decode(&got) != nil || got["session"] != "work" || got["conversation"] != "conversation" || got["message"] != "last" || got["text"] != "Please run this command" {
			t.Errorf("turn payload: %+v", got)
		}
		switch mode {
		case "block":
			_, _ = w.Write([]byte(`{"action":" Run  the command ","block":true}`))
		case "empty":
			_, _ = w.Write([]byte(`{"action":"","block":true}`))
		case "question":
			_, _ = w.Write([]byte(`{"question":"  Publish  now? ","action":"","block":true}`))
		case "both":
			_, _ = w.Write([]byte(`{"question":"Publish now?","action":"Run the command","block":true}`))
		case "recorded":
			_, _ = w.Write([]byte(`{"action":"Run the command","block":false}`))
		case "error":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "malformed":
			_, _ = w.Write([]byte(`no verdict`))
		case "timeout":
			<-r.Context().Done()
		}
	}))
	defer srv.Close()
	t.Setenv("AACP_PANEL_URL", srv.URL)
	wasTimeout := turnTimeout
	turnTimeout = 50 * time.Millisecond
	t.Cleanup(func() { turnTimeout = wasTimeout })
	payload := map[string]any{"hook_event_name": "Stop", "session_id": "conversation", "transcript_path": path}
	for _, tc := range []struct {
		name        string
		over        map[string]any
		text        string
		mode        string
		reason      string
		call, block bool
	}{
		{name: "manual action", mode: "block", call: true, block: true, reason: "request"},
		{name: "question", mode: "question", call: true, block: true, reason: "AskUserQuestion"},
		{name: "question and action", mode: "both", call: true, block: true, reason: "AskUserQuestion"},
		{name: "recorded action", mode: "recorded", call: true},
		{name: "empty action", mode: "empty", call: true},
		{name: "error", mode: "error", call: true},
		{name: "invalid verdict", mode: "malformed", call: true},
		{name: "timeout", mode: "timeout", call: true},
		{name: "answering round", over: map[string]any{"stop_hook_active": true}},
		{name: "subagent", over: map[string]any{"agent_id": "agent"}},
		{name: "empty agent id", over: map[string]any{"agent_id": ""}},
		{name: "codex", over: map[string]any{"turn_id": "turn"}},
		{name: "outside panel", over: map[string]any{"session_id": "unmapped"}},
		{name: "other event", over: map[string]any{"hook_event_name": "SubagentStop"}},
		{name: "report", text: "The build passed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode = tc.mode
			text := tc.text
			if text == "" {
				text = "Please run this command"
			}
			put(text)
			before := calls
			r := turn("Stop", mustJSON(t, laid(payload, tc.over)), time.Now())
			if (calls > before) != tc.call || (r.Block != "") != tc.block || r.Context != "" {
				t.Fatalf("reply %+v, calls %d", r, calls-before)
			}
			if tc.block {
				summary := "“Run the command”"
				if tc.reason == "AskUserQuestion" {
					summary = "“Publish now?”"
				}
				if strings.Contains(r.Block, "\n") || !strings.Contains(r.Block, summary) || !strings.Contains(r.Block, tc.reason) {
					t.Fatalf("reason: %q", r.Block)
				}
				if tc.reason == "AskUserQuestion" && strings.Contains(r.Block, "secret_ask") {
					t.Fatalf("manual action won over the question: %q", r.Block)
				}
			}

		})
	}
	writeFile(t, path, `{"type":"assistant","uuid":"old","message":{"content":"Old?"}}`+"\n"+`{"type":"user","message":{"content":"new turn"}}`+"\n")
	if id, text := lastAgentText(path); id != "" || text != "" {
		t.Fatalf("the prior turn leaked into Stop: %q %q", id, text)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
