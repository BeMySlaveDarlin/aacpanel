package schema

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"aacpanel/internal/action"
)

// Problem is a stored value the launch would not take, and why.
type Problem struct {
	Key string `json:"key"`
	Why string `json:"why"`
}

func (p Problem) String() string {
	return p.Key + ": " + p.Why
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Check returns what in the launch parameters of one level the launcher
// would refuse, drop or misread, in key order. Nothing here depends on the
// host: whether an account takes a model or a model takes an effort is known
// only there, and a value the panel cannot judge is not refused.
func Check(level Level, launch map[string]any) []Problem {
	keys := make([]string, 0, len(launch))
	for key := range launch {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var out []Problem
	for _, key := range keys {
		if why := checkOne(level, key, launch[key]); why != "" {
			out = append(out, Problem{Key: key, Why: why})
		}
	}
	return out
}

func checkOne(level Level, key string, value any) string {
	if why, ok := Retire(key); ok {
		return "retired: " + why
	}
	p, ok := Find(key)
	if !ok {
		return "the launcher does not know this parameter"
	}
	if !p.At(level) {
		return fmt.Sprintf("it is not kept on a %s", level)
	}
	switch p.Kind {
	case KindEnum:
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("a word is expected, not %s", typeName(value))
		}
		if !p.Offers(s) {
			return fmt.Sprintf("%q is not one of %s", s, strings.Join(values(p), ", "))
		}
	case KindModel:
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("a model name is expected, not %s", typeName(value))
		}
		if !action.ModelName(s) {
			return fmt.Sprintf("%q is neither an alias nor a model id", s)
		}
	case KindBool:
		if _, ok := value.(bool); !ok {
			return fmt.Sprintf("on or off is expected, not %s", typeName(value))
		}
	case KindInt:
		n, ok := value.(float64)
		if !ok || n != math.Trunc(n) {
			return fmt.Sprintf("a whole number is expected, not %s", typeName(value))
		}
		if int(n) < p.Min || int(n) > p.Max {
			return fmt.Sprintf("%d is outside %d–%d", int(n), p.Min, p.Max)
		}
	case KindText:
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("a text is expected, not %s", typeName(value))
		}
		return checkText(p, s)
	case KindKV:
		return checkEnv(p, value)
	case KindTokens:
		return checkArgs(p, value)
	}
	return ""
}

func checkText(p Param, s string) string {
	if p.MaxLen > 0 && len([]rune(s)) > p.MaxLen {
		return fmt.Sprintf("longer than %d characters", p.MaxLen)
	}
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Sprintf("holds a forbidden character %q", r)
		}
	}
	return ""
}

func checkEnv(p Param, value any) string {
	obj, ok := value.(map[string]any)
	if !ok {
		return fmt.Sprintf("an object of names and texts is expected, not %s", typeName(value))
	}
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !envKey.MatchString(name) {
			return fmt.Sprintf("%q is not a variable name", name)
		}
		if why, ok := p.Reserved[name]; ok {
			return fmt.Sprintf("%s %s", name, why)
		}
		if _, ok := obj[name].(string); !ok {
			return fmt.Sprintf("%s is %s, not a text", name, typeName(obj[name]))
		}
	}
	return ""
}

func checkArgs(p Param, value any) string {
	list, ok := value.([]any)
	if !ok {
		return fmt.Sprintf("a list of words is expected, not %s", typeName(value))
	}
	for _, item := range list {
		word, ok := item.(string)
		if !ok {
			return fmt.Sprintf("a word is expected, not %s", typeName(item))
		}
		flag, _, _ := strings.Cut(word, "=")
		if why, ok := p.Forbidden[flag]; ok {
			return fmt.Sprintf("%s: %s", flag, why)
		}
	}
	return ""
}

func values(p Param) []string {
	out := make([]string, 0, len(p.Options))
	for _, o := range p.Options {
		out = append(out, o.Value)
	}
	return out
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "true/false"
	case float64:
		return "a number"
	case string:
		return "a text"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("%T", v)
}
