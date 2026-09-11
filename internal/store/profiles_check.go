package store

import (
	"encoding/json"
	"strings"
)

func checkName(name *string, what string) (string, error) {
	if name == nil {
		return "", badRequest("%s is not set", what)
	}
	v := strings.TrimSpace(*name)
	if v == "" {
		return "", badRequest("%s is empty", what)
	}
	if len([]rune(v)) > profileNameMax {
		return "", badRequest("%s is longer than %d characters", what, profileNameMax)
	}
	return v, nil
}

func (s *Store) checkPrefix(prefix *string) (string, error) {
	if prefix == nil {
		return "", nil
	}
	if strings.TrimSpace(*prefix) == "" {
		return "", nil
	}
	return s.checkPath(*prefix)
}

func checkSession(session *string) (*string, error) {
	if session == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*session)
	if v == "" {
		empty := ""
		return &empty, nil
	}
	if len(v) > profileNameMax {
		return nil, badRequest("the session name is longer than %d characters", profileNameMax)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || r == ' ' {
			return nil, badRequest("the session name %q holds a forbidden character", v)
		}
	}
	return &v, nil
}

func checkLaunch(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > launchMax {
		return nil, badRequest("the launch parameters are longer than %d bytes", launchMax)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, badRequest("the launch parameters are not an object: %v", err)
	}
	if probe == nil {
		return nil, badRequest("the launch parameters are not an object")
	}
	if err := checkIntent(probe); err != nil {
		return nil, err
	}
	return raw, nil
}

func checkIntent(launch map[string]any) error {
	value, ok := launch["intent"]
	if !ok {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return badRequest("the opening message is a string, not %T", value)
	}
	if len([]rune(text)) > intentMax {
		return badRequest("the opening message is longer than %d characters", intentMax)
	}
	for _, r := range text {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return badRequest("the opening message holds a forbidden character %q", r)
		}
	}
	return nil
}
