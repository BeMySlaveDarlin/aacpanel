package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The permission modes the panel sets a thread to. They are the presets the
// terminal client of codex offers under /permissions — read only, asking for
// approval, approving through its reviewer — less full access: a tap on a
// phone is not how codex stops asking before it acts, and full access is
// chosen at the start, in the map, or in codex itself.
const (
	ModeReadOnly = "read-only"
	ModeAsk      = "ask"
	ModeAuto     = "auto"
	// ModeCustom is a thread whose settings match none of the modes: full
	// access chosen in the map, a permission profile of the home's own, an
	// approval policy that never asks.
	ModeCustom = "custom"
)

// Modes are the modes the panel sets, in the order a list offers them.
var Modes = []string{ModeReadOnly, ModeAsk, ModeAuto}

// preset is what a mode sets: the permission profile of codex's own the
// thread runs under, when codex asks, and who answers.
type preset struct {
	profile  string
	approval string
	reviewer string
}

var presets = map[string]preset{
	ModeReadOnly: {profile: ":read-only", approval: "on-request", reviewer: "user"},
	ModeAsk:      {profile: ":workspace", approval: "on-request", reviewer: "user"},
	ModeAuto:     {profile: ":workspace", approval: "on-request", reviewer: "auto_review"},
}

// settings are the parts of a thread's settings the panel shows beyond its
// model and effort, which thread/read says. The daemon tells them in the
// answer of thread/start and thread/resume and in thread/settings/updated to
// the clients of the thread, and a thread the link has only read carries none
// of them: known says the permissions are told, planKnown the plan. A change
// the panel made itself is known from its success.
type settings struct {
	known bool
	preset
	planKnown bool
	plan      bool
}

// mode names the mode the settings are, empty while they are not known.
func (s settings) mode() string {
	if !s.known {
		return ""
	}
	for name, p := range presets {
		if p == s.preset {
			return name
		}
	}
	return ModeCustom
}

// settingsWire is what the daemon says of a thread's settings. The answers of
// thread/start and thread/resume name the sandbox "sandbox", the notification
// "sandboxPolicy"; the answer of thread/start has no collaboration mode.
type settingsWire struct {
	Approval      json.RawMessage `json:"approvalPolicy"`
	Reviewer      string          `json:"approvalsReviewer"`
	Sandbox       *sandboxWire    `json:"sandbox"`
	SandboxPolicy *sandboxWire    `json:"sandboxPolicy"`
	Profile       *struct {
		ID string `json:"id"`
	} `json:"activePermissionProfile"`
	Collaboration *struct {
		Mode string `json:"mode"`
	} `json:"collaborationMode"`
}

type sandboxWire struct {
	Type string `json:"type"`
}

// sandboxProfiles name the profile a legacy sandbox policy stands for: a
// thread started with a sandbox rather than a profile has no profile id, and
// workspace-write with the network the config.toml allows is still the
// workspace a person picked.
var sandboxProfiles = map[string]string{
	"readOnly":         ":read-only",
	"workspaceWrite":   ":workspace",
	"dangerFullAccess": ":danger-full-access",
}

// read takes what the daemon said; an answer that says nothing of the
// approvals leaves the settings unknown.
func (w settingsWire) read() settings {
	if len(w.Approval) == 0 || w.Reviewer == "" {
		return settings{}
	}
	s := settings{known: true}
	if json.Unmarshal(w.Approval, &s.approval) != nil {
		// A granular policy is an object: whatever it says, it is no mode of
		// the panel's.
		s.approval = "granular"
	}
	s.reviewer = w.Reviewer
	sandbox := w.Sandbox
	if sandbox == nil {
		sandbox = w.SandboxPolicy
	}
	switch {
	case w.Profile != nil && w.Profile.ID != "":
		s.profile = w.Profile.ID
	case sandbox != nil:
		s.profile = sandboxProfiles[sandbox.Type]
		if s.profile == "" {
			s.profile = sandbox.Type
		}
	}
	if w.Collaboration != nil {
		s.planKnown, s.plan = true, w.Collaboration.Mode == "plan"
	}
	return s
}

// Setting is what a person changes of a running thread: a model with its
// effort or without, an effort, a mode or the plan.
type Setting struct {
	Model  string
	Effort string
	Mode   string
	Plan   *bool
}

// Applied says what a change of settings did.
type Applied struct {
	// Later says the daemon would not change the thread now, and the model and
	// the effort go with the next turn the panel starts.
	Later bool
	// Effort is the effort the thread runs at after the change: a model picked
	// without one keeps the effort of the thread where it takes it, and starts
	// at its own where it does not.
	Effort string
}

