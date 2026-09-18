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

// The longest branch name the map keeps. Git itself has no limit worth naming;
// this one is about a field on a screen.
const baseBranchMax = 200

// checkBase reads the branch a review of the project is measured against. An
// empty value is kept as such: it is not a choice, it means the screen offers
// its default instead of repeating an answer nobody gave.
//
// What git refuses as a ref name is refused here, with the reason said out
// loud rather than left to the database: a leading dash reads as a flag, ".."
// is a range rather than a name, "@{" opens a reflog address, and a name
// ending in .lock collides with the file git keeps beside the ref.
func checkBase(base *string) (*string, error) {
	if base == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*base)
	if v == "" {
		empty := ""
		return &empty, nil
	}
	if len(v) > baseBranchMax {
		return nil, badRequest("the branch name is longer than %d characters", baseBranchMax)
	}
	switch {
	case strings.HasPrefix(v, "-"):
		return nil, badRequest("the branch name %q starts with a dash — git reads such a name as a flag", v)
	case strings.Contains(v, ".."):
		return nil, badRequest("the branch name %q holds \"..\" — that is a range of two names, not one branch", v)
	case strings.Contains(v, "@{"):
		return nil, badRequest("the branch name %q holds \"@{\" — that addresses a reflog entry, not a branch", v)
	case strings.HasSuffix(v, ".lock"):
		return nil, badRequest("the branch name %q ends in .lock — git keeps a file of that name beside every ref", v)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return nil, badRequest("the branch name %q holds a character git does not take", v)
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
