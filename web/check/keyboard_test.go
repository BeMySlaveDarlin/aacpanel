package check

import (
	"regexp"
	"strings"
	"testing"
)

// A switch of the phone's keyboard written as a string in the markup is
// turned on by it: preact sets spellcheck and autocorrect as properties, the
// properties are booleans, and "false" or "off" is a string that is not
// empty. The field then checks the spelling of a path or offers corrections
// to a token. Written as ${false}, the property is false and the attribute
// reads false or off.
func TestKeyboardSwitchesAreBooleans(t *testing.T) {
	stringy := regexp.MustCompile(`(?i)\b(spellcheck|autocorrect)\s*=\s*["'](false|off)["']`)
	files := srcFiles(t)
	checked := 0
	for _, path := range sortedKeys(files) {
		body := files[path]
		checked += strings.Count(body, "spellcheck=${false}")
		for n, line := range strings.Split(body, "\n") {
			if m := stringy.FindString(line); m != "" {
				t.Errorf("%s:%d: %s — a string turns the switch on; write ${false}", path, n+1, m)
			}
		}
	}
	if checked == 0 {
		t.Fatal("not a single spellcheck=${false} in the sources — the test guards the wrong place")
	}
}