// Configure changes the settings of a thread for its next turns, with
// thread/settings/update. A model and an effort are checked against the
// catalogue of the home's daemon first: the daemon takes any name and fails
// only at the turn that runs on it.
//
// thread/settings/update is experimental: the daemon updates itself, and a
// release may drop or change it without a word. A daemon that refuses it still
// takes a model and an effort as fields of turn/start, so the pick is kept and
// goes with the next turn the panel starts, and the answer says so. A mode and
// the plan have no such way and are refused with the daemon's reason.
func (l *Link) Configure(ctx context.Context, threadID string, ch Setting) (Applied, error) {
	c, err := l.client()
	if err != nil {
		return Applied{}, err
	}
	info, err := read(ctx, c, threadID)
	if err != nil {
		return Applied{}, err
	}
	model, effort := info.Model, info.Effort
	params := map[string]any{"threadId": threadID}
	if ch.Model != "" || ch.Effort != "" {
		model, effort, err = l.pick(ctx, info, ch)
		if err != nil {
			return Applied{}, err
		}
		params["model"] = model
		if effort != "" {
			params["effort"] = effort
		}
	}
	var p preset
	if ch.Mode != "" {
		var ok bool
		if p, ok = presets[ch.Mode]; !ok {
			return Applied{}, fmt.Errorf("codex has no mode %q the panel sets; it has %s", ch.Mode,
				strings.Join(Modes, ", "))
		}
		params["permissions"], params["approvalPolicy"], params["approvalsReviewer"] = p.profile, p.approval, p.reviewer
	}
	if ch.Plan != nil {
		// A collaboration mode carries a model and an effort of its own and
		// sets them with itself: the ones the thread runs go with it, or plan
		// mode would switch the model under the person's feet.
		if model == "" {
			return Applied{}, errors.New("codex does not say which model the thread runs, and plan mode would pick one by itself")
		}
		mode := "default"
		if *ch.Plan {
			mode = "plan"
		}
		params["collaborationMode"] = map[string]any{"mode": mode, "settings": map[string]any{
			"model": model, "reasoning_effort": nullable(effort), "developer_instructions": nil}}
	}

	l.sub.Lock()
	defer l.sub.Unlock()
	err = within(ctx, c, "thread/settings/update", params, nil)
	switch {
	case err != nil && refused(err) && ch.Mode == "" && ch.Plan == nil:
		l.later(threadID, model, effort)
		return Applied{Later: true, Effort: effort}, nil
	case err != nil && refused(err):
		return Applied{}, fmt.Errorf("the codex daemon does not change this of a running thread: %w", err)
	case err != nil:
		return Applied{}, err
	}
	l.set(threadID, func(t *thread) {
		if ch.Model != "" || ch.Effort != "" {
			t.info.Model, t.info.Effort = model, effort
		}
		if ch.Mode != "" {
			t.settings.known, t.settings.preset = true, p
		}
		if ch.Plan != nil {
			t.settings.planKnown, t.settings.plan = true, *ch.Plan
		}
	})
	if ch.Model != "" || ch.Effort != "" {
		l.later(threadID, "", "")
	}
	l.save(threadID)
	return Applied{Effort: effort}, nil
}

// pick checks a model and an effort against the catalogue and returns what
// the thread is to run. A model picked alone keeps the thread's effort where
// it takes it and starts at its own where it does not; an effort picked alone
// has to be one the thread's model takes.
func (l *Link) pick(ctx context.Context, info threadInfo, ch Setting) (string, string, error) {
	list, err := l.models(ctx)
	if err != nil {
		return "", "", err
	}
	model := ch.Model
	if model == "" {
		model = info.Model
	}
	i := slices.IndexFunc(list, func(m Model) bool { return m.Model == model })
	switch {
	case i < 0 && ch.Model != "":
		names := make([]string, 0, len(list))
		for _, m := range list {
			names = append(names, m.Model)
		}
		return "", "", fmt.Errorf("codex offers no model %q; it offers %s", model, strings.Join(names, ", "))
	case i < 0:
		return "", "", fmt.Errorf("the thread runs %q, which the catalogue of codex does not list, so the efforts "+
			"it takes are not known — pick the model with the effort", model)
	}
	m := list[i]
	effort := ch.Effort
	switch {
	case effort != "" && !slices.Contains(m.Efforts, effort):
		return "", "", fmt.Errorf("%s does not take the effort %q; it takes %s", model, effort,
			strings.Join(m.Efforts, ", "))
	case effort != "":
	case slices.Contains(m.Efforts, info.Effort):
		effort = info.Effort
	default:
		effort = m.Effort
	}
	return model, effort, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
