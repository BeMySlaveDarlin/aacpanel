package check

import (
	"strings"
	"testing"
)

// A launch takes ultracode as it takes a level: claude starts the session in
// it. The list names it for what it is, xhigh with workflows, the way the
// picker of a live session does.
func TestALaunchOffersUltracodeByWhatItIs(t *testing.T) {
	var got struct {
		Fields string `json:"fields"`
	}
	launchNode(t, launchBundle(t, true), renderPrelude+`
process.stdout.write(JSON.stringify({
    fields: text(LaunchFields({ value: {}, onChange: () => {}, inherited: {} })),
}));
`, &got)
	for _, want := range []string{"value=max", "value=ultracode", "ultracode · xhigh + workflows"} {
		if !strings.Contains(got.Fields, want) {
			t.Errorf("the effort of a launch does not offer %q:\n%s", want, got.Fields)
		}
	}
}
