package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestTheContextGuardIsAnEmptyCompatibilityHook(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/claude/testdata/context-guard.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string
		Input string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no compatibility cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var out, errs bytes.Buffer
			if code := Run([]string{"context-guard"}, bytes.NewBufferString(c.Input), &out, &errs); code != 0 || out.Len() != 0 || errs.Len() != 0 {
				t.Fatalf("compatibility hook: exit %d, output %q, errors %q", code, out.String(), errs.String())
			}
		})
	}
	if Wait("context-guard") != 0 {
		t.Fatal("the compatibility hook waits")
	}
}
