package check

import "testing"

// Code in a line is copied by a tap, and "copied" is said for a moment. A path in code is not copied — a tap on it
// opens the file — and a tap inside a block of code is left to the block and
// its button. A clipboard that refuses says so where it was asked.
func TestATapOnCodeInALineCopiesIt(t *testing.T) {
	var got struct {
		First struct {
			Copied []string
			Tip    string
			Marked bool
		}
		Gone struct {
			Tips   int
			Marked bool
		}
		After struct {
			Copied []string
			Opened []string
			Tips   int
		}
		Refused *struct {
			Text string
			Bad  bool
		}
	}
	runFixture(t, "tapcopy.html", &got)
	f := got.First
	if len(f.Copied) != 1 || f.Copied[0] != "systemctl --user restart panel-exec" {
		t.Fatalf("a tap on the command copied %q", f.Copied)
	}
	if f.Tip != "copied" || !f.Marked {
		t.Errorf("the tap says %q and marks the code %v", f.Tip, f.Marked)
	}
	if got.Gone.Tips != 0 || got.Gone.Marked {
		t.Errorf("the word stays: %d on the page, the code marked %v", got.Gone.Tips, got.Gone.Marked)
	}
	if len(got.After.Copied) != 1 || got.After.Tips != 0 {
		t.Errorf("a tap on a path or inside a block copied %q", got.After.Copied[1:])
	}
	if len(got.After.Opened) != 1 || got.After.Opened[0] != "web/src/md.js" {
		t.Errorf("a tap on a path opened %q: the file, as before", got.After.Opened)
	}
	if got.Refused == nil || got.Refused.Text != "not copied" || !got.Refused.Bad {
		t.Errorf("a clipboard that refused is answered with %+v", got.Refused)
	}
}
