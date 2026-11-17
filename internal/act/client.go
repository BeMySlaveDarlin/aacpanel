package act

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"aacpanel/internal/request"
)

// Create sends no secret values, only the description of the waiting turn.
func Create(ctx context.Context, panel, conversation string, input Input) (string, error) {
	body, err := json.Marshal(struct {
		Conversation string `json:"conversation"`
		Input
	}{conversation, input})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(panel, "/")+"/api/acts", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.New(strings.TrimSpace(string(data)))
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &result) != nil || !request.ValidID(result.ID) {
		return "", errors.New("the panel confirmed no act id; do not repeat the ask blindly")
	}
	return result.ID, nil
}
