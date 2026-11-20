package stream

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"aacpanel/internal/action"
)

//go:embed prompt-idle-v2.txt
var checklistPrompt string

func ClassifyChecklist(ctx context.Context, bin, dir string, input action.ChecklistInput) (action.ChecklistVerdict, error) {
	ctx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	args := []string{"-p", "--output-format", "json", "--model", "haiku", "--no-session-persistence",
		"--strict-mcp-config", "--setting-sources", "project", "--max-turns", "1", "--tools", "",
		"--system-prompt", checklistPrompt}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = dir, probeEnv(os.Environ())
	body, err := json.Marshal(input)
	if err != nil {
		return action.ChecklistVerdict{}, err
	}
	cmd.Stdin = bytes.NewReader(body)
	raw, err := cmd.Output()
	if err != nil {
		return action.ChecklistVerdict{}, fmt.Errorf("the checklist classifier did not answer: %w", err)
	}
	var reply struct {
		Result string `json:"result"`
		Error  bool   `json:"is_error"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.Error {
		return action.ChecklistVerdict{}, errors.New("the checklist classifier returned no verdict")
	}
	return action.ParseChecklistVerdict([]byte(reply.Result))
}
