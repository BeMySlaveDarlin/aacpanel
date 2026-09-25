package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
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
	case "side_question":
		return vetSide(fields)
	case "rename_session":
		title, ok := fields["title"].(string)
		if len(fields) != 2 || !ok || title == "" || fields["source"] != "host" {
			return fmt.Errorf("rename_session passes on only {title, source: host}")
		}
	}
	return nil
}

// vetSide passes a question aside in the one shape claude takes: the question,
// and the side chat so far as pairs of a question and its answer.
func vetSide(fields map[string]any) error {
	question, ok := fields["question"].(string)
	history, isList := fields["history"].([]any)
	if len(fields) != 2 || !ok || question == "" || !isList {
		return fmt.Errorf("side_question passes on only {question, history}")
	}
	for _, turn := range history {
		pair, ok := turn.(map[string]any)
		_, q := pair["question"].(string)
		_, a := pair["response"].(string)
		if !ok || len(pair) != 2 || !q || !a {
			return fmt.Errorf("the history of side_question is pairs of a question and its answer")
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

// trimAnswer passes on of claude's answer only what the panel reads, for the
// requests whose answers reach further than that.
func trimAnswer(subtype string, resp json.RawMessage) json.RawMessage {
	switch subtype {
	case "get_settings":
		return trimSettings(resp)
	case "get_hooks_listing":
		return trimHooks(resp)
	}
	return resp
}

// hiddenSettings are the merged settings the panel does not show: the
// environment carries keys, the rules of permissions are not shown by the
// panel in any form, and hooks have a screen of their own.
var hiddenSettings = map[string]bool{"env": true, "permissions": true, "hooks": true}

// trimSettings leaves of claude's answer to get_settings what the session runs
// with and the merged settings bar the hidden ones, and drops the rest: the
// settings of every source and the environment they carry.
func trimSettings(resp json.RawMessage) json.RawMessage {
	var full struct {
		Subtype   string `json:"subtype"`
		RequestID string `json:"request_id"`
		Error     string `json:"error,omitempty"`
		Response  struct {
			Applied   *Applied                   `json:"applied"`
			Effective map[string]json.RawMessage `json:"effective"`
		} `json:"response"`
	}
	if json.Unmarshal(resp, &full) != nil {
		return nil
	}
	out := map[string]any{"subtype": full.Subtype, "request_id": full.RequestID}
	if full.Error != "" {
		out["error"] = full.Error
	}
	answer := map[string]any{}
	if full.Response.Applied != nil {
		answer["applied"] = full.Response.Applied
	}
	if full.Response.Effective != nil {
		shown := make(map[string]json.RawMessage, len(full.Response.Effective))
		for key, value := range full.Response.Effective {
			if !hiddenSettings[key] {
				shown[key] = value
			}
		}
		answer["effective"] = shown
	}
	if len(answer) > 0 {
		out["response"] = answer
	}
	body, _ := json.Marshal(out)
	return body
}

// trimHooks leaves of claude's answer to get_hooks_listing the rows the panel
// shows. The entry as stored, which a host would edit by, stays behind, and
// so does the catalogue of every event an add form would offer.
func trimHooks(resp json.RawMessage) json.RawMessage {
	var full map[string]json.RawMessage
	var answer map[string]json.RawMessage
	var hooks []map[string]json.RawMessage
	if json.Unmarshal(resp, &full) != nil || json.Unmarshal(full["response"], &answer) != nil {
		return nil
	}
	if raw, ok := answer["hooks"]; ok {
		if json.Unmarshal(raw, &hooks) != nil {
			return nil
		}
		for _, hook := range hooks {
			delete(hook, "editable")
		}
		answer["hooks"], _ = json.Marshal(hooks)
	}
	delete(answer, "eventCatalog")
	full["response"], _ = json.Marshal(answer)
	body, _ := json.Marshal(full)
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

// replyWait is how long claude may take to answer a control request: the
// holder waits that long for claude, and the one who asked the holder a little
// longer. A question aside is answered by the model and takes as long as its
// thinking does; everything else is answered by claude itself.
func replyWait(subtype string) time.Duration {
	if subtype == "side_question" {
		return sideWait
	}
	return controlWait
}
