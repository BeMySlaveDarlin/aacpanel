package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// setupRequests are claude's own requests for the rows of its read-only
// screens. The agents are not among them: claude names them at the handshake.
var setupRequests = map[string]string{
	action.SetupHooks:  "get_hooks_listing",
	action.SetupMemory: "get_memory_dialog",
	action.SetupSkills: "get_skills_dialog",
	action.SetupConfig: "get_settings",
}

// Setup answers one of the read-only screens of what a session is set up
// with. A session on the stream is asked with claude's own requests for the
// rows of those screens; a terminal draws them itself on screens driven by
// keys, and the answer says where they live.
func (e *Executor) Setup(ctx context.Context, target, part string) (*action.Setup, error) {
	if !action.SetupParts[part] {
		return nil, fmt.Errorf("there is no screen %q of what a session is set up with", part)
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	if !onStream(s) {
		return &action.Setup{Transport: action.SwitchConsole}, nil
	}
	out := &action.Setup{Transport: action.SwitchStream}
	if part == action.SetupAgents {
		st, err := streamState(ctx, s)
		if err != nil {
			return nil, err
		}
		out.Agents = initAgents(st.Init)
		return out, nil
	}
	reply, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: setupRequests[part]})
	if err != nil {
		return nil, err
	}
	body := controlBody(reply.Response)
	switch part {
	case action.SetupHooks:
		out.Hooks, err = hooksOf(body)
	case action.SetupMemory:
		out.Memory, err = memoryOf(body)
	case action.SetupSkills:
		out.Skills, err = skillsOf(body)
	case action.SetupConfig:
		out.Config, err = configOf(body)
	}
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", s.Name, err)
	}
	return out, nil
}

