package check

import "testing"

// A row remains cancellable across the local/feed handoff and a reload, and
// the owner's queue settles its state without another transcript record.
func TestAQueuedMessageKeepsItsControlsUntilTheOwnerTakesIt(t *testing.T) {
	for _, screen := range []struct{ name, size, pointer string }{
		{"phone", phoneScreen, phonePointer}, {"desk", deskScreen, deskPointer},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Results []struct {
					Agent                                                                  string `json:"agent"`
					Local, Transition, Reload, OwnerWins, Edit, Withdrawn, Race, Delivered bool
					Sent                                                                   []struct {
						Kind   string `json:"kind"`
						Params struct {
							MessageID string `json:"messageId"`
						} `json:"params"`
					} `json:"sent"`
				} `json:"results"`
				Ambiguous, Old, File, Repeated bool
			}
			runFixtureOn(t, "takeback.html", screen.size, screen.pointer, &got)
			if len(got.Results) != 2 {
				t.Fatalf("both agents must be exercised: %+v", got)
			}
			for _, r := range got.Results {
				for name, ok := range map[string]bool{"local": r.Local, "transition": r.Transition,
					"reload": r.Reload, "owner still waiting": r.OwnerWins, "edit": r.Edit, "withdrawn": r.Withdrawn, "delivery race": r.Race,
					"delivered": r.Delivered} {
					if !ok {
						t.Errorf("%s: %s lost the controls or left a stale queued row", r.Agent, name)
					}
				}
				if len(r.Sent) != 3 || r.Sent[1].Kind != "session.unqueue" || r.Sent[0].Params.MessageID == "" ||
					r.Sent[1].Params.MessageID != r.Sent[0].Params.MessageID || r.Sent[2].Params.MessageID != "delivered-input" {
					t.Errorf("%s: take back did not name the owner's message: %+v", r.Agent, r.Sent)
				}
			}
			if !got.Ambiguous || !got.Old || !got.File || !got.Repeated {
				t.Errorf("ambiguous or old input acquired a neighbour's ID, or files lost theirs: %+v", got)
			}
		})
	}
}
