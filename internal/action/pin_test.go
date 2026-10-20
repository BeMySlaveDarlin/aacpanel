package action

import "testing"

func TestAPinSaysWhetherItIsSetOrCleared(t *testing.T) {
	for _, on := range []bool{true, false} {
		r := Request{ID: "1", Kind: SessionPin, Target: "lead", Pin: &on}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		r.Kind = SessionStop
		if err := r.Validate(); err == nil {
			t.Fatal("a pin rode another action")
		}
	}
	if err := (Request{ID: "1", Kind: SessionPin, Target: "lead"}).Validate(); err == nil {
		t.Fatal("a pin without saying on or off passed")
	}
}
