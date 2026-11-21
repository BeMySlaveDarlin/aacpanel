package hook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	message, text := stopAgentText(p)
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
		Question string `json:"question"`
		Action   string `json:"action"`
		Block    bool   `json:"block"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&verdict) != nil || !verdict.Block {
		return Reply{}
	}
	if what := question.Line(verdict.Question, 140); what != "" {
		return Reply{Block: "This turn asks the person a question in prose: “" + what + "”. A prose question does not reach the person. Ask it through AskUserQuestion. If it is not a question for the person, end the turn."}
	}
	action := question.Line(verdict.Action, 140)
	if action == "" {
		return Reply{}
	}
	return Reply{Block: "This turn asks the person to do something by hand: “" + action + "”. Hand it over as an act so the panel shows it as their move — request with steps (a command goes into its step), or secret_ask for a credential. If it already is an act or is not for the person, end the turn."}
}

// Stream callbacks can precede the transcript write, so Stop's text is
// authoritative and a missing record must not postpone the check.
func stopAgentText(p map[string]any) (string, string) {
	path, _ := p["transcript_path"].(string)
	text, _ := p["last_assistant_message"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return lastAgentText(path)
	}
	for record := range newestFirst(path) {
		if isPrompt(record) {
			break
		}
		if agentText(record) == text {
			if id, _ := record["uuid"].(string); id != "" {
				return id, text
			}
		}
	}
	id, _ := p["session_id"].(string)
	sum := sha256.Sum256([]byte(id + "\x00" + text))
	sum[6] = sum[6]&0x0f | 0x80
	sum[8] = sum[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16]), text
}

func lastAgentText(path string) (string, string) {
	for record := range newestFirst(path) {
		if isPrompt(record) {
			return "", ""
		}
		if text := agentText(record); text != "" {
			id, _ := record["uuid"].(string)
			return id, text
		}
	}
	return "", ""
}

func agentText(record map[string]any) string {
	if record["type"] != "assistant" || truthy(record["isMeta"]) {
		return ""
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
	return strings.TrimSpace(strings.Join(text, "\n\n"))
}
