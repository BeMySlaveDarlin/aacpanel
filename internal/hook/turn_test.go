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

func TestTurnUsesStopTextBeforeTheTranscriptCatchesUp(t *testing.T) {
	dir, proc, config := inPlace(t)
	t.Setenv(CodexHomeEnv, "")
	t.Setenv(HeldEnv, "")
	t.Setenv(PIDEnv, "42")
	fakeProcess(t, proc, 42, 1, "71", []string{"claude", "--name", "work"})
	const conversation = "11111111-1111-4111-8111-111111111111"
	const final = "Please run this command"
	const synthetic = "4643a2bc-221c-8022-a30f-98ac06f7f7e6"
	const recorded = "33333333-3333-4333-8333-333333333333"
	record := func(kind, id string, content any, extra map[string]any) string {
		return string(mustJSON(t, laid(map[string]any{"type": kind, "uuid": id, "message": map[string]any{"content": content}}, extra))) + "\n"
	}
	prompt := record("user", "", "!make test", nil)
	stale := record("assistant", "44444444-4444-4444-8444-444444444444", "Publish the old build?", nil)
	match := record("assistant", recorded, " \n"+final+"\t", nil)
	tool := record("assistant", "55555555-5555-4555-8555-555555555555", []any{map[string]any{"type": "tool_use", "name": "Bash"}}, nil)
	path := filepath.Join(dir, "transcript.jsonl")
	var received []map[string]string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if r.Method != http.MethodPost || r.URL.Path != "/api/turn" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Errorf("unexpected turn request: %s %s %+v", r.Method, r.URL.Path, body)
		}
		received = append(received, body)
		_, _ = w.Write([]byte(`{"action":"Run the command","block":true}`))
	}))
	defer service.Close()
	t.Setenv("AACP_PANEL_URL", service.URL)
	for _, tc := range []struct {
		name, transcript, conversation, wantMessage, wantText string
		last                                                  any
		missing                                               bool
	}{
		{name: "final record missing", transcript: prompt, last: final, wantMessage: synthetic, wantText: final},
		{name: "stale text", transcript: prompt + stale, last: final, wantMessage: synthetic, wantText: final},
		{name: "prior turn matches", transcript: match + prompt, last: final, wantMessage: synthetic, wantText: final},
		{name: "unreadable transcript", missing: true, last: final, wantMessage: synthetic, wantText: final},
		{name: "matching text", transcript: prompt + match + tool, last: "\n " + final + " \n", wantMessage: recorded, wantText: final},
		{name: "match before another text", transcript: prompt + match + stale, last: final, wantMessage: recorded, wantText: final},
		{name: "matching text blocks", transcript: prompt + record("assistant", recorded, []any{
			map[string]any{"type": "text", "text": "Please run"}, map[string]any{"type": "text", "text": "this command"},
		}, nil), last: "Please run\n\nthis command", wantMessage: recorded, wantText: "Please run\n\nthis command"},
		{name: "agent and meta matches", transcript: prompt + record("assistant", recorded, final, map[string]any{"isSidechain": true}) +
			record("assistant", recorded, final, map[string]any{"isMeta": true}), last: final, wantMessage: synthetic, wantText: final},
		{name: "record without uuid", transcript: prompt + record("assistant", "", final, nil), last: final, wantMessage: synthetic, wantText: final},
		{name: "no stop text", transcript: prompt + match, wantMessage: recorded, wantText: final},
		{name: "empty stop text", transcript: prompt + match, last: "", wantMessage: recorded, wantText: final},
		{name: "blank stop text", transcript: prompt + match, last: " \n\t", wantMessage: recorded, wantText: final},
		{name: "stop report supersedes stale question", transcript: prompt + stale, last: "The build passed."},
		{name: "conversation scopes the key", transcript: prompt, conversation: "22222222-2222-4222-8222-222222222222",
			last: final, wantMessage: "7c304791-5934-8f95-9fad-3b12c9a0370a", wantText: final},
		{name: "text scopes the key", transcript: prompt, last: "Publish now?", wantMessage: "887f2917-4bdf-86bd-807a-fecbf2648d4f", wantText: "Publish now?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := tc.conversation
			if id == "" {
				id = conversation
			}
			writeJSON(t, filepath.Join(config, "sessions", "42.json"), map[string]any{"pid": 42, "procStart": "71", "sessionId": id, "name": "work"})
			writeFile(t, path, tc.transcript)
			payload := map[string]any{"hook_event_name": "Stop", "session_id": id, "transcript_path": path}
			if tc.missing {
				payload["transcript_path"] = filepath.Join(dir, "missing.jsonl")
			}
			if tc.last != nil {
				payload["last_assistant_message"] = tc.last
			}
			for range 2 {
				before := len(received)
				code, out, errs := run(t, []string{"turn"}, string(mustJSON(t, payload)))
				if code != 0 || errs != "" {
					t.Fatalf("hook exit %d, stderr %q", code, errs)
				}
				if tc.wantMessage == "" {
					if len(received) != before || out != "" {
						t.Fatalf("a report triggered a review: %q, calls %d", out, len(received)-before)
					}
					continue
				}
				if len(received) != before+1 {
					t.Fatalf("the final text was not reviewed: calls %d, reply %q", len(received)-before, out)
				}
				got := received[before]
				if got["message"] != tc.wantMessage || got["text"] != tc.wantText || got["conversation"] != id || got["session"] != "work" {
					t.Fatalf("turn payload: %+v, want message %q and text %q", got, tc.wantMessage, tc.wantText)
				}
				var reply map[string]string
				if json.Unmarshal([]byte(out), &reply) != nil || reply["decision"] != "block" || !strings.Contains(reply["reason"], "request") {
					t.Fatalf("the manual action was not held: %q", out)
				}
			}
		})
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