// controlBody is claude's own answer inside the control response around it.
func controlBody(raw json.RawMessage) json.RawMessage {
	var wrap struct {
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(raw, &wrap) == nil && len(wrap.Response) > 0 && wrap.Response[0] == '{' {
		return wrap.Response
	}
	return raw
}

// hooksOf reads the rows of the /hooks menu.
func hooksOf(raw json.RawMessage) (*action.Hooks, error) {
	var body struct {
		Events []struct {
			Name      string `json:"name"`
			Summary   string `json:"summary"`
			HookCount int    `json:"hookCount"`
		} `json:"events"`
		Hooks []struct {
			Event        string `json:"event"`
			Matcher      string `json:"matcher"`
			Source       string `json:"source"`
			SourceLabel  string `json:"sourceLabel"`
			PluginName   string `json:"pluginName"`
			Type         string `json:"type"`
			DisplayText  string `json:"displayText"`
			CommandText  string `json:"commandText"`
			ContentLabel string `json:"contentLabel"`
			Condition    string `json:"condition"`
			Timeout      int    `json:"timeout"`
			Disabled     bool   `json:"disabled"`
		} `json:"hooks"`
		Policy struct {
			DisabledByPolicy bool `json:"disabledByPolicy"`
			ManagedOnly      bool `json:"managedOnly"`
			PluginOnly       bool `json:"pluginOnly"`
			AllDisabled      bool `json:"allDisabled"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("the hooks listing did not parse: %w", err)
	}
	out := &action.Hooks{Events: []action.HookEvent{}, Hooks: []action.Hook{}}
	for _, ev := range body.Events {
		out.Events = append(out.Events, action.HookEvent{Name: ev.Name, Summary: ev.Summary, Count: ev.HookCount})
	}
	for _, h := range body.Hooks {
		source := h.SourceLabel
		if source == "" {
			source = h.Source
		}
		out.Hooks = append(out.Hooks, action.Hook{
			Event: h.Event, Matcher: h.Matcher, Source: source, Plugin: h.PluginName, Type: h.Type,
			Label: h.DisplayText, Text: h.CommandText, TextLabel: h.ContentLabel, Condition: h.Condition,
			Timeout: h.Timeout, Disabled: h.Disabled,
		})
	}
	p := body.Policy
	switch {
	case p.DisabledByPolicy:
		out.Blocked = "A managed policy turns every hook off: none of them runs."
	case p.AllDisabled:
		out.Blocked = "Hooks are turned off in the settings: none of them runs."
	case p.ManagedOnly:
		out.Blocked = "A managed policy lets only its own hooks run, and those are not listed."
	case p.PluginOnly:
		out.Blocked = "A managed policy keeps hooks to plugins."
	}
	return out, nil
}

// memoryOf reads the /memory dialog.
func memoryOf(raw json.RawMessage) (*action.Memory, error) {
	var body struct {
		Files []struct {
			Kind        string `json:"kind"`
			Path        string `json:"path"`
			Label       string `json:"label"`
			Description string `json:"description"`
			Exists      bool   `json:"exists"`
		} `json:"files"`
		Folders []struct {
			Kind        string `json:"kind"`
			Path        string `json:"path"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"folders"`
		Memories []struct {
			Name        string  `json:"name"`
			Description *string `json:"description"`
			Type        *string `json:"type"`
			ModifiedMS  int64   `json:"modified_ms"`
		} `json:"memories"`
		AutoMemory struct {
			Status string `json:"status"`
		} `json:"auto_memory"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("the memory dialog did not parse: %w", err)
	}
	out := &action.Memory{Auto: body.AutoMemory.Status,
		Files: []action.MemoryFile{}, Folders: []action.MemoryFile{}, Memories: []action.SavedMemory{}}
	for _, f := range body.Files {
		out.Files = append(out.Files, action.MemoryFile{Kind: f.Kind, Path: f.Path, Label: f.Label,
			Description: f.Description, Missing: !f.Exists})
	}
	for _, f := range body.Folders {
		out.Folders = append(out.Folders, action.MemoryFile{Kind: f.Kind, Path: f.Path, Label: f.Label,
			Description: f.Description})
	}
	for _, m := range body.Memories {
		out.Memories = append(out.Memories, action.SavedMemory{Name: m.Name, Description: deref(m.Description),
			Type: deref(m.Type), Modified: m.ModifiedMS})
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// skillsOf reads the rows of the /skills menu, in its own order.
func skillsOf(raw json.RawMessage) ([]action.Skill, error) {
	var body struct {
		Skills []struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
			Source      string `json:"source"`
			Tokens      int    `json:"tokens"`
			State       string `json:"state"`
			LockedBy    string `json:"locked_by"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("the skills menu did not parse: %w", err)
	}
	out := make([]action.Skill, 0, len(body.Skills))
	for _, s := range body.Skills {
		name := s.DisplayName
		if name == "" {
			name = s.Name
		}
		out = append(out, action.Skill{Name: name, Description: s.Description, Source: s.Source,
			Tokens: s.Tokens, State: s.State, LockedBy: s.LockedBy})
	}
	return out, nil
}

// initAgents reads the kinds of subagents claude named at the handshake.
func initAgents(init json.RawMessage) []action.AgentType {
	var body struct {
		Agents []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Model       string `json:"model"`
		} `json:"agents"`
	}
	out := []action.AgentType{}
	if len(init) == 0 || json.Unmarshal(init, &body) != nil {
		return out
	}
	for _, a := range body.Agents {
		if a.Name != "" {
			out = append(out, action.AgentType{Name: a.Name, Description: a.Description, Model: a.Model})
		}
	}
	return out
}

// configOf reads the merged settings the holder passed on, by name.
func configOf(raw json.RawMessage) ([]action.ConfigEntry, error) {
	var body struct {
		Effective map[string]json.RawMessage `json:"effective"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("the settings did not parse: %w", err)
	}
	keys := make([]string, 0, len(body.Effective))
	for key := range body.Effective {
		if key != "$schema" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]action.ConfigEntry, 0, len(keys))
	for _, key := range keys {
		out = append(out, action.ConfigEntry{Key: key, Value: body.Effective[key]})
	}
	return out, nil
}
