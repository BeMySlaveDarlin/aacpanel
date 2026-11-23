package check

import "testing"

func TestSecretNotepad(t *testing.T) {
	parallel(t)
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desk", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got map[string]bool
			screen.run(t, "secretpad.html", &got)
			for _, key := range []string{"auto", "cards", "filled", "refused", "noReply", "noTemplate", "path", "replaces", "attrs", "hidden", "shown", "dropped", "fresh", "blank", "reset", "once", "noConfirmation", "saved", "carried", "next", "toast", "outside"} {
				if !got[key] {
					t.Errorf("%s: %+v", key, got)
				}
			}
		})
	}
}
