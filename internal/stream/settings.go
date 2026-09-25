package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

// The requests about settings reach further than the panel needs them to.
// apply_flag_settings merges any settings at all into the session — its hooks
// and its permissions among them; update_settings writes a file of settings;
// get_settings answers with the environment of the session, tokens and all.
// So a holder passes on only the shape the panel sends, and answers with only
// what the panel reads.

// Ultracode is the effort above the others: xhigh with workflows run for every
// task, held for a session only.
const Ultracode = "ultracode"

// savedEfforts are the efforts claude saves as a default. Max is not among
// them: claude keeps it for the session it is set in.
var savedEfforts = []string{"low", "medium", "high", "xhigh"}

// Applied is what a session runs with right now, as claude reports it.
type Applied struct {
	Model     string  `json:"model"`
	Effort    *string `json:"effort"`
	Ultracode bool    `json:"ultracode"`
}

// vetSettings refuses a request about settings that is not the one the panel
// sends.
func vetSettings(subtype string, fields map[string]any) error {
	switch subtype {
	case "apply_flag_settings":
		settings, ok := onlyKey(fields, "settings")
		if !ok || len(settings) != 1 {
			return fmt.Errorf("apply_flag_settings passes on only {settings: {ultracode}}")
		}
		if _, ok := settings["ultracode"].(bool); !ok {
			return fmt.Errorf("apply_flag_settings passes on only ultracode, as true or false")
		}
	case "update_settings":
		if len(fields) != 2 || fields["source"] != "userSettings" {
			return fmt.Errorf("update_settings passes on only the settings of the user")
		}
		settings, ok := fields["settings"].(map[string]any)
		level, isText := settings["effortLevel"].(string)
		if !ok || len(settings) != 1 || !isText || !slices.Contains(savedEfforts, level) {
			return fmt.Errorf("update_settings passes on only an effort claude saves: %v", savedEfforts)
		}
	case "get_settings":
		if len(fields) != 0 {
			return fmt.Errorf("get_settings takes no fields")
		}
	}
	return nil
}

func onlyKey(fields map[string]any, key string) (map[string]any, bool) {
	if len(fields) != 1 {
		return nil, false
	}
	inner, ok := fields[key].(map[string]any)
	return inner, ok
}

// trimSettings leaves of claude's answer to get_settings what the session runs
// with, and drops the rest: the settings of every source and the environment
// they carry.
func trimSettings(resp json.RawMessage) json.RawMessage {
	var full struct {
		Subtype   string `json:"subtype"`
		RequestID string `json:"request_id"`
		Error     string `json:"error,omitempty"`
		Response  struct {
			Applied *Applied `json:"applied"`
		} `json:"response"`
	}
	if json.Unmarshal(resp, &full) != nil {
		return nil
	}
	out := map[string]any{"subtype": full.Subtype, "request_id": full.RequestID}
	if full.Error != "" {
		out["error"] = full.Error
	}
	if full.Response.Applied != nil {
		out["response"] = map[string]any{"applied": full.Response.Applied}
	}
	body, _ := json.Marshal(out)
	return body
}

// applied asks claude what the session runs with.
func (h *Holder) applied(ctx context.Context) (Applied, error) {
	resp, err := h.control(ctx, "get_settings", nil, controlWait)
	if err != nil {
		return Applied{}, err
	}
	var body struct {
		Response struct {
			Applied *Applied `json:"applied"`
		} `json:"response"`
	}
	if json.Unmarshal(resp, &body) != nil || body.Response.Applied == nil {
		return Applied{}, fmt.Errorf("claude did not say what the session runs with")
	}
	return *body.Response.Applied, nil
}

// flagsApplied follows a change of ultracode through to what the session runs
// with. Claude answers the request with success even where ultracode cannot
// hold — a model without xhigh, workflows turned off — and turns nothing on;
// only what it reports next tells.
func (h *Holder) flagsApplied(ctx context.Context, fields map[string]any) error {
	settings, _ := fields["settings"].(map[string]any)
	want, _ := settings["ultracode"].(bool)
	got, err := h.applied(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	switch {
	case got.Ultracode:
		h.state.Effort = Ultracode
	case h.state.Effort == Ultracode:
		h.state.Effort = ""
		if got.Effort != nil {
			h.state.Effort = *got.Effort
		}
	}
	h.mu.Unlock()
	h.saveSummary()
	if want && !got.Ultracode {
		return fmt.Errorf("ultracode did not take in this session: its model does not take xhigh, or workflows are off")
	}
	return nil
}
