package launcher

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	keyModel          = "model"
	keyEffort         = "effort"
	keyPermissionMode = "permissionMode"
	keyRemoteControl  = "remoteControl"
	keyEnv            = "env"
	keyArgs           = "args"
	keyRoom           = "room"
	keyIntent         = "intent"
)

// Params is what makes one launch differ from another.
type Params struct {
	Model          string
	Effort         string
	PermissionMode string
	RemoteControl  *bool
	Env            map[string]string
	Args           []string
	Room           string
	Intent         string
}

func parseParams(raw json.RawMessage) (Params, []string) {
	var p Params
	if len(raw) == 0 {
		return p, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return p, []string{"launch parameters were not parsed: " + err.Error()}
	}

	var warns []string
	str := func(key string, dst *string) {
		if err := json.Unmarshal(obj[key], dst); err != nil {
			warns = append(warns, fmt.Sprintf("parameter %s is not a string — skipped", key))
		}
	}

	var unknown []string
	for key := range obj {
		switch key {
		case keyModel:
			str(key, &p.Model)
		case keyEffort:
			str(key, &p.Effort)
		case keyPermissionMode:
			str(key, &p.PermissionMode)
		case keyRoom:
			str(key, &p.Room)
		case keyIntent:
			str(key, &p.Intent)
		case keyRemoteControl:
			var on bool
			if err := json.Unmarshal(obj[key], &on); err != nil {
				warns = append(warns, "parameter remoteControl is not true/false — skipped")
				break
			}
			p.RemoteControl = &on
		case keyEnv:
			if err := json.Unmarshal(obj[key], &p.Env); err != nil {
				p.Env = nil
				warns = append(warns, "parameter env is not an object of strings — skipped")
			}
		case keyArgs:
			if err := json.Unmarshal(obj[key], &p.Args); err != nil {
				p.Args = nil
				warns = append(warns, "parameter args is not a list of strings — skipped")
			}
		default:
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		warns = append(warns, "the launcher does not know these parameters: "+strings.Join(unknown, ", "))
	}
	slices.Sort(warns)
	return p, warns
}

func claudeArgs(name, resume string, p Params) []string {
	args := []string{"-n", name}
	if p.RemoteControl == nil || *p.RemoteControl {
		args = append(args, "--remote-control", name)
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	if p.Effort != "" {
		args = append(args, "--effort", p.Effort)
	}
	if p.PermissionMode != "" {
		args = append(args, "--permission-mode", p.PermissionMode)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if p.Intent != "" {
		args = append(args, p.Intent)
	}
	return append(args, p.Args...)
}
