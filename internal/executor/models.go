package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// Models answers what a session can be switched to. A session on the stream
// is asked nothing new: its holder kept the list claude gave at the handshake,
// with the efforts each model takes. A terminal session has no such list to
// give, and the answer says where it lives so the panel knows to fall back.
func (e *Executor) Models(ctx context.Context, target string) (*action.Models, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return nil, err
	}
	if !onStream(s) {
		return &action.Models{Transport: action.SwitchConsole}, nil
	}
	st, err := streamState(ctx, s)
	if err != nil {
		return nil, err
	}
	return &action.Models{
		Transport: action.SwitchStream,
		List:      initModels(st.Init),
		Mode:      st.Mode,
		Effort:    st.Effort,
		Picked:    st.Picked,
	}, nil
}

// initModels reads the models out of claude's answer to the handshake.
func initModels(init json.RawMessage) []action.Model {
	var body struct {
		Models []struct {
			Value       string   `json:"value"`
			Resolved    string   `json:"resolvedModel"`
			Name        string   `json:"displayName"`
			Description string   `json:"description"`
			Efforts     []string `json:"supportedEffortLevels"`
		} `json:"models"`
	}
	if len(init) == 0 || json.Unmarshal(init, &body) != nil {
		return nil
	}
	out := make([]action.Model, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Value == "" {
			continue
		}
		out = append(out, action.Model{Value: m.Value, Resolved: m.Resolved, Name: m.Name,
			Description: m.Description, Efforts: m.Efforts})
	}
	return out
}

// sessionSet changes the one setting a pick names. A model and an effort go
// the way a person would type them — the same slash command, into a terminal
// or onto the stream — and only the mode, which has no command, goes its own.
func (e *Executor) sessionSet(ctx context.Context, target string, set *action.Setting) (string, error) {
	switch {
	case set == nil:
		return "", fmt.Errorf("no setting arrived: there is nothing to change")
	case set.Model != "":
		return e.sessionCommand(ctx, target, &action.Command{Name: "model", Arg: set.Model})
	case set.Effort != "":
		return e.sessionCommand(ctx, target, &action.Command{Name: "effort", Arg: set.Effort})
	}
	return e.sessionMode(ctx, target, set.Mode)
}

// sessionMode puts a live session into a permission mode. On the stream that
// is claude's own request; a terminal has no request for it, only a key that
// cycles through the modes on its screen.
func (e *Executor) sessionMode(ctx context.Context, target, mode string) (string, error) {
	if !slices.Contains(action.Modes, mode) {
		return "", fmt.Errorf("there is no permission mode %q", mode)
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if !onStream(s) {
		return "", fmt.Errorf("session %s runs in a terminal: its mode is switched on its own screen, "+
			"with shift+tab, and the panel does not press it yet", s.Name)
	}
	if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "set_permission_mode",
		Fields: map[string]any{"mode": mode}}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s runs in the %s mode now", s.Name, mode), nil
}
