package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"aacpanel/internal/action"
	"aacpanel/internal/stream"
)

// How long a pick holds depends on where the session lives. On the stream
// claude takes a model or an effort typed as a command for the session alone,
// and a default is written besides: an effort by claude itself, into the
// settings of the model it runs, and a model by the executor, into the
// settings of the contour. In a terminal claude saves a model or an effort
// typed as a command as the default, and holds one for the session only
// through its own dialog — which the panel does not press its way through.

func (e *Executor) setModel(ctx context.Context, target, model, scope string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	cmd := &action.Command{Name: "model", Arg: model}
	if !onStream(s) {
		if scope == action.ScopeSession {
			return "", termSavesPicks(s)
		}
		return e.sessionCommand(ctx, target, cmd)
	}
	detail, err := streamCommand(ctx, s, cmd)
	if err != nil || scope != action.ScopeDefault {
		return detail, err
	}
	path, err := saveDefaultModel(s.Config, model)
	if err != nil {
		return "", fmt.Errorf("%s; the default was not saved: %w", detail, err)
	}
	return fmt.Sprintf("%s; %s is the default model of new sessions now (%s)", detail, model, path), nil
}

func (e *Executor) setEffort(ctx context.Context, target, effort, scope string) (string, error) {
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	cmd := &action.Command{Name: "effort", Arg: effort}
	if !onStream(s) {
		if scope == action.ScopeSession && effort != action.Ultracode {
			return "", termSavesPicks(s)
		}
		return e.sessionCommand(ctx, target, cmd)
	}
	if effort == action.Ultracode {
		if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "apply_flag_settings",
			Fields: map[string]any{"settings": map[string]any{"ultracode": true}}}); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s runs on ultracode now: xhigh, with workflows for every task — for this session only", s.Name), nil
	}
	saved := ""
	if scope == action.ScopeDefault {
		if effort == "max" {
			saved = "; max holds for this session only: claude does not save it as a default"
		} else {
			if _, err := streamAsk(ctx, s, stream.Request{Op: stream.OpControl, Subtype: "update_settings",
				Fields: map[string]any{"source": "userSettings", "settings": map[string]any{"effortLevel": effort}}}); err != nil {
				return "", fmt.Errorf("the default effort was not saved, and the session was left as it was: %w", err)
			}
			saved = "; " + effort + " is the default effort of its model for new sessions now"
		}
	}
	detail, err := streamCommand(ctx, s, cmd)
	if err != nil {
		return "", err
	}
	return detail + saved, nil
}

func termSavesPicks(s liveSession) error {
	return fmt.Errorf("session %s runs in a terminal: claude there saves a model or an effort typed as a command as "+
		"the default for new sessions, and holds one for the session alone only through its own dialog. "+
		"Pick it as the default, or switch the session to the stream", s.Name)
}

// saveDefaultModel writes the model new sessions of a contour start with into
// its settings. The file is claude's own, and it is written the way claude
// writes it: the keys in their order, two spaces a level, a line break at the
// end — a rewrite that reordered it would put a diff of the whole file in
// front of the person for the change of one line.
func saveDefaultModel(config, model string) (string, error) {
	if config == "" {
		return "", fmt.Errorf("the config directory of the session is not known")
	}
	path := filepath.Join(config, "settings.json")
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o600)
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		raw = []byte("{}")
	case err != nil:
		return "", err
	default:
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
	}
	body, err := withKey(raw, "model", model)
	if err != nil {
		return "", fmt.Errorf("%s is not an object of settings: %w", path, err)
	}
	tmp := path + ".aacpanel.tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// withKey sets one top-level key of a JSON object and keeps every other key
// where it stood; a key the object did not have goes at its end.
func withKey(raw []byte, key string, value any) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("the file does not hold a JSON object")
	}
	type entry struct {
		key string
		val json.RawMessage
	}
	var entries []entry
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		entries = append(entries, entry{name, val})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, fmt.Errorf("the object of settings is not closed")
	}
	fresh, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range entries {
		if entries[i].key == key {
			entries[i].val, found = fresh, true
		}
	}
	if !found {
		entries = append(entries, entry{key, fresh})
	}
	var out bytes.Buffer
	if len(entries) == 0 {
		return []byte("{}\n"), nil
	}
	out.WriteString("{\n")
	for i, en := range entries {
		name, _ := json.Marshal(en.key)
		out.WriteString("  ")
		out.Write(name)
		out.WriteString(": ")
		if err := json.Indent(&out, en.val, "  ", "  "); err != nil {
			return nil, err
		}
		if i < len(entries)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	return out.Bytes(), nil
}
