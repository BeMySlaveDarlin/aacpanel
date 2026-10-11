package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"aacpanel/internal/action"
	"aacpanel/internal/schema"
)

// helperStart preserves the caller separately from the map's visual parent:
// a helper in another project still owes its opening session a report.
func helperStart(p *action.Project, params map[string]any) error {
	if value, present := params["openedBy"]; present {
		var ok bool
		p.OpenedBy, ok = value.(string)
		if !ok || p.OpenedBy == "" {
			return fmt.Errorf("openedBy must name the calling conversation")
		}
	}
	if value, present := params["codex"]; present {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		read := json.NewDecoder(bytes.NewReader(raw))
		read.DisallowUnknownFields()
		if err := read.Decode(&p.Codex); err != nil || p.Codex == nil {
			return fmt.Errorf("codex must hold model, effort, sandbox or writableRoots")
		}
		if err := p.Codex.Validate(); err != nil {
			return err
		}
		var launch struct{ Agent string }
		if err := json.Unmarshal(p.Launch, &launch); err != nil {
			return err
		}
		if launch.Agent != schema.AgentCodex {
			return fmt.Errorf("Codex launch options need agent codex")
		}
	}
	return nil
}
