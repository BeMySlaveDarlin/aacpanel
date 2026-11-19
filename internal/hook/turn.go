package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
	"aacpanel/internal/question"
)

const turnWait = 15 * time.Second

var turnTimeout = turnWait

func turn(_ string, payload []byte, _ time.Time) Reply {
	p, ok := loadNumbers(payload).(map[string]any)
	if !ok || truthy(p["stop_hook_active"]) {
		return Reply{}
	}
	if _, agent := p["agent_id"]; agent {
		return Reply{}
	}
	if event, ok := p["hook_event_name"]; ok && event != "Stop" {
		return Reply{}
	}
	if _, codex := codexHome(nil, p); codex {
		return Reply{}
	}
	id, _ := p["session_id"].(string)
	path, _ := p["transcript_path"].(string)
	message, text := lastAgentText(path)
	if id == "" || message == "" || !question.Candidate(text) {
		return Reply{}
	}
	pid, session := claudeOf(id)
	if session == nil {
		return Reply{}
	}
	name := sessionName(pid, session)
	if name == "" {
		return Reply{}
	}
	body, err := json.Marshal(map[string]string{"conversation": id, "session": name, "message": message, "text": text})
	if err != nil {
		return Reply{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hostcfg.PanelURL(), "/")+"/api/turn", bytes.NewReader(body))
	if err != nil {
		return Reply{}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Reply{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Reply{}
	}
	var verdict struct {
		Action string `json:"action"`
		Block  bool   `json:"block"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&verdict) != nil || !verdict.Block {
		return Reply{}
	}
	action := question.Line(verdict.Action, 140)
	if action == "" {
		return Reply{}
	}
	return Reply{Block: "This turn asks the person to do something by hand: “" + action + "”. Hand it over as an act so the panel shows it as their move — request with steps (a command goes into its step), or secret_ask for a credential. If it already is an act or is not for the person, end the turn."}
}

func lastAgentText(path string) (string, string) {
	for record := range newestFirst(path) {
		if isPrompt(record) {
			return "", ""
		}
		if record["type"] != "assistant" || truthy(record["isMeta"]) {
			continue
		}
		var text []string
		switch content := contentOf(record).(type) {
		case string:
			text = append(text, content)
		case []any:
			for _, item := range content {
				if b, ok := item.(map[string]any); ok && b["type"] == "text" {
					if s, ok := b["text"].(string); ok {
						text = append(text, s)
					}
				}
			}
		}
		if joined := strings.TrimSpace(strings.Join(text, "\n\n")); joined != "" {
			id, _ := record["uuid"].(string)
			return id, joined
		}
	}
	return "", ""
}
