package check

import (
	"strings"
	"testing"
)

// A launch does not offer ultracode: claude drops it at launch and starts at
// its default effort, so the list would promise what the session never gets.
// A map that holds it anyway says so under the field rather than showing a
// blank choice.
func TestALaunchDoesNotOfferUltracode(t *testing.T) {
	var got struct {
		Fields string `json:"fields"`
		Held   string `json:"held"`
	}
	launchNode(t, launchBundle(t, true), renderPrelude+`
process.stdout.write(JSON.stringify({
    fields: text(LaunchFields({ value: {}, onChange: () => {}, inherited: {} })),
    held: text(LaunchFields({ value: { effort: "ultracode" }, onChange: () => {}, inherited: {} })),
}));
`, &got)
	if !strings.Contains(got.Fields, "value=max") {
		t.Errorf("the effort of a launch lost its levels:\n%s", got.Fields)
	}
	if strings.Contains(got.Fields, "value=ultracode") {
		t.Errorf("the effort of a launch offers ultracode, which claude drops at launch:\n%s", got.Fields)
	}
	if !strings.Contains(got.Held, "claude does not take it at launch") {
		t.Errorf("a map holding ultracode says nothing about it:\n%s", got.Held)
	}
}
