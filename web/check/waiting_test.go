package check

import "testing"

// One conversation exercises the form lifecycle on both shells without a
// browser per case. The checks read controls and messages, never dimensions.
func TestMoveFormsOnPhoneAndDesktop(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desktop", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got map[string]bool
			screen.run(t, "waiting.html", &got)
			for _, key := range []string{"head", "next", "runConfirms", "run", "auto", "waitTime", "markers", "noteQuiet", "focus", "replyKeeps", "keepsCurrent", "cantReason", "failureKeeps", "cant", "done", "nextForm", "secretPath", "decline", "quietReply", "fullReply", "fold", "change", "round", "dismissHint", "dismissFirst", "discussion", "order", "closedWins", "detachedReply"} {
				if !got[key] {
					t.Errorf("%s: %+v", key, got)
				}
			}
		})
	}
}
