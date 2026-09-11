package store

import (
	"encoding/json"
	"fmt"
)

const (
	launchEnv  = "env"
	launchArgs = "args"
)

// EffectiveLaunch merges the project's launch parameters on top of the profile's.
func EffectiveLaunch(profile, project json.RawMessage) (json.RawMessage, error) {
	base, err := launchObject(profile, "the profile")
	if err != nil {
		return nil, err
	}
	over, err := launchObject(project, "the project")
	if err != nil {
		return nil, err
	}

	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if k == launchEnv {
			if merged, ok := mergeEnv(base[k], v); ok {
				out[k] = merged
				continue
			}
		}
		out[k] = v
	}

	return json.Marshal(out)
}

func mergeEnv(from, to any) (map[string]any, bool) {
	base, ok := from.(map[string]any)
	if !ok {
		return nil, false
	}
	over, ok := to.(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out, true
}

func launchObject(raw json.RawMessage, whose string) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the launch parameters of %s were not parsed: %w", whose, err)
	}
	return out, nil
}
