package action

import (
	"strings"
	"testing"
)

// A guard is a line of a file the host reads: a path that is not clean and
// absolute, or one with a tab or a break in it, would forge another place.
func TestGuardsAreCheckedBeforeTheHostKeepsThem(t *testing.T) {
	good := Request{Ask: AskGuards, Guards: []Guard{
		{Path: "/srv/proj/Pets/service/aacpanel", Cap: 80, Restart: true},
		{Path: "/srv/proj/Algo", Cap: 70},
	}}
	if err := good.Validate(); err != nil {
		t.Fatalf("honest guards were refused: %v", err)
	}
	if err := (Request{Ask: AskGuards}).Validate(); err != nil {
		t.Errorf("an empty map was refused: %v", err)
	}
	bad := map[string]Request{
		"relative path":     {Ask: AskGuards, Guards: []Guard{{Path: "opt/x", Cap: 80}}},
		"unclean path":      {Ask: AskGuards, Guards: []Guard{{Path: "/opt/x/../y", Cap: 80}}},
		"tab in the path":   {Ask: AskGuards, Guards: []Guard{{Path: "/opt/x\t50\t1", Cap: 80}}},
		"break in the path": {Ask: AskGuards, Guards: []Guard{{Path: "/opt/x\n/opt/y", Cap: 80}}},
		"no cap":            {Ask: AskGuards, Guards: []Guard{{Path: "/opt/x"}}},
		"a full window":     {Ask: AskGuards, Guards: []Guard{{Path: "/opt/x", Cap: 100}}},
		"a target":          {Ask: AskGuards, Target: "aacpanel"},
		"too many": {Ask: AskGuards, Guards: func() []Guard {
			out := make([]Guard, guardsMax+1)
			for i := range out {
				out[i] = Guard{Path: "/opt/" + strings.Repeat("x", i%50+1), Cap: 80}
			}
			return out
		}()},
	}
	for name, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
