package taskreg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func Call(ctx context.Context, panel string, in Request, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(panel, "/")+"/api/tasks/registry", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("the panel did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, e := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if e != nil {
			return e
		}
		return fmt.Errorf("task registry: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

type Context struct {
	Project Project `json:"project"`
	Queue   struct {
		Tasks []Task `json:"tasks"`
		Total int    `json:"total"`
	} `json:"queue"`
	Handoff      *Handoff `json:"handoff"`
	HandoffTasks []Task   `json:"handoffTasks"`
}
