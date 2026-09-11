package launcher

import (
	"os"
	"strings"
	"testing"
)

func TestIntentKeyIsSpelledTheSameEverywhere(t *testing.T) {
	quoted := `"` + keyIntent + `"`
	for _, place := range []struct {
		file string
		want string
	}{
		{"../store/profiles_check.go", "launch[" + quoted + "]"},
		{"../../web/src/screens/profiles/launch.js", "key === " + quoted},
		{"../../web/src/screens/profiles/launch.js", "l." + keyIntent},
	} {
		raw, err := os.ReadFile(place.file)
		if err != nil {
			t.Fatalf("%s: %v", place.file, err)
		}
		if !strings.Contains(string(raw), place.want) {
			t.Errorf("%s does not know the key %q: looked for %q. "+
				"A key renamed on one side alone gives neither an error nor a warning — "+
				"the intent simply stops arriving", place.file, keyIntent, place.want)
		}
	}
}
